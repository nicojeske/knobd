package device

import (
	"fmt"

	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
)

// noteToControl maps every note number the X-Touch Mini's MC-mode map
// documents to the Control it identifies. Built directly from
// specs/reference/xtouch-mini-midi-map.md's tables — note numbers are
// deliberately non-contiguous, so this is a literal lookup table, not a
// formula.
var noteToControl = buildNoteMap()

func buildNoteMap() map[byte]model.Control {
	m := make(map[byte]model.Control)

	// Encoder pushes 1-8: notes 32-39, a 1:1 offset (verified directly:
	// pushing encoder 1 sent note 32, encoder 8 sent note 39).
	for i := 0; i < 8; i++ {
		m[byte(32+i)] = model.Control{Kind: model.ControlEncoderPush, Index: i + 1}
	}

	// Grid buttons, top row 1-8 left to right and bottom row 9-16 left
	// to right — non-contiguous, do not assume note = 40 + index.
	topRow := [8]byte{89, 90, 40, 41, 42, 43, 44, 45}
	bottomRow := [8]byte{87, 88, 91, 92, 86, 93, 94, 95}
	for i, note := range topRow {
		m[note] = model.Control{Kind: model.ControlButton, Index: i + 1}
	}
	for i, note := range bottomRow {
		m[note] = model.Control{Kind: model.ControlButton, Index: i + 9}
	}

	// Side buttons.
	m[84] = model.Control{Kind: model.ControlSideButton, Index: 1}
	m[85] = model.Control{Kind: model.ControlSideButton, Index: 2}

	return m
}

// controlToNote is the inverse of noteToControl, for EncodeLED (led.go):
// given a button/side-button Control, which note number's LED addresses
// it. TestNoteMapIsWellFormed proves noteToControl is injective, so this
// inversion loses no information; encoder pushes are included too (the
// map is built from the same table) even though EncodeLED rejects them
// with ErrNoLED before ever consulting this map — they have no LED.
var controlToNote = invertNoteMap(noteToControl)

func invertNoteMap(m map[byte]model.Control) map[model.Control]byte {
	inv := make(map[model.Control]byte, len(m))
	for note, ctrl := range m {
		inv[ctrl] = note
	}
	return inv
}

// encoderTurnCC returns the 1-based encoder index for a CC controller
// number in the encoders' range (16-23), or ok=false outside it.
func encoderTurnCC(cc byte) (index int, ok bool) {
	if cc < 16 || cc > 23 {
		return 0, false
	}
	return int(cc-16) + 1, true
}

// ringCC returns the CC controller number for an encoder's LED ring
// (48-55), given the encoder's 1-based index (1-8) — see led.go and
// specs/reference/xtouch-mini-midi-map.md's LED section (verified
// directly on CC 48 and CC 55, the two ends of the range).
func ringCC(index int) (cc byte, ok bool) {
	if index < 1 || index > 8 {
		return 0, false
	}
	return byte(47 + index), true
}

// midiChannel returns the 1-based MIDI channel of a status byte (channel
// 1 = the low nibble 0x0).
func midiChannel(status byte) int {
	return int(status&0x0F) + 1
}

func (xtouchMiniCodec) Decode(msg midi.Message) (Event, bool, error) {
	kind := msg.Status & 0xF0
	channel := midiChannel(msg.Status)

	switch kind {
	case 0xB0: // control change
		if channel != 1 {
			return Event{}, false, fmt.Errorf(
				"device: control change on MIDI channel %d, expected channel 1: %w", channel, ErrStandardMode)
		}
		idx, ok := encoderTurnCC(msg.Data1)
		if !ok {
			return Event{}, false, fmt.Errorf(
				"device: control change %d, expected an encoder controller 16-23 (Standard mode is documented to use 1-9 for this unit family, but that hasn't been captured from this unit — switch it back to MC mode): %w",
				msg.Data1, ErrStandardMode)
		}
		v := msg.Data2
		switch {
		case v == 0 || v == 64:
			// The map says these were never observed and should be
			// treated as "no movement" if they ever appear.
			return Event{}, false, nil
		case v <= 63:
			return Event{
				Control: model.Control{Kind: model.ControlEncoder, Index: idx},
				Kind:    EventTurn,
				Delta:   int(v),
				Time:    msg.Time,
			}, true, nil
		default: // 65-127
			return Event{
				Control: model.Control{Kind: model.ControlEncoder, Index: idx},
				Kind:    EventTurn,
				Delta:   -(int(v) - 64),
				Time:    msg.Time,
			}, true, nil
		}

	case 0x90, 0x80: // note on / note off
		if channel != 1 {
			return Event{}, false, fmt.Errorf(
				"device: note message on MIDI channel %d, expected channel 1: %w", channel, ErrStandardMode)
		}
		ctrl, ok := noteToControl[msg.Data1]
		if !ok {
			// Not on the map at all — nothing to report, but not
			// confidently "wrong mode" either (see Decode's doc
			// comment on ok=false, err=nil).
			return Event{}, false, nil
		}
		evKind := EventButtonUp
		if kind == 0x90 && msg.Data2 > 0 {
			evKind = EventButtonDown
		}
		return Event{Control: ctrl, Kind: evKind, Time: msg.Time}, true, nil

	case 0xE0: // pitch bend
		if channel != 9 {
			return Event{}, false, fmt.Errorf(
				"device: pitch bend on MIDI channel %d, expected channel 9 (the fader): %w", channel, ErrUnknownMessage)
		}
		// Full 14-bit pitch bend value (LSB in Data1, MSB in Data2),
		// narrowed to 7 bits. The unit has only ever been observed to
		// send Data1 == 0 (LSB always 0), so this is equivalent to
		// int(msg.Data2) for every captured message but degrades
		// gracefully rather than silently discarding bits a future
		// firmware revision might populate.
		full := int(msg.Data2)<<7 | int(msg.Data1)
		return Event{
			Control: model.Control{Kind: model.ControlFader, Index: 1},
			Kind:    EventFaderMove,
			Value:   full >> 7,
			Time:    msg.Time,
		}, true, nil

	default:
		if msg.Status >= 0xF0 {
			// System realtime/common/SysEx: midi.parser already handles
			// framing for these; nothing for the codec to decode.
			return Event{}, false, nil
		}
		return Event{}, false, fmt.Errorf("device: unrecognized status byte %#02x: %w", msg.Status, ErrUnknownMessage)
	}
}
