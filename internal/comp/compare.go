package comp

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// Years is how many years a comparison covers.
const Years = 4

// How old a current-package number can get before the comparison says to check it again.
const (
	staleAfter      = 365 * 24 * time.Hour
	stalePriceAfter = 90 * 24 * time.Hour
)

// Line is one row of a package: whole dollars per year, plus the four-year total.
type Line struct {
	Label   string  `json:"label"`
	Amounts []int64 `json:"amounts"`
	Total   int64   `json:"total"`
	Note    string  `json:"note"`
	// A one-time amount (a sign-on, a balance vesting), not part of yearly pay.
	OneOff bool `json:"oneOff"`
}

// Column is one package over the comparison's years. Private equity is kept apart from Lines and never counted in
// Totals: it's paper value until there's a sale or listing.
type Column struct {
	Lines   []Line  `json:"lines"`
	Totals  []int64 `json:"totals"`
	Total   int64   `json:"total"`
	Private []Line  `json:"private"`
}

// Forfeit is something the owner walks away from by leaving on the start date.
type Forfeit struct {
	Label   string `json:"label"`
	Amount  int64  `json:"amount"`
	Note    string `json:"note"`
	Private bool   `json:"private"`
}

type Comparison struct {
	// The day the years count from: the incoming start date, or today when the offer doesn't say.
	Start    string `json:"start"`
	Years    int    `json:"years"`
	Current  Column `json:"current"`
	Incoming Column `json:"incoming"`
	// Incoming minus current, per year and over all years. Guaranteed pay only.
	Difference      []int64   `json:"difference"`
	DifferenceTotal int64     `json:"differenceTotal"`
	Forfeited       []Forfeit `json:"forfeited"`
	// To ask the company: what the offer doesn't say.
	Questions []string `json:"questions"`
	// What the numbers take for granted.
	Assumptions []string `json:"assumptions"`
	// About the current package: stale or missing numbers to fix in the data pack.
	Warnings []string `json:"warnings"`
	// Anything else the offer mentions that isn't counted.
	Notes []string `json:"notes"`
}

