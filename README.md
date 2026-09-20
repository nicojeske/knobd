# knobd

A background daemon that maps a Behringer X-Touch Mini MIDI controller to
PipeWire volume control — all applications, a single application, an
application group, or a microphone — by turning a knob. Buttons run
configurable macros (mute, solo, scenes, focused-app binding, media
transport, and more). Configuration happens through a desktop UI; the
daemon itself runs headless as a systemd user service.

**Status: scaffolding.** No feature is implemented yet. See
[`specs/README.md`](specs/README.md) for the milestone plan and
[`specs/reference/`](specs/reference/) for the hardware/environment facts
this project is built on.

## Repository layout

```
daemon/       Go daemon (cmd/knobd + internal packages) — see daemon/README.md
ui/           Tauri + React + TypeScript configuration UI — see ui/README.md
packaging/    systemd unit, udev rule, KWin focus-tracking script
testdata/     Captured MIDI and PipeWire fixtures used by daemon tests
docs/         Generated artifacts (JSON Schema, OpenAPI spec)
specs/        Milestone specs, ADRs, and reference docs — start here
```

## Requirements

- Linux with ALSA rawmidi (`/dev/snd/midi*`) and PipeWire (with the
  `pipewire-pulse` compatibility server) — developed against Arch/CachyOS
  + KDE Plasma 6 on Wayland; see `specs/reference/environment.md` for the
  exact versions this was built and tested on.
- Go 1.25+ for the daemon.
- Node 20+ and a Rust toolchain (`rustup`) for the Tauri UI — not needed
  until milestone M07.
- Membership in the `audio` group (for `/dev/snd/midi*` access).

## Building

```bash
cd daemon && go build ./... && go vet ./...
```

The UI is scaffolded but not buildable yet — see `ui/README.md`.

## Where to start reading

1. `specs/README.md` — milestone index and how specs are written.
2. `specs/reference/xtouch-mini-midi-map.md` — the hardware protocol.
3. `specs/adr/` — the four architectural decisions behind the daemon.
4. `CLAUDE.md` — conventions for working in this repo.
