// Package device translates between raw midi.Message bytes and the
// controller-agnostic Event type, for one specific piece of hardware —
// currently only the Behringer X-Touch Mini. If knobd ever supports a
// second controller, it gets its own Codec implementation here rather
// than branching inside the engine.
//
// The real implementation (xtouch.go) targets Mackie Control (MC) mode,
// captured live and documented in specs/reference/xtouch-mini-midi-map.md:
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
//     instead of relative. The codec detects traffic that can't be
//     MC-mode (wrong channel, or an out-of-range controller) and returns
//     ErrStandardMode rather than silently misinterpreting it — see
//     xtouch.go and errors.go.
//
// M05's EncodeLED (led.go) targets the same reference doc's LED
// section, confirmed live via `knobd calibrate-leds`:
//   - Button LEDs (grid and side buttons — not encoder pushes, which
//     have no LED of their own): Note On, channel 1, same note numbers
//     as input. Velocity 0 = off, 1-2 = blinking, 3-127 = solid on —
//     the *opposite* split from the publicly-documented guess this
//     package originally scaffolded against.
//   - Encoder rings: CC 48-55, channel 1, `value = (mode<<4)|position`.
//     Only 11 of the ring's 13 physical segments are individually
//     addressable (`position` 0-11, clamping above 11); `mode` is one
//     of four confirmed display styles (see LEDMode). knobd only emits
//     mode 2 (fill), the natural rendering of a 0-100% volume.
//   - The fader and encoder pushes have no LED at all; EncodeLED
//     returns ErrNoLED for either.
package device

import (
	"time"

	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
)

// EventKind identifies what happened, independent of any timing. The
// zero value is EventKindUnknown rather than a real kind on purpose: a
// zero Event{} (which is exactly what Decode returns alongside ok=false)
// must never read as a plausible event.
//
// Notably absent: nothing here says "held" or "released after a hold",
// or folds a press/release pair into one gesture. Codec.Decode is a
// pure, stateless translation of one MIDI message at a time (Scope in
// specs/milestones/M02-midi-transport.md); turning a raw down/up pair
// into model.GesturePress/GestureHold/GestureRelease/GestureDoublePress
// against engine.HoldThreshold is the mapping engine's job (M04).
type EventKind int

const (
	EventKindUnknown EventKind = iota
	// EventTurn is a relative encoder movement; Delta carries the signed
	// detent count.
	EventTurn
	// EventButtonDown is a button, encoder-push, or side-button going
	// down (note-on with velocity > 0, or an explicit note-off velocity
	// convention some controllers use... in practice, velocity > 0).
	EventButtonDown
	// EventButtonUp is the corresponding release (velocity 0, or a real
	// note-off).
	EventButtonUp
	// EventFaderMove is an absolute fader position; Value carries it.
	EventFaderMove
)

func (k EventKind) String() string {
	switch k {
	case EventTurn:
		return "turn"
	case EventButtonDown:
		return "down"
	case EventButtonUp:
		return "up"
	case EventFaderMove:
		return "fader"
	default:
		return "unknown"
	}
}

// Event is a decoded, controller-agnostic input event.
type Event struct {
	Control model.Control
	Kind    EventKind
	// Delta carries the signed detent count for an EventTurn; unused
	// otherwise.
	Delta int
	// Value carries the absolute 0-127 position for an EventFaderMove;
	// unused otherwise. Kept as a separate field from Delta rather than
	// reusing it — an absolute position and a relative step are not
	// interchangeable, and a caller that (say) accumulates Delta into a
	// running total must never be handed one by mistake.
	Value int
	// Time is copied from the originating midi.Message — see its doc
	// comment for why gesture timing depends on that being close to the
	// hardware event.
	Time time.Time
}

// LEDMode selects one of the encoder ring's four hardware display
// styles, confirmed against the physical unit (see
// specs/reference/xtouch-mini-midi-map.md's LED section) via
// `knobd calibrate-leds`. Only LEDModeFill is used by anything in this
// codebase today — volume is naturally a fill bar — the other three are
// defined because the wire format supports them at no extra cost and a
// future action (e.g. a stereo balance control) may want one.
type LEDMode int

const (
	// LEDModeSingleDot lights exactly one LED, at Position.
	LEDModeSingleDot LEDMode = iota
	// LEDModePan lights a 3-LED-wide bar centered near Position.
	LEDModePan
	// LEDModeFill lights a bar growing from the ring's addressable end
	// through Position — the mode knobd uses for volume display.
	LEDModeFill
	// LEDModeSpread lights a bar that grows symmetrically outward from
	// the ring's center.
	LEDModeSpread
)

// LEDUpdate describes one LED (or ring) state to push to the device.
type LEDUpdate struct {
	Control model.Control
	// On is used for a plain button LED (grid or side button); Position
	// and Mode are used for an encoder ring. Exactly one set of fields
	// is meaningful, depending on Control.Kind. The fader and an
	// encoder's own push (model.ControlFader, model.ControlEncoderPush)
	// have no LED at all on this unit — EncodeLED returns ErrNoLED for
	// either.
	On bool
	// Position is a ring position, 0-11: 0 is off/empty, 11 is the last
	// of the ring's 11 individually addressable LEDs (of 13 physical
	// segments — the two end segments never light, confirmed on the
	// unit). A value outside 0-11 is clamped by EncodeLED rather than
	// rejected, matching the hardware's own behavior at the wire level
	// (11, 12, and 15 all render identically).
	Position int
	Mode     LEDMode
}

// Codec converts between raw MIDI and the domain-level Event/LEDUpdate
// types for one specific controller model.
type Codec interface {
	// Decode interprets one raw MIDI message. It returns ok=false, err=nil
	// for a message that is legal MC-mode traffic but carries no
	// meaningful event on its own — a realtime byte, a message for a
	// control not on the map, or a CC value the map says means "no
	// movement" (0 or 64). It returns a non-nil err only when the
	// message indicates the controller itself is in an unsupported state
	// (see ErrStandardMode) or is otherwise unrecognizable as this
	// device's protocol at all (see ErrUnknownMessage); neither is fatal
	// to a caller's event loop — log and continue.
	//
	// Decode is stateless: calling it repeatedly, in any order, with the
	// same message always produces the same result. In particular a
	// note-off with no corresponding prior note-on still decodes to a
	// plain EventButtonUp rather than being rejected — Decode does not
	// track what came before.
	Decode(msg midi.Message) (event Event, ok bool, err error)

	// EncodeLED produces the raw MIDI message(s) that would apply
	// update to the device. Returns ErrNoLED if update.Control has no
	// LED at all (the fader, an encoder push), or wraps ErrUnknownMessage
	// if update.Control isn't a recognized control on this unit.
	EncodeLED(update LEDUpdate) ([]midi.Message, error)
}

// NewXTouchMiniCodec returns the Codec for a Behringer X-Touch Mini in
// Mackie Control mode.
func NewXTouchMiniCodec() Codec {
	return xtouchMiniCodec{}
}

type xtouchMiniCodec struct{}

// EncodeLED is implemented in led.go.

var _ Codec = xtouchMiniCodec{}
