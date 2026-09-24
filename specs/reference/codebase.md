# Codebase orientation

What a new spec-planning session needs before touching code, and that
neither `docs/codemap.md` (generated signatures) nor a milestone spec
(what should be true when it ships) captures on their own: how the
pieces run together at runtime, how `cmd/knobd/main.go` wires them up,
the recipes for the changes that come up in almost every milestone, and
which milestone owns which package. Read this and `docs/codemap.md`
before spawning an Explore agent — spawn one only for what these two
don't answer (a function body, a specific bug, something this doc turns
out to be wrong about).

## Runtime data flow

```
/dev/snd/*  --Read()-->  midi.Port  --Decode()-->  device.Codec  --gesture-->  engine.Engine
                (midi.Supervisor            (device.Event)                (event loop, engine.go
                 reconnects on unplug)                                     Run())
                                                                                  |
                                                                     dispatchGesture / handleAudioEvent
                                                                                  v
                                                                          actions.Registry
                                                                       (handler for the bound
                                                                        ActionType, e.g.
                                                                        volumeHandlers,
                                                                        assignHandlers)
                                                                                  |
                                                   +------------------------------+------------------+
                                                   v                                                  v
                                          audio.Backend                                     focus.Provider
                                    (audio.Supervisor -> PipeWire                    (KWin script over D-Bus,
                                     native protocol, ADR 0002)                       ADR 0003)
                                                   |
                                                   v
                                          engine.NotifyLEDDirty -> RepaintLEDs -> device.Codec.Encode
                                                   -> midi.Port.Write (LED feedback, M05)
```

`api.Server`/`api.Hub` sit beside the engine, not in its path: they read
a `Snapshot`/`State` off it and relay `PUT /config` into
`engine.SetConfig`. The UI never talks to MIDI/PipeWire/focus directly
(see `CLAUDE.md` project shape) — it talks to the daemon's local HTTP+SSE
API over `$XDG_RUNTIME_DIR/knobd.sock`, bridged into Tauri's `invoke()`
calls by `ui/src-tauri/src/socket.rs`. See
`specs/adr/0004-ipc-over-unix-socket.md` for why unix socket + SSE
instead of WebSocket, and `docs/README.md` for how the OpenAPI/config
JSON Schemas turn into `ui/src/types/*.ts`.

## Wiring: `daemon/cmd/knobd/main.go`, `runDaemon`

Construction order (each step's why is commented in the source, not
repeated here):

1. Parse flags, resolve `-config`/`-socket` defaults, load and migrate
   config (`config.LoadAndUpgrade`).
2. Start `midi.Supervisor` and `audio.Supervisor` — both begin
   discover/connect/reconnect immediately in the background and never
   block; a missing controller or PipeWire is not a startup failure.
3. `focus.New` — best-effort; on failure the daemon runs with
   `focus.Unavailable()` and `focused` targets/`knob.assign_focused_app`
   just don't resolve.
4. `actions.NewRegistry()`, then register handler sets
   (`actions.NewVolumeHandlers`, `actions.NewAssignHandlers`) — handlers
   are registered once at startup, before `engine.Run` starts, because
   `Registry` isn't safe to mutate concurrently with `Execute`.
5. `engine.New(engine.Deps{...})` — `eng` and `hub` (`api.Hub`) are
   forward-declared as `var` above this because `Deps.OnStateChanged`/
   `OnInput` close over them before either is assigned; neither callback
   fires until both `eng.Run` and `hub.Run` are already consuming their
   own inputs, so this is safe, not a race.
6. `newConfigStore` wraps `eng` + the config file path so `PUT /config`
   and the SIGHUP reload path both funnel through `configStore.SetConfig`.
7. `assignHandlers` registered against `store` + `focusProv`, then
   `sceneHandlers` (`actions.NewSceneHandlers`, against `volumeHandlers`
   + `store`) and `mixHandlers` (`actions.NewMixHandlers`, against
   `volumeHandlers`) — M08's handler sets, following the same
   register-before-`eng.Run` rule. `media.New` (best-effort, same
   posture as `focus.New`: no session bus is never a startup failure)
   and `media.NewTracker` are constructed alongside `focusProv` earlier,
   and `mediaHandlers` (`actions.NewMediaHandlers`, against the tracker
   + `media.Backend` + a `media.Notifier` + a live
   `Config.Media.IgnorePlayers` reader) registers here too — M09.
   `spotify.Service` (M10) is constructed right after, sharing the same
   session-bus connection media dialed (`org.freedesktop.secrets` lives
   on the session bus too) for its `SecretStore`; `spotifyHandlers`
   (`actions.NewSpotifyHandlers`, against `spotifySvc.Client()` + the
   same `media.Notifier`) registers alongside the others, and
   `spotifyHandlers.Run` is started as its own goroutine (step 9) since
   it queues Spotify Web API calls off the engine's dispatch goroutine
   rather than executing them inline.
