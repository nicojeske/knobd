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
| `volume.adjust` — the default | Has a type — [M03](../milestones/M03-audio-control.md)/[M04](../milestones/M04-mapping-engine-daemon.md) |
| `volume.balance` — L/R pan | Has a type — M03 |
| `media.seek` | Has a type — [M09](../milestones/M09-media-transport-mpris.md) |
| ★ `app.cycle` — scroll through currently-playing apps like a real mixer's channel strip | Planned — M08 |
| `sink.cycle` (scroll rather than one-button-per-device) | Planned — [M11](../milestones/M11-extended-actions.md) |
| `brightness.adjust` | Planned — M11 |
| `scroll.emulate` | Planned — M11 |
| `desktop.switch` | Planned — M11 |

## Audio buttons

| Action | Status |
|---|---|
| `volume.mute_toggle` | Has a type — M03/M04 |
| `volume.set` — jump to a preset level | Has a type — M03 |
| ★ `audio.solo_toggle` — mute everything but the target | Has a type — [M08](../milestones/M08-layers-groups-scenes.md) |
| ★ `audio.duck_hold` — while held, drop everything except the target to a low percentage | Has a type — M08 |
| `audio.move_to_sink` — send an app's audio to another output | Planned — M11 |
| ★ `sink.cycle_default` — headphones ↔ speakers ↔ HDMI on one button | Has a type — M11 |
| `mic.mute_toggle` | Covered by `volume.mute_toggle` with a `default_source`/`source` target |
| ★ `mic.push_to_talk` | Has a type — M11 |
| ★ `mic.push_to_mute` | Has a type — M11 |
| `audio.mute_all` | Covered by `volume.mute_toggle` with an `all_streams` target |

## Scenes

| Action | Status |
|---|---|
| ★ `scene.apply` — recall a saved mix (e.g. "Meeting": music 10%, Discord 100%, mic live) | Has a type — M08 |
| ★ `scene.save` — overwrite a scene with the current live mix | Has a type — M08 |

Short press recalls, long press overwrites — the same short/hold
gesture split used for `knob.assign_focused_app` — is the intended UX,
decided at M08 design time, not baked into the action types themselves
(a binding's `Gesture` already carries that distinction).

## Routing & binding

| Action | Status |
|---|---|
| ★ `knob.assign_focused_app` — bind the triggering encoder to whatever app is currently focused | Has a type — M04/[M06](../milestones/M06-focus-tracking.md) |
| `knob.clear` — remove the triggering control's binding | Has a type — M04 |
| `knob.lock_toggle` — stop a knob responding to turns, to avoid accidental nudges | Has a type — M04 |
| ★ `layer.momentary` — active only while held (side buttons, by default) | Has a type — M08 |
| ★ `layer.latch` — active until switched again | Has a type — M08 |
| `layer.cycle` — advance through a fixed layer order | Has a type — M08 |

## Media (MPRIS)

| Action | Status |
|---|---|
| `media.play_pause` / `next` / `previous` / `shuffle_toggle` / `repeat_cycle` | Has a type (`MediaTransportAction` with a `MediaCommand`) — M09 |
| `media.target_cycle` — choose which player the transport controls | Planned — M09 |
| `media.now_playing` — notification on track change | Planned — M09 |

## Spotify (Web API, beyond what MPRIS can do)

| Action | Status |
|---|---|
| ★ `spotify.like_toggle` | Planned — [M10](../milestones/M10-spotify-web-api.md) |
| ★ `spotify.add_to_playlist` — one playlist per button | Planned — M10 |
| `spotify.remove_from_playlist` | Planned — M10 |
| `spotify.start_playlist` | Planned — M10 |
| `spotify.queue_track` | Planned — M10 |
| `spotify.transfer_playback` | Planned — M10 |

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

- ★ **Dynamic app pool** — an encoder with no explicit binding
  auto-attaches to whatever is newly making sound (newest first),
  releasing when that stream ends. Makes the mixer useful with zero
  configuration. Planned for M04.
- ★ **LED ring as the display** — the ring shows volume, the button LED
  shows mute state, so the surface communicates without looking at a
  screen. Planned for M05, deliberately scheduled early rather than as
  late polish.
