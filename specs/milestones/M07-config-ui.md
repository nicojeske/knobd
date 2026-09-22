# M07: Config UI

## Status

In progress (2026-09-23). Daemon-side API surface designed; see Design and
Data model changes below.

## Depends on

M04 (a real API to talk to). Benefits from, but doesn't strictly need,
M05 (live LED-equivalent state to mirror in the UI) and M06 (focused-app
assignment to surface).

## Goal

A Tauri desktop app, launched from a tray icon, where every control on a
visual X-Touch Mini panel can be clicked to bind, MIDI-learn works
(physically touch a control on the real device to select it in the UI
instead of clicking), and profiles/bindings/app matchers/groups can be
managed without hand-editing `config.json`.

## Scope

**In**: `rustup` + Tauri Linux prerequisites actually installed and
`ui/`'s Tauri build working end to end (currently blocked — see
`specs/reference/environment.md`), Tauri commands proxying to
`daemon/internal/api`'s unix socket (replacing every "not implemented"
stub in `ui/src/api/client.ts`), a visual representation of the
controller's layout (from `specs/reference/xtouch-mini-midi-map.md`),
click-to-bind, MIDI learn (listen for the next controller event and use
it as the selection instead of a click), live state display over
WebSocket, app/group pickers backed by `GET /streams`-equivalent live
data, profile management, and a TypeScript client generated from
`docs/openapi.json` once `daemon/internal/api` has real routes
(`ui/src/types/config.ts` itself is already generated, from
`docs/config.schema.json` — see M01).

**Out**: layer semantics, `group` target resolution, and scenes/solo/duck
handlers (M08 owns those — M07 can build CRUD for groups/scenes against
`model.Config`, but a `group` target won't resolve at runtime until M08
ships, and the UI says so via `GET /capabilities` rather than pretending).
Packaging/distribution of the UI app, and any decision about embedding
the UI's static assets in the daemon binary vs. shipping Tauri's own
bundle, is M12 — M07 only needs `npm run tauri build` to produce
something M12 can package later.

**In, but new daemon behavior** (revising this milestone's original
framing, which said "not a place to sneak in new daemon behavior" — that
undersold what a *client* for live state and MIDI learn actually
requires): the live-state push channel, a `GET /audio` picker feed, MIDI
learn's engine tap and REST toggle, and `GET /capabilities`. None of
these exist in `daemon/internal/api` today, and ADR 0004 already
anticipated a streaming channel ("HTTP + WebSocket") without designing
one. See Design and Data model changes below for the actual shapes; per
`specs/README.md`'s rule that a spec disagreeing with reality is a bug
in the spec, this supersedes the milestone's original Scope wording.

## Design

Rust and every Tauri Linux system dependency are already installed on
this machine (see `specs/reference/environment.md`, corrected in this
change) — the toolchain is no longer the milestone's first risk. The
remaining Tauri-side blockers are small: `ui/src-tauri/Cargo.toml`
declares a `[lib]` target with no `src/lib.rs`, there is no
`capabilities/` directory (needed for `@tauri-apps/api/event`'s
`listen()`, not for our own `#[tauri::command]`s), and the icon set
under `ui/src-tauri/icons/` doesn't exist yet. Fix those, confirm
`npm run tauri dev` opens a window, then work outward from
`ui/src/api/client.ts`'s stubs.

**Daemon-side additions** (new, in this milestone — see Data model
changes for exact shapes):

- `daemon/internal/api/routes.go` declares the route table as data, so
  `docs/openapi.json` and the `net/http.ServeMux` registration are both
  driven from one source and cannot drift apart.
- `daemon/cmd/schemagen` gains a `-kind` flag and emits, alongside
  today's `docs/config.schema.json`: `docs/openapi.json` (OpenAPI 3.1,
  a ~130-line local document model — no new dependency; `invopop/
  jsonschema` still links only into `schemagen`, never into `knobd`,
  per ADR 0005) and `docs/device-layout.json` (per-`ControlKind` index
  ranges and the gesture-validity matrix, so the binding editor doesn't
  hand-duplicate `model.Control.Validate`'s literals or
  `ControlKind.SupportsGesture`).
- `GET /audio` — one route returning live sinks, sources and streams
  together (not three routes), each stream carrying its **raw,
  undigested** PipeWire property bag plus which configured
  `AppMatcher`s currently hit it. The app/group picker needs the raw
  bag because what a given app actually publishes varies (some streams
  carry only `node.name`); pre-digesting it would throw that away.
- `GET /capabilities` — `implementedActions` from the live
  `actions.Registry` (today: 5 of `model.ActionTypes()`'s 21 —
  `volume.adjust`, `volume.set`, `volume.mute_toggle`,
  `volume.follow`, `knob.assign_focused_app` — far more than the
  `volume.balance` gap this spec used to call out alone),
  `supportedTargetKinds`, and `features{layers, scenes, learn}`. This
  is what lets the UI badge an unimplemented action or a `group`
  target with a reason that disappears on its own as M08/M09/M11 land,
  instead of a hand-kept TypeScript list going stale.
