package radar

import (
	"log/slog"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/comp"
)

type Priority int

const (
	PriorityLow Priority = iota
	PriorityNormal
	PriorityHigh
)

const (
	LowPriorityAdjustment  = -15
	HighPriorityAdjustment = 15
)

// Ranking is the owner-configured ordering: high-priority companies first, then fit (the resume match blended with
// location and pay, see FitSettings) adjusted by priority. Priority nudges rather than hides, so a great
// low-priority item still surfaces.
type Ranking struct {
	Prefs Preferences
	// What the owner makes now, for the pay part of fit; nil leaves pay out.
	PayNow           *comp.Yardstick
	sourcePriorities map[[2]string]Priority
	high, low        map[string]bool
}

func NewRanking(prefs Preferences, watchlist Watchlist) *Ranking {
	r := &Ranking{Prefs: prefs, sourcePriorities: map[[2]string]Priority{}, high: map[string]bool{}, low: map[string]bool{}}
	for _, s := range watchlist.Sources {
		key := [2]string{strings.ToLower(s.Ats), strings.ToLower(s.Board)}
		if _, seen := r.sourcePriorities[key]; !seen {
			r.sourcePriorities[key] = parsePriority(s.Priority)
		}
	}
	for _, c := range prefs.CompanyPriority.High {
		r.high[NormalizeCompany(c)] = true
	}
	for _, c := range prefs.CompanyPriority.Low {
		r.low[NormalizeCompany(c)] = true
	}
	return r
}

func parsePriority(p string) Priority {
	switch strings.ToLower(p) {
	case "low":
		return PriorityLow
	case "high":
		return PriorityHigh
	default:
		return PriorityNormal
	}
}

func (r *Ranking) TierRank(tier string) int {
	for i, t := range r.Prefs.Tiers {
		if strings.EqualFold(t.Name, tier) {
			return i
		}
	}
	return 1 << 30
}

func (r *Ranking) CategoryRank(category string) int {
	for i, c := range r.Prefs.Categories {
		if strings.EqualFold(c.Name, category) {
			return i
		}
	}
	return 1 << 30
}

// PriorityOf: company priority (any source) wins over the watchlist source's priority.
func (r *Ranking) PriorityOf(item ItemView) Priority {
	company := NormalizeCompany(item.Company)
	if r.high[company] {
		return PriorityHigh
	}
	if r.low[company] {
		return PriorityLow
	}
	if p, ok := r.sourcePriorities[[2]string{strings.ToLower(item.Source), strings.ToLower(item.Board)}]; ok {
		return p
	}
	return PriorityNormal
}

// FitParts is how an item's fit is put together.
type FitParts struct {
	// The model's resume match (the stored FitScore); nil until scored.
	Resume *int `json:"resume"`
	// By location tier.
	Location int `json:"location"`
	// The listed pay against pay now; nil when there's no current package to compare with.
	Pay *int `json:"pay"`
	// PayListed is false when the posting lists no pay and Pay is the unknown_pay stand-in.
	PayListed bool `json:"payListed"`
	// The weighted blend; 0 until scored.
	Fit int `json:"fit"`
}

func (r *Ranking) Parts(item ItemView) FitParts {
	fs := r.Prefs.Fit
	w := fs.Weights
	if w.Resume == 0 && w.Location == 0 && w.Pay == 0 {
		w = FitWeights{Resume: 60, Location: 20, Pay: 20}
	}
	p := FitParts{Resume: item.FitScore, Location: r.LocationScore(item.Tier)}
	if r.PayNow != nil {
		pay, listed := r.PayNow.PayScore(item.CompMin, item.CompMax)
		if !listed {
			pay = 50
			if fs.UnknownPay != nil {
				pay = *fs.UnknownPay
			}
		}
		p.Pay, p.PayListed = &pay, listed
	} else {
		w.Pay = 0
	}
	if p.Resume == nil || w.Resume+w.Location+w.Pay <= 0 {
		return p
	}
	sum := w.Resume**p.Resume + w.Location*p.Location
	if p.Pay != nil {
		sum += w.Pay * *p.Pay
	}
	p.Fit = int(math.Round(float64(sum) / float64(w.Resume+w.Location+w.Pay)))
	return p
}

