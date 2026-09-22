// Package actions executes model.Action values. Each action family
// (volume, layers/scenes, media, system) gets its own file here once its
// milestone starts; see specs/reference/action-catalog.md for the full
// brainstormed list, most of which has no handler — or even a
// model.Action type — yet.
//
// TODO: implement handlers incrementally, one per milestone:
//   - Volume actions (model.ActionVolumeAdjust etc.): M03/M04.
//   - Layer/scene actions: M08.
//   - Media transport actions: M09.
//   - Extended/system actions (model.ActionShellRun etc.): M11.
package actions

import (
	"context"
	"fmt"
	"time"

	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/model"
)

// Invocation is everything a Handler needs about one firing of one
// binding: the action itself, the physical gesture that triggered it,
// and the audio graph entities the engine already resolved the action's
// model.Target to.
//
// Refs is authoritative: a Handler must use it rather than re-resolving
// Target itself. Target resolution needs the config's AppMatchers, the
// live stream graph, and the focus provider, all of which live in
// engine — and engine is also where the read-modify-write serialization
// audio.Backend's doc comment requires is owned (a single dispatch
// goroutine calling Execute). Target is carried only so a Handler can
// produce a legible error message; it must never be resolved again here.
type Invocation struct {
	Action  model.Action
	Control model.Control
	Gesture model.Gesture

	// Delta is the signed detent count for a GestureTurn firing; 0
	// otherwise. It may be greater than one detent's worth when the
	// engine coalesced several rapid turns into one dispatch, so a
	// Handler must scale by it rather than assume a single step.
	Delta int
	// Value is the absolute 0-127 position for a GestureMove firing
	// (currently only the fader); 0 otherwise.
	Value int
	// At is the hardware timestamp the triggering device.Event carried.
	At time.Time

	// Refs are the live audio entities Target (or, for the dynamic-pool
	// case a later milestone may add, some other selection with no
	// model.Target at all) resolved to at dispatch time. See the type's
	// doc comment above: use this, not Target.
	Refs []audio.Ref
	// Target is the action's configured target, carried for error
	// messages only.
	Target model.Target
}

// Handler executes one concrete model.Action, given the Invocation it
// fired in. Implementations receive the already-resolved Action value
// (e.g. a VolumeAdjustAction, not the model.Action interface) via
// inv.Action, which they must type-assert themselves.
type Handler interface {
	Execute(ctx context.Context, inv Invocation) error
}

// Registry dispatches a model.Action to the Handler registered for its
// model.ActionType.
type Registry struct {
	handlers map[model.ActionType]Handler
}

// NewRegistry returns an empty Registry. Register handlers with
// Register before calling Execute.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[model.ActionType]Handler)}
}

// HandlerFunc adapts a plain function to Handler, the way
// http.HandlerFunc adapts a function to http.Handler.
type HandlerFunc func(ctx context.Context, inv Invocation) error

func (f HandlerFunc) Execute(ctx context.Context, inv Invocation) error { return f(ctx, inv) }

// Register associates a Handler with an ActionType, overwriting any
// previous registration.
func (r *Registry) Register(actionType model.ActionType, h Handler) {
	r.handlers[actionType] = h
}

// Execute dispatches inv.Action to its registered Handler.
func (r *Registry) Execute(ctx context.Context, inv Invocation) error {
	if inv.Action == nil {
		return fmt.Errorf("actions: cannot execute a nil action")
	}
	h, ok := r.handlers[inv.Action.ActionType()]
	if !ok {
		return fmt.Errorf("actions: no handler registered for action type %q", inv.Action.ActionType())
	}
	return h.Execute(ctx, inv)
}
