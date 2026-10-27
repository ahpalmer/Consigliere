package radar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ahpalmer/consigliere/internal/agent"
	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/store"
)

type ScoringSummary struct{ Scored, Cached, Declined, Failed, Remaining int }

// ScoringRunner scores items that passed the rule filter and whose score is missing or stale (rubric/profile/model
// changed). Fresh-enough items only, highest-ranked tiers first, a batch at a time. Then it sends instant alerts.
type ScoringRunner struct {
	DB          *sql.DB
	Scorer      Scorer
	Ranking     RankingFactory
	Alerts      *Alerts
	State       *agent.State
	Clock       clock.Clock
	BatchSize   int
	Concurrency int

	running atomic.Bool
}

func (r *ScoringRunner) IsRunning() bool { return r.running.Load() }

// CountPending is how many items still need a score under the current rubric/profile.
func (r *ScoringRunner) CountPending(ctx context.Context) (int, error) {
	sc, err := r.Scorer.BuildContext()
	if err != nil {
		return 0, err
	}
	ranking, err := r.Ranking.Create()
	if err != nil {
		return 0, err
	}
	where, args := pendingWhere(sc.Key(), ranking.StaleBefore(r.Clock.Now()))
	var n int
	err = r.DB.QueryRowContext(ctx, `SELECT count(*) FROM "RadarItems" WHERE `+where, args...).Scan(&n)
	return n, err
}

// RunInBackground is fire-and-forget for chat commands, so Telegram isn't blocked while a batch scores.
func (r *ScoringRunner) RunInBackground() {
	go func() {
		if _, _, err := r.Run(context.Background()); err != nil {
			slog.Error("background scoring failed", "err", err)
		}
	}()
}

// Run returns ok=false when a run is already in progress.
func (r *ScoringRunner) Run(ctx context.Context) (ScoringSummary, bool, error) {
	if !r.running.CompareAndSwap(false, true) {
		return ScoringSummary{}, false, nil
	}
	defer r.running.Store(false)
	summary, err := r.scoreBatch(ctx)
	if err != nil {
		return summary, true, err
	}
	if _, err := r.Alerts.Send(ctx); err != nil {
		return summary, true, fmt.Errorf("alerts: %w", err)
	}
	return summary, true, nil
}

func pendingWhere(key string, staleBefore *time.Time) (string, []any) {
	stale, staleArgs := staleClause(staleBefore)
	return `"Passed" = 1 AND "DuplicateOfId" IS NULL AND ("ScoringKey" IS NULL OR "ScoringKey" <> ?)` + stale, append([]any{key}, staleArgs...)
}

type candidate struct {
	id          int64
	contentHash string
	tier        string
	recency     time.Time
}

