package comp

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/datapack"
	"github.com/ahpalmer/consigliere/internal/llm"
	"github.com/ahpalmer/consigliere/internal/store"
)

var today = time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC)

func date(s string) time.Time {
	t, err := parseDate(s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestVestsWithCliff(t *testing.T) {
	g, assumed := withDefaults(Grant{Kind: EquityPublic, Value: 100_000})
	if !assumed || g.Years != 4 || g.CliffMonths != 12 || g.EveryMonths != 3 {
		t.Fatalf("defaults = %+v, assumed %v", g, assumed)
	}
	events := vests(g, date("2025-01-04"))
	if len(events) != 13 {
		t.Fatalf("got %d vests, want 13 (the cliff, then 12 quarters)", len(events))
	}
	if events[0].at != date("2026-01-04") || events[0].amount != 25_000 {
		t.Errorf("cliff vest = %v %v, want 2026-01-04 25000", events[0].at, events[0].amount)
	}
	if events[1].amount != 6_250 || events[12].at != date("2029-01-04") {
		t.Errorf("after the cliff: %v, last %v", events[1].amount, events[12].at)
	}
	sum := 0.0
	for _, v := range events {
		sum += v.amount
	}
	if sum != 100_000 {
		t.Errorf("vests add up to %v", sum)
	}
}

func TestVestsUnevenYears(t *testing.T) {
	g, assumed := withDefaults(Grant{Kind: EquityPublic, Value: 100_000, PercentByYear: []float64{5, 15, 40, 40}, EveryMonths: 12})
	if assumed || g.Years != 4 {
		t.Fatalf("years from percent_by_year: %+v", g)
	}
	var got []float64
	for _, v := range vests(g, date("2027-01-04")) {
		got = append(got, v.amount)
	}
	if !slices.Equal(got, []float64{5_000, 15_000, 40_000, 40_000}) {
		t.Errorf("got %v", got)
	}
}

func currentPackage() *Package {
	return &Package{
		AsOf:      "2026-06-01",
		StartDate: "2025-01-04",
		Base:      &Base{Amount: 180_000},
		Bonus:     &Bonus{TargetPercent: 10, PaysOn: "03-15"},
		SignOn:    &SignOn{Amount: 10_000, PaidOn: "2026-06-01", ClawbackMonths: 12},
		Equity: []Grant{{Label: "New hire", Kind: EquityPublic, Shares: 1_000, Price: 200, Granted: "2025-01-04",
			Years: 4, CliffMonths: 12, EveryMonths: 3, AsOf: "2026-11-01"}},
		Retirement: &Retirement{MatchPercent: 50, UpToPercent: 6, UnvestedBalance: 3_000, VestsOn: "2027-06-01"},
		Health:     &Health{AnnualCost: 2_400},
	}
}

func incomingPackage() *Package {
	return &Package{
		Company:    "Globex",
		StartDate:  "2027-01-04",
		Base:       &Base{Amount: 200_000},
		Bonus:      &Bonus{TargetPercent: 15},
		SignOn:     &SignOn{Amount: 25_000, ClawbackMonths: 12},
		Equity:     []Grant{{Kind: EquityPublic, Value: 160_000, Years: 4}},
		Retirement: &Retirement{MatchPercent: 100, UpToPercent: 4},
		Perks:      []Perk{{Name: "Learning", Annual: 1_000}},
		Notes:      []string{"20 days PTO"},
	}
}

func line(t *testing.T, col Column, label string) Line {
	t.Helper()
	for _, l := range append(slices.Clone(col.Lines), col.Private...) {
		if l.Label == label {
			return l
		}
	}
	t.Fatalf("no line %q in %+v", label, col.Lines)
	return Line{}
}

func TestCompare(t *testing.T) {
	c := Compare(currentPackage(), incomingPackage(), today)

	if c.Start != "2027-01-04" || c.Years != 4 {
		t.Fatalf("start %s, years %d", c.Start, c.Years)
	}
	// Staying: current stock vests 4 quarters a year until the grant runs out in January 2029; the vest on the start
	// date itself is already the owner's. The unvested retirement balance lands in year one.
	if got := line(t, c.Current, "Stock: New hire").Amounts; !slices.Equal(got, []int64{50_000, 50_000, 0, 0}) {
		t.Errorf("current stock = %v", got)
	}
	if got := line(t, c.Current, "Retirement balance vesting").Amounts; !slices.Equal(got, []int64{3_000, 0, 0, 0}) {
		t.Errorf("retirement balance = %v", got)
	}
	if got := line(t, c.Current, "Health (your premiums, less HSA)").Amounts[0]; got != -2_400 {
		t.Errorf("health = %d", got)
	}
	if !slices.Equal(c.Current.Totals, []int64{254_000, 251_000, 201_000, 201_000}) || c.Current.Total != 907_000 {
		t.Errorf("current totals = %v (%d)", c.Current.Totals, c.Current.Total)
	}

	// Leaving: the sign-on lands in year one, less the current sign-on repaid; the new grant vests quarterly from
	// the start date with no cliff.
	if got := line(t, c.Incoming, "Repay current sign-on").Amounts; !slices.Equal(got, []int64{-10_000, 0, 0, 0}) {
		t.Errorf("repayment = %v", got)
	}
	if got := line(t, c.Incoming, "Stock: the stock grant").Amounts; !slices.Equal(got, []int64{40_000, 40_000, 40_000, 40_000}) {
		t.Errorf("incoming stock = %v", got)
	}
	if got := line(t, c.Incoming, "Retirement match").Amounts[0]; got != 8_000 {
		t.Errorf("incoming match = %d", got)
	}
	if !slices.Equal(c.Incoming.Totals, []int64{294_000, 279_000, 279_000, 279_000}) || c.Incoming.Total != 1_131_000 {
		t.Errorf("incoming totals = %v (%d)", c.Incoming.Totals, c.Incoming.Total)
	}
	if !slices.Equal(c.Difference, []int64{40_000, 28_000, 78_000, 78_000}) || c.DifferenceTotal != 224_000 {
		t.Errorf("difference = %v (%d)", c.Difference, c.DifferenceTotal)
	}

	// 295 days of an $18,000 bonus since the March 15 payout.
	want := []Forfeit{
		{Label: "Current sign-on bonus, repaid", Amount: 10_000, Note: "clawback runs to 2027-06-01"},
		{Label: "Bonus earned since the last payout", Amount: 14_548, Note: "pro-rated since 2026-03-15; it pays 2027-03-15 if you stay"},
		{Label: "Unvested retirement match", Amount: 3_000, Note: "vests 2027-06-01"},
		{Label: "Unvested stock: New hire", Amount: 100_000, Note: "next vest 2027-04-04"},
	}
	if !slices.Equal(c.Forfeited, want) {
		t.Errorf("forfeited =\n%+v\nwant\n%+v", c.Forfeited, want)
	}

	if !slices.ContainsFunc(c.Questions, func(q string) bool { return strings.Contains(q, "health premiums") }) {
		t.Errorf("no health question in %q", c.Questions)
	}
	if slices.ContainsFunc(c.Questions, func(q string) bool { return strings.Contains(q, "start date") }) {
		t.Errorf("asked for a start date the offer gives: %q", c.Questions)
	}
	if len(c.Warnings) != 0 {
		t.Errorf("warnings = %q", c.Warnings)
	}
	if !slices.Equal(c.Notes, []string{"20 days PTO"}) {
		t.Errorf("notes = %q", c.Notes)
	}
}

func TestComparePrivateEquityAndGaps(t *testing.T) {
	cur := currentPackage()
	cur.Equity[0].AsOf = "2026-06-01" // a stock price five months old
	cur.Bonus.PaysOn = ""
	inc := &Package{Base: &Base{Amount: 190_000}, None: []string{SectionBonus},
		Equity: []Grant{{Label: "Options", Kind: EquityPrivate, Value: 400_000}}}

	c := Compare(cur, inc, today)

	if c.Start != "2026-11-20" {
		t.Errorf("start = %s, want today", c.Start)
	}
	// Private equity is shown on its own and never counted in totals.
	// With the assumed schedule (1-year cliff, then quarterly) the whole grant vests inside the four years.
	if len(c.Incoming.Private) != 1 || c.Incoming.Private[0].Total != 400_000 {
		t.Errorf("private = %+v", c.Incoming.Private)
	}
	// Base pay, less the current sign-on repaid.
	if c.Incoming.Total != 4*190_000-10_000 {
		t.Errorf("incoming total = %d", c.Incoming.Total)
	}
	for _, want := range []string{"start date", "Options: how many shares", "sign-on", "401(k)", "vesting schedule for Options"} {
		if !slices.ContainsFunc(c.Questions, func(q string) bool { return strings.Contains(q, want) }) {
			t.Errorf("no question about %q in %q", want, c.Questions)
		}
	}
	for _, unwanted := range []string{"yearly bonus", "Is there equity"} {
		if slices.ContainsFunc(c.Questions, func(q string) bool { return strings.Contains(q, unwanted) }) {
			t.Errorf("asked %q, which the offer settles", unwanted)
		}
	}
	for _, want := range []string{"stock price for New hire was last checked 2026-06-01", "bonus.pays_on"} {
		if !slices.ContainsFunc(c.Warnings, func(w string) bool { return strings.Contains(w, want) }) {
			t.Errorf("no warning about %q in %q", want, c.Warnings)
		}
	}
}

// The owner's case: 50% of everything they put in, no limit, and steady yearly stock. The incoming match is worked out
// on the same contribution, so a "100% of the first 4%" offer matches far less.
func TestRetirementContributionAndAnnualStock(t *testing.T) {
	cur := &Package{AsOf: "2026-11-01", Base: &Base{Amount: 120_000},
		Retirement: &Retirement{MatchPercent: 50, Contribution: 24_000},
		Equity:     []Grant{{Label: "Refreshers", Kind: EquityPublic, Annual: 12_000}}}
	inc := &Package{StartDate: "2027-01-04", Base: &Base{Amount: 140_000}, Retirement: &Retirement{MatchPercent: 100, UpToPercent: 4}}

	c := Compare(cur, inc, today)

	if got := line(t, c.Current, "Retirement match"); got.Amounts[0] != 12_000 || got.Note != "50% of what you put in" {
		t.Errorf("current match = %+v", got)
	}
	if got := line(t, c.Incoming, "Retirement match").Amounts[0]; got != 5_600 {
		t.Errorf("incoming match = %d, want 4%% of 140,000", got)
	}
	if got := line(t, c.Current, "Stock: Refreshers").Amounts; !slices.Equal(got, []int64{12_000, 12_000, 12_000, 12_000}) {
		t.Errorf("yearly stock = %v", got)
	}
	if len(c.Forfeited) != 0 {
		t.Errorf("forfeited = %+v", c.Forfeited)
	}
	if !slices.ContainsFunc(c.Assumptions, func(a string) bool { return strings.Contains(a, "$24,000 a year") }) {
		t.Errorf("assumptions = %q", c.Assumptions)
	}
}

func TestParse(t *testing.T) {
	p, err := Parse(incomingPackage().YAML())
	if err != nil || p.Base.Amount != 200_000 || p.Equity[0].Years != 4 {
		t.Fatalf("round trip: %+v %v", p, err)
	}
	for text, want := range map[string]string{
		"":                                 "empty",
		"base: {amount: 1}\nbasee: 2\n":    "basee",
		"bonus: {target_percent: 10}\n":    "base.amount is required",
		"base: {amount: 1, as_of: soon}\n": "isn't a date",
		"base: {amount: 1}\nnone: [pto]\n": "isn't one of",
		"base: {amount: 1}\nequity: [{kind: public, value: 5, percent_by_year: [50, 40]}]\n": "adds up to 90",
		"base: {amount: 1}\nequity: [{kind: rsu, value: 5}]\n":                               "kind must be",
		"base: {amount: 1}\nequity: [{kind: public, value: 5, every_months: 5}]\n":           "every_months",
		"base: {amount: 1}\nequity: [{kind: public, value: 5, annual: 5}]\n":                 "not both",
	} {
		if _, err := Parse(text); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) = %v, want an error about %q", text, err, want)
		}
	}
}