8. `api.NewHub`, then adapters into `api.New`: `newAudioGraph`,
   `newCapabilitiesProvider`, `newLearnController` — each is a small
   point-of-use interface adapter defined in `cmd/knobd` itself (see
   `docs/codemap.md`'s `cmd/knobd` section for their exact shapes), not
   in the packages they wrap.
9. Four goroutines race in `runDaemon`: `eng.Run`, `srv.ListenAndServe`,
   `hub.Run`, and a connection-status watcher; whichever exits first
   cancels a shared `runCtx` and the others unwind. Hand-rolled instead
   of `errgroup` — see the comment at that call site for why.
   `spotifyHandlers.Run` (M10) is started as a fifth goroutine here too,
   but deliberately left out of that race — it never returns an error
   worth cancelling the daemon over, it just stops when `runCtx` is
   canceled.
10. `SIGHUP` reloads `config.json` from disk via `config.Load` (not
    `LoadAndUpgrade` — a hand-edited file at the current schema version
    shouldn't be silently rewritten).

To add a new daemon-side dependency (a new backend, a new handler set),
follow this order: construct it after whatever it depends on, register
handlers before `eng.Run` starts, and wire any "notify the API something
changed" callback through the same forward-declared-`var` pattern if it
needs to reach `hub` before `hub` exists.

## Recipes

**Add a new action type** (a new thing a control binding can do):
1. Add the `model.ActionType` constant and any new params struct in
   `daemon/internal/model` (see existing action param types there).
2. Add/extend a handler set in `daemon/internal/actions` (pattern:
   `NewVolumeHandlers`/`NewAssignHandlers` — a constructor taking a
   point-of-use interface onto whatever backend it needs, a `Register(*
   actions.Registry)` method) and register it in `cmd/knobd/main.go`.
3. `make schema` — the action type shows up in the config JSON Schema
   and OpenAPI capabilities.
4. `make ui-codegen` — regenerate `ui/src/types/{config,api}.ts` so the
   binding editor can offer the new action type.
5. UI: extend the binding editor's action picker under `ui/src/components`
   (see `docs/codemap.md`'s UI section / `ui/src/components` tree for
   current structure).
6. Update `specs/reference/action-catalog.md` and the owning milestone
   spec's checklist.

If the handler needs dispatch-time state only `engine` has (the config's
`AppMatchers`/`AppGroups`, the live stream/focus graph) beyond a single
resolved `Target` — M08's `scene.apply`/`scene.save` (several targets
per scene) and `audio.solo_toggle`/`audio.duck_hold` ("every other
known stream") are the examples — the action executes one of two ways
instead of a plain `Registry` dispatch:
- **State the handler itself must own** (M08's three `layer.*` actions,
  which mutate `engine.layerState`) is executed *inline* in
  `engine.dispatchGesture`, never registered with `actions.Registry` at
  all. `engine.InlineActionTypes()` lists these so `cmd/knobd`'s
  capabilities adapter still reports them as implemented.
- **State the handler needs but doesn't own** goes through `actions.
  Registry` as normal, but `engine` resolves the extra pieces at
  dispatch time and carries them on `actions.Invocation` (see
  `Scene`/`SceneRefs` and `Others`'s doc comments there) via a
  dedicated `engine` dispatch helper (`dispatchScene`,
  `dispatchWithOthers`) instead of the generic `dispatchResolvedAction`
  path. The handler must use what `Invocation` carries, never
  re-resolve — the same rule `Invocation.Refs` already established in
  M04.

**Add/change a config field** (anything under `model.Config`):
1. Change the type in `daemon/internal/model`.
2. If the JSON shape changes, bump `schemaVersion` and add a migration
   step in `daemon/internal/config` (see existing `v*_to_v*.go`-style
   migrations there and their tests against `testdata/config/`).
3. `make schema` (regenerates `docs/config.schema.json`, gated in CI).
4. `make ui-codegen` for `ui/src/types/config.ts`.

**Add a hardware-facing backend** (new I/O surface: a new transport, a
new focus provider, etc.):
1. Define a small interface at the point of use (existing examples:
   `midi.Port`, `audio.Backend`, `focus.Provider`, `media.Backend`)
   rather than depending on a concrete type — see `CLAUDE.md`
   conventions and ADR 0001 (no CGo: a new backend can't introduce one
   without an ADR).
2. Ship a `Fake*` implementation in the same package for tests (see each
   package's doc comment, referenced from `docs/codemap.md`).
3. Table-driven tests against the fake; if the backend needs a fixture
   capture, add it under `testdata/<kind>/` with a README explaining what
   it contains (see `testdata/midi/README.md`, `testdata/pipewire/README.md`
   for the pattern).

**After any change to an exported Go signature, interface, or struct
field** under `daemon/internal`/`daemon/cmd`: run `make codemap` and
commit the result — CI fails otherwise (same gate as `make schema`).

## Testing conventions

- Table-driven tests, Go stdlib `testing` only.
- Every hardware-facing package's fake lives beside its interface (see
  each package's own doc comment — `docs/codemap.md` reproduces the
  first paragraph of each). Unit tests run against the fake; real
  end-to-end verification against the physical X-Touch Mini and a live
  PipeWire session is a milestone's Verification section, done by hand.
- `testdata/` holds real captures (raw MIDI bytes, a trimmed `pw-dump`
  graph, config fixtures at old schema versions for migration tests) —
  see each subdirectory's README for what it contains and which tests
  consume it (`docs/codemap.md`'s per-package "testdata used" lines also
  list this).
- `make test` (daemon), `make ui-test` (vitest + the Tauri Rust bridge's
  `cargo test`), `make lint`/`make ui-lint`. `make schema` and
  `make codemap` are generate-and-diff steps, not test runs, but CI
  treats a stale result as a failure the same way.

## Milestone → packages

| Milestone | Introduced / owns |
|---|---|
| M01 | Repo scaffolding, `daemon/internal/config` (schema versioning), `daemon/internal/model` basics |
| M02 | `daemon/internal/midi` (`Port`, `FakePort`, discovery), `daemon/internal/device` (`Codec` for the X-Touch Mini) |
| M03 | `daemon/internal/audio` (`Backend`, `FakeBackend`, PipeWire native protocol per ADR 0002) |
| M04 | `daemon/internal/engine` (event loop), `daemon/internal/actions` (registry + volume handlers), `daemon/internal/api` (local HTTP API), `daemon/cmd/knobd` wiring |
| M05 | LED feedback: `engine.NotifyLEDDirty`/`RepaintLEDs`, `device.Codec` LED encoding |
| M06 | `daemon/internal/focus` (`Provider`, KWin script per ADR 0003), `daemon/internal/proctree`, assign-focused-app action |
| M07 | `ui/` (Tauri + React config app), `api.Hub`/SSE (ADR 0004 Update), `daemon/internal/schema` + `docs/*.json` generation |
| M08 | Layers, groups, scenes, solo/duck — done; `engine.layerState`/`dispatchGesture`, `resolver.resolveGroup`, `actions.SceneHandlers`/`MixHandlers` |
| M09 | Media transport (MPRIS) — done; `daemon/internal/media` (`Backend`/`Tracker`/`Notifier`), `actions.MediaHandlers`, `engine/dispatch.go`'s `media.seek` coalescing, `api.State.Media` |
| M10 | Spotify Web API — done; `daemon/internal/spotify` (`Auth`/`TokenManager`/`Client`/`SecretStore`/`Service`), `actions.SpotifyHandlers` (own worker goroutine), `/spotify/*` API routes, `api.State.Spotify` |
| M11 | Extended actions — not started; new `actions` handler sets |
| M12 | Packaging: `packaging/`, PKGBUILDs, config migration-on-upgrade, unit/install paths |

See `specs/README.md` for current status and full dependency notes —
this table is for "which package do I look at for what M0N shipped",
not a status tracker; don't let it drift into duplicating that file.
