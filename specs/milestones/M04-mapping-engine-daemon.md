# M04: Mapping engine & daemon

## Status

Not started. **This is the milestone that makes the project usable**:
turn a knob, an application's volume changes.

## Depends on

M02 (MIDI transport + device codec), M03 (audio backend).

## Goal

A running `knobd` daemon that: reads the active config, decodes
controller input via M02, resolves the active binding for each
`(Control, Gesture)` on the active layer, resolves that binding's
`Target` against the live audio graph via M03, and executes the
resulting `Action` — with the "encoder with no explicit binding
auto-attaches to whatever is newly making sound" dynamic-pool behavior
working by default, so the mixer is useful before any configuration has
happened. Also stands up the unix-socket API skeleton (`daemon/internal
/api`) and the systemd user unit, even though the UI (M07) doesn't exist
yet to talk to it.

## Scope

**In**: `engine.Engine.Run`'s real implementation — gesture detection
(press vs. hold via `engine.HoldThreshold`, double-press), layer
resolution (layer 0 always active; M08 adds real layer-switching, but
the *lookup* mechanism belongs here), target resolution at dispatch time,
dispatch through `actions.Registry`, the dynamic app pool, volume/mute
action handlers (`daemon/internal/actions`), the API server's basic
CRUD for config (`GET`/`PUT /config`) and a `GET /state` snapshot
endpoint, the systemd unit
(`packaging/systemd/knobd.service`) actually being installed/enabled by
some documented step (even a manual one — full packaging is M12).

**Out**: LED feedback (M05), focus tracking / `knob.assign_focused_app`
actually working (M06 — the action type exists per M01 but has no
handler until focus resolution exists), the config UI itself (M07),
layers/scenes/solo/duck beyond the base lookup mechanism (M08), media
(M09), extended actions (M11).

## Design

`engine.Engine` (`daemon/internal/engine/engine.go`) already has its
`Deps` (a `midi.Port`, `device.Codec`, `audio.Backend`, `focus.Provider`,
and `model.Config`) and `HoldThreshold` scaffolded — implement `Run` as
the read loop: `Port.Read` → `Codec.Decode` → gesture-timing state
machine → binding lookup against the active profile/layer → for
target-carrying actions, `audio.Resolve`/`Backend` calls → `actions.
Registry.Execute`.

Register volume-family handlers (`model.ActionVolumeAdjust`,
`ActionVolumeSet`, `ActionVolumeMuteToggle`, `ActionVolumeBalance`) with
the `actions.Registry` from M01 — these are the first real
`actions.Handler` implementations.

For the dynamic app pool: maintain a set of encoders with no explicit
binding in the active profile; when `audio.Backend.Subscribe` reports a
new stream, assign it to the next free pooled encoder (documented
ordering: e.g. first-come order, oldest pooled encoder gets the newest
stream — pick one and write it down here once decided) and release the
assignment when that stream disappears.

API server: implement `api.SocketPath` (mirroring `config.Path`'s
XDG-fallback pattern) and enough of `api.Server` to serve `GET /config`,
`PUT /config` (validating + saving via `daemon/internal/config`), and
`GET /state`. Full WebSocket push and the rest of the CRUD surface is
M07's job once there's a UI to actually consume it, but the socket
should exist and be inspectable with `curl --unix-socket` after this
milestone.

## Data model changes

None expected — this milestone consumes M01's model rather than
extending it, aside from whatever `GET /state` response shape the API
needs (a new type, but not a config-schema-affecting one).

## Acceptance criteria

- [ ] Starting `knobd` with a config that binds an encoder to
      `VolumeAdjustAction{Target: {Kind: TargetApp, Ref: "..."}}`, and
      turning that physical encoder, changes the named application's
      volume, observable in `pavucontrol`/KDE's mixer.
- [ ] An unbound encoder attaches to a newly-launched audio stream and
      controls its volume with no configuration.
- [ ] Press vs. hold on an encoder-push/button reliably produces
      `GesturePress` vs. `GestureHold` at the 600ms boundary
      (`engine.HoldThreshold`), tested with `midi.FakePort` injecting
      precisely-timed note on/off pairs — no physical hardware required
      for this test.
- [ ] `curl --unix-socket $XDG_RUNTIME_DIR/knobd.sock http://localhost/config`
      returns the current config as JSON.
- [ ] `knobd.service` runs under `systemctl --user` and survives a
      config file edit + `PUT /config` without needing a restart.

## Verification

```bash
cd daemon && go test ./internal/engine/... ./internal/actions/...
go run ./cmd/knobd --config ~/.config/knobd/config.json
# in another terminal: bind an encoder to an app via a hand-edited
# config, turn it, watch the app's volume change live
curl --unix-socket $XDG_RUNTIME_DIR/knobd.sock http://localhost/config
```

## Risks & open questions

- Dynamic-pool assignment ordering (which pooled encoder gets a new
  stream) needs a concrete, documented rule — "do something reasonable"
  isn't testable. Decide and record it here during implementation.
- Whether gesture timing lives in `engine` or `device` was left open by
  M02's spec — resolve consistently with whatever M02 actually shipped.
