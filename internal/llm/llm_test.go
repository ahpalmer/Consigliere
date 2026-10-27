package llm

import (
	"strings"
	"testing"
)

func TestSystemPromptIsEmbeddedAndOwnerIsSubstituted(t *testing.T) {
	p := SystemPrompt("Vito")
	if !strings.Contains(p, "Vito") || strings.Contains(p, "{{owner}}") {
		t.Fatalf("prompt not rendered: %q", p[:min(len(p), 120)])
	}
}

func TestUsageTrackerAccumulatesPerModel(t *testing.T) {
	u := NewUsageTracker()
	u.Record("b", 10, 5, 1)
	u.Record("a", 1, 1, 0)
	u.Record("b", 10, 5, 2)
	s := u.Snapshot()
	if len(s) != 2 || s[0].Model != "a" || s[1].Calls != 2 || s[1].InputTokens != 20 || s[1].CachedTokens != 3 {
		t.Fatalf("%+v", s)
	}
}
