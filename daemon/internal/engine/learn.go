package engine

import (
	"context"
	"time"
)

// DefaultLearnTimeout is how long learn mode stays armed with no
// explicit deadline given. Short enough that a UI that crashed or was
// killed mid-learn can't leave the controller's normal dispatch
// suppressed for long; long enough for a person to reach the device and
// touch the control they mean.
const DefaultLearnTimeout = 15 * time.Second

// MaxLearnTimeout caps what SetLearnUntil's caller may request; a
// caller asking for longer gets this instead. daemon/internal/api's
// POST /learn handler is expected to honor and report this, not the
// requested value, when the two differ.
const MaxLearnTimeout = 60 * time.Second

// learnRequest is SetLearnUntil's message to Run, mirroring
// configRequest's request/reply shape.
type learnRequest struct {
	deadline time.Time
	reply    chan error
}

// SetLearnUntil arms (a non-zero deadline) or disarms (the zero
// time.Time) MIDI learn mode. While armed, every decoded device.Event is
// handed to Deps.OnInput and NOT dispatched as an action -- the gesture
// machine never sees it -- so touching a control to identify it in the
// UI cannot also change a volume or fire any other bound action. Learn
// is one-shot: Run disarms it itself on the first captured input (see
// Run's doc comment on the msgs case), so a caller does not need to call
// this again to turn learn back off after a successful capture, only to
// re-arm it for a second learn.
//
// Safe to call concurrently with Run -- it is a channel round trip on
// the run goroutine, exactly like SetConfig, so there is no lock and no
// window where the run loop and the caller disagree about whether learn
// is armed. Returns ctx.Err() if Run isn't consuming (not started yet,
// or already returned) before ctx is done.
func (e *Engine) SetLearnUntil(ctx context.Context, deadline time.Time) error {
	reply := make(chan error, 1)
	select {
	case e.learnCh <- learnRequest{deadline: deadline, reply: reply}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
