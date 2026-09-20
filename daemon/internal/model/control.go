// Package model defines the domain types shared across knobd: the
// physical controls on the X-Touch Mini, the bindings that map a
// control+gesture to an action, and the targets those actions act on.
//
// None of these types know how to talk to MIDI, PipeWire, or KWin —
// that is the job of the midi, audio, and focus packages respectively.
// model is deliberately dependency-free so it can be imported by the
// UI's generated types (via docs/config.schema.json) without pulling in
// the daemon's I/O code.
package model

import "fmt"

// ControlKind identifies which family of physical control a Control
// refers to. See specs/reference/xtouch-mini-midi-map.md for the wire
// encoding of each kind.
type ControlKind string

const (
	// ControlEncoder is one of the 8 endless rotary encoders (turning).
	ControlEncoder ControlKind = "encoder"
	// ControlEncoderPush is the push-button built into an encoder.
	ControlEncoderPush ControlKind = "encoder_push"
	// ControlButton is one of the 16 top/bottom grid buttons.
	ControlButton ControlKind = "button"
	// ControlSideButton is one of the 2 side buttons (layer switches by
	// default; see specs/milestones/M08-layers-groups-scenes.md).
	ControlSideButton ControlKind = "side_button"
	// ControlFader is the single motorized... actually non-motorized
	// fader on the unit.
	ControlFader ControlKind = "fader"
)

// ControlKinds returns every valid ControlKind, for validation and for
// daemon/internal/schema's enum generation.
func ControlKinds() []ControlKind {
	return []ControlKind{ControlEncoder, ControlEncoderPush, ControlButton, ControlSideButton, ControlFader}
}

// Control identifies one physical control on the device.
//
// Index is 1-based and matches the labels printed on the hardware:
// encoders and encoder-pushes are 1-8, grid buttons are 1-16 (top row
// 1-8, bottom row 9-16), side buttons are 1-2 (upper, lower), and the
// fader is always 1. Index is ignored (and must be 0) for kinds that
// only have one instance in a future revision, but every current kind
// requires a positive Index.
type Control struct {
	Kind  ControlKind `json:"kind"`
	Index int         `json:"index"`
}

// Validate reports whether the Control's Index is in range for its Kind.
func (c Control) Validate() error {
	switch c.Kind {
	case ControlEncoder, ControlEncoderPush:
		if c.Index < 1 || c.Index > 8 {
			return fmt.Errorf("model: %s index %d out of range [1,8]", c.Kind, c.Index)
		}
	case ControlButton:
		if c.Index < 1 || c.Index > 16 {
			return fmt.Errorf("model: button index %d out of range [1,16]", c.Index)
		}
	case ControlSideButton:
		if c.Index < 1 || c.Index > 2 {
			return fmt.Errorf("model: side_button index %d out of range [1,2]", c.Index)
		}
	case ControlFader:
		if c.Index != 1 {
			return fmt.Errorf("model: fader index %d must be 1", c.Index)
		}
	default:
		return fmt.Errorf("model: unknown control kind %q", c.Kind)
	}
	return nil
}

// Gesture identifies how a Control was actuated.
type Gesture string

const (
	// GestureTurn fires for every relative encoder movement; the amount
	// is carried by the event, not the gesture.
	GestureTurn Gesture = "turn"
	// GesturePress fires on a short press-and-release, below the
	// hold threshold (see engine.HoldThreshold).
	GesturePress Gesture = "press"
	// GestureHold fires once when a press has been held past the hold
	// threshold, while the control is still down.
	GestureHold Gesture = "hold"
	// GestureRelease fires when a control is released after a
	// GestureHold was already delivered (short presses only deliver
	// GesturePress, not Release, to keep the common case to one event).
	GestureRelease Gesture = "release"
	// GestureDoublePress fires instead of a second GesturePress when two
	// presses land inside the double-press window.
	GestureDoublePress Gesture = "double_press"
)

// Gestures returns every valid Gesture, for validation and for
// daemon/internal/schema's enum generation.
func Gestures() []Gesture {
	return []Gesture{GestureTurn, GesturePress, GestureHold, GestureRelease, GestureDoublePress}
}

// SupportsGesture reports whether a gesture is meaningful for a control
// kind. The fader currently only ever produces absolute position updates
// and is modeled as a continuous target write rather than a gesture; see
// specs/milestones/M04-mapping-engine-daemon.md.
func (k ControlKind) SupportsGesture(g Gesture) bool {
	switch k {
	case ControlEncoder:
		return g == GestureTurn
	case ControlEncoderPush, ControlButton, ControlSideButton:
		return g == GesturePress || g == GestureHold || g == GestureRelease || g == GestureDoublePress
	case ControlFader:
		return false
	default:
		return false
	}
}
