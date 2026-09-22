package engine

import "time"

// Clock is the timing source Engine uses for gesture detection. The real
// implementation (realClock) wraps the time package directly; tests
// inject a fake so the 600ms hold boundary and the double-press window
// can be exercised deterministically, with no sleeping and no elapsed
// real time.
type Clock interface {
	Now() time.Time
	// NewTimer returns a Timer armed to fire once, d from now. Engine
	// creates exactly one and Resets it every loop iteration to the
	// earliest pending gesture deadline, rather than one timer per held
	// control.
	NewTimer(d time.Duration) Timer
}

// Timer is the subset of time.Timer Clock needs: a channel that fires
// once, and Reset/Stop to rearm or disarm it. Modeled after time.Timer
// so realTimer is a thin wrapper and a fake can drive it without a real
// clock.
type Timer interface {
	C() <-chan time.Time
	Reset(d time.Duration) bool
	Stop() bool
}

// realClock is the production Clock, backed by the time package.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) NewTimer(d time.Duration) Timer {
	return &realTimer{t: time.NewTimer(d)}
}

type realTimer struct{ t *time.Timer }

func (r *realTimer) C() <-chan time.Time        { return r.t.C }
func (r *realTimer) Reset(d time.Duration) bool { return r.t.Reset(d) }
func (r *realTimer) Stop() bool                 { return r.t.Stop() }

var _ Clock = realClock{}
var _ Timer = (*realTimer)(nil)
