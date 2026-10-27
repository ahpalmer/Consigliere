package radar

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/ahpalmer/consigliere/internal/channel"
	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/store"
)

// Alerts sends strong matches right away, one message each with feedback buttons: anything in an "instant" tier,
// plus high-priority companies in any tier.
type Alerts struct {
	DB       *sql.DB
	Ranking  RankingFactory
	Channel  channel.Channel
	Clock    clock.Clock
	MinScore int
}

func (a *Alerts) Send(ctx context.Context) (int, error) {
	ranking, err := a.Ranking.Create()
	if err != nil {
		return 0, err
	}
	instant := map[string]bool{}
	for _, t := range ranking.Prefs.Tiers {
		if strings.EqualFold(t.Alert, "instant") {
			instant[strings.ToLower(t.Name)] = true
		}
	}
	now := a.Clock.Now()
	stale, args := staleClause(ranking.StaleBefore(now))
	// Company priority needs name normalization, so the tier/priority test runs in memory on the (small) unsent set.
	candidates, err := scanItemViews(a.DB.QueryContext(ctx, `SELECT `+itemViewColumns+` FROM "RadarItems" WHERE "Passed" = 1 AND "DuplicateOfId" IS NULL
		AND "FitScore" IS NOT NULL AND "AlertedAt" IS NULL AND "DigestedAt" IS NULL`+stale, args...))
	if err != nil {
		return 0, err
	}
	var alertable []ItemView
	for _, i := range candidates {
		if (instant[strings.ToLower(i.Tier)] || ranking.PriorityOf(i) == PriorityHigh) && ranking.RankScore(i) >= a.MinScore {
			alertable = append(alertable, i)
		}
	}
	ranking.Sort(alertable)

	stamp := store.FormatTime(now)
	for _, item := range alertable {
		marker := "🔔 "
		if ranking.PriorityOf(item) == PriorityHigh {
			marker = "★ "
		}
		if err := a.Channel.Notify(ctx, marker+Detail(item, now), FeedbackButtons(item.ID)...); err != nil {
			return 0, err
		}
		// Alerted items count as digested so they don't show up again in the next digest.
		if _, err := a.DB.ExecContext(ctx, `UPDATE "RadarItems" SET "AlertedAt" = ?, "DigestedAt" = ? WHERE "Id" = ?`, stamp, stamp, item.ID); err != nil {
			return 0, err
		}
	}
	return len(alertable), nil
}

// Digest formats scored items that haven't been sent yet, best first (priority companies → tier → category →
// adjusted fit), then marks them sent. Unscored, stale, and low-ranking items are held back.
type Digest struct {
	DB       *sql.DB
	Ranking  RankingFactory
	Clock    clock.Clock
	MaxItems int
	MinScore int
}

type DigestResult struct {
	Text             string // "" when there's nothing to send
	Shown, Remaining int
}

