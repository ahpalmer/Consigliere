package pipeline

import (
	"strings"
	"testing"
	"time"
)

func TestMonthlySummaryCountsLastMonthAndOutreachConversion(t *testing.T) {
	f := newFixture(t) // Wed Oct 28
	a := f.activate(t, 1)
	b := f.activate(t, 2)
	f.pl.Tick(f.ctx, a.ID, "outreach")
	f.pl.Applied(f.ctx, a.ID)
	f.pl.Applied(f.ctx, b.ID)
	f.pl.Interview(f.ctx, a.ID, time.Date(2026, 10, 30, 14, 0, 0, 0, newYork), "")
	f.pl.Complete(f.ctx, b.ID, "rejected", "")

	m := &MonthlySummary{Pipeline: f.pl, Channel: f.channel}
	f.clock.Advance(4 * 24 * time.Hour) // Sun Nov 1
	text, err := m.Compose(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"October in the pipeline: 2 started, 2 applied, 1 interview.", "Closed: 1 rejected.",
		"1 of 1 (100%) with outreach, 0 of 1 (0%) without"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}

	f.clock.Advance(31 * 24 * time.Hour) // December: November was quiet
	if text, _ := m.Compose(f.ctx); text != "" {
		t.Fatalf("quiet month: %q", text)
	}
}
