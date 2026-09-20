// Package focus tells knobd which application currently owns the
// focused window, for TargetFocused and knob.assign_focused_app (see
// model.ActionKnobAssignFocusedApp).
//
// TODO(M06): implement kwinProvider. This is the one part of knobd that
// cannot be done a "normal" way: this system runs KDE Plasma 6.7.5 on
// Wayland, where there is no portal or global API for "what window is
// focused" (unlike X11's xdotool/wmctrl, confirmed absent from this
// system during planning). The only supported route, and the one
// kdotool itself uses, is a small KWin script:
//   - Load packaging/kwin/knobd-focus.js via D-Bus:
//     org.kde.KWin /Scripting org.kde.kwin.Scripting.loadScript(path,
//     "knobd"), then org.kde.kwin.Scripting.start().
//   - The script subscribes to workspace.windowActivated and calls back
//     into a D-Bus service knobd exposes itself (register one under a
//     well-known name at startup), passing whatever KWin's window API
//     exposes (pid, resourceClass, desktopFileName, caption).
//   - Confirm early whether KWin scripts are allowed to call back out
//     via callDBus in this Plasma version — if not, the fallback is
//     having the script write to a FIFO or a small local file knobd
//     tails instead.
//
// See specs/adr/0003-focus-tracking-via-kwin-script.md for why this
// approach was chosen over alternatives.
package focus

import "context"

// AppInfo identifies the application that owns a focused window, using
// whatever KWin exposes — which, like audio.Stream's properties, should
// be treated as a bag of best-effort hints rather than a fully reliable
// key. ResourceClass is generally the most useful field for matching
// against model.AppMatcher.DesktopIDs.
type AppInfo struct {
	PID           int
	ResourceClass string
	DesktopFileID string
	Caption       string
}

// Provider reports focus changes.
type Provider interface {
	// Watch streams an AppInfo every time the focused window changes,
	// until ctx is canceled. The channel is closed when Watch stops
	// delivering events.
	Watch(ctx context.Context) (<-chan AppInfo, error)

	// Current returns the currently focused application without
	// waiting for a change, for resolving TargetFocused/
	// knob.assign_focused_app at the moment a gesture fires rather than
	// only on the next focus change.
	Current(ctx context.Context) (AppInfo, error)

	Close() error
}

// New connects to the window manager's focus-tracking mechanism.
// TODO(M06): implement kwinProvider; see the package doc comment.
func New(ctx context.Context) (Provider, error) {
	return nil, errNotImplemented("focus.New")
}
