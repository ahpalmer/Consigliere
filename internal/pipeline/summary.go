package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/channel"
	"github.com/ahpalmer/consigliere/internal/store"
)

// MonthlySummary looks back at last month's pipeline (what was started, applied to, interviewed, closed) and at
// what's worked over all time: how often applications with outreach reached an interview, versus without.
type MonthlySummary struct {
	Pipeline *Pipeline
	Channel  channel.Channel
	Cron     string
}

func (m *MonthlySummary) Name() string     { return "monthly-summary" }
func (m *MonthlySummary) Schedule() string { return m.Cron }

func (m *MonthlySummary) Run(ctx context.Context) error {
	text, err := m.Compose(ctx)
	if err != nil || text == "" {
		return err
	}
	return m.Channel.Notify(ctx, text)
}

// Compose summarizes the calendar month before now, in the owner's time zone; "" when nothing happened.
func (m *MonthlySummary) Compose(ctx context.Context) (string, error) {
	now := m.Pipeline.Clock.Now().In(m.Pipeline.Location)
	end := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, m.Pipeline.Location)
	start := end.AddDate(0, -1, 0)
	rows, err := m.Pipeline.DB.QueryContext(ctx, `SELECT "Kind", coalesce("Detail", '') FROM "PursuitEvents" WHERE "At" >= ? AND "At" < ?
		AND "Kind" IN ('activated', 'applied', 'interview', 'completed')`, store.FormatTime(start), store.FormatTime(end))
	if err != nil {
		return "", err
	}
	counts, outcomes := map[string]int{}, map[string]int{}
	for rows.Next() {
		var kind, detail string
		if err := rows.Scan(&kind, &detail); err != nil {
			rows.Close()
			return "", err
		}
		counts[kind]++
		if kind == "completed" {
			outcome, _, _ := strings.Cut(detail, " ")
			outcomes[outcome]++
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(counts) == 0 {
		return "", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "🗓 %s in the pipeline: %d started, %d applied, %d %s.", start.Format("January"), counts["activated"], counts["applied"],
		counts["interview"], plural(counts["interview"], "interview", "interviews"))
	if counts["completed"] > 0 {
		var parts []string
		for _, o := range Outcomes {
			if n := outcomes[o]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, o))
			}
		}
		fmt.Fprintf(&sb, "\nClosed: %s.", strings.Join(parts, ", "))
	}

	// All time, since one month is rarely enough to compare.
	var withOutreach, withInterview, without, withoutInterview int
	err = m.Pipeline.DB.QueryRowContext(ctx, `SELECT
		count(*) FILTER (WHERE o."PursuitId" IS NOT NULL),
		count(*) FILTER (WHERE o."PursuitId" IS NOT NULL AND (p."InterviewAt" IS NOT NULL OR p."StageReached" IN ('interview', 'offer'))),
		count(*) FILTER (WHERE o."PursuitId" IS NULL),
		count(*) FILTER (WHERE o."PursuitId" IS NULL AND (p."InterviewAt" IS NOT NULL OR p."StageReached" IN ('interview', 'offer')))
		FROM "Pursuits" p LEFT JOIN "PursuitSteps" o ON o."PursuitId" = p."Id" AND o."Step" = 'outreach'
		WHERE p."AppliedAt" IS NOT NULL`).Scan(&withOutreach, &withInterview, &without, &withoutInterview)
	if err != nil {
		return "", err
	}
	if withOutreach+without > 0 {
		fmt.Fprintf(&sb, "\nAll time, applications reaching an interview: %s with outreach, %s without.",
			rate(withInterview, withOutreach), rate(withoutInterview, without))
	}
	return sb.String(), nil
}

func rate(n, of int) string {
	if of == 0 {
		return "none yet"
	}
	return fmt.Sprintf("%d of %d (%d%%)", n, of, n*100/of)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
