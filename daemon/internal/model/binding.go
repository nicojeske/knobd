package model

import (
	"encoding/json"
	"fmt"
)

// Binding maps one physical Control's Gesture, on a given Layer, to an
// Action. See specs/milestones/M04-mapping-engine-daemon.md for how the
// engine resolves the active set of bindings (layer 0 is always active;
// higher layers overlay it, see specs/milestones/M08-layers-groups-scenes.md).
type Binding struct {
	Layer   int
	Control Control
	Gesture Gesture
	Action  Action
}

// bindingJSON mirrors Binding but with Action as a discriminated-union
// envelope, since encoding/json cannot (de)serialize an interface field
// on its own.
type bindingJSON struct {
	Layer   int             `json:"layer"`
	Control Control         `json:"control"`
	Gesture Gesture         `json:"gesture"`
	Action  json.RawMessage `json:"action"`
}

// MarshalJSON implements json.Marshaler.
func (b Binding) MarshalJSON() ([]byte, error) {
	actionJSON, err := EncodeAction(b.Action)
	if err != nil {
		return nil, fmt.Errorf("model: marshal binding: %w", err)
	}
	return json.Marshal(bindingJSON{
		Layer:   b.Layer,
		Control: b.Control,
		Gesture: b.Gesture,
		Action:  actionJSON,
	})
}

// UnmarshalJSON implements json.Unmarshaler.
func (b *Binding) UnmarshalJSON(data []byte) error {
	var raw bindingJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("model: unmarshal binding: %w", err)
	}
	action, err := DecodeAction(raw.Action)
	if err != nil {
		return fmt.Errorf("model: unmarshal binding: %w", err)
	}
	b.Layer = raw.Layer
	b.Control = raw.Control
	b.Gesture = raw.Gesture
	b.Action = action
	return nil
}

// Validate reports whether the binding is internally consistent: a
// valid Control, a Gesture that control kind actually produces, and a
// non-nil Action. It does not check that any Target the Action refers to
// actually exists — that is a config-wide check, done in
// config.Validate against the full set of AppMatchers/AppGroups/Scenes.
func (b Binding) Validate() error {
	if err := b.Control.Validate(); err != nil {
		return err
	}
	if !b.Control.Kind.SupportsGesture(b.Gesture) {
		return fmt.Errorf("model: control kind %q does not support gesture %q", b.Control.Kind, b.Gesture)
	}
	if b.Action == nil {
		return fmt.Errorf("model: binding has no action")
	}
	// layer.momentary and audio.duck_hold are both "active while held"
	// actions (see their doc comments); the engine looks each up
	// specifically by GestureHold (see engine.dispatchGesture and, for
	// layer.momentary, Run's raw EventButtonDown handling), so any other
	// gesture would silently never fire.
	switch b.Action.(type) {
	case LayerMomentaryAction:
		if b.Gesture != GestureHold {
			return fmt.Errorf("model: layer.momentary must be bound to gesture %q, not %q", GestureHold, b.Gesture)
		}
	case AudioDuckHoldAction:
		if b.Gesture != GestureHold {
			return fmt.Errorf("model: audio.duck_hold must be bound to gesture %q, not %q", GestureHold, b.Gesture)
		}
	}
	return nil
}