func TestSamplePackParses(t *testing.T) {
	pack, err := datapack.Open("../../samples/data-pack")
	if err != nil {
		t.Fatal(err)
	}
	p, ok, err := LoadCurrent(pack)
	if err != nil || !ok {
		t.Fatalf("sample comp/current.yaml: ok=%v err=%v", ok, err)
	}
	Compare(p, incomingPackage(), today)
}

type fakeLLM struct{ reply string }

func (f fakeLLM) Structured(_ context.Context, req llm.StructuredRequest) (string, error) {
	if req.Task != llm.TaskExtract {
		panic("wrong task " + req.Task)
	}
	return f.reply, nil
}

func TestExtract(t *testing.T) {
	reply := `{"company":"Globex","start_date":"2027-01-04","base":200000,
		"bonus":{"target_percent":15,"amount":null},
		"sign_on":{"amount":25000,"clawback_months":null},
		"equity":[{"label":"RSUs","kind":"public","value":160000,"shares":800,"price":null,"years":4,"cliff_months":12,"every_months":5,"percent_by_year":[]}],
		"retirement":null,"health":{"annual_cost":null,"employer_hsa":750},
		"perks":[{"name":"Learning","annual":1000},{"name":"Gym","annual":null}],
		"none":["retirement","pto"],"notes":["20 days PTO"," "]}`
	p, err := Extractor{LLM: fakeLLM{reply}, Owner: "Sam"}.Extract(context.Background(), "the offer", today)
	if err != nil {
		t.Fatal(err)
	}
	g := p.Equity[0]
	switch {
	case p.AsOf != "2026-11-20" || p.Base.Amount != 200_000 || p.Bonus.TargetPercent != 15:
		t.Errorf("basics: %+v", p)
	case g.Shares != 0 || g.Value != 160_000 || g.EveryMonths != 0 || g.CliffMonths != 12:
		t.Errorf("shares with no price, or an impossible frequency, should be dropped: %+v", g)
	case p.Health.EmployerHSA != 750 || len(p.Perks) != 1 || p.SignOn.ClawbackMonths != 0:
		t.Errorf("health %+v perks %+v sign-on %+v", p.Health, p.Perks, p.SignOn)
	case !slices.Equal(p.None, []string{"retirement"}) || !slices.Equal(p.Notes, []string{"20 days PTO"}):
		t.Errorf("none %q notes %q", p.None, p.Notes)
	}

	if _, err := (Extractor{LLM: fakeLLM{`{"base":null,"equity":[],"perks":[],"none":[],"notes":[]}`}}).Extract(context.Background(), "hi", today); err != ErrNotAnOffer {
		t.Errorf("no base: err = %v", err)
	}
}

