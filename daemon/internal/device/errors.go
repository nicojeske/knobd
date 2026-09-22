package device

import (
	"errors"
)

// ErrStandardMode means traffic arrived that cannot be Mackie Control
// mode (see xtouch.go's checkMCMode): the unit is presumably in
// "Standard" mode, sending absolute CC instead of relative deltas.
// specs/reference/xtouch-mini-midi-map.md's Standard-mode section has
// more on why the codec can say "this isn't MC mode" confidently but
// can't say "this is exactly Standard mode" with the same confidence —
// this unit has never been switched out of MC mode to confirm the
// numbers.
var ErrStandardMode = errors.New("device: controller does not appear to be in Mackie Control mode")

// ErrUnknownMessage means the message doesn't match anything in the
// X-Touch Mini's MC-mode map at all — not a channel/controller mismatch
// specific enough to suspect Standard mode, just traffic this codec has
// no entry for.
var ErrUnknownMessage = errors.New("device: message does not match the X-Touch Mini MC-mode map")

// ErrNoLED means EncodeLED was asked to light a Control that has no LED
// at all on this unit: the fader, or an encoder's own push (its ring is
// the only indicator for that encoder). Confirmed by sending full
// velocity to an encoder push's note and observing no response
// anywhere — see specs/reference/xtouch-mini-midi-map.md's LED section.
var ErrNoLED = errors.New("device: control has no LED on this unit")
