package api

import (
	"context"
	"time"

	"github.com/njeske/knobd/internal/model"
)

// LearnRequest is POST /learn's optional body.
type LearnRequest struct {
	// TimeoutMs is how long to arm learn for; 0 (or an omitted body)
	// means the engine's own default. A value the engine considers too
	// long is clamped, not rejected -- LearnState.ExpiresAt reports the
	// value actually honored.
	TimeoutMs int `json:"timeoutMs,omitempty"`
}

// LearnState is both POST /learn's response and the Learn field
// embedded in State: a reconnecting UI (or a fresh GET /state poll)
// learns learn's current status for free, with no separate GET /learn.
type LearnState struct {
	Active bool `json:"active"`
	// ExpiresAt is nil when Active is false.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// LearnInput is one control touched while learn was armed, pushed as a
// learn_input event over GET /events (see events.go). SuggestedGesture
// is derived statelessly from the raw device.EventKind that captured it
// (turn/fader-move/button-down -> turn/move/press) -- learn deliberately
// does not run the gesture machine (see engine/learn.go's doc comment),
// so this is a suggestion for the UI's gesture picker to seed, not a
// claim about what the user actually meant.
type LearnInput struct {
	Control          model.Control `json:"control"`
	SuggestedGesture model.Gesture `json:"suggestedGesture"`
	// Delta is the signed detent count when SuggestedGesture is "turn";
	// 0 otherwise.
	Delta int `json:"delta,omitempty"`
	// Value is the absolute 0-127 position when SuggestedGesture is
	// "move"; 0 otherwise.
	Value int       `json:"value,omitempty"`
	At    time.Time `json:"at"`
}

// LearnController is how the API arms and disarms MIDI learn.
// Implementations should clamp an out-of-range requested timeout rather
// than reject it, and report what was actually honored in the returned
// LearnState.
type LearnController interface {
	StartLearn(ctx context.Context, timeout time.Duration) (LearnState, error)
	StopLearn(ctx context.Context) error
}
