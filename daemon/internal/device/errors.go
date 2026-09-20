package device

import (
	"errors"
	"fmt"
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

func errNotImplemented(fn string) error {
	return fmt.Errorf("device: %s is not implemented yet (see specs/milestones/M05-led-feedback.md)", fn)
}
