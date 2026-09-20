// Package device translates between raw midi.Message bytes and the
// controller-agnostic model.Control/model.Gesture domain types, for one
// specific piece of hardware — currently only the Behringer X-Touch
// Mini. If knobd ever supports a second controller, it gets its own
// Codec implementation here rather than branching inside the engine.
//
// TODO(M02): implement xtouchMiniCodec for real. The wire protocol to
// implement against — captured live and documented in
// specs/reference/xtouch-mini-midi-map.md — is Mackie Control (MC) mode:
//   - Encoders 1-8 turn: CC 16-23 on channel 1, RELATIVE values (1-63 =
//     clockwise by N, 65-127 = counter-clockwise by N-64). There is no
//     absolute position to read back.
//   - Encoder pushes 1-8: Note On/Off 32-39, channel 1.
//   - Grid buttons 1-16: Note On/Off on a non-contiguous set of note
//     numbers (see the reference doc) — do not assume note number =
//     button index.
//   - Side buttons: Note On/Off 84-85, channel 1.
//   - Fader: pitch bend on channel 9 (status byte 0xE8), 7-bit value.
//   - The unit can also run in "Standard" mode, sending absolute CC
//     instead of relative — the codec should detect this from the first
//     encoder message and either support it or fail clearly rather than
//     silently misinterpreting relative deltas as absolute positions.
//
// TODO(M05): implement EncodeLED once the ring/button LED byte encoding
// has been empirically verified against the physical unit (writing to
// the device is a state change, so it was deliberately not tested while
// scaffolding this package — see the risk noted in the M05 spec).
package device

import (
	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
)

// Event is a decoded, controller-agnostic input event.
type Event struct {
	Control model.Control
	Gesture model.Gesture
	// Delta carries the signed step count for a GestureTurn event (how
	// many detents, and which direction, an encoder moved). It is
	// unused for every other gesture.
	Delta int
}

// LEDMode selects how an encoder's ring should render a value. See
// specs/milestones/M05-led-feedback.md.
type LEDMode int

const (
	LEDModeSingleDot LEDMode = iota
	LEDModeBar
	LEDModeSpread
)

// LEDUpdate describes one LED (or ring) state to push to the device.
type LEDUpdate struct {
	Control model.Control
	// On is used for a plain button LED; Position/Mode are used for an
	// encoder ring. Exactly one set of fields is meaningful, depending
	// on Control.Kind.
	On       bool
	Position int // 0-12, ring position
	Mode     LEDMode
}

// Codec converts between raw MIDI and the domain-level Event/LEDUpdate
// types for one specific controller model.
type Codec interface {
	// Decode interprets one raw MIDI message. It returns ok=false for
	// messages that do not correspond to a complete, meaningful event
	// (e.g. a note-off that the codec has already folded into the
	// preceding note-on as a GestureRelease).
	Decode(msg midi.Message) (event Event, ok bool, err error)

	// EncodeLED produces the raw MIDI message(s) that would apply
	// update to the device.
	EncodeLED(update LEDUpdate) ([]midi.Message, error)
}

// NewXTouchMiniCodec returns the Codec for a Behringer X-Touch Mini in
// Mackie Control mode. TODO(M02): implement; see the package doc
// comment for the target wire protocol.
func NewXTouchMiniCodec() Codec {
	return xtouchMiniCodec{}
}

type xtouchMiniCodec struct{}

func (xtouchMiniCodec) Decode(midi.Message) (Event, bool, error) {
	return Event{}, false, errNotImplemented("xtouchMiniCodec.Decode")
}

func (xtouchMiniCodec) EncodeLED(LEDUpdate) ([]midi.Message, error) {
	return nil, errNotImplemented("xtouchMiniCodec.EncodeLED")
}

var _ Codec = xtouchMiniCodec{}
