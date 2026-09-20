# M11: Extended actions

## Status

Not started.

## Depends on

M04 (engine/dispatch). Independent of M05/M06/M08/M09/M10 — can be built
in parallel with any of them.

## Goal

Round out the action catalog with the remaining system-level actions
from `specs/reference/action-catalog.md`: the `shell.run` escape hatch,
output-device cycling, microphone push-to-talk/push-to-mute, KDE
Do-Not-Disturb and Night Light toggles, monitor brightness, and OBS
WebSocket control.

## Scope

**In**: handlers for `model.ActionShellRun`, `ActionSinkCycleDefault`,
`ActionMicPushToTalk`/`MicPushToMute` (already scaffolded in
`action.go`); new action types + handlers for `app.launch`,
`window.focus_app`, `kde.dnd_toggle`, `kde.nightlight_toggle`,
`desktop.goto`, `keys.send`, `obs.record_toggle`/`obs.scene_switch`
(all currently "Planned" in the action catalog).

**Out**: a UI for configuring OBS connection details or brightness
device selection beyond what M07's generic action-parameter editing
already provides — no bespoke screens unless a specific action turns
out to need one.

## Design

`shell.run`: execute `Command` via `sh -c` with the daemon's own
environment/privileges — no privilege escalation, ever. Per
`ShellRunAction`'s doc comment in `model/action.go`, the UI must always
display the literal command, never hide it behind a friendly label,
since it can do anything the user's shell can.

`sink.cycle_default`: use `audio.Backend` (M03) to read/set the system
default sink, cycling through `SinkNames` in order.

Mic push-to-talk/mute: `GestureHold`/`GestureRelease` toggling
`SetMute` on the default source — the "gesture, not a toggle" version of
`volume.mute_toggle`.

KDE-specific actions (`kde.dnd_toggle`, `kde.nightlight_toggle`,
`desktop.goto`): D-Bus calls against the relevant Plasma interfaces
(`org.kde.KWin` for `NightLight`/desktop switching — several are already
visible in `qdbus6 org.kde.KWin`'s introspection from planning; DND is a
notifications-settings toggle, likely via
`org.freedesktop.Notifications` or a Plasma-specific config key —
confirm the exact mechanism during implementation).

`obs.record_toggle`/`obs.scene_switch`: OBS WebSocket protocol (v5) —a
small client is enough; no need for a full SDK for two calls.

`keys.send`: synthesizing a keypress on Wayland has the same
"no universal API" problem as focus tracking (ADR 0003) — likely needs
either a KWin-script-based approach or `wtype`/`ydotool`-equivalent
functionality; investigate as part of this action's design rather than
assuming a library exists.

`app.launch`: straightforward `os/exec` of a configured command, or
launching via a `.desktop` file's `Exec` line through
`gio launch`/`kioclient5 exec` for more correct desktop-file semantics.

## Data model changes

New `ActionType` constants + structs for `app.launch`, `window.
focus_app`, `kde.dnd_toggle`, `kde.nightlight_toggle`, `desktop.goto`,
`keys.send`, `obs.record_toggle`, `obs.scene_switch` — following the
same registration pattern as every other action (see M01's design
note).

## Acceptance criteria

- [ ] `shell.run` executes an arbitrary command and the UI always shows
      it verbatim wherever the binding is displayed.
- [ ] `sink.cycle_default` audibly moves output between at least two
      real devices on the dev machine (built-in analog output and the
      Audeze Maxwell Dongle are both present).
- [ ] Push-to-talk/push-to-mute correctly track hold/release even for
      a hold longer than any expected gesture-timing edge case.
- [ ] Each new KDE/OBS/keys action does what it says, verified manually
      against the actual system state it claims to change.

## Verification

Manual, per action — there's no single end-to-end scenario for a
grab-bag milestone like this one. Exercise each action individually and
confirm its real-world effect.

## Risks & open questions

- `keys.send` on Wayland is the one item here with a genuinely open
  design question (see Design) rather than a routine D-Bus/exec call —
  don't assume it's as simple as the others until investigated.
- KDE Do-Not-Disturb's actual control mechanism (D-Bus interface vs.
  config file vs. `kwriteconfig`) isn't confirmed yet.