func TestOffers(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO "Pursuits" ("Company", "Title", "Stage", "ActivatedAt", "UpdatedAt") VALUES ('Globex', 'SRE', 'active', ?, ?)`,
		store.FormatTime(today), store.FormatTime(today)); err != nil {
		t.Fatal(err)
	}
	offers := &Offers{DB: db, Clock: clock.NewFake(today)}
	o, err := offers.Add(ctx, 1, "pasted", incomingPackage())
	if err != nil || o.PursuitID != 1 || !strings.Contains(o.Package, "amount: 200000") {
		t.Fatalf("add: %+v %v", o, err)
	}
	if _, err := offers.Update(ctx, o.ID, "base: {amount: -5}\n"); err == nil {
		t.Error("saved a negative base")
	}
	if o, err = offers.Update(ctx, o.ID, "base: {amount: 210000}\n"); err != nil || o.Package != "base: {amount: 210000}\n" {
		t.Errorf("update: %+v %v", o, err)
	}
	if list, _ := offers.List(ctx, 1); len(list) != 1 {
		t.Errorf("list = %d", len(list))
	}
	if err := offers.Delete(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := offers.Get(ctx, o.ID); err != ErrNotFound {
		t.Errorf("after delete: %v", err)
	}
}

func TestMoney(t *testing.T) {
	for v, want := range map[int64]string{0: "$0", 999: "$999", 1_000: "$1,000", 185_000: "$185,000", -12_500: "-$12,500", 1_234_567: "$1,234,567"} {
		if got := Money(v); got != want {
			t.Errorf("Money(%d) = %s, want %s", v, got, want)
		}
	}
}
