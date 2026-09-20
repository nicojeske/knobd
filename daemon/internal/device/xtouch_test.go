package device

import (
	"errors"
	"testing"
	"time"

	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
)

func TestNoteMapIsWellFormed(t *testing.T) {
	seen := make(map[model.Control]byte)
	for note, ctrl := range noteToControl {
		if err := ctrl.Validate(); err != nil {
			t.Errorf("note %d maps to invalid control %+v: %v", note, ctrl, err)
		}
		if other, dup := seen[ctrl]; dup {
			t.Errorf("control %+v is mapped from both note %d and note %d", ctrl, other, note)
		}
		seen[ctrl] = note
	}

	wantCount := 8 /* encoder pushes */ + 16 /* grid buttons */ + 2 /* side buttons */
	if len(noteToControl) != wantCount {
		t.Errorf("noteToControl has %d entries, want %d", len(noteToControl), wantCount)
	}
}

func TestDecodeEveryControl(t *testing.T) {
	c := NewXTouchMiniCodec()
	now := time.Now()

	cases := []struct {
		name string
		msg  midi.Message
		want Event
	}{
		{"encoder 1 clockwise 1", midi.Message{Status: 0xB0, Data1: 16, Data2: 1, Time: now},
			Event{Control: model.Control{Kind: model.ControlEncoder, Index: 1}, Kind: EventTurn, Delta: 1, Time: now}},
		{"encoder 8 clockwise 63", midi.Message{Status: 0xB0, Data1: 23, Data2: 63, Time: now},
			Event{Control: model.Control{Kind: model.ControlEncoder, Index: 8}, Kind: EventTurn, Delta: 63, Time: now}},
		{"encoder 1 counter-clockwise 1", midi.Message{Status: 0xB0, Data1: 16, Data2: 65, Time: now},
			Event{Control: model.Control{Kind: model.ControlEncoder, Index: 1}, Kind: EventTurn, Delta: -1, Time: now}},
		{"encoder 8 counter-clockwise 63", midi.Message{Status: 0xB0, Data1: 23, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlEncoder, Index: 8}, Kind: EventTurn, Delta: -63, Time: now}},

		{"encoder-push 1 down", midi.Message{Status: 0x90, Data1: 32, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlEncoderPush, Index: 1}, Kind: EventButtonDown, Time: now}},
		{"encoder-push 1 up", midi.Message{Status: 0x90, Data1: 32, Data2: 0, Time: now},
			Event{Control: model.Control{Kind: model.ControlEncoderPush, Index: 1}, Kind: EventButtonUp, Time: now}},
		{"encoder-push 8 down", midi.Message{Status: 0x90, Data1: 39, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlEncoderPush, Index: 8}, Kind: EventButtonDown, Time: now}},

		{"top row 1 down", midi.Message{Status: 0x90, Data1: 89, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlButton, Index: 1}, Kind: EventButtonDown, Time: now}},
		{"top row 2 down", midi.Message{Status: 0x90, Data1: 90, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlButton, Index: 2}, Kind: EventButtonDown, Time: now}},
		{"top row 3 down", midi.Message{Status: 0x90, Data1: 40, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlButton, Index: 3}, Kind: EventButtonDown, Time: now}},
		{"top row 8 down", midi.Message{Status: 0x90, Data1: 45, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlButton, Index: 8}, Kind: EventButtonDown, Time: now}},

		{"bottom row 9 down", midi.Message{Status: 0x90, Data1: 87, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlButton, Index: 9}, Kind: EventButtonDown, Time: now}},
		{"bottom row 10 down", midi.Message{Status: 0x90, Data1: 88, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlButton, Index: 10}, Kind: EventButtonDown, Time: now}},
		{"bottom row 11 down", midi.Message{Status: 0x90, Data1: 91, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlButton, Index: 11}, Kind: EventButtonDown, Time: now}},
		{"bottom row 12 down", midi.Message{Status: 0x90, Data1: 92, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlButton, Index: 12}, Kind: EventButtonDown, Time: now}},
		{"bottom row 13 down", midi.Message{Status: 0x90, Data1: 86, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlButton, Index: 13}, Kind: EventButtonDown, Time: now}},
		{"bottom row 14 down", midi.Message{Status: 0x90, Data1: 93, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlButton, Index: 14}, Kind: EventButtonDown, Time: now}},
		{"bottom row 15 down", midi.Message{Status: 0x90, Data1: 94, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlButton, Index: 15}, Kind: EventButtonDown, Time: now}},
		{"bottom row 16 down", midi.Message{Status: 0x90, Data1: 95, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlButton, Index: 16}, Kind: EventButtonDown, Time: now}},

		{"upper side button down", midi.Message{Status: 0x90, Data1: 84, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlSideButton, Index: 1}, Kind: EventButtonDown, Time: now}},
		{"lower side button down", midi.Message{Status: 0x90, Data1: 85, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlSideButton, Index: 2}, Kind: EventButtonDown, Time: now}},

		{"fader at 0", midi.Message{Status: 0xE8, Data1: 0, Data2: 0, Time: now},
			Event{Control: model.Control{Kind: model.ControlFader, Index: 1}, Kind: EventFaderMove, Value: 0, Time: now}},
		{"fader at 96", midi.Message{Status: 0xE8, Data1: 0, Data2: 96, Time: now},
			Event{Control: model.Control{Kind: model.ControlFader, Index: 1}, Kind: EventFaderMove, Value: 96, Time: now}},
		{"fader at 127", midi.Message{Status: 0xE8, Data1: 0, Data2: 127, Time: now},
			Event{Control: model.Control{Kind: model.ControlFader, Index: 1}, Kind: EventFaderMove, Value: 127, Time: now}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := c.Decode(tc.msg)
			if err != nil {
				t.Fatalf("Decode(%+v) error = %v", tc.msg, err)
			}
			if !ok {
				t.Fatalf("Decode(%+v) ok = false, want true", tc.msg)
			}
			if got != tc.want {
				t.Errorf("Decode(%+v) = %+v, want %+v", tc.msg, got, tc.want)
			}
		})
	}
}

