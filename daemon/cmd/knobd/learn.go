package main

import (
	"context"
	"time"

	"github.com/njeske/knobd/internal/api"
	"github.com/njeske/knobd/internal/engine"
)

// learnEngine is the slice of *engine.Engine learnController needs -- a
// point-of-use interface so it's testable without a real engine.
type learnEngine interface {
	SetLearnUntil(ctx context.Context, deadline time.Time) error
}

// learnController is cmd/knobd's api.LearnController adapter. Clamping
// lives here (rather than in api's handler) so it holds for every
// caller, and the honored value is what StartLearn reports back.
type learnController struct {
	eng learnEngine
	// now is time.Now by default; tests override it to avoid a real
	// sleep.
	now func() time.Time
}

func newLearnController(eng learnEngine) *learnController {
	return &learnController{eng: eng, now: time.Now}
}

// StartLearn implements api.LearnController.
func (l *learnController) StartLearn(ctx context.Context, timeout time.Duration) (api.LearnState, error) {
	switch {
	case timeout <= 0:
		timeout = engine.DefaultLearnTimeout
	case timeout > engine.MaxLearnTimeout:
		timeout = engine.MaxLearnTimeout
	}
	deadline := l.now().Add(timeout)
	if err := l.eng.SetLearnUntil(ctx, deadline); err != nil {
		return api.LearnState{}, err
	}
	return api.LearnState{Active: true, ExpiresAt: &deadline}, nil
}

// StopLearn implements api.LearnController.
func (l *learnController) StopLearn(ctx context.Context) error {
	return l.eng.SetLearnUntil(ctx, time.Time{})
}