// Compare lays the incoming package next to the current one, both from the start date for Years years, as if the
// owner either stays or leaves on that day. today is when it's computed, for the start date fallback and staleness.
func Compare(current, incoming *Package, today time.Time) Comparison {
	today = day(today)
	c := Comparison{Years: Years, Questions: []string{}, Assumptions: []string{}, Warnings: []string{}, Forfeited: []Forfeit{},
		Notes: slices.Clone(incoming.Notes)}
	if c.Notes == nil {
		c.Notes = []string{}
	}

	start := today
	if t, err := parseDate(incoming.StartDate); err == nil {
		start = t
	} else {
		c.Questions = append(c.Questions, "What's the start date? The comparison counts from today until you know.")
	}
	c.Start = start.Format(time.DateOnly)

	cur := newBuilder(start)
	inc := newBuilder(start)
	c.Assumptions = append(c.Assumptions,
		"Base pay stays flat on both sides: no raises, promotions or new stock grants are assumed.",
		"Bonuses are counted at target, every year.")

	// The owner's own retirement contribution doesn't change with the job, so both matches are worked out on it.
	contribution := 0.0
	if current.Retirement != nil {
		contribution = current.Retirement.Contribution
	}

	// Base, bonus, retirement, health and perks recur every year.
	for _, side := range []struct {
		b *builder
		p *Package
	}{{cur, current}, {inc, incoming}} {
		base := side.p.Base.Amount
		side.b.yearly("Base pay", base, "")
		if bonus := side.p.bonusAmount(); bonus > 0 {
			note := ""
			if b := side.p.Bonus; b.Amount == 0 {
				note = fmt.Sprintf("%g%% target", b.TargetPercent)
			}
			side.b.yearly("Bonus (target)", bonus, note)
		}
		if r := side.p.Retirement; r != nil {
			if match := r.Annual + matched(r, base, contribution)*r.MatchPercent/100; match > 0 {
				side.b.yearly("Retirement match", match, matchNote(r))
			}
		}
		if h := side.p.Health; h != nil && (h.AnnualCost != 0 || h.EmployerHSA != 0) {
			side.b.yearly("Health (your premiums, less HSA)", h.EmployerHSA-h.AnnualCost, "")
		}
		if len(side.p.Perks) > 0 {
			total, names := 0.0, []string{}
			for _, k := range side.p.Perks {
				total += k.Annual
				names = append(names, k.Name)
			}
			side.b.yearly("Perks", total, strings.Join(names, ", "))
		}
	}
	if retirementMatched(current) || retirementMatched(incoming) {
		if contribution > 0 {
			c.Assumptions = append(c.Assumptions, fmt.Sprintf("You keep putting %s a year in your 401(k), on both sides.", Money(round(contribution))))
		} else {
			c.Assumptions = append(c.Assumptions, "You put in just enough to get each full match. Add retirement.contribution to use what you really put in.")
		}
	}

	// Sign-on: the incoming one lands in year one; a current one inside its clawback window is repaid on leaving.
	if s := incoming.SignOn; s != nil && s.Amount > 0 {
		note := ""
		if s.ClawbackMonths > 0 {
			note = fmt.Sprintf("repaid if you leave within %d months", s.ClawbackMonths)
		}
		inc.once("Sign-on bonus", 0, s.Amount, note)
	}
	if s := current.SignOn; s != nil && s.Amount > 0 && s.ClawbackMonths > 0 {
		if paid, err := parseDate(s.PaidOn); err == nil {
			if until := paid.AddDate(0, s.ClawbackMonths, 0); start.Before(until) {
				inc.once("Repay current sign-on", 0, -s.Amount, "you'd leave inside its clawback window")
				c.Forfeited = append(c.Forfeited, Forfeit{Label: "Current sign-on bonus, repaid", Amount: round(s.Amount),
					Note: "clawback runs to " + until.Format(time.DateOnly)})
				c.Assumptions = append(c.Assumptions, "The current sign-on bonus is repaid in full, not pro-rated.")
			}
		} else {
			c.Warnings = append(c.Warnings, "sign_on has a clawback but no paid_on date, so a repayment can't be worked out.")
		}
	}

	// The current bonus earned since its last payout is lost by leaving before the next one.
	if b := current.Bonus; b != nil && current.bonusAmount() > 0 {
		if b.PaysOn == "" {
			c.Warnings = append(c.Warnings, "Add bonus.pays_on (MM-DD) to see the bonus you'd give up by leaving.")
		} else if last, next, ok := lastPayout(b.PaysOn, start); ok && start.After(last) {
			earned := current.bonusAmount() * start.Sub(last).Hours() / 24 / 365
			c.Forfeited = append(c.Forfeited, Forfeit{Label: "Bonus earned since the last payout", Amount: round(earned),
				Note: fmt.Sprintf("pro-rated since %s; it pays %s if you stay", last.Format(time.DateOnly), next.Format(time.DateOnly))})
		}
	}

	// Unvested retirement money vests on its date if the owner stays, and is lost if they leave.
	if r := current.Retirement; r != nil && r.UnvestedBalance > 0 {
		if vests, err := parseDate(r.VestsOn); err == nil && vests.After(start) {
			cur.at("Retirement balance vesting", vests, r.UnvestedBalance, "vests "+r.VestsOn)
			c.Forfeited = append(c.Forfeited, Forfeit{Label: "Unvested retirement match", Amount: round(r.UnvestedBalance), Note: "vests " + r.VestsOn})
		} else if r.VestsOn == "" {
			c.Forfeited = append(c.Forfeited, Forfeit{Label: "Unvested retirement match", Amount: round(r.UnvestedBalance)})
			c.Warnings = append(c.Warnings, "Add retirement.vests_on so staying counts the unvested balance in the right year.")
		}
	}

	// Equity: each grant's vests inside the window, by year. Current grants count only what vests after the start
	// date, and everything unvested then is forfeited.
	for _, g := range current.Equity {
		if g.Annual > 0 {
			cur.annualGrant(g)
			continue
		}
		granted, err := parseDate(g.Granted)
		if err != nil {
			if granted, err = parseDate(current.StartDate); err != nil {
				c.Warnings = append(c.Warnings, fmt.Sprintf("%s has no granted date, so it's left out.", grantName(g)))
				continue
			}
			c.Warnings = append(c.Warnings, fmt.Sprintf("%s has no granted date; vesting is counted from your start date.", grantName(g)))
		}
		g, assumed := withDefaults(g)
		if assumed {
			c.Warnings = append(c.Warnings, fmt.Sprintf("%s has no vesting schedule; assumed %s.", grantName(g), defaultVesting))
		}
		events := vests(g, granted)
		cur.grant(g, events)
		unvested := 0.0
		for _, v := range events {
			if v.at.After(start) {
				unvested += v.amount
			}
		}
		if unvested > 0 {
			label := "Unvested stock: " + grantName(g)
			note := ""
			if next := nextVest(events, start); !next.IsZero() {
				note = "next vest " + next.Format(time.DateOnly)
			}
			c.Forfeited = append(c.Forfeited, Forfeit{Label: label, Amount: round(unvested), Note: note, Private: g.Kind == EquityPrivate})
		}
	}
	if slices.ContainsFunc(current.Equity, func(g Grant) bool { return g.Kind == EquityPublic }) {
		c.Assumptions = append(c.Assumptions, "Current stock tapers off over the years because future refreshers aren't counted.")
	}
	for _, g := range incoming.Equity {
		if g.Annual > 0 {
			inc.annualGrant(g)
			continue
		}
		granted := start
		if t, err := parseDate(g.Granted); err == nil {
			granted = t
		}
		g, assumed := withDefaults(g)
		if assumed {
			c.Questions = append(c.Questions, fmt.Sprintf("What's the vesting schedule for %s? Assumed %s.", grantName(g), defaultVesting))
		}
		inc.grant(g, vests(g, granted))
	}
	if slices.ContainsFunc(append(slices.Clone(current.Equity), incoming.Equity...), func(g Grant) bool { return g.Kind == EquityPublic }) {
		c.Assumptions = append(c.Assumptions, "Public stock is valued at the price given and doesn't move.")
	}

	c.Current, c.Incoming = cur.column(), inc.column()
	c.Difference = make([]int64, Years)
	for i := range Years {
		c.Difference[i] = c.Incoming.Totals[i] - c.Current.Totals[i]
	}
	c.DifferenceTotal = c.Incoming.Total - c.Current.Total

	c.Questions = append(c.Questions, questions(incoming, c.Forfeited)...)
	c.Warnings = append(c.Warnings, staleness(current, today)...)
	return c
}

