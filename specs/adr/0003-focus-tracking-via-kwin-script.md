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

## Resolved: `callDBus` does call out to an arbitrary service

M06 confirmed this empirically, as its first work session, before
writing any production code:

- KDE's KWin scripting API documents a global `callDBus(QString
  service, QString path, QString interface, QString method, QVariant
  args..., QJSValue callback)`. `service` is a caller-supplied
  parameter, not fixed to `org.kde.KWin` — confirmed against
  `develop.kde.org`'s KWin scripting API reference.
- `/usr/lib/libkwin.so` (6.7.5, this machine) links
  `QDBusConnection::asyncCall` and
  `QDBusAbstractInterface::asyncCallWithArgumentList`, and contains the
  string `callDBus` — consistent with the documented signature actually
  being wired to an outbound async D-Bus call, not just callable-into
  scripting entry points.
- **Live end-to-end test**: a throwaway KWin script
  (`workspace.windowActivated.connect(...)`, reporting via `callDBus`
  with 5 arguments and no trailing callback) loaded via
  `org.kde.kwin.Scripting.loadScript`/`.start()`, against a standalone
  Go program holding a scratch D-Bus name
  (`io.github.njeske.knobdsmoke`) and exporting a `FocusChanged(string)`
  method. Switching focus between a terminal and a launched KCalc
  window produced exactly one `FocusChanged` call per switch, arriving
  with the correct `resourceClass`/`desktopFileId`/`caption`/`pid` for
  each window. `workspace.activeWindow` was also non-null at script
  load time, confirmed via the same mechanism (an unconditional initial
  report), which matters since there is no separate "give me the
  current window" call.
- **A caveat the live test also turned up**: `loadScript`'s return value
  is not a reliable failure signal. Given a nonexistent script path, it
  returned a plugin id (not the documented sentinel) and
  `isScriptLoaded` for that plugin name subsequently reported `true`.
  The daemon cannot rely on `loadScript`'s return value, or on
  `isScriptLoaded`, to detect a bad path or a script that failed to
  parse — only the absence of the load-time handshake report (the
  script's own unconditional first `callDBus` call) is diagnostic, so
  `focus.New` treats "no handshake within a few seconds" as the failure
  condition to warn on, not the loader calls' return values.

The FIFO/file-tail fallback this ADR previously described is dropped —
it is not needed, and no longer described anywhere in the codebase.

## Original open question (superseded, kept for history)

Whether a KWin script in Plasma 6.7 is actually permitted to call back
*out* to an arbitrary D-Bus service via `callDBus(...)` — as opposed to
only being callable *from* other D-Bus clients — was not confirmed
during planning (doing so would require registering a service and is a
state change, out of scope for a read-only planning session). See the
"Resolved" section above for the answer M06 found.

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
  anything else in the daemon. It uses
  `github.com/godbus/dbus/v5` (pure Go, no CGo — satisfies ADR 0001),
  the daemon's third direct dependency after `jfreymuth/pulse` and
  `invopop/jsonschema`.
- Depends on KWin's scripting API remaining stable enough across Plasma
  versions; re-verify against `specs/reference/environment.md`'s KWin
  version if targeting a different one. `activeWindow`/`normalWindow`/
  `desktopFileName` are KWin 6 property names (`activeClient` in KWin
  5) — a future major-version rename would leave the script running but
  silently reporting nothing useful, since `callDBus` failures are
  themselves silent; this is why `focus.New`'s liveness check is a
  received-handshake timeout, not a loader return value (see above).
- The KWin script is **embedded in the daemon binary** (`go:embed`) and
  materialized to `$XDG_RUNTIME_DIR` at startup, rather than installed
  to a fixed path by packaging. `packaging/kwin/knobd-focus.js` no
  longer exists; the canonical script lives at
  `daemon/internal/focus/script/knobd-focus.js`. This keeps ADR 0001's
  single-static-binary property intact and — since the script/daemon
  wire contract (the JSON payload shape) has no error-visible failure
  mode if the two drift — removes the possibility of a stale on-disk
  script left over from an older build being loaded by a newer daemon.
