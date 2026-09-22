package device

import (
	"fmt"

	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
)

// ledRingMaxPosition is the highest real LED position on an encoder
// ring: 11 of the ring's 13 physical segments are individually
// addressable, confirmed live via `knobd calibrate-leds` (values 11,
// 12, and 15 all rendered identically — the hardware itself clamps).
// See specs/reference/xtouch-mini-midi-map.md's LED section.
const ledRingMaxPosition = 11

// ledButtonOnVelocity is the Note On velocity EncodeLED uses for a
// button LED's "on" state. Velocities 3-127 all render as solid on;
// 127 is chosen for maximum brightness/consistency. Velocities 1-2
// blink instead — confirmed live, the opposite of the pre-verification
// guess this package originally carried. Nothing in knobd uses
// blinking yet.
const ledButtonOnVelocity = 127

// EncodeLED implements Codec. It returns ErrNoLED for a Control with no
// LED at all on this unit (the fader, an encoder's own push), and an
// error wrapping ErrUnknownMessage for a Control this codec doesn't
// recognize (an out-of-range index, or a kind that doesn't exist on the
// X-Touch Mini). See specs/reference/xtouch-mini-midi-map.md's LED
// section for how the byte-level encoding below was confirmed.
func (xtouchMiniCodec) EncodeLED(update LEDUpdate) ([]midi.Message, error) {
	switch update.Control.Kind {
	case model.ControlEncoder:
		cc, ok := ringCC(update.Control.Index)
		if !ok {
			return nil, fmt.Errorf("device: encoder ring index %d out of range [1,8]: %w", update.Control.Index, ErrUnknownMessage)
		}
		pos := update.Position
		if pos < 0 {
			pos = 0
		}
		if pos > ledRingMaxPosition {
			pos = ledRingMaxPosition
		}
		value := byte(update.Mode)<<4 | byte(pos)
		return []midi.Message{{Status: 0xB0, Data1: cc, Data2: value}}, nil

	case model.ControlButton, model.ControlSideButton:
		note, ok := controlToNote[update.Control]
		if !ok {
			return nil, fmt.Errorf("device: no LED note mapped for %+v: %w", update.Control, ErrUnknownMessage)
		}
		velocity := byte(0)
		if update.On {
			velocity = ledButtonOnVelocity
		}
		return []midi.Message{{Status: 0x90, Data1: note, Data2: velocity}}, nil

	case model.ControlEncoderPush, model.ControlFader:
		return nil, fmt.Errorf("device: %s has no LED on this unit: %w", update.Control.Kind, ErrNoLED)

	default:
		return nil, fmt.Errorf("device: unrecognized control kind %q: %w", update.Control.Kind, ErrUnknownMessage)
	}
}
