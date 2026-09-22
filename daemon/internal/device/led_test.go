package device

import (
	"errors"
	"testing"

	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
)

func TestEncodeLEDRing(t *testing.T) {
	c := NewXTouchMiniCodec()

	tests := []struct {
		name      string
		update    LEDUpdate
		wantData1 byte
		wantData2 byte
	}{
		{
			name:      "encoder 1, fill mode, position 0 (off)",
			update:    LEDUpdate{Control: model.Control{Kind: model.ControlEncoder, Index: 1}, Mode: LEDModeFill, Position: 0},
			wantData1: 48,
			wantData2: 0x20, // mode 2 << 4 | position 0
		},
		{
			name:      "encoder 1, fill mode, position 11 (full)",
			update:    LEDUpdate{Control: model.Control{Kind: model.ControlEncoder, Index: 1}, Mode: LEDModeFill, Position: 11},
			wantData1: 48,
			wantData2: 0x2B, // mode 2 << 4 | position 11
		},
		{
			name:      "encoder 8, fill mode, position 11 -- confirms CC 48-55 addressing",
			update:    LEDUpdate{Control: model.Control{Kind: model.ControlEncoder, Index: 8}, Mode: LEDModeFill, Position: 11},
			wantData1: 55,
			wantData2: 0x2B,
		},
		{
			name:      "single-dot mode encodes mode 0",
			update:    LEDUpdate{Control: model.Control{Kind: model.ControlEncoder, Index: 1}, Mode: LEDModeSingleDot, Position: 5},
			wantData1: 48,
			wantData2: 0x05,
		},
		{
			name:      "pan mode encodes mode 1",
			update:    LEDUpdate{Control: model.Control{Kind: model.ControlEncoder, Index: 1}, Mode: LEDModePan, Position: 4},
			wantData1: 48,
			wantData2: 0x14,
		},
		{
			name:      "spread mode encodes mode 3",
			update:    LEDUpdate{Control: model.Control{Kind: model.ControlEncoder, Index: 1}, Mode: LEDModeSpread, Position: 4},
			wantData1: 48,
			wantData2: 0x34,
		},
		{
			name:      "position above 11 clamps rather than erroring -- matches hardware behavior",
			update:    LEDUpdate{Control: model.Control{Kind: model.ControlEncoder, Index: 1}, Mode: LEDModeFill, Position: 15},
			wantData1: 48,
			wantData2: 0x2B,
		},
		{
			name:      "negative position clamps to 0",
			update:    LEDUpdate{Control: model.Control{Kind: model.ControlEncoder, Index: 1}, Mode: LEDModeFill, Position: -3},
			wantData1: 48,
			wantData2: 0x20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs, err := c.EncodeLED(tt.update)
			if err != nil {
				t.Fatalf("EncodeLED(%+v) error = %v", tt.update, err)
			}
			if len(msgs) != 1 {
				t.Fatalf("EncodeLED(%+v) = %d messages, want 1", tt.update, len(msgs))
			}
			got := msgs[0]
			want := midi.Message{Status: 0xB0, Data1: tt.wantData1, Data2: tt.wantData2}
			if got.Status != want.Status || got.Data1 != want.Data1 || got.Data2 != want.Data2 {
				t.Errorf("EncodeLED(%+v) = %+v, want status=%#02x data1=%d data2=%#02x", tt.update, got, want.Status, want.Data1, want.Data2)
			}
		})
	}
}

func TestEncodeLEDRingIndexOutOfRange(t *testing.T) {
	c := NewXTouchMiniCodec()
	for _, idx := range []int{0, 9, -1} {
		update := LEDUpdate{Control: model.Control{Kind: model.ControlEncoder, Index: idx}, Mode: LEDModeFill, Position: 5}
		_, err := c.EncodeLED(update)
		if !errors.Is(err, ErrUnknownMessage) {
			t.Errorf("EncodeLED(index=%d) error = %v, want ErrUnknownMessage", idx, err)
		}
	}
}

