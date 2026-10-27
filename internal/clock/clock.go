// Package clock abstracts time so tests can control it.
package clock

import (
	"sync"
	"time"
)

type Clock interface{ Now() time.Time }

type Real struct{}

func (Real) Now() time.Time { return time.Now().UTC() }

// Fake is a settable clock for tests.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

func NewFake(now time.Time) *Fake { return &Fake{now: now.UTC()} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}
