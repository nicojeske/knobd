# M06: Focus tracking

## Status

Not started.

## Depends on

M04 (a running engine for `TargetFocused`/`knob.assign_focused_app` to
plug into).

## Goal

`model.TargetFocused` resolves to whatever application currently owns
the focused window, live, and pressing-and-holding a knob
(`model.ActionKnobAssignFocusedApp`) rebinds that knob to it — the
feature specifically requested for this project.

## Scope

**In**: `focus.New`'s real KWin-backed implementation, the KWin script
(`packaging/kwin/knobd-focus.js`), the daemon's own D-Bus callback
service, resolving a `focus.AppInfo` to a `model.AppMatcher`/live
`audio.Stream`s (reusing/extending `audio.Resolve`'s matching against
`DesktopIDs`), wiring `ActionKnobAssignFocusedApp`'s handler into
`actions.Registry`.

**Out**: any UI for reviewing/editing the resulting binding — that's
M07. Multi-monitor/multi-desktop nuances beyond "whatever KWin reports
as activated" are not a goal unless testing surfaces a real problem.

## Design

**First and most important task**: resolve the open question in
ADR 0003 — can a KWin script actually `callDBus` *out* to an arbitrary
service, or only be called *into*? Spend the first work session on this
specifically, with the FIFO/file-tail fallback (also described in the
ADR) ready to go if the answer is no. Don't build the rest of the
milestone on an unverified assumption here.

Once that's settled: implement `packaging/kwin/knobd-focus.js` per its
header comment (subscribe to `workspace.windowActivated`, report
`resourceClass`/`caption`/pid-if-available), and `focus.New`'s KWin-
backed `Provider` (loading the script via `org.kde.kwin.Scripting`,
running the daemon's own D-Bus service to receive callbacks, exposing
`Watch`/`Current`).

Resolving a `focus.AppInfo` to actual audio streams: match
`ResourceClass` against `model.AppMatcher.DesktopIDs` (extend
`audio.Resolve` or add a sibling resolver — decide during
implementation) with the same `/proc/<pid>/exe` fallback path used for
audio-side resolution in M03, since a browser's focused-window PID is
often not the same PID that owns its audio stream (parent/child process
relationship) — walk the process tree if a direct PID match fails.

Implement `ActionKnobAssignFocusedApp`'s handler: on `GestureHold` of an
encoder-push, resolve the currently-focused app via `focus.Provider
.Current`, create or reuse an `AppMatcher` for it, and rewrite that
encoder's binding in the active profile to a fresh `VolumeAdjustAction`
targeting it — then persist the updated config (M04's config-save path).

## Data model changes

None expected beyond what M01 scaffolded
(`model.ActionKnobAssignFocusedApp`, `TargetFocused`).

## Acceptance criteria

- [ ] Confirmed whether KWin's `callDBus` supports calling out to an
      arbitrary service in this Plasma version; ADR 0003 updated with
      the answer either way.
- [ ] `focus.Provider.Watch` delivers an event when switching focus
      between two different applications' windows.
- [ ] Long-pressing an encoder-push bound to nothing (or to something
      else) rebinds it to the focused application and that binding
      persists across a daemon restart.
- [ ] The browser-audio-PID-mismatch case (focused window's PID differs
      from its audio stream's PID) is handled via the process-tree
      fallback and verified against Brave (installed on the dev
      machine) specifically.

## Verification

Physical/manual: focus different applications' windows and confirm
`focus.Provider.Current` reports the right one (a debug endpoint or log
line is enough); long-press a knob while a specific app is focused and
confirm it starts controlling that app's volume, including after
restarting `knobd`.

## Risks & open questions

- The `callDBus` direction question (see Design) is the load-bearing
  risk for this entire milestone — everything else is comparatively
  routine once it's answered.
- KWin scripting API stability across Plasma versions is unverified
  beyond 6.7.5 (the dev machine's version).
