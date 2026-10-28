package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Tasks runs fire-and-forget work (background scans, scoring) under the app's lifetime, so shutdown can cancel it
// and wait for it to finish before closing shared resources like the database.
type Tasks struct {
	ctx context.Context
	wg  sync.WaitGroup
}

func NewTasks(ctx context.Context) *Tasks { return &Tasks{ctx: ctx} }

// Go runs f in the background with the app context, logging any error.
func (t *Tasks) Go(name string, f func(ctx context.Context) error) {
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		if err := f(t.ctx); err != nil && t.ctx.Err() == nil {
			slog.Error("background task failed", "task", name, "err", err)
		}
	}()
}

// Wait blocks until all tasks finish or the timeout passes, and reports whether they all finished.
func (t *Tasks) Wait(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() { t.wg.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
