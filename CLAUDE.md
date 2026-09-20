# CLAUDE.md

Guidance for working in this repository. This project is **spec-driven**:
features are designed in `specs/milestones/M*.md` before they are
implemented. Read the relevant milestone spec (and its `Depends on` list)
before writing code for it, and update the spec's status/checklist as
work completes rather than letting the spec drift from reality.

## Project shape

- `daemon/` — Go module `github.com/njeske/knobd`. The background
  service: MIDI I/O, PipeWire control, focus tracking, the mapping
  engine, and the local API. See `daemon/README.md`.
- `ui/` — Tauri + React + TypeScript configuration app. Talks to the
  daemon over a unix socket (`$XDG_RUNTIME_DIR/knobd.sock`), never
  directly to MIDI or PipeWire. See `ui/README.md`.
- `packaging/` — systemd unit, udev rule, the KWin focus-tracking script
  installed alongside the daemon.
- `testdata/` — real captures (MIDI bytes, a trimmed PipeWire node graph)
  used as fixtures. See each subdirectory's README for what a capture
  contains and which tests consume it.
- `specs/` — milestone specs, ADRs, and reference docs. `specs/README.md`
  is the index.

## Conventions

- **Go**: standard `gofmt`/`go vet` cleanliness, table-driven tests,
  errors wrapped with `%w` and enough context to debug without a
  debugger attached. Prefer small interfaces defined at the point of use
  (`midi.Port`, `audio.Backend`, `focus.Provider`) over concrete types, so
  backends can be swapped (see `specs/adr/`) and unit-tested without
  hardware or a running PipeWire.
- **No CGo** in the daemon unless an ADR says otherwise — the static,
  single-binary property is deliberate (see `specs/adr/0001-*.md`).
- **TypeScript**: `strict` mode, no `any`. API types are generated from
  the daemon's Go structs (see `docs/`), not hand duplicated.
- **Config** (`~/.config/knobd/config.json`) is versioned with a
  `schemaVersion` field. Any change to `daemon/internal/model` that
  affects the JSON shape needs a migration in `daemon/internal/config`
  and a regenerated `docs/config.schema.json` (`make schema`, gated in
  CI — see `docs/README.md`).
- **Commits**: one milestone (or a clearly-scoped slice of one) per
  logical change. Reference the milestone id (`M04`, etc.) in the
  message when applicable. Commit finished work as you go — don't leave
  a clean, verified change sitting uncommitted waiting for a separate
  "commit it" request. Commit straight to `main` — this repo doesn't use
  feature branches, so don't create one (including the usual
  default-branch-gets-a-branch-first habit).

## Testing without the physical controller

Every hardware-facing package is built behind an interface with a fake
implementation for tests (`midi.FakePort`, `audio.FakeBackend`,
`focus.FakeProvider` — see each package's own doc comment, at the top of
its primary file). Real end-to-end verification against the physical
X-Touch Mini and a live PipeWire session should still happen before a
milestone is marked done; the specs say what to check by hand.

## Environment this was developed on

KDE Plasma 6 on Wayland, PipeWire with `pipewire-pulse`, Arch/CachyOS. Do
not assume X11 tooling (`xdotool`, `wmctrl`) is available — see
`specs/reference/environment.md` and ADR 0003 for why focus tracking goes
through a KWin script instead.
