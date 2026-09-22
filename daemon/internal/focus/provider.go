// Package focus tells knobd which application currently owns the
// focused window, for TargetFocused and knob.assign_focused_app (see
// model.ActionKnobAssignFocusedApp).
//
// This system runs KDE Plasma on Wayland, where there is no portal or
// global API for "what window is focused" (unlike X11's
// xdotool/wmctrl, confirmed absent from this system during planning).
// The only supported route, and the one kdotool itself uses, is a
// small KWin script (embedded at daemon/internal/focus/script,
// materialized to disk and loaded via D-Bus at startup — see kwin.go)
// that subscribes to workspace.windowActivated and calls back into a
// D-Bus service knobd exposes itself. Confirmed live during M06
// planning: a KWin script's callDBus can call out to an arbitrary
// service, not just back into KWin itself — see
// specs/adr/0003-focus-tracking-via-kwin-script.md for the evidence
// and for why this approach was chosen over the alternatives.
package focus

import "context"

// AppInfo identifies the application that owns a focused window, using
// whatever KWin exposes — which, like audio.Stream's properties, should
// be treated as a bag of best-effort hints rather than a fully reliable
// key. ResourceClass is generally the most useful field for matching
// against model.AppMatcher.DesktopIDs.
//
// Caption is deliberately never used for matching anywhere in this
// package or in audio.ResolveFocused: window titles are volatile (a
// browser's caption changes with every page), and feeding one into
// model.AppMatcher.MediaNameRx would be an unbounded false-positive
// generator. It exists only for a human-facing debug/status display.
type AppInfo struct {
	PID           int
	ResourceClass string
	DesktopFileID string
	Caption       string
	// Binary is filled by a real Provider (never by FakeProvider unless
	// a test sets it explicitly) with the base name of
	// /proc/<PID>/exe's target — a single readlink, done once per
	// focus change, off the engine's hot path. It exists specifically
	// for the case ResourceClass/DesktopFileID alone miss: a browser's
	// window resourceClass is often a different string than the
	// binary its audio stream reports (e.g. Brave: resourceClass
	// "brave-browser", binary "brave") — see
	// specs/milestones/M06-focus-tracking.md and
	// daemon/internal/audio/focus.go.
	Binary string
}

// Provider reports focus changes.
type Provider interface {
	// Watch streams an AppInfo every time the focused window changes,
	// until ctx is canceled. The channel is closed when Watch stops
	// delivering events. Implementations must keep the channel open,
	// self-healing any backend reconnect internally (e.g. kwinProvider
	// reinstalling its KWin script after a compositor restart) —
	// engine.Engine calls Watch exactly once per Run and does not retry
	// it; only ctx cancellation should ever close the channel.
	Watch(ctx context.Context) (<-chan AppInfo, error)

	// Current returns the currently focused application without
	// waiting for a change, for resolving TargetFocused/
	// knob.assign_focused_app at the moment a gesture fires rather than
	// only on the next focus change.
	//
	// Current must not block on I/O: it is called inline on the
	// engine's run goroutine (see engine/resolver.go's resolveFocused),
	// so an implementation must answer from an already-cached value —
	// the last AppInfo it received, or the zero value before the
	// first one arrives — never a live round trip to KWin or D-Bus.
	Current(ctx context.Context) (AppInfo, error)

	Close() error
}

// New connects to the window manager's focus-tracking mechanism.
// TODO(M06): implement kwinProvider; see the package doc comment.
func New(ctx context.Context) (Provider, error) {
	return nil, errNotImplemented("focus.New")
}