func TestDecodeRelativeDeltas(t *testing.T) {
	c := NewXTouchMiniCodec()

	cases := []struct {
		value     byte
		wantDelta int
		wantOK    bool
	}{
		{1, 1, true},
		{3, 3, true},
		{63, 63, true},
		{65, -1, true},
		{67, -3, true},
		{127, -63, true},
		{0, 0, false},  // "no movement" per the map
		{64, 0, false}, // "no movement" per the map
	}
	for _, tc := range cases {
		msg := midi.Message{Status: 0xB0, Data1: 16, Data2: tc.value}
		ev, ok, err := c.Decode(msg)
		if err != nil {
			t.Fatalf("Decode(value=%d) error = %v", tc.value, err)
		}
		if ok != tc.wantOK {
			t.Fatalf("Decode(value=%d) ok = %v, want %v", tc.value, ok, tc.wantOK)
		}
		if ok && ev.Delta != tc.wantDelta {
			t.Errorf("Decode(value=%d).Delta = %d, want %d", tc.value, ev.Delta, tc.wantDelta)
		}
	}
}

func TestDecodeFaderPitchBendLSB(t *testing.T) {
	c := NewXTouchMiniCodec()
	// Every captured fader message has LSB (Data1) == 0. Confirm the
	// full 14-bit decode still lands on the expected 7-bit value when
	// the LSB is populated too, rather than only ever reading Data2.
	ev, ok, err := c.Decode(midi.Message{Status: 0xE8, Data1: 0x7F, Data2: 0x60})
	if err != nil || !ok {
		t.Fatalf("Decode: ok=%v err=%v", ok, err)
	}
	if ev.Value != 0x60 {
		t.Errorf("Value = %d, want %d", ev.Value, 0x60)
	}
}

func TestDecodeUnknownNoteIsOkFalseNoError(t *testing.T) {
	c := NewXTouchMiniCodec()
	// Note 60 is not on the X-Touch Mini's map at all.
	ev, ok, err := c.Decode(midi.Message{Status: 0x90, Data1: 60, Data2: 127})
	if err != nil {
		t.Fatalf("Decode of an unmapped note returned an error: %v", err)
	}
	if ok {
		t.Errorf("Decode of an unmapped note returned ok=true: %+v", ev)
	}
}

