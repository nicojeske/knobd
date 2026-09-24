# Action catalog

Every button/knob action brainstormed for knobd, grouped by family. ★
marks the ones judged genuinely high-value rather than merely possible —
weight these more heavily when a milestone has to cut scope.

"Has a type" means a concrete `model.Action` implementation already
exists in
[`daemon/internal/model/action.go`](../../daemon/internal/model/action.go)
(scaffolded with no handler behind it yet); "planned" means it's real
work for the milestone listed. Adding a type for something in "planned"
is in scope for that milestone; it doesn't need to happen earlier.

## Encoders (turn)

| Action | Status |
|---|---|
| `volume.adjust` — the default | Implemented — [M03](../milestones/M03-audio-control.md)/[M04](../milestones/M04-mapping-engine-daemon.md) |
| `volume.balance` — L/R pan | **Not planned.** `audio.Backend` has no per-channel volume write (`VolumeState.Channels` is read-only, used only to preserve balance across a `SetVolume`), and stereo balance isn't a feature this project wants. The type is kept (M01) but has no registered handler as of M04. |
| `media.seek` | Implemented — [M09](../milestones/M09-media-transport-mpris.md) |
| ★ `app.cycle` — scroll through currently-playing apps like a real mixer's channel strip | Not yet scheduled — M08's own scope turned out to be layers/groups/scenes/solo/duck only; this wasn't part of it |
| `sink.cycle` (scroll rather than one-button-per-device) | Planned — [M11](../milestones/M11-extended-actions.md) |
| `brightness.adjust` | Planned — M11 |
| `scroll.emulate` | Planned — M11 |
| `desktop.switch` | Planned — M11 |

## Fader (move)

The fader is a continuous control, not a button or an encoder: it
produces `model.GestureMove` (added in M04) with an absolute 0-127
position rather than a press or a relative turn.

| Action | Status |
|---|---|
| `volume.follow` — the target's volume tracks the fader's position between `MinPercent` and `MaxPercent`, no response curve | Implemented — M04 |

## Audio buttons

| Action | Status |
|---|---|
| `volume.mute_toggle` | Implemented — M03/M04 |
| `volume.set` — jump to a preset level | Implemented — M03/M04 |
| ★ `audio.solo_toggle` — mute everything but the target | Implemented — [M08](../milestones/M08-layers-groups-scenes.md) |
| ★ `audio.duck_hold` — while held, drop everything except the target to a low percentage | Implemented — M08 |
| `audio.move_to_sink` — send an app's audio to another output | Planned — M11 |
| ★ `sink.cycle_default` — headphones ↔ speakers ↔ HDMI on one button | Has a type — M11 |
| `mic.mute_toggle` | Covered by `volume.mute_toggle` with a `default_source`/`source` target |
| ★ `mic.push_to_talk` | Has a type — M11 |
| ★ `mic.push_to_mute` | Has a type — M11 |
| `audio.mute_all` | Covered by `volume.mute_toggle` with an `all_streams` target |

## Scenes

| Action | Status |
|---|---|
| ★ `scene.apply` — recall a saved mix (e.g. "Meeting": music 10%, Discord 100%, mic live) | Implemented — M08 |
| ★ `scene.save` — overwrite a scene with the current live mix | Implemented — M08 |

Short press recalls, long press overwrites — the same short/hold
gesture split used for `knob.assign_focused_app` — is the intended UX;
a binding's `Gesture` already carries that distinction, so this is a
config choice per binding, not something M08 baked into the action
types themselves.

## Routing & binding

| Action | Status |
|---|---|
| ★ `knob.assign_focused_app` — bind the triggering encoder to whatever app is currently focused | Has a type (M01) — [M06](../milestones/M06-focus-tracking.md) implements the handler; this is knobd's actual zero-configuration story now that the dynamic app pool (below) has been dropped |
| `knob.clear` — remove the triggering control's binding | Has a type (M01) — not yet scheduled; M04 left it unimplemented (no handler registered) |
| `knob.lock_toggle` — stop a knob responding to turns, to avoid accidental nudges | Has a type (M01) — not yet scheduled; M04 left it unimplemented (no handler registered) |
| ★ `layer.momentary` — active only while held (side buttons, by default) | Implemented — M08 |
| ★ `layer.latch` — active until switched again | Implemented — M08 |
| `layer.cycle` — advance through a fixed layer order | Implemented — M08 |

## Media (MPRIS)

| Action | Status |
|---|---|
| `media.play_pause` / `next` / `previous` / `shuffle_toggle` / `repeat_cycle` | Implemented (`MediaTransportAction` with a `MediaCommand`) — M09 |
| `media.target_cycle` — choose which player the transport controls | Implemented — M09 |
| `media.now_playing` — notification of the current track (button-triggered, not automatic on track change) | Implemented — M09 |

## Spotify (Web API, beyond what MPRIS can do)

| Action | Status |
|---|---|
| ★ `spotify.like_toggle` | Done — [M10](../milestones/M10-spotify-web-api.md) |
| ★ `spotify.add_to_playlist` — one playlist per button | Done — M10 |
| `spotify.remove_from_playlist` | Done — M10 |
| `spotify.start_playlist` | Done — M10 |
| `spotify.queue_track` | Done — M10 |
| `spotify.transfer_playback` | Done — M10 |
| `spotify.volume_adjust` — relative step on the active Spotify Connect device's volume, via the Web API (not the local mixer) | Done — M10 |
| `spotify.volume_set` — jump the active Spotify Connect device's volume to a preset level | Done — M10 |

## System

| Action | Status |
|---|---|
| ★ `shell.run` — escape hatch for anything not otherwise modeled | Has a type — M11 |
| `app.launch` | Planned — M11 |
| `window.focus_app` | Planned — M11 |
| `kde.dnd_toggle` | Planned — M11 |
| `kde.nightlight_toggle` | Planned — M11 |
| `desktop.goto` | Planned — M11 |
| `keys.send` — synthesize a keypress | Planned — M11 |
| `obs.record_toggle` / `obs.scene_switch` | Planned — M11 |

## Cross-cutting behaviours (not actions on a specific control)

- **Dynamic app pool — dropped.** The original idea (an encoder with no
  explicit binding auto-attaches to whatever is newly making sound) was
  planned for M04 but was rejected during that milestone's planning:
  mappings should be configured explicitly. `knob.assign_focused_app`
  (above, M06) is the intended zero-configuration path instead — press
  a knob, it grabs whatever window is currently focused.
- ★ **LED ring as the display** — the ring shows volume, the button LED
  shows mute state, so the surface communicates without looking at a
  screen. Planned for M05, deliberately scheduled early rather than as
  late polish.
