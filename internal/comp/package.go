// Package comp compares an incoming offer's pay package with the owner's current one, year by year (M9).
//
// Both packages share one shape (Package). The current one lives in the data pack (comp/current.yaml); an incoming
// one is pasted in, read by a cheap model into the same shape, and kept as YAML the owner can correct. All the math
// is plain code here: no model ever adds anything up.
package comp

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ahpalmer/consigliere/internal/datapack"
)

const CurrentPath = "comp/current.yaml"

// Section names, as used in Package.None.
const (
	SectionBonus      = "bonus"
	SectionSignOn     = "sign_on"
	SectionEquity     = "equity"
	SectionRetirement = "retirement"
	SectionHealth     = "health"
	SectionPerks      = "perks"
)

var Sections = []string{SectionBonus, SectionSignOn, SectionEquity, SectionRetirement, SectionHealth, SectionPerks}

// Equity kinds. Public grants (RSUs in a listed company) count toward pay; private ones (startup shares or options)
// are paper value and are always shown apart.
const (
	EquityPublic  = "public"
	EquityPrivate = "private"
)

// Package is one pay package. Amounts are yearly US dollars unless a field says otherwise. A nil section means the
// package doesn't say; a section listed in None is known not to exist. Every section carries the date its numbers
// were last checked (as_of), falling back to the package's own.
type Package struct {
	Company string `yaml:"company,omitempty" json:"company"`
	AsOf    string `yaml:"as_of,omitempty" json:"asOf"`
	// Current: when the owner started. Incoming: the proposed start date.
	StartDate  string      `yaml:"start_date,omitempty" json:"startDate"`
	Base       *Base       `yaml:"base,omitempty" json:"base"`
	Bonus      *Bonus      `yaml:"bonus,omitempty" json:"bonus"`
	SignOn     *SignOn     `yaml:"sign_on,omitempty" json:"signOn"`
	Equity     []Grant     `yaml:"equity,omitempty" json:"equity"`
	Retirement *Retirement `yaml:"retirement,omitempty" json:"retirement"`
	Health     *Health     `yaml:"health,omitempty" json:"health"`
	Perks      []Perk      `yaml:"perks,omitempty" json:"perks"`
	// Sections known not to exist (e.g. "there's no bonus"), so no question is asked about them.
	None []string `yaml:"none,omitempty" json:"none"`
	// Anything else the offer mentions that isn't counted (PTO, relocation, remote policy), as written.
	Notes []string `yaml:"notes,omitempty" json:"notes"`
}

type Base struct {
	Amount float64 `yaml:"amount" json:"amount"`
	AsOf   string  `yaml:"as_of,omitempty" json:"asOf"`
}

// Bonus is a yearly bonus: a target percent of base, or a fixed amount.
type Bonus struct {
	TargetPercent float64 `yaml:"target_percent,omitempty" json:"targetPercent"`
	Amount        float64 `yaml:"amount,omitempty" json:"amount"`
	// Month and day it pays out each year (MM-DD), to work out what leaving forfeits.
	PaysOn string `yaml:"pays_on,omitempty" json:"paysOn"`
	AsOf   string `yaml:"as_of,omitempty" json:"asOf"`
}

// SignOn is a one-time bonus, repaid in full when leaving within ClawbackMonths of PaidOn.
type SignOn struct {
	Amount         float64 `yaml:"amount" json:"amount"`
	PaidOn         string  `yaml:"paid_on,omitempty" json:"paidOn"`
	ClawbackMonths int     `yaml:"clawback_months,omitempty" json:"clawbackMonths"`
	AsOf           string  `yaml:"as_of,omitempty" json:"asOf"`
}

