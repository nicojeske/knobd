package media

import "errors"

// ErrUnavailable is returned by Unavailable's Backend, for a system
// where MPRIS support couldn't be started at all (see New's doc
// comment) -- mirrors focus.ErrUnavailable.
var ErrUnavailable = errors.New("media: MPRIS support is not available (see specs/milestones/M09-media-transport-mpris.md)")

// ErrNoPlayer is returned when a command has no player to send to: an
// empty PlayerRef with nothing currently selected, or a non-empty one
// that matches no known, non-ignored player.
var ErrNoPlayer = errors.New("media: no matching MPRIS player is running")

// ErrUnsupported is returned when a player is currently known but
// doesn't support the requested operation: CanControl false for
// play/pause/next/previous, CanSeek false for media.seek, or a nil
// Shuffle/empty LoopStatus for the corresponding transport command.
var ErrUnsupported = errors.New("media: player does not support this command")
