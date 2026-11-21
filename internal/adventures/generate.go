package adventures

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ahpalmer/consigliere/internal/llm"
)

//go:embed prompts/generate.md
var generatePrompt string

type LLM interface {
	Structured(ctx context.Context, req llm.StructuredRequest) (string, error)
}

// Generator suggests 5 ideas for the owner's constraints, from their settings, past ratings and favorites. The ideas
// come back unsaved; the owner saves the ones they like.
type Generator struct {
	LLM   LLM
	Owner string
	Store *Store
}

var ideaSchema = llm.ObjectSchema(map[string]any{
	"ideas": map[string]any{"type": "array", "items": llm.ObjectSchema(map[string]any{
		"title":       map[string]any{"type": "string"},
		"description": map[string]any{"type": "string"},
		"place":       map[string]any{"type": "string"},
		"cost":        map[string]any{"type": "string", "enum": []string{"$", "$$", "$$$", "$$$$"}},
		"duration":    map[string]any{"type": "string"},
		"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	})},
})

func (g Generator) Generate(ctx context.Context, settings Settings, constraints string) ([]Idea, error) {
	user, err := g.describe(ctx, settings, constraints)
	if err != nil {
		return nil, err
	}
	reply, err := g.LLM.Structured(ctx, llm.StructuredRequest{
		Task:   llm.TaskDraft,
		System: []llm.SystemBlock{{Text: llm.Render(generatePrompt, g.Owner)}},
		User:   user,
		Schema: ideaSchema,
	})
	if err != nil {
		return nil, err
	}
	var raw struct{ Ideas []Idea }
	if err := json.Unmarshal([]byte(reply), &raw); err != nil {
		return nil, fmt.Errorf("generator returned invalid JSON: %w", err)
	}
	ideas := []Idea{}
	for _, i := range raw.Ideas {
		i.Source = "generated"
		if i.clean() == nil {
			ideas = append(ideas, i)
		}
	}
	return ideas[:min(len(ideas), 5)], nil
}

// describe is everything the model needs: settings, constraints, how past adventures went, favorites, the bank.
func (g Generator) describe(ctx context.Context, settings Settings, constraints string) (string, error) {
	var b strings.Builder
	line := func(label, v string) {
		if v = strings.TrimSpace(v); v == "" {
			v = "(not given)"
		}
		fmt.Fprintf(&b, "%s: %s\n", label, v)
	}
	line("Today", g.Store.Today())
	line("Home", settings.Home)
	line("Goes with", settings.With)
	line("Likes and avoids", settings.Likes)
	line("Constraints for this request", constraints)

	history, err := g.Store.History(ctx)
	if err != nil {
		return "", err
	}
	b.WriteString("\nHow past adventures went:\n")
	rated := 0
	for _, a := range history {
		if a.Rating == nil || rated == 30 {
			continue
		}
		rated++
		fmt.Fprintf(&b, "- %s (%d/5)", a.Title, *a.Rating)
		if a.Review != "" {
			fmt.Fprintf(&b, ": %s", a.Review)
		}
		b.WriteString("\n")
	}
	if rated == 0 {
		b.WriteString("(none rated yet)\n")
	}

	ideas, err := g.Store.Ideas(ctx, "", false)
	if err != nil {
		return "", err
	}
	var favorites, bank []string
	for _, i := range ideas {
		if i.Favorite {
			favorites = append(favorites, i.Title)
		} else {
			bank = append(bank, i.Title)
		}
	}
	fmt.Fprintf(&b, "\nFavorite ideas: %s\n", listOrNone(favorites))
	fmt.Fprintf(&b, "Already in the bank: %s\n", listOrNone(bank[:min(len(bank), 80)]))
	return b.String(), nil
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, "; ")
}
