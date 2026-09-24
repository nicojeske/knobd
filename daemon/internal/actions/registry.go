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
	// Layer is the active layer the triggering gesture was dispatched
	// on. It exists for knob.assign_focused_app's handler (M06), which
	// must rewrite a binding on the layer that's actually active rather
	// than assuming layer 0 — a real concern once M08 lands more than
	// one layer.
	Layer int

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

	// Refs are the live audio entities Target resolved to at dispatch
	// time. See the type's doc comment above: use this, not Target.
	Refs []audio.Ref
	// Target is the action's configured target, carried for error
	// messages only.
	Target model.Target

	// Scene is the resolved model.Scene a SceneApplyAction/
	// SceneSaveAction names, and SceneRefs is engine's dispatch-time
	// resolution of each of Scene.Entries' Target, one slice per entry
	// in the same order -- SceneHandlers must use SceneRefs[i] for
	// Scene.Entries[i], never re-resolve. Both are nil for every other
	// action type.
	Scene     *model.Scene
	SceneRefs [][]audio.Ref

	// Others are every currently-known playback stream not already in
	// Refs, resolved by engine at dispatch time for
	// AudioSoloToggleAction/AudioDuckHoldAction -- the "everything else"
	// those two actions mute/duck. Nil for every other action type.
	Others []audio.Ref
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

// ActionTypes returns every model.ActionType with a registered Handler,
// in no particular order. This is what GET /capabilities reports as
// "implementedActions": only this Registry, assembled at daemon startup
// (cmd/knobd/main.go), actually knows which of model.ActionTypes()' 21
// entries do anything -- as of this writing, 5 do.
func (r *Registry) ActionTypes() []model.ActionType {
	types := make([]model.ActionType, 0, len(r.handlers))
	for t := range r.handlers {
		types = append(types, t)
	}
	return types
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