- MIDI learn: `daemon/internal/engine` gains a tap
  (`Engine.SetLearnUntil`) that, while armed, hands every decoded
  `device.Event` to `Deps.OnInput` and **drops it** — the gesture
  machine and action dispatch never see it, so touching a control to
  identify it in the UI cannot also move a volume. One-shot: it
  disarms on the first captured input or its deadline, whichever comes
  first. `POST /learn` / `DELETE /learn` toggle it.
- `GET /events` — a **Server-Sent Events** stream, not WebSocket.
  ADR 0004 said "HTTP + WebSocket"; this milestone amends that (see
  the `## Update (M07)` section added to the ADR) because the channel
  is strictly one-directional — learn is entered by `POST /learn`, not
  by a client→server frame — and SSE needs no new dependency on either
  the Go or the Rust side, stays `curl --unix-socket`-debuggable (a
  property ADR 0004 already cared about), and isn't hijacked, so
  `http.Server.Shutdown` accounts for it once `Serve` gains a
  `BaseContext` tying request contexts to the daemon's own shutdown
  context. Pushes full `api.State` snapshots (not deltas — a dropped
  delta is unrecoverable and would need its own resync protocol; a
  snapshot is small enough, ~2-3 KB compact, that "drop the stale one,
  keep the newest" is sufficient backpressure), throttled leading-edge
  + trailing at 30ms, citing `engine`'s existing `ledFlushInterval`
  rationale (the fader's ~404 messages per free-play session). Also
  carries `config_changed` notifications (see below) and learn events.
  All hub logic lives behind a transport-agnostic interface so a later
  swap to WebSocket, if ever needed, is one file.
- `configStore.SetConfig` (the single funnel for `PUT /config`, SIGHUP,
  and M06's `knob.assign_focused_app` — which can rewrite a binding
  *and* append an `AppMatcher`) gains a revision counter and fires a
  `config_changed` event after every successful apply, outside its
  mutex. The UI re-`GET`s on that signal rather than the event carrying
  the config itself.

No ETag/If-Match concurrency control is added in this milestone (see
Risks) — every UI editor does read-modify-write against a config
fetched moments before applying, which narrows the lost-update window
from minutes to milliseconds, and the `config_changed` banner covers
the rest by prompting a reload rather than silently overwriting.

**UI-side**: the Rust bridge (`ui/src-tauri/src/`) is a thin unix-socket
HTTP client (hand-rolled over `hyper`'s connection API, not
`hyperlocal` — no pooling or routing is needed for one socket at one
path) that never deserializes daemon payloads into Rust structs; it
passes `serde_json::Value` through so the type authority stays
Go model → `docs/openapi.json` → TypeScript, with no third
hand-maintained copy. It holds the SSE connection and re-emits frames
as Tauri events (`app.emit`) — the webview's only path to the daemon is
`listen()`, so ADR 0004's "the webview itself never has raw socket
access" holds structurally. `ui/src/types/config.ts` stays generated
from `docs/config.schema.json` (`npm run codegen:config`, unchanged
since M01); a new `npm run codegen:api` generates
`ui/src/types/api.ts` from `docs/openapi.json` via `openapi-typescript`
(types only — the UI calls `invoke()`, never `fetch`).

The visual panel is one SVG element. Its geometry (screen position of
each control) is a hand-written, vitest-checked table; its semantics
(which kinds exist, valid index ranges, legal gestures per kind) come
from the generated `docs/device-layout.json`, removing the one
significant hand-duplication risk the panel and binding editor would
otherwise carry.

## Data model changes

Nothing changes in `daemon/internal/model` or `docs/config.schema.json`
— every addition below is new API surface (`daemon/internal/api`),
kept deliberately out of the config schema (`api.State` already
documents this constraint from M04).

- `api.AudioGraph{Now, Sinks []AudioDevice, Sources []AudioDevice,
  Streams []AudioStream}` — `GET /audio`. `AudioStream` carries
  `Ref, ID, Direction, DisplayName, Props map[string]string
  (raw PipeWire props), MatcherIDs []string, Corked bool,
  VolumePercent, Muted`.
- `api.Capabilities{ImplementedActions []model.ActionType,
  SupportedTargetKinds []model.TargetKind, Features
  {Layers, Scenes, Learn bool}}` — `GET /capabilities`.
- `api.LearnRequest{TimeoutMs int}`, `api.LearnState{Active bool,
  ExpiresAt *time.Time}` — `POST /learn` (body optional; clamped to
  `engine.DefaultLearnTimeout`=15s / `MaxLearnTimeout`=60s) and
  `DELETE /learn`. `api.State` grows `Learn LearnState`, so a
  reconnecting UI learns learn-mode status for free from `GET /state`
  or the first `state` push — no separate `GET /learn`.
- `api.LearnInput{Control model.Control, SuggestedGesture
  model.Gesture, Delta, Value int, At time.Time}` — pushed as a
  `learn_input` event while learn is armed. `SuggestedGesture` is a
  stateless map from the raw `device.EventKind` (turn→turn,
  fader_move→move, button_down→press); learn deliberately does **not**
  run the gesture machine, so the UI's gesture picker (constrained by
  `docs/device-layout.json`'s matrix) makes the real choice, seeded
  with this suggestion.
- `api.Event{Type EventType, Seq uint64, Now time.Time, Hello *Hello,
  State *State, Config *ConfigChanged, Learn *LearnState, Input
  *LearnInput, Error *ErrorResponse}` — the `GET /events` SSE envelope.
  `EventType` ∈ `hello | state | config_changed | learn | learn_input |
  error`. `ConfigChanged{Revision uint64}` — a revision number, not the
  config itself.
- `docs/openapi.json` (generated, committed, CI-gated) and
  `docs/device-layout.json` (generated, committed, CI-gated) join
  `docs/config.schema.json` as `make schema`'s output.

## Acceptance criteria

- [ ] `npm run tauri dev` and `npm run tauri build` both work on the
      development machine.
- [ ] `ui/src/api/client.ts` is a real client backed by `invoke()`; no
      stubs remain.
- [ ] Clicking a control on the visual panel opens a binding editor for
      it; saving persists to the daemon and is reflected on the physical
      device without restarting either process.
- [ ] MIDI learn: click "learn," then turn/press the physical control
      you mean, and the UI selects the right one — and the controller's
      normal behavior (e.g. changing a volume) does not fire while
      learning.
- [ ] The tray icon's "Configure..." (or equivalent) menu item opens the
      window; closing the window doesn't kill the daemon or the tray icon.
- [ ] `docs/openapi.json` and `docs/device-layout.json` are generated by
      `make schema`, committed, and CI-gated the same way as
      `docs/config.schema.json`.
- [ ] `GET /capabilities` correctly reports which of the 21 action types
      have a registered handler, and the UI reflects that rather than
      offering all 21 as equally functional.
- [x] `ui/src/types/config.ts` is generated, not hand-maintained — done
      in M01, ahead of the rest of this milestone.

## Verification

Automated: `make test` (new daemon tests for the route table, `/audio`,
`/capabilities`, learn suppression, the SSE hub's throttling/backpressure,
and a `Serve`-returns-promptly-on-shutdown regression test), `make schema
&& git diff --exit-code` over all three generated docs, and in `ui/`:
`npm run codegen && git diff --exit-code -- src/types/`, `npm run lint`,
`npm run typecheck`, `npm test`, `npx vite build`, and in
`ui/src-tauri/`: `cargo fmt --check && cargo clippy --all-targets -- -D
warnings && cargo test`.

By hand, no hardware needed: `curl --unix-socket $XDG_RUNTIME_DIR/knobd.sock
http://localhost/audio`, `.../capabilities`, and
`curl -N --unix-socket $XDG_RUNTIME_DIR/knobd.sock http://localhost/events`
to watch live frames.

With the physical X-Touch Mini and a live PipeWire session: `npm run
tauri dev`, bind an encoder to a running app via the UI, confirm it
takes effect live; use MIDI learn to bind a second control and confirm
turning the physical knob during learn does **not** change any volume;
sweep the fader during learn and confirm the UI doesn't lag; hold an
encoder-push to assign the focused app while the UI is open and confirm
the UI updates from `config_changed` without a manual refresh; restart
the daemon and confirm both bindings persisted; confirm the tray's
"Configure…" reopens the window after closing it, with the daemon and
tray icon both still alive throughout.

## Risks & open questions

- **Residual lost-update race.** Per-dialog apply (read moments before
  write) narrows the window between a UI edit and a concurrent
  device-triggered config change to milliseconds, but does not close
  it — an ETag/If-Match scheme was considered and deliberately deferred
  rather than added speculatively; it remains available as a follow-up
  if the race proves to matter in practice.
- **SSE vs. the letter of ADR 0004.** Mitigated by an explicit
  `## Update (M07)` section on the ADR and by keeping the hub's logic
  transport-agnostic, so a later move to WebSocket is one file, not a
  redesign.
- `openapi-typescript` may not cleanly resolve a `$ref` from
  `docs/openapi.json` out to the separate `docs/config.schema.json`
  file; the fallback is describing `/config`'s request/response body as
  an opaque object in the OpenAPI doc and letting `ui/src/types/config.ts`
  supply the real type — the daemon validates authoritatively either way.
- 16 of the 21 `model.ActionType`s have no registered handler as of this
  writing. `GET /capabilities` makes that visible rather than hidden,
  but the binding editor will look sparse until M08/M09/M11 land more
  handlers.
- `ui/src-tauri/tauri.conf.json`'s `bundle.targets` is narrowed to
  `["deb"]` for this milestone (AppImage bundling needs `patchelf`,
  which isn't installed, plus a network download of `linuxdeploy`) —
  the full target list and the embed-vs-separate-bundle question stay
  M12's to decide, once M12 has this build to look at.
