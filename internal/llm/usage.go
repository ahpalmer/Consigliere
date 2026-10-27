package llm

import (
	"sort"
	"sync"
)

// ModelUsage is token accounting for one model since process start.
type ModelUsage struct {
	Model                                          string
	Calls, InputTokens, OutputTokens, CachedTokens int64
}

// UsageTracker accumulates usage per model. Persisted to the DB in a later milestone.
type UsageTracker struct {
	mu      sync.Mutex
	byModel map[string]*ModelUsage
}

func NewUsageTracker() *UsageTracker { return &UsageTracker{byModel: map[string]*ModelUsage{}} }

func (u *UsageTracker) Record(model string, input, output, cached int64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	m, ok := u.byModel[model]
	if !ok {
		m = &ModelUsage{Model: model}
		u.byModel[model] = m
	}
	m.Calls++
	m.InputTokens += input
	m.OutputTokens += output
	m.CachedTokens += cached
}

func (u *UsageTracker) Snapshot() []ModelUsage {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]ModelUsage, 0, len(u.byModel))
	for _, m := range u.byModel {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}
