package radar

import (
	"errors"
	"testing"
)

func TestPastedPostingIsRescoredAndKeptWhenTheAlertComesAgain(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.source.items["acme"] = []FetchedItem{fake("1", "Engineer")}
	h.scan()
	h.score()
	id := h.rows()[0].ID

	h.scorer.set("Engineer", 88, "software")
	posting := "Build the platform.\nThe base pay range is $160,000 - $200,000."
	if err := h.scoring.Paste(h.ctx, id, "  "+posting+"\n"); err != nil {
		t.Fatal(err)
	}
	r := h.rows()[0]
	if r.Description != posting || r.FitScore.Int64 != 88 || r.CompMax.Float64 != 200_000 {
		t.Fatalf("after paste: %+v", r)
	}
	if last := h.scorer.texts[len(h.scorer.texts)-1]; last != posting {
		t.Fatalf("scored on %q", last)
	}

	// The alert reports the same job again with its snippet: the pasted text stays, and nothing is re-scored.
	again := fake("1", "Engineer")
	again.Description = "Snippet only"
	h.source.items["acme"] = []FetchedItem{again}
	h.scan()
	calls := len(h.scorer.calls)
	h.score()
	if r := h.rows()[0]; r.Description != posting || len(h.scorer.calls) != calls {
		t.Fatalf("pasted text replaced or re-scored: %+v, calls %d → %d", r, calls, len(h.scorer.calls))
	}
}

func TestPasteKeepsTheAlertsPayWhenThePostingListsNone(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	pay := 150_000.0
	it := fake("1", "Engineer")
	it.CompMax = &pay
	h.source.items["acme"] = []FetchedItem{it}
	h.scan()
	if err := h.scoring.Paste(h.ctx, h.rows()[0].ID, "No pay listed here."); err != nil {
		t.Fatal(err)
	}
	if r := h.rows()[0]; r.CompMax.Float64 != 150_000 || !r.FitScore.Valid {
		t.Fatalf("%+v", r)
	}
}

func TestPasteRespectsThePauseAndMissingItems(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	if err := h.scoring.Paste(h.ctx, 99, "text"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing item: %v", err)
	}
	h.state.Pause()
	if err := h.scoring.Paste(h.ctx, 99, "text"); !errors.Is(err, ErrPaused) {
		t.Fatalf("paused: %v", err)
	}
}