// Grant is an equity grant. Its value is Value, or Shares × Price when shares are given (so updating the price is
// enough when the stock moves). It vests from Granted over Years: nothing before CliffMonths, then every EveryMonths,
// splitting each year's PercentByYear evenly (even years when that's empty).
//
// A grant can instead be just Annual: a steady yearly amount (refreshers stacking up), counted every year with no
// vesting schedule and nothing forfeited.
type Grant struct {
	Label         string    `yaml:"label,omitempty" json:"label"`
	Kind          string    `yaml:"kind,omitempty" json:"kind"`
	Annual        float64   `yaml:"annual,omitempty" json:"annual"`
	Value         float64   `yaml:"value,omitempty" json:"value"`
	Shares        float64   `yaml:"shares,omitempty" json:"shares"`
	Price         float64   `yaml:"price,omitempty" json:"price"`
	Granted       string    `yaml:"granted,omitempty" json:"granted"`
	Years         int       `yaml:"years,omitempty" json:"years"`
	CliffMonths   int       `yaml:"cliff_months,omitempty" json:"cliffMonths"`
	EveryMonths   int       `yaml:"every_months,omitempty" json:"everyMonths"`
	PercentByYear []float64 `yaml:"percent_by_year,omitempty" json:"percentByYear"`
	AsOf          string    `yaml:"as_of,omitempty" json:"asOf"`
}

// Retirement is the employer's retirement contribution: a match (MatchPercent of the owner's contributions, on
// contributions up to UpToPercent of base when that's set) and/or a flat Annual amount.
type Retirement struct {
	MatchPercent float64 `yaml:"match_percent,omitempty" json:"matchPercent"`
	UpToPercent  float64 `yaml:"up_to_percent,omitempty" json:"upToPercent"`
	Annual       float64 `yaml:"annual,omitempty" json:"annual"`
	// Current package: what the owner puts in a year. Both packages' matches are worked out on it; without it, the
	// owner is assumed to put in exactly UpToPercent of base.
	Contribution float64 `yaml:"contribution,omitempty" json:"contribution"`
	// Current package only: employer money not vested yet, lost by leaving before VestsOn.
	UnvestedBalance float64 `yaml:"unvested_balance,omitempty" json:"unvestedBalance"`
	VestsOn         string  `yaml:"vests_on,omitempty" json:"vestsOn"`
	AsOf            string  `yaml:"as_of,omitempty" json:"asOf"`
}

// Health is what health cover costs the owner (their share of premiums) less what the employer puts in an HSA.
type Health struct {
	AnnualCost  float64 `yaml:"annual_cost,omitempty" json:"annualCost"`
	EmployerHSA float64 `yaml:"employer_hsa,omitempty" json:"employerHsa"`
	AsOf        string  `yaml:"as_of,omitempty" json:"asOf"`
}

type Perk struct {
	Name   string  `yaml:"name" json:"name"`
	Annual float64 `yaml:"annual" json:"annual"`
	AsOf   string  `yaml:"as_of,omitempty" json:"asOf"`
}

// GrantValue is the grant's total value in dollars.
func (g Grant) GrantValue() float64 {
	if g.Shares > 0 {
		return g.Shares * g.Price
	}
	return g.Value
}

func (p *Package) isNone(section string) bool {
	for _, s := range p.None {
		if s == section {
			return true
		}
	}
	return false
}

// Parse reads a package from YAML and checks it. Unknown keys are refused, so a typo doesn't silently drop a value.
func Parse(text string) (*Package, error) {
	var p Package
	dec := yaml.NewDecoder(strings.NewReader(text))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("empty package")
		}
		return nil, err
	}
	return &p, p.Validate()
}

// YAML is the package as the owner edits it.
func (p *Package) YAML() string {
	out, err := yaml.Marshal(p)
	if err != nil {
		panic(err) // plain structs always marshal
	}
	return string(out)
}

// LoadCurrent reads comp/current.yaml. ok=false when the pack has none.
func LoadCurrent(pack *datapack.Pack) (*Package, bool, error) {
	text, ok, err := pack.ReadText(CurrentPath)
	if err != nil || !ok {
		return nil, ok, err
	}
	p, err := Parse(text)
	if err != nil {
		return nil, true, fmt.Errorf("%s: %w", CurrentPath, err)
	}
	return p, true, nil
}

