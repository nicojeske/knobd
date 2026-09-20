# M01: Foundations

## Status

**Mostly done** (as of 2026-09-20, this scaffolding session). Remaining
work is listed under Acceptance criteria below.

## Depends on

Nothing — this is the base.

## Goal

A repository that compiles, tests, and lints cleanly; a domain model
(`daemon/internal/model`) that the rest of the project builds on; a
config file format that can be loaded, saved, and migrated; and a
`specs/` structure so every later milestone has a place to be designed
before it's built.

## Scope

**In**: repo layout, Go module, `daemon/internal/model` (Control,
Gesture, Target, AppMatcher, AppGroup, Action tagged union, Binding,
Config, Scene), `daemon/internal/config` (load/save/migrate), structured
logging, `cmd/knobd`'s minimal entry point (loads config, logs, exits
cleanly on signal — no MIDI/audio/focus/engine/api yet), CI, the
interface-only stubs for `midi`/`device`/`audio`/`focus`/`engine`/
`actions`/`api` (with fakes for the hardware-facing ones), and this
`specs/` directory.

**Out**: anything that touches real hardware, PipeWire, or KWin —
that's M02/M03/M06. The UI beyond a scaffold that typechecks and builds
— that's M07.

## Design

See `daemon/README.md` for the package-by-package breakdown and
`CLAUDE.md` for repo-wide conventions. The one design idea worth calling
out here: `model.Target` never points at a live stream/device ID
directly (see `daemon/internal/model/target.go`'s doc comments) — every
target is a selector resolved at dispatch time, because the things it
could point at (streams, the focused window) change continuously. This
shapes every milestone from M03 onward and is why it's decided at M01
rather than left implicit.

`model.Action` is a closed, centrally-registered tagged union (see
`action.go`'s `actionRegistry` and its completeness-checking test in
`action_test.go`) rather than a `map[string]interface{}` params bag —
the goal (per the project's own requirement for "a typed language...
so the code can be better maintained") is that adding a new action is a
compile-time-checked, four-place change (constant, struct, registry
entry, handler) rather than something that can silently typo its way
into a runtime failure.

## Data model changes

This milestone *is* the data model — see
`daemon/internal/model/{control,target,action,binding,config}.go`.
`model.CurrentSchemaVersion = 1`.

## Acceptance criteria

- [x] `go build ./... && go vet ./... && go test ./...` clean from
      `daemon/`.
- [x] `gofmt -l .` empty.
- [x] `model.Config` round-trips through JSON, including the `Action`
      tagged union, with cross-referential validation (`Config.
      Validate`) catching dangling references between profiles/
      bindings/app matchers/groups/scenes.
- [x] `config.Load`/`config.Save` round-trip, `Save` is atomic
      (temp-file + rename) and validates before writing.
- [x] `midi.FakePort`, `audio.FakeBackend`, `focus.FakeProvider` exist
      and are used by their own package's tests, ready for `engine`
      (M04) to depend on.
- [x] `cmd/knobd` builds, loads a config, logs, and exits cleanly on
      SIGINT/SIGTERM.
- [x] CI (`.github/workflows/ci.yml`) runs `gofmt`/`vet`/`test`/`build`
      for the daemon and `typecheck`/`build` for the UI.
- [x] `ui/` typechecks (`npm run typecheck`) and builds with Vite
      (`npx vite build`) — Tauri/Rust build is explicitly out of scope
      until M07 (no Rust toolchain on this machine yet).
- [ ] JSON Schema generation (`docs/config.schema.json`, the Makefile's
      `schema` target) — deferred; see below.
- [ ] A real migration chain example — deferred, since
      `model.CurrentSchemaVersion` is still 1 and there is nothing to
      migrate from yet. `config.Migrate`'s structure (version 0 treated
      as version 1, a no-op at the current version, a documented error
      for a version this build doesn't know how to reach) is in place
      and tested; the first real step gets added the day
      `CurrentSchemaVersion` becomes 2.

## Verification

```bash
cd daemon && go build ./... && go vet ./... && go test ./... && gofmt -l .
cd ../ui && npm install && npm run typecheck && npx vite build
```

Run the daemon directly and confirm it loads a config and shuts down
cleanly:

```bash
cd daemon && go run ./cmd/knobd --config /tmp/knobd-test-config.json
# Ctrl+C — should log "shutting down" and exit 0
```

## Risks & open questions

- **JSON Schema generation from a Go tagged union** is more work than a
  straightforward struct-to-schema reflection pass (the `Action`
  interface field needs an explicit `oneOf` per registered type). Worth
  a small dedicated design pass rather than bolting on ad hoc — tracked
  as the main open item for "finishing" M01, not blocking any other
  milestone.
- The `actions.Registry` (in `daemon/internal/actions`) is implemented
  and tested as generic dispatch infrastructure, but has zero handlers
  registered — every concrete handler is a later milestone's job (see
  `registry.go`'s doc comment for which milestone owns which family).
