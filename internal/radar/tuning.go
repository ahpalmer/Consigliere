package radar

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ahpalmer/consigliere/internal/channel"
	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/store"
)

// TuningSummary is the weekly look at 👍/👎 against the scores: where the scorer and the owner disagree is what
// to fix in the profile or preferences. Sent only in weeks with feedback.
type TuningSummary struct {
	DB      *sql.DB
	Channel channel.Channel
	Clock   clock.Clock
	Cron    string
}

func (t *TuningSummary) Name() string     { return "scoring-feedback-summary" }
func (t *TuningSummary) Schedule() string { return t.Cron }

// Scores at or above this that got 👎 are overrated; below it with 👍, underrated.
const disagreeAt = 70

func (t *TuningSummary) Run(ctx context.Context) error {
	text, err := t.Compose(ctx)
	if err != nil || text == "" {
		return err
	}
	return t.Channel.Notify(ctx, text)
}

func (t *TuningSummary) Compose(ctx context.Context) (string, error) {
	since := t.Clock.Now().AddDate(0, 0, -7)
	rows, err := t.DB.QueryContext(ctx, `SELECT "Id", "Company", "Title", "FitScore", "Feedback" FROM "RadarItems"
		WHERE "Feedback" IS NOT NULL AND "FeedbackAt" >= ? ORDER BY "FeedbackAt"`, store.FormatTime(since))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var good, bad int
	var overrated, underrated []string
	for rows.Next() {
		var (
			id             int64
			company, title string
			fit            sql.NullInt64
			feedback       int
		)
		if err := rows.Scan(&id, &company, &title, &fit, &feedback); err != nil {
			return "", err
		}
		line := fmt.Sprintf("• #%d %s · %s (scored %d)", id, title, company, fit.Int64)
		switch {
		case feedback > 0:
			good++
			if fit.Valid && fit.Int64 < disagreeAt {
				underrated = append(underrated, line)
			}
		default:
			bad++
			if fit.Valid && fit.Int64 >= disagreeAt {
				overrated = append(overrated, line)
			}
		}
	}
	if err := rows.Err(); err != nil || good+bad == 0 {
		return "", err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "📊 Scoring this week: %d 👍, %d 👎.", good, bad)
	writeSome := func(label string, lines []string) {
		if len(lines) == 0 {
			return
		}
		sb.WriteString("\n\n" + label)
		for i, l := range lines {
			if i == 8 {
				fmt.Fprintf(&sb, "\n…and %d more", len(lines)-8)
				break
			}
			sb.WriteString("\n" + l)
		}
	}
	writeSome(fmt.Sprintf("Scored %d+ but you said 👎 (too generous):", disagreeAt), overrated)
	writeSome(fmt.Sprintf("Scored under %d but you said 👍 (too strict):", disagreeAt), underrated)
	if len(overrated)+len(underrated) > 0 {
		sb.WriteString("\n\nTo tune it, say what these have in common in the data pack: " + PreferencesPath +
			" (titles, deal-breakers, categories) or profile/summary.md (what you want next); the scorer reads both.")
	} else {
		sb.WriteString(" Scores and your calls agree.")
	}
	return strings.TrimSpace(sb.String()), nil
}