func TestEncodeLEDButton(t *testing.T) {
	c := NewXTouchMiniCodec()

	tests := []struct {
		name         string
		update       LEDUpdate
		wantNote     byte
		wantVelocity byte
	}{
		{
			name:         "button 1 on -- top row, note 89, confirmed live",
			update:       LEDUpdate{Control: model.Control{Kind: model.ControlButton, Index: 1}, On: true},
			wantNote:     89,
			wantVelocity: ledButtonOnVelocity,
		},
		{
			name:         "button 1 off",
			update:       LEDUpdate{Control: model.Control{Kind: model.ControlButton, Index: 1}, On: false},
			wantNote:     89,
			wantVelocity: 0,
		},
		{
			name:         "button 9 on -- bottom row, note 87, confirmed live",
			update:       LEDUpdate{Control: model.Control{Kind: model.ControlButton, Index: 9}, On: true},
			wantNote:     87,
			wantVelocity: ledButtonOnVelocity,
		},
		{
			name:         "upper side button on -- note 84, confirmed live",
			update:       LEDUpdate{Control: model.Control{Kind: model.ControlSideButton, Index: 1}, On: true},
			wantNote:     84,
			wantVelocity: ledButtonOnVelocity,
		},
		{
			name:         "lower side button on -- note 85, confirmed live",
			update:       LEDUpdate{Control: model.Control{Kind: model.ControlSideButton, Index: 2}, On: true},
			wantNote:     85,
			wantVelocity: ledButtonOnVelocity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs, err := c.EncodeLED(tt.update)
			if err != nil {
				t.Fatalf("EncodeLED(%+v) error = %v", tt.update, err)
			}
			if len(msgs) != 1 {
				t.Fatalf("EncodeLED(%+v) = %d messages, want 1", tt.update, len(msgs))
			}
			got := msgs[0]
			if got.Status != 0x90 || got.Data1 != tt.wantNote || got.Data2 != tt.wantVelocity {
				t.Errorf("EncodeLED(%+v) = %+v, want status=0x90 data1=%d data2=%d", tt.update, got, tt.wantNote, tt.wantVelocity)
			}
		})
	}
}

func TestEncodeLEDNoLEDControls(t *testing.T) {
	c := NewXTouchMiniCodec()

	tests := []struct {
		name    string
		control model.Control
	}{
		{"fader has no LED", model.Control{Kind: model.ControlFader, Index: 1}},
		{"encoder push has no LED -- confirmed live, ring is the only indicator", model.Control{Kind: model.ControlEncoderPush, Index: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.EncodeLED(LEDUpdate{Control: tt.control})
			if !errors.Is(err, ErrNoLED) {
				t.Errorf("EncodeLED(%+v) error = %v, want ErrNoLED", tt.control, err)
			}
		})
	}
}

func TestEncodeLEDUnknownControlKind(t *testing.T) {
	c := NewXTouchMiniCodec()
	_, err := c.EncodeLED(LEDUpdate{Control: model.Control{Kind: "bogus", Index: 1}})
	if !errors.Is(err, ErrUnknownMessage) {
		t.Errorf("EncodeLED(bogus kind) error = %v, want ErrUnknownMessage", err)
	}
}

// TestControlToNoteInvertsNoteToControl guards against the two tables
// drifting apart: EncodeLED's button path depends on controlToNote
// being the exact inverse of noteToControl (Decode's table).
func TestControlToNoteInvertsNoteToControl(t *testing.T) {
	if len(controlToNote) != len(noteToControl) {
		t.Fatalf("controlToNote has %d entries, noteToControl has %d", len(controlToNote), len(noteToControl))
	}
	for note, ctrl := range noteToControl {
		gotNote, ok := controlToNote[ctrl]
		if !ok {
			t.Errorf("controlToNote is missing an entry for %+v (noteToControl[%d])", ctrl, note)
			continue
		}
		if gotNote != note {
			t.Errorf("controlToNote[%+v] = %d, want %d", ctrl, gotNote, note)
		}
	}
}
