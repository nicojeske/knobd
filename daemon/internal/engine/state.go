package engine

import (
	"sort"

	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/focus"
	"github.com/njeske/knobd/internal/model"
)

// Snapshot is engine's state as of the moment Snapshot was called --
// what daemon/internal/api's GET /state endpoint serves. It is built
// entirely from state the run goroutine already holds (the binding
// index and the resolver's stream/device cache, plus the optional
// StateObserver's cached levels), so building one never blocks on
// audio.Backend.
type Snapshot struct {
	ActiveProfileID string
	ActiveLayer     int
	Controls        []ControlSnapshot
	// Focused is the resolver's cached last-known focused application
	// (see resolver.setFocused) — the zero value if nothing has been
	// reported yet. cmd/knobd/state.go translates this into
	// api.FocusState.ResourceClass.
	Focused focus.AppInfo
}

// ControlSnapshot is one bound (control, gesture)'s current behavior and
// what it resolves to right now.
type ControlSnapshot struct {
	Control    model.Control
	Gesture    model.Gesture
	ActionType model.ActionType
	// Target is the action's configured target; nil for an action that
	// carries none (e.g. a layer action).
	Target *model.Target
	// Refs are what Target currently resolves to; nil if it resolves to
	// nothing right now (the app isn't running) or Target is nil.
	Refs []audio.Ref
	// Volume is Refs[0]'s last-known level, from the StateObserver's
	// cache; nil if there is no ref, no observer, or nothing has been
	// observed for it yet.
	Volume *audio.VolumeState
}

// buildSnapshot runs entirely on the caller's goroutine (the engine's
// run loop, via the snapshotCh case in Run) using state that goroutine
// already owns -- no locking, no backend calls.
func (e *Engine) buildSnapshot(cfg model.Config, bindings *bindingIndex, res *resolver, layer int) Snapshot {
	entries := bindings.activeBindings(layer)
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Control.Kind != b.Control.Kind {
			return a.Control.Kind < b.Control.Kind
		}
		if a.Control.Index != b.Control.Index {
			return a.Control.Index < b.Control.Index
		}
		return a.Gesture < b.Gesture
	})

	controls := make([]ControlSnapshot, 0, len(entries))
	for _, ab := range entries {
		cs := ControlSnapshot{Control: ab.Control, Gesture: ab.Gesture, ActionType: ab.Action.ActionType()}
		if target, ok := model.TargetOf(ab.Action); ok {
			t := target
			cs.Target = &t
			if refs, err := res.resolve(target); err == nil && len(refs) > 0 {
				cs.Refs = refs
				if e.deps.Observer != nil {
					if st, ok := e.deps.Observer.CachedLevel(refs[0]); ok {
						cs.Volume = &st
					}
				}
			}
		}
		controls = append(controls, cs)
	}

	return Snapshot{ActiveProfileID: cfg.ActiveProfileID, ActiveLayer: layer, Controls: controls, Focused: res.focused}
}
