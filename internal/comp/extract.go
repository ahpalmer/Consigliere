package comp

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/llm"
)

//go:embed prompts/extract.md
var extractPrompt string

// Offers longer than this are mostly boilerplate (benefits booklets, legal terms).
const maxOfferChars = 30_000

var ErrNotAnOffer = errors.New("couldn't find a base salary in that text; paste the offer itself")

type LLM interface {
	Structured(ctx context.Context, req llm.StructuredRequest) (string, error)
}

// Extractor reads a pasted offer into a Package with a cheap model. It only copies numbers out; Compare does the math.
type Extractor struct {
	LLM   LLM
	Owner string
}

func (e Extractor) Extract(ctx context.Context, text string, today time.Time) (*Package, error) {
	if len(text) > maxOfferChars {
		text = text[:maxOfferChars] + "\n[truncated]"
	}
	reply, err := e.LLM.Structured(ctx, llm.StructuredRequest{
		Task:   llm.TaskExtract,
		System: []llm.SystemBlock{{Text: llm.Render(extractPrompt, e.Owner)}},
		User:   "Today: " + today.Format(time.DateOnly) + "\n\nThe offer:\n" + text,
		Schema: extractSchema,
	})
	if err != nil {
		return nil, err
	}
	var raw rawOffer
	if err := json.Unmarshal([]byte(reply), &raw); err != nil {
		return nil, fmt.Errorf("extractor returned invalid JSON: %w", err)
	}
	if raw.Base == nil || *raw.Base <= 0 {
		return nil, ErrNotAnOffer
	}
	p := raw.pack(today)
	if err := p.Validate(); err != nil {
		// A date the model got wrong shouldn't lose the rest; the owner can fix it in the YAML.
		p.StartDate = ""
		for i := range p.Equity {
			p.Equity[i].PercentByYear = nil
		}
		if err := p.Validate(); err != nil {
			return nil, fmt.Errorf("extracted package doesn't check out: %w", err)
		}
	}
	return p, nil
}

// rawOffer is the extractor's JSON: null where the offer doesn't say.
type rawOffer struct {
	Company   string   `json:"company"`
	StartDate string   `json:"start_date"`
	Base      *float64 `json:"base"`
	Bonus     *struct {
		TargetPercent *float64 `json:"target_percent"`
		Amount        *float64 `json:"amount"`
	} `json:"bonus"`
	SignOn *struct {
		Amount         *float64 `json:"amount"`
		ClawbackMonths *float64 `json:"clawback_months"`
	} `json:"sign_on"`
	Equity []struct {
		Label         string    `json:"label"`
		Kind          string    `json:"kind"`
		Value         *float64  `json:"value"`
		Shares        *float64  `json:"shares"`
		Price         *float64  `json:"price"`
		Years         *float64  `json:"years"`
		CliffMonths   *float64  `json:"cliff_months"`
		EveryMonths   *float64  `json:"every_months"`
		PercentByYear []float64 `json:"percent_by_year"`
	} `json:"equity"`
	Retirement *struct {
		MatchPercent *float64 `json:"match_percent"`
		UpToPercent  *float64 `json:"up_to_percent"`
		Annual       *float64 `json:"annual"`
	} `json:"retirement"`
	Health *struct {
		AnnualCost  *float64 `json:"annual_cost"`
		EmployerHSA *float64 `json:"employer_hsa"`
	} `json:"health"`
	Perks []struct {
		Name   string   `json:"name"`
		Annual *float64 `json:"annual"`
	} `json:"perks"`
	None  []string `json:"none"`
	Notes []string `json:"notes"`
}

