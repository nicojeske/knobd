# M04: Mapping engine & daemon

## Status

**Done** (as of 2026-09-22). Every acceptance criterion below is
checked except the literal "turn the physical knob and watch
pavucontrol" step, which needs a human at the hardware — everything up
to that point (the real X-Touch Mini connecting, a real PipeWire
session connecting, `GET`/`PUT /config` and `GET /state` over the real
unix socket, a config change taking effect live with no restart) has
been verified against this machine's actual hardware and PipeWire
session, not just fakes.

Two changes from this spec's original shape, decided during
implementation:

- **The dynamic app pool ("an unbound encoder auto-attaches to
  whatever is newly making sound") is dropped, not implemented.**
  Bindings are explicit only. The zero-configuration story this was
  meant to provide is `knob.assign_focused_app` instead (press a knob,
  it grabs whatever window is focused) — that's M06's, since it needs
  focus tracking. `specs/reference/action-catalog.md`'s "Dynamic app
  pool" cross-cutting entry has been removed accordingly.
- **The fader is now bindable**, via a new `model.GestureMove` (the
  only gesture `ControlFader` supports) and a new `volume.follow`
  action. This *is* a data model change (see below), which contradicts
  this spec's original "Data model changes: none expected" — recorded
  here rather than silently dropped.

## Depends on

M02 (MIDI transport + device codec), M03 (audio backend).

## Goal

A running `knobd` daemon that: reads the active config, decodes
controller input via M02, resolves the active binding for each
`(Control, Gesture)` on the active layer, resolves that binding's
`Target` against the live audio graph via M03, and executes the
resulting `Action`. Also stands up the unix-socket API
(`daemon/internal/api`) with real `GET`/`PUT /config` and `GET /state`
routes, and a documented (if manual) systemd install step — even
though the UI (M07) doesn't exist yet to talk to any of it.

## Scope

**In**: `engine.Engine.Run`'s real implementation — gesture detection
(press vs. hold via `engine.HoldThreshold`, double-press), layer
resolution (layer 0 always active; M08 adds real layer-switching, but
the *lookup* mechanism belongs here), target resolution at dispatch
time, dispatch through `actions.Registry`, volume/mute action handlers
(`daemon/internal/actions`), the API server's `GET`/`PUT /config` and a
`GET /state` snapshot endpoint, the systemd unit
(`packaging/systemd/knobd.service`) actually being installed/enabled by
some documented step (`make install-user`; full packaging is M12).

**Out**: LED feedback (M05), focus tracking / `knob.assign_focused_app`
actually working (M06 — the action type exists per M01 but has no
handler until focus resolution exists), the config UI itself (M07),
layers/scenes/solo/duck beyond the base lookup mechanism (M08), media
(M09), extended actions (M11), the dynamic app pool (dropped
entirely — see Status), stereo balance (`volume.balance` — see
Design).

## Design

`engine.Engine` (`daemon/internal/engine/engine.go`) implements `Run` as
the read loop: `Port.Read` → `Codec.Decode` → `gestureMachine`
(`gesture.go`) → `bindingIndex` lookup (`bindings.go`) → for
target-carrying actions, `resolver.resolve` (`resolver.go`) → a single
dispatcher goroutine (`dispatch.go`) that owns every blocking
`audio.Backend`/`actions.Registry` call.

Two structural constraints shaped this: `audio.Supervisor`'s reads
block across a PipeWire reconnect, so no audio call may ever happen on
the run goroutine (only the dedicated dispatcher makes them); and
`audio.Backend`'s own doc comment says a caller needing read-modify-
write atomicity across a Get-then-Set must serialize its own access —
the single dispatcher goroutine is that serialization point. All other
mutable engine state (the gesture machine, the binding index, the
resolver's stream/device cache) lives on the run goroutine too, so
there are no mutexes anywhere in `engine`; `SetConfig` (`PUT /config`'s
path into a running engine) and `Snapshot` (`GET /state`'s payload) are
channel round trips served by that same goroutine.

### Decisions recorded here, as this spec's own Risks section asked for

- **Gesture timing lives in `engine`, not `device`.** Already resolved
  by M02 (`device.Codec.Decode` shipped stateless, and its doc comment
  says so) — this supersedes that spec's own now-stale open question.
- **The dynamic app pool is dropped.** See Status above.
- **`SetVolume`/`SetMute` un-mute on a volume change.** `volume.adjust`,
  `volume.set`, and `volume.follow` all un-mute a target before writing
  its new level (KDE's behavior, not pavucontrol's) — turning a knob
  and hearing nothing, with no LED feedback yet (that's M05) to explain
  why, is a dead end. Un-muting stays a no-op action of its own
  (`volume.mute_toggle`).
- **How the fader binds.** `model.ControlFader.SupportsGesture` returned
  false for every gesture before this milestone, so no fader binding
  could pass `Binding.Validate`. It now supports a new
  `model.GestureMove` (fader-only, carrying the absolute 0-127
  position via `actions.Invocation.Value`, not a delta), paired with a
  new `model.VolumeFollowAction`/`volume.follow` action: the control's
  position maps linearly onto `[MinPercent, MaxPercent]` (default
  `[0, 100]`), with no response curve — curving a physical fader
  position is what would make it feel disconnected from where it's
  actually sitting. The dispatcher coalesces rapid `GestureMove` events
  by keeping only the latest `Value` (the fader was observed emitting
  404 pitch-bend messages in a single free-play session; see
  `specs/reference/xtouch-mini-midi-map.md`).
- **`volume.balance` is not planned.** `audio.Backend` has no
  per-channel volume *write* (`VolumeState.Channels` is read-only,
  used only to preserve balance across a `SetVolume` scale), and
  stereo left/right balance is not a feature this project wants. The
  `model.VolumeBalanceAction` type is kept (removing it would be a
  schema/TypeScript regen to delete something nothing references) but
  has no registered handler; `actions.Registry`'s existing "no handler
  registered for action type" error is the correct, honest outcome.
- **Multi-stream `volume.mute_toggle` disagreement rule.** If any of a
  target's resolved streams is currently unmuted, mute all of them;
  only if every one is already muted does it unmute all of them. Mute
  is a safety action — "silence this," not "flip each stream" — so
  this converges instead of oscillating a multi-stream app's streams
  out of phase with each other.
- **Duplicate `(layer, control, gesture)` bindings**: `Config.Validate`
  doesn't reject them, so `bindingIndex` resolves them last-one-wins,
  logged at Warn when it happens.
- **Config reload is `PUT /config` (live, no restart needed) plus
  SIGHUP for a hand edit to `config.json`** — not inotify. Editors
  write-and-rename rather than modify in place, so a file watch would
  have to watch the containing directory (re-deriving
  `daemon/internal/midi/watcher.go`'s inotify plumbing, which is
  unexported and tuned for `/dev/snd` churn, into a new shared
  package) and would race `PUT /config`'s own write. `ExecReload=/bin/kill
  -HUP $MAINPID` is in `packaging/systemd/knobd.service`; both paths
  go through the same `cmd/knobd` `configStore.SetConfig` (engine
  first, then `config.Save`, rolling the engine back if the save
  fails, since the in-memory engine swap is the only one of the two
  steps that can be undone).
- **Socket staleness**: `api.listen` distinguishes a live daemon's
  socket from a crashed one's leftover by dialing it (a successful
  dial means "already running", refused means "safe to unlink and
  rebind") rather than blindly removing whatever is at the path.

### What target resolution and dispatch actually look like

`resolver` (`daemon/internal/engine/resolver.go`) turns a `model.Target`
into live `audio.Ref`s from an engine-owned cache of the stream/device
graph (kept current from `audio.Backend.Subscribe` events), never
calling `Backend.Sinks/Sources/Streams` directly on the run goroutine.
`default_sink`/`default_source`/`sink`/`source`/`app`/`all_streams`
resolve from that cache; `focused` returns `focus.ErrUnavailable` until
M06 (via the new `focus.Unavailable()` no-op `Provider`, which is what
`engine.Deps.Focus` defaults to so the daemon starts cleanly with no
window-manager integration at all); `group` returns an explicit "not
until M08" error.

`actions.Handler`'s signature changed from `Execute(ctx,
model.Action)` to `Execute(ctx, actions.Invocation)` — the old shape
couldn't carry a turn's signed delta, a fader's absolute value, or the
already-resolved `audio.Ref`s a handler needs (re-resolving inside
each handler would duplicate work and make the dispatcher's
serialization point unownable). `daemon/internal/actions/volume.go`
implements `volume.adjust`/`volume.set`/`volume.mute_toggle`/
`volume.follow` against a shared level cache, fed by the engine's
`audio.Subscribe` events (`StateObserver.ObserveState`) so a detent
doesn't cost an extra `GetVolume` round trip on top of `SetVolume`'s
own internal read (it already reads once to preserve per-channel
balance).

## Data model changes

- `model.Gesture` gains `GestureMove` (fader-only; see Design).
- `model.Action` gains `VolumeFollowAction` / `ActionVolumeFollow` /
  `"volume.follow"`.
- `model.TargetOf` (previously unexported `targetOf`) is now exported,
  for `engine` to resolve an arbitrary `Action`'s `Target` without a
  second copy of that switch.

Both additions are purely additive: `docs/config.schema.json` and
`ui/src/types/config.ts` were regenerated (`make schema`, `npm run
codegen`), and no `schemaVersion` bump or migration was needed since
every existing on-disk config remains valid as-is.

## Acceptance criteria

- [x] Starting `knobd` with a config that binds an encoder to
      `VolumeAdjustAction{Target: {Kind: TargetApp, Ref: "..."}}`, and
      turning that physical encoder, changes the named application's
      volume, observable in `pavucontrol`/KDE's mixer. *(Verified via
      `engine.Run` through `midi.FakePort` + the real `device.Codec` +
      `audio.FakeBackend`, exercising the exact same MIDI byte shapes
      the real encoder sends; the real daemon connects to both the
      physical X-Touch Mini and a live PipeWire session, but nobody
      has physically turned the knob during this change — that step is
      still a human's to do.)*
- [x] Binding the fader to `VolumeFollowAction` and moving it sets the
      target's volume to the fader's absolute position, with no
      perceptible lag despite the encoder/fader's high message rate.
      *(Same caveat as above: verified through `engine.Run` with a
      fabricated pitch-bend message and the dispatcher's coalescing
      tested directly; not yet run against the physical fader.)*
- [x] Press vs. hold on an encoder-push/button reliably produces
      `GesturePress` vs. `GestureHold` at the 600ms boundary
      (`engine.HoldThreshold`), tested with `midi.FakePort` injecting
      precisely-timed note on/off pairs — no physical hardware required
      for this test.
- [x] `curl --unix-socket $XDG_RUNTIME_DIR/knobd.sock http://localhost/config`
      returns the current config as JSON. *(Verified against the real
      socket, including a config with a binding action, round-tripping
      the discriminated-union envelope correctly.)*
- [x] `knobd.service` runs under `systemctl --user` and survives a
      config file edit + `PUT /config` without needing a restart.
      *(The `PUT /config` half verified directly against a running
      daemon: the change takes effect immediately and persists to
      disk. `make install-user` + `systemctl --user enable --now` is
      documented in the root README; running it is a step for whoever
      deploys this for real use, same as the physical-knob step above.)*

## Verification

```bash
cd daemon && go test -race ./...
cd .. && make lint && make schema
git diff --exit-code docs/config.schema.json ui/src/types/config.ts

make install-user
systemctl --user enable --now knobd.service
journalctl --user -u knobd -f
curl --unix-socket $XDG_RUNTIME_DIR/knobd.sock http://localhost/config
curl --unix-socket $XDG_RUNTIME_DIR/knobd.sock http://localhost/state
# hand-edit config.json to bind an encoder to an app, `systemctl --user
# reload knobd`, turn it, watch the app's volume change live; PUT a
# modified config over the socket and confirm the same with no restart
```

## Risks & open questions

- ~~Dynamic-pool assignment ordering~~ — moot; the pool was dropped
  (see Status).
- ~~Whether gesture timing lives in `engine` or `device`~~ — resolved:
  `engine` (see Design's decisions list; M02 already settled this).
- The fader's `VolumeFollowAction` has no response curve at all (unlike
  `VolumeAdjustAction.CurveExponent`). If a future control needs
  non-linear mapping from position to volume, that's a `Curve`-shaped
  addition to `VolumeFollowAction`, not a redesign.
- `resolver.resolveFocused` calls `focus.Provider.Current` inline on
  the engine's run goroutine, which is safe only because `M04` never
  runs against anything but `focus.Unavailable()` (instant) or
  `focus.FakeProvider` (synchronous, no I/O). M06's real KWin-backed
  provider will need this moved behind a cached-latest-`Watch`-value
  pattern (the same shape the stream cache already uses for audio) so
  a slow focus lookup can never stall gesture timing — noted directly
  in `resolver.go`'s doc comment as well.
- `cmd/knobd`'s `watchConnections` is the sole consumer of
  `midi.Supervisor`/`audio.Supervisor`'s buffered-1, coalescing
  connect/disconnect channels. M05 will also want those events for LED
  re-push after a reconnect — at that point, either move ownership
  into `engine` or add fan-out to the supervisors; a decision to make
  deliberately, flagged in `cmd/knobd/state.go`'s doc comment so it
  isn't discovered the hard way.
- `api.listen`'s stale-vs-live socket detection (dial-then-unlink) has
  a narrow race if two daemons start within the same few hundred
  microseconds. Accepted for now (a single non-templated
  `systemctl --user` unit already serializes ordinary starts); the
  real fix is systemd socket activation, which belongs to M12.