func (p *Package) bonusAmount() float64 {
	if p.Bonus == nil {
		return 0
	}
	if p.Bonus.Amount > 0 {
		return p.Bonus.Amount
	}
	return p.Base.Amount * p.Bonus.TargetPercent / 100
}

// matched is how much of the owner's contribution the employer matches: all of it up to UpToPercent of base (all of
// it when there's no limit), or, when the contribution isn't known, exactly that limit.
func matched(r *Retirement, base, contribution float64) float64 {
	limit := base * r.UpToPercent / 100
	switch {
	case contribution == 0:
		return limit
	case r.UpToPercent == 0:
		return contribution
	}
	return min(contribution, limit)
}

func matchNote(r *Retirement) string {
	switch {
	case r.MatchPercent == 0:
		return ""
	case r.UpToPercent == 0:
		return fmt.Sprintf("%g%% of what you put in", r.MatchPercent)
	}
	return fmt.Sprintf("%g%% match on up to %g%% of base", r.MatchPercent, r.UpToPercent)
}

func retirementMatched(p *Package) bool { return p.Retirement != nil && p.Retirement.MatchPercent > 0 }

// questions are what to ask the company about the incoming package, for every section it doesn't settle.
func questions(p *Package, forfeited []Forfeit) []string {
	var out []string
	missing := func(section string) bool { return !p.isNone(section) }
	switch {
	case p.Bonus == nil && missing(SectionBonus):
		out = append(out, "Is there a yearly bonus? If so, what's the target, and what has it actually paid out the last few years?")
	case p.Bonus != nil && p.bonusAmount() > 0:
		out = append(out, "What has the bonus actually paid out against target the last few years, and is the first year pro-rated?")
	}
	if p.SignOn == nil && missing(SectionSignOn) {
		left := int64(0)
		for _, f := range forfeited {
			if !f.Private {
				left += f.Amount
			}
		}
		q := "Is there a sign-on bonus?"
		if left > 0 {
			q += fmt.Sprintf(" Worth asking for one to cover the %s you'd leave behind.", Money(left))
		}
		out = append(out, q)
	} else if p.SignOn != nil && p.SignOn.ClawbackMonths == 0 {
		out = append(out, "Does the sign-on bonus have to be repaid if you leave early, and within how long?")
	}
	if len(p.Equity) == 0 && missing(SectionEquity) {
		out = append(out, "Is there equity? If so, how many shares or what dollar value, and how does it vest?")
	}
	for _, g := range p.Equity {
		switch {
		case g.Kind == EquityPrivate:
			out = append(out, fmt.Sprintf("For %s: how many shares or options, the strike price, the latest 409A value, the preferred price "+
				"at the last round, total shares outstanding, and how long you'd have to exercise after leaving.", grantName(g)))
		case g.Shares == 0 && g.Annual == 0:
			out = append(out, fmt.Sprintf("How is %s turned into shares: the price on the grant date, or an average?", grantName(g)))
		}
	}
	if slices.ContainsFunc(p.Equity, func(g Grant) bool { return g.Kind == EquityPublic }) {
		out = append(out, "Are there yearly stock refreshers, and how big are they usually?")
	}
	switch {
	case p.Retirement == nil && missing(SectionRetirement):
		out = append(out, "What's the 401(k) match, and does it vest over time?")
	case p.Retirement != nil:
		out = append(out, "Does the 401(k) match vest right away, or over time?")
	}
	if p.Health == nil && missing(SectionHealth) {
		out = append(out, "What would you pay in health premiums a year for your coverage, and does the company put money in an HSA?")
	}
	return out
}

