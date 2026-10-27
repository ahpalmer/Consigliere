package radar

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/datapack"
	"github.com/ahpalmer/consigliere/internal/llm"
)

//go:embed prompts/score.md
var scoreRubric string

const OtherCategory = "other"

// Long descriptions are mostly boilerplate (benefits, EEO); the first ~12K chars carry the signal.
const maxDescriptionChars = 12_000

type ScoreResult struct {
	Fit           int
	Category      string
	Summary       string
	Reasons, Gaps []string
}

// ScoreInput is what the scorer needs to know about an item.
type ScoreInput struct {
	Company, Title, Locations, Tier string
	CompMin, CompMax                *float64
	PostedAt                        *time.Time
	Description                     string
}

// ScoringContext is the stable, cacheable part of every scoring request: rubric + owner profile + categories.
type ScoringContext struct {
	Model, Rubric, Profile string
	Categories             []string
}

// Key changes whenever the rubric, profile, categories, or model change, which makes existing scores stale.
func (c ScoringContext) Key() string {
	return Hash(c.Model, c.Rubric, c.Profile, strings.Join(c.Categories, "|"))
}

type Scorer interface {
	BuildContext() (ScoringContext, error)
	// Score returns nil when the model declined to score the item.
	Score(ctx context.Context, item ScoreInput, sc ScoringContext) (*ScoreResult, error)
}

// LLMScorer scores one item against the owner's profile with a cheap model and structured output.
type LLMScorer struct {
	LLM    *llm.Client
	Model  string
	Owner  string
	Pack   *datapack.Pack
	Config ConfigLoader
	Clock  clock.Clock
}

func (s LLMScorer) BuildContext() (ScoringContext, error) {
	prefs, err := s.Config.Preferences()
	if err != nil {
		return ScoringContext{}, err
	}
	var categories []string
	for _, c := range prefs.Categories {
		if c.Name != "" && !slices.Contains(categories, c.Name) {
			categories = append(categories, c.Name)
		}
	}
	if !slices.Contains(categories, OtherCategory) {
		categories = append(categories, OtherCategory)
	}

	var profile strings.Builder
	dir, err := s.Pack.Resolve("profile")
	if err != nil {
		return ScoringContext{}, err
	}
	if entries, err := os.ReadDir(dir); err == nil {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			text, _, err := s.Pack.ReadText("profile/" + name)
			if err != nil {
				return ScoringContext{}, err
			}
			fmt.Fprintf(&profile, "## %s\n%s\n\n", name, strings.TrimSpace(text))
		}
	}
	prefsText, ok, err := s.Pack.ReadText(PreferencesPath)
	if err != nil {
		return ScoringContext{}, err
	}
	if !ok {
		prefsText = "(none)"
	}
	fmt.Fprintf(&profile, "## preferences\n%s\n\n## categories, highest priority first\n", strings.TrimSpace(prefsText))
	for _, c := range prefs.Categories {
		fmt.Fprintf(&profile, "- %s: %s\n", c.Name, c.Description)
	}
	fmt.Fprintf(&profile, "- %s: anything that fits none of the above", OtherCategory)

	return ScoringContext{Model: s.Model, Rubric: llm.Render(scoreRubric, s.Owner), Profile: strings.TrimSpace(profile.String()), Categories: categories}, nil
}

func (s LLMScorer) Score(ctx context.Context, item ScoreInput, sc ScoringContext) (*ScoreResult, error) {
	text, err := s.LLM.Structured(ctx, llm.StructuredRequest{
		Task: llm.TaskScore,
		System: []llm.SystemBlock{
			{Text: sc.Rubric},
			// Identical across every item in a run, so it's cached after the first call.
			{Text: sc.Profile, Cache: true},
		},
		User: describe(item, s.Clock.Now()),
		Schema: llm.ObjectSchema(map[string]any{
			"fit":      map[string]any{"type": "integer", "description": "0-100"},
			"category": map[string]any{"type": "string", "enum": sc.Categories},
			"summary":  map[string]any{"type": "string"},
			"reasons":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"gaps":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}),
	})
	if errors.Is(err, llm.ErrRefused) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var raw struct {
		Fit      int      `json:"fit"`
		Category string   `json:"category"`
		Summary  string   `json:"summary"`
		Reasons  []string `json:"reasons"`
		Gaps     []string `json:"gaps"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("scorer returned invalid JSON: %w", err)
	}
	if !slices.Contains(sc.Categories, raw.Category) {
		raw.Category = OtherCategory
	}
	return &ScoreResult{
		Fit:      min(max(raw.Fit, 0), 100),
		Category: raw.Category,
		Summary:  strings.TrimSpace(raw.Summary),
		Reasons:  raw.Reasons[:min(len(raw.Reasons), 3)],
		Gaps:     raw.Gaps[:min(len(raw.Gaps), 3)],
	}, nil
}

func describe(item ScoreInput, now time.Time) string {
	age := "unknown"
	if item.PostedAt != nil {
		days := int(now.Sub(*item.PostedAt).Hours() / 24)
		age = fmt.Sprintf("%s (%d days ago)", item.PostedAt.Format("2006-01-02"), max(0, days))
	}
	pay := "not listed"
	if item.CompMin != nil || item.CompMax != nil {
		f := func(p *float64) string {
			if p == nil {
				return "?"
			}
			return thousands(*p)
		}
		pay = fmt.Sprintf("%s - %s (listed range, usually base pay)", f(item.CompMin), f(item.CompMax))
	}
	description := item.Description
	if len(description) > maxDescriptionChars {
		description = description[:maxDescriptionChars] + "\n[truncated]"
	}
	tier := item.Tier
	if tier == "" {
		tier = "none"
	}
	return fmt.Sprintf("Today: %s\nCompany: %s\nTitle: %s\nLocations: %s\nLocation tier (from the rule filter): %s\nPay: %s\nPosted: %s\n\nDescription:\n%s",
		now.Format("2006-01-02"), item.Company, item.Title, item.Locations, tier, pay, age, description)
}
