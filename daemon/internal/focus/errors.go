package focus

import "errors"

// ErrUnavailable is returned by Unavailable's Provider from Current, for
// a system with no window-manager integration running at all (or one
// running before M06 shipped).
var ErrUnavailable = errors.New("focus: focus tracking is not available (see specs/milestones/M06-focus-tracking.md)")

// ErrNoSessionBus is returned by New when no D-Bus session bus is
// reachable at all -- knobd started outside a graphical session
// entirely. cmd/knobd treats this the same as any other focus-tracking
// startup failure: log a warning and fall back to Unavailable(), never
// a fatal error (see main.go's comment on why no backend being present
// at startup is treated as fatal).
var ErrNoSessionBus = errors.New("focus: no D-Bus session bus available")

// ErrNameTaken is returned by New when Options.BusName is already owned
// by another process on the session bus -- almost certainly another
// knobd instance already running. New deliberately does not attempt to
// steal the name (see kwinProvider's doc comment in kwin.go): a D-Bus
// connection dies with its owning process, so a name already held means
// a real second daemon, and reclaiming it would leave that other
// instance silently focus-blind instead of failing this one loudly.
var ErrNameTaken = errors.New("focus: bus name already owned by another process")