// staleness lists current-package numbers with no date or an old one. Stock prices go stale faster than the rest.
func staleness(p *Package, today time.Time) []string {
	var out []string
	check := func(what, asOf string, limit time.Duration) {
		if asOf == "" {
			asOf = p.AsOf
		}
		t, err := parseDate(asOf)
		switch {
		case err != nil:
			out = append(out, fmt.Sprintf("%s has no as_of date; add one when you check it.", what))
		case today.Sub(t) > limit:
			out = append(out, fmt.Sprintf("%s was last checked %s; worth updating.", what, asOf))
		}
	}
	check("Base pay", p.Base.AsOf, staleAfter)
	if p.Bonus != nil {
		check("The bonus", p.Bonus.AsOf, staleAfter)
	}
	for _, g := range p.Equity {
		if g.Kind == EquityPublic && g.Shares > 0 {
			check("The stock price for "+grantName(g), g.AsOf, stalePriceAfter)
		} else {
			check(grantName(g), g.AsOf, staleAfter)
		}
	}
	if p.Retirement != nil {
		check("The retirement match", p.Retirement.AsOf, staleAfter)
	}
	if p.Health != nil {
		check("Health costs", p.Health.AsOf, staleAfter)
	}
	for _, k := range p.Perks {
		check("The perk "+k.Name, k.AsOf, staleAfter)
	}
	return out
}

