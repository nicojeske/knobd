# ADR 0003: Focus tracking via a KWin script over D-Bus

**Status**: Accepted

## Context

`model.TargetFocused` and `knob.assign_focused_app` both need to know
which application currently owns the focused window. The development
machine — and the project's stated target — runs **KDE Plasma 6.7.5 on
Wayland**.

Wayland has no equivalent of X11's `_NET_ACTIVE_WINDOW` root-window
property: there is deliberately no protocol-level, cross-compositor way
for an arbitrary client to ask "what window is focused." Confirmed
during planning: `xdotool`, `wmctrl`, and `kdotool` (a Wayland-era
wrapper that itself depends on this same mechanism) are all **absent**
from this system, and even if `kdotool` were installed, understanding
what it actually does is the relevant design question, not just calling
it as a black box.

KWin — the compositor Plasma uses — exposes a **scripting API**
(`org.kde.kwin.Scripting` over D-Bus, confirmed present:
`qdbus6 org.kde.KWin /Scripting` lists `loadScript`/`start`) that can
load a small JavaScript file with access to `workspace.windowActivated`
and window properties like `resourceClass`. This is the same mechanism
`kdotool` itself is built on.

## Decision

Ship a KWin script (`packaging/kwin/knobd-focus.js`) that:

1. Subscribes to `workspace.windowActivated`.
2. On each activation, reads the activated window's available
   properties (`resourceClass`, `caption`, and a PID/desktop-file ID if
   KWin's API exposes one for the window).
3. Reports that information back to the daemon.

The daemon loads this script at startup via
`org.kde.KWin /Scripting org.kde.kwin.Scripting.loadScript(path, "knobd")`
followed by `.start()` — no manual installation step required of the
user beyond the script file existing on disk (handled by M12 packaging).

`daemon/internal/focus.Provider` is the abstraction boundary: nothing
outside this package (and the KWin script itself) knows or cares that
the mechanism is KWin-specific.

## Open question this ADR does not resolve

Whether a KWin script in Plasma 6.7 is actually permitted to call back
*out* to an arbitrary D-Bus service via `callDBus(...)` — as opposed to
only being callable *from* other D-Bus clients — was not confirmed
during planning (doing so would require registering a service and is a
state change, out of scope for a read-only planning session). **This is
the first thing M06 needs to verify**, per
`daemon/internal/focus/provider.go`'s TODO. If `callDBus` turns out not
to support this direction, the fallback — documented in the same TODO —
is to have the script write focus-change events to a FIFO or small file
that the daemon tails instead of using D-Bus for that leg of the round
trip.

## Alternatives considered

- **A portal-based approach** (XDG Desktop Portal's `org.freedesktop.
  portal.GlobalShortcuts` or similar): portals are the "proper" Wayland
  way to ask for user-mediated global capabilities, but there is no
  portal for "tell me the focused window's identity" as a background
  service — the closest ones require the app to already be in the
  foreground itself, which the daemon isn't.
- **Polling `/proc` for the process with focus via some other signal**:
  there is no such signal available without compositor cooperation on
  Wayland; this isn't X11.
- **Requiring X11/XWayland-only support** (using `xdotool` under
  XWayland): would silently fail to track native Wayland windows
  (increasingly the common case), and the tooling isn't even installed
  here. Rejected.

## Consequences

- Focus tracking is inherently **KDE/KWin-specific**. Supporting GNOME
  (Mutter has its own, different, extension-based mechanism) or another
  compositor would need a second `focus.Provider` implementation entirely
  — acceptable, since the project's stated environment is KDE Plasma.
- The daemon needs a small D-Bus service of its own (to be the callback
  target) — a new piece of surface area not otherwise needed by
  anything else in the daemon.
- Depends on KWin's scripting API remaining stable enough across Plasma
  versions; re-verify against `specs/reference/environment.md`'s KWin
  version if targeting a different one.
