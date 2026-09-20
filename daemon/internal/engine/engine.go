// Package engine is knobd's core event loop: it turns decoded
// device.Event values into (Control, Gesture) lookups against the
// active model.Profile's bindings, resolves each binding's model.Target
// against the audio/focus backends, and dispatches the resulting
// model.Action to daemon/internal/actions.
//
// TODO(M04): implement Engine. Key pieces that do not exist yet:
//   - Gesture detection: turning a raw button down/up pair into
//     model.GesturePress vs. model.GestureHold requires a timer (the
//     approved plan's default: <600ms = press, >=600ms = hold) and
//     tracking of the previous press for model.GestureDoublePress.
//   - Layer resolution: which of a profile's layers is "active" right
//     now (see model.ActionLayerMomentary/LayerLatch/LayerCycle,
//     specs/milestones/M08-layers-groups-scenes.md) determines which
//     binding a given (Control, Gesture) resolves to; layer 0 is always
//     the base and higher layers overlay it.
//   - Target resolution happens at dispatch time, not bind time — see
//     model.Target's doc comment for why (streams and focus both change
//     continuously).
//   - The dynamic app pool behavior (an unbound encoder auto-attaching
//     to whatever is newly making sound) lives here too, once M04 scopes
//     it in.
package engine

import (
	"context"

	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/device"
	"github.com/njeske/knobd/internal/focus"
	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
)

// HoldThreshold is the minimum duration a button/encoder-push must be
// held before it produces a GestureHold instead of a GesturePress. Fixed
// per the approved project plan; TODO(M07): consider exposing this as a
// per-user preference if it turns out to need tuning.
const HoldThreshold = 600_000_000 // 600ms, in time.Duration's underlying unit (ns)

// Deps bundles the backends Engine needs. All fields are required; use
// the midi/audio/focus Fake* implementations in tests.
type Deps struct {
	Port   midi.Port
	Codec  device.Codec
	Audio  audio.Backend
	Focus  focus.Provider
	Config model.Config
}

// Engine runs the main event loop described in the package doc comment.
// TODO(M04): implement.
type Engine struct {
	deps Deps
}

// New constructs an Engine. It does not start it — call Run for that.
func New(deps Deps) *Engine {
	return &Engine{deps: deps}
}

// Run drives the event loop until ctx is canceled or an unrecoverable
// error occurs. TODO(M04): implement.
func (e *Engine) Run(ctx context.Context) error {
	return errNotImplemented("Engine.Run")
}
