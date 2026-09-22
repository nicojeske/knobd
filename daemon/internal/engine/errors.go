package engine

import (
	"errors"
	"fmt"
)

func errNotImplemented(fn string) error {
	return fmt.Errorf("engine: %s is not implemented yet (see specs/milestones/M04-mapping-engine-daemon.md)", fn)
}

// errTargetUnsupported is wrapped into the error resolve returns for a
// model.TargetGroup — group resolution is M08's job (union of every
// matcher in the group, per its Design section), not M04's.
var errTargetUnsupported = errors.New("engine: target kind not supported yet")

// errAudioSubscriptionClosed is the fatal error Run returns when
// audio.Backend.Subscribe's channel closes while ctx is still live: per
// Subscribe's doc comment that only happens on a genuine backend
// failure, never a graceful reconnect (audio.Supervisor keeps
// subscriptions alive across those, emitting audio.EventResync
// instead).
var errAudioSubscriptionClosed = errors.New("engine: audio event subscription closed unexpectedly")