// Validate checks what the math relies on: dates parse, amounts aren't negative, vesting adds up.
func (p *Package) Validate() error {
	var errs []error
	date := func(field, value string) {
		if value != "" {
			if _, err := parseDate(value); err != nil {
				errs = append(errs, fmt.Errorf("%s: %q isn't a date like 2026-03-01", field, value))
			}
		}
	}
	amount := func(field string, v float64) {
		if v < 0 {
			errs = append(errs, fmt.Errorf("%s can't be negative", field))
		}
	}
	date("as_of", p.AsOf)
	date("start_date", p.StartDate)
	if p.Base == nil || p.Base.Amount <= 0 {
		errs = append(errs, errors.New("base.amount is required"))
	} else {
		date("base.as_of", p.Base.AsOf)
	}
	if b := p.Bonus; b != nil {
		amount("bonus.target_percent", b.TargetPercent)
		amount("bonus.amount", b.Amount)
		date("bonus.as_of", b.AsOf)
		if b.PaysOn != "" {
			if _, err := time.Parse("01-02", b.PaysOn); err != nil {
				errs = append(errs, fmt.Errorf("bonus.pays_on: %q isn't a month and day like 03-15", b.PaysOn))
			}
		}
	}
	if s := p.SignOn; s != nil {
		amount("sign_on.amount", s.Amount)
		date("sign_on.paid_on", s.PaidOn)
		date("sign_on.as_of", s.AsOf)
		if s.ClawbackMonths < 0 {
			errs = append(errs, errors.New("sign_on.clawback_months can't be negative"))
		}
	}
	for i, g := range p.Equity {
		field := fmt.Sprintf("equity[%d]", i)
		if g.Kind != EquityPublic && g.Kind != EquityPrivate {
			errs = append(errs, fmt.Errorf("%s.kind must be %q or %q", field, EquityPublic, EquityPrivate))
		}
		amount(field+".annual", g.Annual)
		if g.Annual > 0 && (g.Value > 0 || g.Shares > 0) {
			errs = append(errs, fmt.Errorf("%s: give annual, or value/shares, not both", field))
		}
		amount(field+".value", g.Value)
		amount(field+".shares", g.Shares)
		amount(field+".price", g.Price)
		date(field+".granted", g.Granted)
		date(field+".as_of", g.AsOf)
		if g.Years < 0 || g.CliffMonths < 0 || g.EveryMonths < 0 {
			errs = append(errs, fmt.Errorf("%s: vesting numbers can't be negative", field))
		}
		if g.EveryMonths > 0 && 12%g.EveryMonths != 0 {
			errs = append(errs, fmt.Errorf("%s.every_months must be 1, 2, 3, 4, 6 or 12", field))
		}
		if len(g.PercentByYear) > 0 {
			if g.Years > 0 && len(g.PercentByYear) != g.Years {
				errs = append(errs, fmt.Errorf("%s: percent_by_year has %d entries for %d years", field, len(g.PercentByYear), g.Years))
			}
			sum := 0.0
			for _, v := range g.PercentByYear {
				sum += v
			}
			if sum < 99.5 || sum > 100.5 {
				errs = append(errs, fmt.Errorf("%s: percent_by_year adds up to %g, not 100", field, sum))
			}
		}
	}
	if r := p.Retirement; r != nil {
		amount("retirement.match_percent", r.MatchPercent)
		amount("retirement.up_to_percent", r.UpToPercent)
		amount("retirement.annual", r.Annual)
		amount("retirement.contribution", r.Contribution)
		amount("retirement.unvested_balance", r.UnvestedBalance)
		date("retirement.vests_on", r.VestsOn)
		date("retirement.as_of", r.AsOf)
	}
	if h := p.Health; h != nil {
		amount("health.annual_cost", h.AnnualCost)
		amount("health.employer_hsa", h.EmployerHSA)
		date("health.as_of", h.AsOf)
	}
	for i, k := range p.Perks {
		amount(fmt.Sprintf("perks[%d].annual", i), k.Annual)
		date(fmt.Sprintf("perks[%d].as_of", i), k.AsOf)
	}
	for _, s := range p.None {
		known := false
		for _, name := range Sections {
			known = known || s == name
		}
		if !known {
			errs = append(errs, fmt.Errorf("none: %q isn't one of %s", s, strings.Join(Sections, ", ")))
		}
	}
	return errors.Join(errs...)
}

func parseDate(s string) (time.Time, error) { return time.Parse(time.DateOnly, s) }