func grantName(g Grant) string {
	if g.Label != "" {
		return g.Label
	}
	if g.Kind == EquityPrivate {
		return "the equity grant"
	}
	return "the stock grant"
}

// lastPayout is the latest MM-DD payout on or before at, and the one after it.
func lastPayout(paysOn string, at time.Time) (last, next time.Time, ok bool) {
	md, err := time.Parse("01-02", paysOn)
	if err != nil {
		return last, next, false
	}
	last = time.Date(at.Year(), md.Month(), md.Day(), 0, 0, 0, 0, time.UTC)
	if last.After(at) {
		last = last.AddDate(-1, 0, 0)
	}
	return last, last.AddDate(1, 0, 0), true
}

// --- building columns ---

// builder accumulates a package's lines in exact dollars, rounding only when the column is finished.
type builder struct {
	start time.Time
	lines []*rawLine
}

type rawLine struct {
	label, note     string
	amounts         [Years]float64
	private, oneOff bool
}

func newBuilder(start time.Time) *builder { return &builder{start: start} }

func (b *builder) add(label, note string) *rawLine {
	l := &rawLine{label: label, note: note}
	b.lines = append(b.lines, l)
	return l
}

func (b *builder) yearly(label string, amount float64, note string) {
	l := b.add(label, note)
	for i := range Years {
		l.amounts[i] = amount
	}
}

func (b *builder) once(label string, year int, amount float64, note string) {
	l := b.add(label, note)
	l.amounts[year], l.oneOff = amount, true
}

// at adds amount in the year that holds the date, if it's inside the window.
func (b *builder) at(label string, when time.Time, amount float64, note string) {
	if i := b.year(when); i >= 0 {
		l := b.add(label, note)
		l.amounts[i], l.oneOff = amount, true
	}
}

// year is the index of the year (start, start+1y] that holds t, or -1 outside the window.
func (b *builder) year(t time.Time) int {
	for i := range Years {
		if t.After(b.start.AddDate(i, 0, 0)) && !t.After(b.start.AddDate(i+1, 0, 0)) {
			return i
		}
	}
	return -1
}

func (b *builder) grant(g Grant, events []vest) {
	label := "Stock: " + grantName(g)
	note := ""
	if g.Kind == EquityPrivate {
		label = "Equity (paper value): " + grantName(g)
	}
	if g.Shares > 0 {
		note = fmt.Sprintf("%g shares at %s", g.Shares, Money(round(g.Price)))
	}
	l := b.add(label, note)
	l.private = g.Kind == EquityPrivate
	for _, v := range events {
		if i := b.year(v.at); i >= 0 {
			l.amounts[i] += v.amount
		}
	}
}

func (b *builder) annualGrant(g Grant) {
	label := "Stock: " + grantName(g)
	if g.Kind == EquityPrivate {
		label = "Equity (paper value): " + grantName(g)
	}
	b.yearly(label, g.Annual, "yearly")
	b.lines[len(b.lines)-1].private = g.Kind == EquityPrivate
}

func (b *builder) column() Column {
	col := Column{Lines: []Line{}, Totals: make([]int64, Years), Private: []Line{}}
	for _, raw := range b.lines {
		l := Line{Label: raw.label, Note: raw.note, Amounts: make([]int64, Years), OneOff: raw.oneOff}
		for i, a := range raw.amounts {
			l.Amounts[i] = round(a)
			l.Total += l.Amounts[i]
		}
		if raw.private {
			col.Private = append(col.Private, l)
			continue
		}
		col.Lines = append(col.Lines, l)
		for i, a := range l.Amounts {
			col.Totals[i] += a
		}
		col.Total += l.Total
	}
	return col
}

func round(v float64) int64 { return int64(math.Round(v)) }

func day(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }

// Money formats whole dollars: $185,000, -$12,500.
func Money(v int64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	digits := fmt.Sprint(v)
	var sb strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(d)
	}
	return sign + "$" + sb.String()
}