func TestDecodeUnbalancedNoteOff(t *testing.T) {
	// Regression test for the real first line of testdata/midi/guided-capture.txt:
	// a note-off with no preceding note-on. Decode is stateless, so this
	// must still decode cleanly as a release, not be rejected.
	c := NewXTouchMiniCodec()
	ev, ok, err := c.Decode(midi.Message{Status: 0x90, Data1: 90, Data2: 0})
	if err != nil || !ok {
		t.Fatalf("Decode: ok=%v err=%v", ok, err)
	}
	if ev.Kind != EventButtonUp {
		t.Errorf("Kind = %v, want EventButtonUp", ev.Kind)
	}
}

func TestDecodeRealNoteOffStatus(t *testing.T) {
	// The unit has only been observed to send velocity-0 note-on for
	// releases, but a real 0x80 note-off is legal MIDI and must decode
	// the same way.
	c := NewXTouchMiniCodec()
	ev, ok, err := c.Decode(midi.Message{Status: 0x80, Data1: 89, Data2: 64})
	if err != nil || !ok {
		t.Fatalf("Decode: ok=%v err=%v", ok, err)
	}
	if ev.Kind != EventButtonUp {
		t.Errorf("Kind = %v, want EventButtonUp", ev.Kind)
	}
}

func TestDecodeStandardMode(t *testing.T) {
	c := NewXTouchMiniCodec()

	cases := []struct {
		name string
		msg  midi.Message
	}{
		{"CC on channel 11 (the commonly-documented Standard-mode default)", midi.Message{Status: 0xBA, Data1: 1, Data2: 64}},
		{"CC on channel 1 but controller out of the encoder range", midi.Message{Status: 0xB0, Data1: 1, Data2: 64}},
		{"note on channel 11", midi.Message{Status: 0x9A, Data1: 32, Data2: 127}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok, err := c.Decode(tc.msg)
			if ok {
				t.Fatalf("Decode(%+v) ok = true, want false", tc.msg)
			}
			if !errors.Is(err, ErrStandardMode) {
				t.Fatalf("Decode(%+v) error = %v, want ErrStandardMode", tc.msg, err)
			}
		})
	}
}

func TestDecodeMCModeStreamNeverTriggersStandardModeError(t *testing.T) {
	c := NewXTouchMiniCodec()
	// One message per control family, all legal MC-mode traffic.
	msgs := []midi.Message{
		{Status: 0xB0, Data1: 16, Data2: 3},
		{Status: 0x90, Data1: 32, Data2: 127},
		{Status: 0x90, Data1: 89, Data2: 127},
		{Status: 0x90, Data1: 84, Data2: 127},
		{Status: 0xE8, Data1: 0, Data2: 64},
	}
	for _, msg := range msgs {
		if _, _, err := c.Decode(msg); errors.Is(err, ErrStandardMode) {
			t.Errorf("Decode(%+v) incorrectly returned ErrStandardMode", msg)
		}
	}
}

func TestDecodeUnknownMessage(t *testing.T) {
	c := NewXTouchMiniCodec()
	_, ok, err := c.Decode(midi.Message{Status: 0xC0, Data1: 5}) // program change
	if ok {
		t.Fatal("Decode(program change) ok = true, want false")
	}
	if !errors.Is(err, ErrUnknownMessage) {
		t.Fatalf("Decode(program change) error = %v, want ErrUnknownMessage", err)
	}
}

func TestDecodeSystemMessagesAreOkFalseNoError(t *testing.T) {
	c := NewXTouchMiniCodec()
	for _, status := range []byte{0xF8, 0xFA, 0xFE, 0xFF} {
		ev, ok, err := c.Decode(midi.Message{Status: status})
		if err != nil {
			t.Errorf("Decode(status=%#02x) error = %v, want nil", status, err)
		}
		if ok {
			t.Errorf("Decode(status=%#02x) = %+v, ok = true, want false", status, ev)
		}
	}
}

func TestDecodeIsStateless(t *testing.T) {
	c := NewXTouchMiniCodec()
	msg := midi.Message{Status: 0x90, Data1: 89, Data2: 127}

	first, ok1, err1 := c.Decode(msg)
	second, ok2, err2 := c.Decode(msg)
	if ok1 != ok2 || err1 != err2 || first != second {
		t.Errorf("Decode is not stateless: first=(%+v,%v,%v) second=(%+v,%v,%v)",
			first, ok1, err1, second, ok2, err2)
	}
}
