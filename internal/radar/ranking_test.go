package radar

import (
	"slices"
	"testing"
	"time"

	"github.com/ahpalmer/consigliere/internal/comp"
)

func TestFitBlendsResumeLocationAndPay(t *testing.T) {
	prefs := Preferences{Tiers: []Tier{{Name: "local"}, {Name: "remote", Remote: true}, {Name: "relocation"}}}
	r := NewRanking(prefs, Watchlist{})
	r.PayNow = &comp.Yardstick{Base: 100_000, Total: 150_000}
	score := func(v int) *int { return &v }
	pay := func(v float64) *float64 { return &v }

	// Default weights 60/20/20. Remote is the second tier (75); pay at total scores 80.
	parts := r.Parts(ItemView{FitScore: score(90), Tier: "remote", CompMin: pay(150_000), CompMax: pay(150_000)})
	if parts.Location != 75 || *parts.Pay != 80 || !parts.PayListed || parts.Fit != 85 {
		t.Errorf("parts = %+v, pay %d", parts, *parts.Pay)
	}
	// No pay listed: the 50 stand-in.
	if parts := r.Parts(ItemView{FitScore: score(90), Tier: "local"}); *parts.Pay != 50 || parts.PayListed || parts.Fit != 84 {
		t.Errorf("unlisted pay: %+v", parts)
	}
	// Unscored items rank at 0 whatever their location and pay.
	if parts := r.Parts(ItemView{Tier: "local"}); parts.Fit != 0 {
		t.Errorf("unscored: %+v", parts)
	}

	// Without a current package, pay drops out of the weights.
	r.PayNow = nil
	if parts := r.Parts(ItemView{FitScore: score(90), Tier: "relocation"}); parts.Pay != nil || parts.Fit != 80 {
		t.Errorf("no pay now: %+v", parts)
	}

	// Explicit weights, a tier score and an unknown-pay score from the preferences.
	unknown := 30
	r.Prefs.Fit = FitSettings{Weights: FitWeights{Resume: 2, Location: 1, Pay: 1}, Location: map[string]int{"Relocation": 10}, UnknownPay: &unknown}
	r.PayNow = &comp.Yardstick{Base: 100_000, Total: 150_000}
	if parts := r.Parts(ItemView{FitScore: score(80), Tier: "relocation"}); parts.Location != 10 || *parts.Pay != 30 || parts.Fit != 50 {
		t.Errorf("configured: %+v", parts)
	}
}

// Sorting is by fit alone (after priority companies); digests group the chosen items by tier and category without
// changing the order inside a group.
func TestSortByFitThenGroupForDigests(t *testing.T) {
	prefs := Preferences{Tiers: []Tier{{Name: "local"}, {Name: "remote"}}, Categories: []Category{{Name: "software"}, {Name: "robotics"}},
		Fit: FitSettings{Weights: FitWeights{Resume: 1}}}
	r := NewRanking(prefs, Watchlist{})
	item := func(id int64, fit int, tier, category string) ItemView {
		return ItemView{ID: id, FitScore: &fit, Tier: tier, Category: category, FirstSeenAt: time.Unix(id, 0)}
	}
	items := []ItemView{item(1, 55, "local", "software"), item(2, 90, "remote", "robotics"), item(3, 70, "local", "robotics"), item(4, 80, "local", "software")}
	ids := func() []int64 {
		var out []int64
		for _, i := range items {
			out = append(out, i.ID)
		}
		return out
	}
	r.Sort(items)
	if got := ids(); !slices.Equal(got, []int64{2, 4, 3, 1}) {
		t.Errorf("sorted = %v", got)
	}
	r.Group(items)
	if got := ids(); !slices.Equal(got, []int64{4, 1, 3, 2}) {
		t.Errorf("grouped = %v", got)
	}
}
