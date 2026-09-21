package audio

import "errors"

// ErrDisconnected means a write (SetVolume/SetMute) was attempted while
// Supervisor has no live connection to PipeWire. Unlike a read/
// enumeration call (which blocks across a reconnect — see Supervisor's
// doc comment), a write fails immediately: a volume change is a state
// push the caller can safely drop and re-send once reconnected, the same
// asymmetry midi.Supervisor uses for Write vs. Read.
var ErrDisconnected = errors.New("audio: not currently connected")
