package radar

import (
	"regexp"
	"sort"
	"strings"
	"time"
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

// Ranking is the owner-configured ordering: high-priority companies first, then location tier, then category
// (both in preferences order), then fit adjusted by priority. Priority nudges rather than hides, so a great
// low-priority item still surfaces.
type Ranking struct {
	Prefs            Preferences
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

// RankScore is fit plus priority adjustment; what thresholds and ordering compare against.
func (r *Ranking) RankScore(item ItemView) int {
	score := 0
	if item.FitScore != nil {
		score = *item.FitScore
	}
	switch r.PriorityOf(item) {
	case PriorityLow:
		score += LowPriorityAdjustment
	case PriorityHigh:
		score += HighPriorityAdjustment
	}
	return score
}

// Sort orders items best first.
func (r *Ranking) Sort(items []ItemView) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if ka, kb := r.PriorityOf(a) == PriorityHigh, r.PriorityOf(b) == PriorityHigh; ka != kb {
			return ka
		}
		if ta, tb := r.TierRank(a.Tier), r.TierRank(b.Tier); ta != tb {
			return ta < tb
		}
		if ca, cb := r.CategoryRank(a.Category), r.CategoryRank(b.Category); ca != cb {
			return ca < cb
		}
		if sa, sb := r.RankScore(a), r.RankScore(b); sa != sb {
			return sa > sb
		}
		return recency(a).After(recency(b))
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

// RankingFactory reads config fresh each time so edits to priorities and categories apply immediately.
type RankingFactory struct{ Config ConfigLoader }

func (f RankingFactory) Create() (*Ranking, error) {
	prefs, err := f.Config.Preferences()
	if err != nil {
		return nil, err
	}
	watchlist, err := f.Config.Watchlist()
	if err != nil {
		return nil, err
	}
	return NewRanking(prefs, watchlist), nil
}
