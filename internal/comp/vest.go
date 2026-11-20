package comp

import "time"

// The schedule assumed for a grant that doesn't state one.
const defaultVesting = "4 years with a 1-year cliff, then quarterly"

type vest struct {
	at     time.Time
	amount float64
}

// withDefaults fills in a grant's missing vesting terms. assumed is true when the grant gave no schedule at all.
func withDefaults(g Grant) (Grant, bool) {
	assumed := g.Years == 0 && len(g.PercentByYear) == 0
	if g.Years == 0 {
		g.Years = len(g.PercentByYear)
	}
	if g.Years == 0 {
		g.Years = 4
	}
	if assumed {
		g.CliffMonths = 12
	}
	if g.EveryMonths == 0 {
		g.EveryMonths = 3
	}
	return g, assumed
}

// vests lists a grant's vest dates and dollar amounts, from granted. Each year's percent is split evenly over that
// year's vests; whatever falls before the cliff vests on the cliff.
func vests(g Grant, granted time.Time) []vest {
	value := g.GrantValue()
	perYear := 12 / g.EveryMonths
	var (
		out   []vest
		carry float64
	)
	for m := g.EveryMonths; m <= g.Years*12; m += g.EveryMonths {
		year := (m - 1) / 12
		pct := 100 / float64(g.Years)
		if len(g.PercentByYear) > year {
			pct = g.PercentByYear[year]
		}
		carry += value * pct / 100 / float64(perYear)
		if m < g.CliffMonths {
			continue
		}
		out = append(out, vest{at: granted.AddDate(0, m, 0), amount: carry})
		carry = 0
	}
	return out
}

// nextVest is the first vest after t, or zero.
func nextVest(events []vest, t time.Time) time.Time {
	for _, v := range events {
		if v.at.After(t) {
			return v.at
		}
	}
	return time.Time{}
}