func (d *Digest) Next(ctx context.Context) (DigestResult, error) {
	ranking, err := d.Ranking.Create()
	if err != nil {
		return DigestResult{}, err
	}
	now := d.Clock.Now()
	stale, args := staleClause(ranking.StaleBefore(now))
	unsent := `"Passed" = 1 AND "DuplicateOfId" IS NULL AND "DigestedAt" IS NULL` + stale

	var awaiting int
	if err := d.DB.QueryRowContext(ctx, `SELECT count(*) FROM "RadarItems" WHERE `+unsent+` AND "FitScore" IS NULL AND "ScoredAt" IS NULL`, args...).Scan(&awaiting); err != nil {
		return DigestResult{}, err
	}
	scored, err := scanItemViews(d.DB.QueryContext(ctx, `SELECT `+itemViewColumns+` FROM "RadarItems" WHERE `+unsent+` AND "FitScore" IS NOT NULL`, args...))
	if err != nil {
		return DigestResult{}, err
	}
	var pending []ItemView
	for _, i := range scored {
		if ranking.RankScore(i) >= d.MinScore {
			pending = append(pending, i)
		}
	}
	ranking.Sort(pending)
	if len(pending) == 0 {
		if awaiting > 0 {
			return DigestResult{Text: fmt.Sprintf("Nothing scored yet; %d item(s) still being scored.", awaiting)}, nil
		}
		return DigestResult{}, nil
	}

	shown := pending[:min(len(pending), max(d.MaxItems, 1))]
	remaining := len(pending) - len(shown)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Radar: %d of %d new\n", len(shown), len(pending))
	for start := 0; start < len(shown); {
		key := groupKey(ranking, shown[start])
		end := start
		for end < len(shown) && groupKey(ranking, shown[end]) == key {
			end++
		}
		fmt.Fprintf(&sb, "\n— %s (%d) —\n", key, end-start)
		for _, item := range shown[start:end] {
			sb.WriteString(Line(item, now) + "\n")
		}
		start = end
	}
	sb.WriteString("\n")
	if remaining > 0 {
		fmt.Fprintf(&sb, "%d more — send /digest again.\n", remaining)
	}
	if awaiting > 0 {
		fmt.Fprintf(&sb, "%d still being scored.\n", awaiting)
	}
	sb.WriteString("/item <id> for details · /good or /bad <id> to rate")

	ids := make([]any, len(shown))
	for i, item := range shown {
		ids[i] = item.ID
	}
	if _, err := d.DB.ExecContext(ctx, `UPDATE "RadarItems" SET "DigestedAt" = ? WHERE "Id" IN (`+store.Placeholders(len(ids))+`)`,
		append([]any{store.FormatTime(now)}, ids...)...); err != nil {
		return DigestResult{}, err
	}
	return DigestResult{Text: sb.String(), Shown: len(shown), Remaining: remaining}, nil
}

func groupKey(r *Ranking, i ItemView) string {
	key := orDefault(i.Tier, "untiered") + " · " + orDefault(i.Category, "uncategorized")
	if r.PriorityOf(i) == PriorityHigh {
		key = "★ priority · " + key
	}
	return key
}

// Reset marks every sent item as unsent, so the next digest starts again from the top.
func (d *Digest) Reset(ctx context.Context) (int64, error) {
	res, err := d.DB.ExecContext(ctx, `UPDATE "RadarItems" SET "DigestedAt" = NULL WHERE "Passed" = 1 AND "DuplicateOfId" IS NULL AND "DigestedAt" IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

const feedbackPrefix = "rf:"

// FeedbackButtons are the 👍/👎 buttons under an item.
func FeedbackButtons(id int64) []channel.Button {
	return []channel.Button{
		{Label: "👍 Good", Data: fmt.Sprintf("%s+:%d", feedbackPrefix, id)},
		{Label: "👎 Bad", Data: fmt.Sprintf("%s-:%d", feedbackPrefix, id)},
	}
}

// Feedback stores 👍/👎 on items, from buttons or /good and /bad, for rubric tuning.
type Feedback struct {
	DB    *sql.DB
	Clock clock.Clock
}

func (f *Feedback) Prefix() string { return feedbackPrefix }

func (f *Feedback) HandleCallback(ctx context.Context, data string) (string, error) {
	// rf:+:123 / rf:-:123
	parts := strings.Split(data, ":")
	if len(parts) != 3 {
		return "Unrecognized button.", nil
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "Unrecognized button.", nil
	}
	value := -1
	if parts[1] == "+" {
		value = 1
	}
	n, err := f.Set(ctx, []int64{id}, value)
	if err != nil {
		return "", err
	}
	if n != 1 {
		return fmt.Sprintf("Item #%d not found.", id), nil
	}
	if value > 0 {
		return "Noted 👍", nil
	}
	return "Noted 👎", nil
}

// Set records value (+1/-1) on the items and returns how many were updated.
func (f *Feedback) Set(ctx context.Context, ids []int64, value int) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := f.DB.ExecContext(ctx, `UPDATE "RadarItems" SET "Feedback" = ?, "FeedbackAt" = ? WHERE "Id" IN (`+store.Placeholders(len(ids))+`)`,
		append([]any{value, store.FormatTime(f.Clock.Now())}, store.Args(ids)...)...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