// LocationScore is a tier's score: from fit.location, else by the tier's order (100, 75, 50, then 25).
func (r *Ranking) LocationScore(tier string) int {
	for name, score := range r.Prefs.Fit.Location {
		if strings.EqualFold(name, tier) {
			return min(max(score, 0), 100)
		}
	}
	rank := r.TierRank(tier)
	if rank >= len(r.Prefs.Tiers) {
		return 25
	}
	return max(100-25*rank, 25)
}

// RankScore is fit plus priority adjustment; what thresholds and ordering compare against.
func (r *Ranking) RankScore(item ItemView) int {
	score := r.Parts(item).Fit
	switch r.PriorityOf(item) {
	case PriorityLow:
		score += LowPriorityAdjustment
	case PriorityHigh:
		score += HighPriorityAdjustment
	}
	return score
}

// Sort orders items best first: high-priority companies, then fit (location and pay are already in it).
func (r *Ranking) Sort(items []ItemView) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if ka, kb := r.PriorityOf(a) == PriorityHigh, r.PriorityOf(b) == PriorityHigh; ka != kb {
			return ka
		}
		if sa, sb := r.RankScore(a), r.RankScore(b); sa != sb {
			return sa > sb
		}
		return recency(a).After(recency(b))
	})
}

// Group orders already-sorted items into digest sections (priority, then tier, then category, in preferences order),
// keeping the best-first order within each.
func (r *Ranking) Group(items []ItemView) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if ka, kb := r.PriorityOf(a) == PriorityHigh, r.PriorityOf(b) == PriorityHigh; ka != kb {
			return ka
		}
		if ta, tb := r.TierRank(a.Tier), r.TierRank(b.Tier); ta != tb {
			return ta < tb
		}
		return r.CategoryRank(a.Category) < r.CategoryRank(b.Category)
	})
}

func recency(i ItemView) time.Time {
	if i.PostedAt != nil {
		return *i.PostedAt
	}
	return i.FirstSeenAt
}

// StaleBefore: items posted before this are stale. Nil when there's no age limit.
func (r *Ranking) StaleBefore(now time.Time) *time.Time {
	if r.Prefs.MaxPostingAgeDays <= 0 {
		return nil
	}
	t := now.AddDate(0, 0, -r.Prefs.MaxPostingAgeDays)
	return &t
}

var (
	nonWord       = regexp.MustCompile(`[^\p{L}\p{N}]+`)
	legalSuffixes = map[string]bool{"inc": true, "llc": true, "ltd": true, "corp": true, "corporation": true, "co": true, "company": true, "plc": true, "gmbh": true}
)

// NormalizeCompany: "GitHub, Inc." → "github". Matched exactly after this, so "Sierra" doesn't catch
// "Sierra Nevada Corporation".
func NormalizeCompany(name string) string {
	words := strings.Fields(nonWord.ReplaceAllString(strings.ToLower(name), " "))
	for len(words) > 1 && legalSuffixes[words[len(words)-1]] {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

// RankingFactory reads config fresh each time so edits to priorities, categories and the current package apply
// immediately.
type RankingFactory struct {
	Config ConfigLoader
	Clock  clock.Clock // for pay now; the system clock when nil
}

func (f RankingFactory) Create() (*Ranking, error) {
	prefs, err := f.Config.Preferences()
	if err != nil {
		return nil, err
	}
	watchlist, err := f.Config.Watchlist()
	if err != nil {
		return nil, err
	}
	r := NewRanking(prefs, watchlist)
	// A broken current package shouldn't stop the radar: it ranks without pay until it's fixed.
	switch current, ok, err := comp.LoadCurrent(f.Config.Pack); {
	case err != nil:
		slog.Warn("ranking without pay", "err", err)
	case ok:
		now := time.Now()
		if f.Clock != nil {
			now = f.Clock.Now()
		}
		y := comp.NowFrom(current, now)
		r.PayNow = &y
	}
	return r, nil
}
