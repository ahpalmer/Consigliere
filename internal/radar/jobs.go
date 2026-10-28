package radar

import (
	"context"
	"fmt"
	"log/slog"
)

// ScanJob polls every source, then scores right away so instant alerts go out within the hour.
type ScanJob struct {
	Scanner *Scanner
	Scoring *ScoringRunner
	Cron    string
}

func (j *ScanJob) Name() string     { return "radar-scan" }
func (j *ScanJob) Schedule() string { return j.Cron }

func (j *ScanJob) Run(ctx context.Context) error {
	summary, ok, err := j.Scanner.Scan(ctx)
	if !ok {
		slog.Info("skipping scheduled scan; one is already running")
		return nil
	}
	if err != nil {
		return err
	}
	// Surface total outages to the scheduler's failure alert; single-source errors show in /sources.
	failed := 0
	for _, s := range summary.Sources {
		if s.Error != "" {
			failed++
		}
	}
	if failed > 0 && failed == len(summary.Sources) {
		return fmt.Errorf("all %d sources failed; first error: %s", failed, summary.Sources[0].Error)
	}
	_, _, err = j.Scoring.Run(ctx)
	return err
}

// ScoreJob works through any scoring backlog (e.g. after a profile edit makes every score stale).
type ScoreJob struct {
	Scoring *ScoringRunner
	Cron    string
}

func (j *ScoreJob) Name() string     { return "radar-score" }
func (j *ScoreJob) Schedule() string { return j.Cron }

func (j *ScoreJob) Run(ctx context.Context) error {
	_, _, err := j.Scoring.Run(ctx)
	return err
}
