package engine

import (
	"log/slog"

	"github.com/njeske/knobd/internal/model"
)

// bindingKey identifies one lookup slot: a control's gesture on a
// specific layer.
type bindingKey struct {
	Layer   int
	Control model.Control
	Gesture model.Gesture
}

// bindingIndex is the dispatch-time view of the active profile's
// bindings, rebuilt whenever the config changes and never mutated
// afterward. Layer resolution — falling back to layer 0 when the active
// layer has no binding for a (control, gesture) — is already the shape
// M08 needs; M04 just always resolves against layer 0 (see engine.go).
type bindingIndex struct {
	byKey map[bindingKey]model.Action
	// doubleBound records every control with a GestureDoublePress
	// binding on any layer, which is what tells the gesture machine to
	// defer that control's plain presses (see gestureMachine's doc
	// comment).
	doubleBound map[model.Control]bool
}

// newBindingIndex builds an index from cfg's active profile. It never
// panics on malformed input: a binding that fails Binding.Validate is
// skipped with a warning rather than aborting the build, since
// config.Load/PUT /config already validate and a defensive skip here is
// only ever exercised by config that reached the engine some other way.
func newBindingIndex(cfg model.Config, log *slog.Logger) *bindingIndex {
	ix := &bindingIndex{
		byKey:       make(map[bindingKey]model.Action),
		doubleBound: make(map[model.Control]bool),
	}

	var profile *model.Profile
	for i := range cfg.Profiles {
		if cfg.Profiles[i].ID == cfg.ActiveProfileID {
			profile = &cfg.Profiles[i]
			break
		}
	}
	if profile == nil {
		if cfg.ActiveProfileID != "" {
			log.Warn("engine: active profile not found in config; no bindings active", "activeProfileId", cfg.ActiveProfileID)
		}
		return ix
	}

	for i, b := range profile.Bindings {
		if err := b.Validate(); err != nil {
			log.Warn("engine: skipping invalid binding", "profile", profile.ID, "index", i, "err", err)
			continue
		}
		key := bindingKey{Layer: b.Layer, Control: b.Control, Gesture: b.Gesture}
		if _, exists := ix.byKey[key]; exists {
			log.Warn("engine: duplicate binding for (layer, control, gesture); last one wins",
				"layer", b.Layer, "control", b.Control, "gesture", b.Gesture)
		}
		ix.byKey[key] = b.Action

		if b.Gesture == model.GestureDoublePress {
			ix.doubleBound[b.Control] = true
		}
	}

	return ix
}

// lookup resolves (control, gesture) on layer, falling back to layer 0
// — layers overlay the base rather than replacing it wholesale, per
// model.Binding's doc comment and M08's Design section.
func (ix *bindingIndex) lookup(layer int, c model.Control, g model.Gesture) (model.Action, bool) {
	if a, ok := ix.byKey[bindingKey{Layer: layer, Control: c, Gesture: g}]; ok {
		return a, true
	}
	if layer == 0 {
		return nil, false
	}
	a, ok := ix.byKey[bindingKey{Layer: 0, Control: c, Gesture: g}]
	return a, ok
}

// deferPress reports whether c has a GestureDoublePress binding on any
// layer, for gestureMachine's deferPress predicate.
func (ix *bindingIndex) deferPress(c model.Control) bool {
	return ix.doubleBound[c]
}