func (r *ScoringRunner) scoreBatch(ctx context.Context) (ScoringSummary, error) {
	sc, err := r.Scorer.BuildContext()
	if err != nil {
		return ScoringSummary{}, err
	}
	key := sc.Key()
	ranking, err := r.Ranking.Create()
	if err != nil {
		return ScoringSummary{}, err
	}
	where, args := pendingWhere(key, ranking.StaleBefore(r.Clock.Now()))
	rows, err := r.DB.QueryContext(ctx, `SELECT "Id", "ContentHash", "Tier", coalesce("PostedAt", "FirstSeenAt") FROM "RadarItems" WHERE `+where, args...)
	if err != nil {
		return ScoringSummary{}, err
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		var tier sql.NullString
		var recency string
		if err := rows.Scan(&c.id, &c.contentHash, &tier, &recency); err != nil {
			rows.Close()
			return ScoringSummary{}, err
		}
		c.tier = tier.String
		if c.recency, err = store.ParseTime(recency); err != nil {
			rows.Close()
			return ScoringSummary{}, err
		}
		candidates = append(candidates, c)
	}
	rows.Close()

	// Best tiers first so the useful stuff is ready soonest (new items haven't been categorized yet).
	sort.SliceStable(candidates, func(i, j int) bool {
		if a, b := ranking.TierRank(candidates[i].tier), ranking.TierRank(candidates[j].tier); a != b {
			return a < b
		}
		return candidates[i].recency.After(candidates[j].recency)
	})
	batch := candidates[:min(len(candidates), max(r.BatchSize, 1))]
	if len(batch) == 0 {
		return ScoringSummary{}, nil
	}

	// Identical content scores once: the first item in each group calls the model, the rest hit the cache.
	var order []string
	groups := map[string][]int64{}
	for _, c := range batch {
		if _, ok := groups[c.contentHash]; !ok {
			order = append(order, c.contentHash)
		}
		groups[c.contentHash] = append(groups[c.contentHash], c.id)
	}

	var scored, cached, declined, failed atomic.Int32
	sem := make(chan struct{}, max(r.Concurrency, 1))
	var wg sync.WaitGroup
	for _, hash := range order {
		ids := groups[hash]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			for _, id := range ids {
				if r.State.Paused() || ctx.Err() != nil {
					return // kill switch stops spend mid-batch
				}
				outcome, err := r.scoreOne(ctx, id, sc, key)
				switch {
				case err != nil:
					failed.Add(1)
					slog.Warn("scoring failed", "item", id, "err", err)
				case outcome == outcomeScored:
					scored.Add(1)
				case outcome == outcomeCached:
					cached.Add(1)
				default:
					declined.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	s := ScoringSummary{Scored: int(scored.Load()), Cached: int(cached.Load()), Declined: int(declined.Load()), Failed: int(failed.Load())}
	s.Remaining = len(candidates) - s.Scored - s.Cached - s.Declined
	slog.Info("scoring", "scored", s.Scored, "cached", s.Cached, "declined", s.Declined, "failed", s.Failed, "remaining", s.Remaining)
	if s.Failed > 0 && s.Scored+s.Cached+s.Declined == 0 {
		return s, fmt.Errorf("all %d scoring attempts failed; see logs", s.Failed)
	}
	return s, nil
}

type scoreOutcome int

const (
	outcomeScored scoreOutcome = iota
	outcomeCached
	outcomeDeclined
)

func (r *ScoringRunner) scoreOne(ctx context.Context, id int64, sc ScoringContext, key string) (scoreOutcome, error) {
	var (
		input            ScoreInput
		contentHash      string
		tier             sql.NullString
		compMin, compMax sql.NullFloat64
		posted           sql.NullString
	)
	err := r.DB.QueryRowContext(ctx, `SELECT "Company", "Title", "Locations", "Tier", "CompMin", "CompMax", "PostedAt", "Description", "ContentHash"
		FROM "RadarItems" WHERE "Id" = ?`, id).Scan(&input.Company, &input.Title, &input.Locations, &tier, &compMin, &compMax, &posted, &input.Description, &contentHash)
	if err != nil {
		return 0, err
	}
	input.Tier, input.CompMin, input.CompMax = tier.String, floatPtr(compMin), floatPtr(compMax)
	if input.PostedAt, err = store.ParseTimePtr(posted); err != nil {
		return 0, err
	}
	now := store.FormatTime(r.Clock.Now())

	// Same content already scored under the same rubric/profile (e.g. a re-post, or the same text on two boards).
	var hit struct {
		fit                              int
		category, summary, reasons, gaps sql.NullString
	}
	err = r.DB.QueryRowContext(ctx, `SELECT "FitScore", "Category", "FitSummary", "FitReasons", "FitGaps" FROM "RadarItems"
		WHERE "Id" <> ? AND "ContentHash" = ? AND "ScoringKey" = ? AND "FitScore" IS NOT NULL LIMIT 1`, id, contentHash, key).
		Scan(&hit.fit, &hit.category, &hit.summary, &hit.reasons, &hit.gaps)
	if err == nil {
		_, err = r.DB.ExecContext(ctx, `UPDATE "RadarItems" SET "FitScore" = ?, "Category" = ?, "FitSummary" = ?, "FitReasons" = ?, "FitGaps" = ?,
			"ScoringKey" = ?, "ScoredAt" = ? WHERE "Id" = ?`, hit.fit, hit.category, hit.summary, hit.reasons, hit.gaps, key, now, id)
		return outcomeCached, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	result, err := r.Scorer.Score(ctx, input, sc)
	if err != nil {
		return 0, err
	}
	// A declined item keeps a null score (so it never digests) but records the key so it isn't retried every run.
	if result == nil {
		_, err = r.DB.ExecContext(ctx, `UPDATE "RadarItems" SET "FitScore" = NULL, "Category" = NULL, "FitSummary" = 'Declined to score.',
			"FitReasons" = NULL, "FitGaps" = NULL, "ScoringKey" = ?, "ScoredAt" = ? WHERE "Id" = ?`, key, now, id)
		return outcomeDeclined, err
	}
	_, err = r.DB.ExecContext(ctx, `UPDATE "RadarItems" SET "FitScore" = ?, "Category" = ?, "FitSummary" = ?, "FitReasons" = ?, "FitGaps" = ?,
		"ScoringKey" = ?, "ScoredAt" = ? WHERE "Id" = ?`,
		result.Fit, result.Category, result.Summary, strings.Join(result.Reasons, "\n"), strings.Join(result.Gaps, "\n"), key, now, id)
	return outcomeScored, err
}
