// Package scheduler runs recurring jobs on cron schedules inside the host process.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/ahpalmer/consigliere/internal/agent"
	"github.com/ahpalmer/consigliere/internal/channel"
)

// Job is recurring work. It's skipped while the agent is paused.
type Job interface {
	Name() string
	// Schedule is a standard 5-field cron expression, evaluated in the scheduler's time zone.
	Schedule() string
	Run(ctx context.Context) error
}

// Scheduler runs each job on its schedule. A job that starts failing alerts the owner once until it recovers.
// With a healthchecks ping URL set, every run is also reported externally: the dead-man's switch for a dead host.
type Scheduler struct {
	Jobs     []Job
	State    *agent.State
	Channel  channel.Channel
	Location *time.Location
	PingURL  string
	HTTP     *http.Client
}

func (s *Scheduler) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	for _, job := range s.Jobs {
		schedule, err := cron.ParseStandard(job.Schedule())
		if err != nil {
			return fmt.Errorf("job %s: bad schedule %q: %w", job.Name(), job.Schedule(), err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.loop(ctx, job, schedule)
		}()
	}
	wg.Wait()
	return nil
}

func (s *Scheduler) loop(ctx context.Context, job Job, schedule cron.Schedule) {
	failing := false
	for {
		next := schedule.Next(time.Now().In(s.Location))
		slog.Info("next run", "job", job.Name(), "at", next.Format("2006-01-02 15:04 MST"))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
		}
		if s.State.Paused() {
			slog.Info("skipping job: agent is paused", "job", job.Name())
			continue
		}
		if err := job.Run(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("scheduled job failed", "job", job.Name(), "err", err)
			s.ping(ctx, job.Name(), false)
			if !failing {
				s.notify(ctx, fmt.Sprintf("⚠ %s failed: %v", job.Name(), err))
			}
			failing = true
			continue
		}
		s.ping(ctx, job.Name(), true)
		if failing {
			s.notify(ctx, fmt.Sprintf("✅ %s recovered.", job.Name()))
		}
		failing = false
	}
}

func (s *Scheduler) notify(ctx context.Context, text string) {
	if err := s.Channel.Notify(ctx, text); err != nil {
		slog.Warn("failure alert not delivered", "err", err)
	}
}

// ping reports to healthchecks.io: "{base}/{job}" on success, ".../fail" on failure. Monitoring never breaks a job.
func (s *Scheduler) ping(ctx context.Context, jobName string, success bool) {
	if s.PingURL == "" {
		return
	}
	url := strings.TrimRight(s.PingURL, "/") + "/" + jobName
	if !success {
		url += "/fail"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"?create=1", nil)
	if err != nil {
		return
	}
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("healthchecks ping failed", "job", jobName, "err", err)
		return
	}
	resp.Body.Close()
}
