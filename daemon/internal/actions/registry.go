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

	"github.com/njeske/knobd/internal/model"
)

// Handler executes one concrete model.Action. Implementations receive
// the already-resolved Action value (e.g. a VolumeAdjustAction, not the
// model.Action interface), via the registry's type switch in Execute.
type Handler interface {
	Execute(ctx context.Context, action model.Action) error
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

// Register associates a Handler with an ActionType, overwriting any
// previous registration.
func (r *Registry) Register(actionType model.ActionType, h Handler) {
	r.handlers[actionType] = h
}

// Execute dispatches action to its registered Handler.
func (r *Registry) Execute(ctx context.Context, action model.Action) error {
	if action == nil {
		return fmt.Errorf("actions: cannot execute a nil action")
	}
	h, ok := r.handlers[action.ActionType()]
	if !ok {
		return fmt.Errorf("actions: no handler registered for action type %q", action.ActionType())
	}
	return h.Execute(ctx, action)
}