func (r rawOffer) pack(today time.Time) *Package {
	asOf := today.Format(time.DateOnly)
	p := &Package{Company: strings.TrimSpace(r.Company), AsOf: asOf, StartDate: r.StartDate, Base: &Base{Amount: num(r.Base)}}
	if b := r.Bonus; b != nil {
		p.Bonus = &Bonus{TargetPercent: num(b.TargetPercent), Amount: num(b.Amount)}
	}
	if s := r.SignOn; s != nil && num(s.Amount) > 0 {
		p.SignOn = &SignOn{Amount: num(s.Amount), ClawbackMonths: int(num(s.ClawbackMonths))}
	}
	for _, g := range r.Equity {
		kind := g.Kind
		if kind != EquityPublic {
			kind = EquityPrivate
		}
		grant := Grant{Label: strings.TrimSpace(g.Label), Kind: kind, Value: num(g.Value), Shares: num(g.Shares), Price: num(g.Price),
			Years: int(num(g.Years)), CliffMonths: int(num(g.CliffMonths)), EveryMonths: int(num(g.EveryMonths)), PercentByYear: g.PercentByYear}
		// Shares with no price can't be valued; keep the dollar value instead and let the questions ask.
		if grant.Shares > 0 && grant.Price == 0 {
			grant.Shares = 0
		}
		if grant.EveryMonths > 0 && 12%grant.EveryMonths != 0 {
			grant.EveryMonths = 0
		}
		p.Equity = append(p.Equity, grant)
	}
	if rt := r.Retirement; rt != nil {
		p.Retirement = &Retirement{MatchPercent: num(rt.MatchPercent), UpToPercent: num(rt.UpToPercent), Annual: num(rt.Annual)}
	}
	if h := r.Health; h != nil {
		p.Health = &Health{AnnualCost: num(h.AnnualCost), EmployerHSA: num(h.EmployerHSA)}
	}
	for _, k := range r.Perks {
		if a := num(k.Annual); a > 0 && strings.TrimSpace(k.Name) != "" {
			p.Perks = append(p.Perks, Perk{Name: strings.TrimSpace(k.Name), Annual: a})
		}
	}
	for _, s := range r.None {
		if slices.Contains(Sections, s) && !slices.Contains(p.None, s) {
			p.None = append(p.None, s)
		}
	}
	for _, n := range r.Notes {
		if n = strings.TrimSpace(n); n != "" {
			p.Notes = append(p.Notes, n)
		}
	}
	return p
}

// num reads a nullable number, treating null and negatives as not stated.
func num(v *float64) float64 {
	if v == nil || *v < 0 {
		return 0
	}
	return *v
}

func nullable(schema map[string]any) map[string]any {
	return map[string]any{"anyOf": []any{schema, map[string]any{"type": "null"}}}
}

var (
	number        = nullable(map[string]any{"type": "number"})
	text          = map[string]any{"type": "string"}
	extractSchema = llm.ObjectSchema(map[string]any{
		"company":    text,
		"start_date": text,
		"base":       number,
		"bonus":      nullable(llm.ObjectSchema(map[string]any{"target_percent": number, "amount": number})),
		"sign_on":    nullable(llm.ObjectSchema(map[string]any{"amount": number, "clawback_months": number})),
		"equity": map[string]any{"type": "array", "items": llm.ObjectSchema(map[string]any{
			"label":           text,
			"kind":            map[string]any{"type": "string", "enum": []string{EquityPublic, EquityPrivate}},
			"value":           number,
			"shares":          number,
			"price":           number,
			"years":           number,
			"cliff_months":    number,
			"every_months":    number,
			"percent_by_year": map[string]any{"type": "array", "items": map[string]any{"type": "number"}},
		})},
		"retirement": nullable(llm.ObjectSchema(map[string]any{"match_percent": number, "up_to_percent": number, "annual": number})),
		"health":     nullable(llm.ObjectSchema(map[string]any{"annual_cost": number, "employer_hsa": number})),
		"perks":      map[string]any{"type": "array", "items": llm.ObjectSchema(map[string]any{"name": text, "annual": number})},
		"none":       map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": Sections}},
		"notes":      map[string]any{"type": "array", "items": text},
	})
)
