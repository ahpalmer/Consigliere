package comp

import "time"

// Yardstick is what the owner makes now, for a quick read on job postings: base pay, and total yearly pay (base,
// bonus, retirement match, stock vesting this year, health and perks; no one-offs, no private equity). Postings
// usually list base pay only, so a posting is held against both.
type Yardstick struct {
	Base  int64 `json:"base"`
	Total int64 `json:"total"`
}

// NowFrom is the current package's yardstick: its first year from today.
func NowFrom(current *Package, today time.Time) Yardstick {
	c := Compare(current, &Package{Base: current.Base}, today)
	y := Yardstick{Base: round(current.Base.Amount)}
	for _, l := range c.Current.Lines {
		if !l.OneOff {
			y.Total += l.Amounts[0]
		}
	}
	return y
}

// Posting verdicts: the listed range is all at or above total pay now, reaches it, or is all below it.
const (
	PayAbove    = "above"
	PayOverlaps = "overlaps"
	PayBelow    = "below"
)

// PostingPay is a posting's listed range held against the yardstick, as listed minus now, low and high.
type PostingPay struct {
	Verdict string    `json:"verdict"`
	VsBase  [2]int64  `json:"vsBase"`
	VsTotal [2]int64  `json:"vsTotal"`
	Now     Yardstick `json:"now"`
}

// Posting compares a listed pay range (either end may be missing) with pay now. nil when nothing is listed.
func (y Yardstick) Posting(min, max *float64) *PostingPay {
	if min == nil && max == nil {
		return nil
	}
	if min == nil {
		min = max
	}
	if max == nil {
		max = min
	}
	lo, hi := round(*min), round(*max)
	p := &PostingPay{Now: y, VsBase: [2]int64{lo - y.Base, hi - y.Base}, VsTotal: [2]int64{lo - y.Total, hi - y.Total}}
	switch {
	case lo >= y.Total:
		p.Verdict = PayAbove
	case hi >= y.Total:
		p.Verdict = PayOverlaps
	default:
		p.Verdict = PayBelow
	}
	return p
}
