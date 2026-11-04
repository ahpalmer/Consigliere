package radar

import (
	"strings"
	"testing"
	"time"

	"github.com/ahpalmer/consigliere/internal/store"
)

func TestTuningSummaryShowsWhereScoresAndFeedbackDisagree(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	now := store.FormatTime(h.clock.Now())
	for i, row := range []struct {
		title    string
		fit      int
		feedback int
		at       string
	}{
		{"Sales Engineer", 82, -1, now},   // overrated
		{"Robotics Engineer", 55, 1, now}, // underrated
		{"Platform Engineer", 90, 1, now}, // agreed
		{"Old Miss", 85, -1, store.FormatTime(h.clock.Now().AddDate(0, 0, -10))},
	} {
		if _, err := h.db.Exec(`INSERT INTO "RadarItems" ("Source", "Board", "ExternalId", "Company", "Title", "Locations", "Url", "Description",
			"FirstSeenAt", "LastSeenAt", "ContentHash", "Fingerprint", "Passed", "FitScore", "Feedback", "FeedbackAt")
			VALUES ('greenhouse', 'acme', ?, 'Acme', ?, '', '', '', ?, ?, 'h', 'f', 1, ?, ?, ?)`, i, row.title, now, now, row.fit, row.feedback, row.at); err != nil {
			t.Fatal(err)
		}
	}
	ts := &TuningSummary{DB: h.db, Channel: h.channel, Clock: h.clock}
	text, err := ts.Compose(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2 👍, 1 👎", "(too generous):\n• #1 Sales Engineer · Acme (scored 82)", "(too strict):\n• #2 Robotics Engineer", PreferencesPath} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Old Miss") || strings.Contains(text, "Platform Engineer") {
		t.Errorf("only this week's disagreements belong:\n%s", text)
	}

	h.clock.Advance(8 * 24 * time.Hour)
	if text, _ := ts.Compose(h.ctx); text != "" {
		t.Fatalf("a week without feedback should say nothing, got %q", text)
	}
}
