// Package agent holds process-wide runtime state.
package agent

import (
	"sync/atomic"
	"time"
)

// State's Paused flag is the kill switch: schedulers and outbound LLM calls check it.
type State struct {
	StartedAt time.Time
	paused    atomic.Bool
}

func NewState(now time.Time) *State { return &State{StartedAt: now} }

func (s *State) Paused() bool { return s.paused.Load() }
func (s *State) Pause()       { s.paused.Store(true) }
func (s *State) Resume()      { s.paused.Store(false) }
