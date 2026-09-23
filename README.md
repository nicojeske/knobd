# knobd

A background daemon that maps a Behringer X-Touch Mini MIDI controller to
PipeWire volume control — all applications, a single application, an
application group, or a microphone — by turning a knob. Buttons run
configurable macros (mute, solo, scenes, focused-app binding, media
transport, and more). Configuration happens through a desktop UI; the
daemon itself runs headless as a systemd user service.

**Status: usable.** Turning a bound knob changes an application's
volume; the fader follows a target's level continuously; a mute toggle
converges a multi-stream app instead of oscillating it. Zero-config
"grab whatever I'm focused on" (`knob.assign_focused_app`) and the rest
of the config UI are still ahead — see [`specs/README.md`](specs/README.md)
for the milestone plan and [`specs/reference/`](specs/reference/) for
the hardware/environment facts this project is built on.

## Repository layout

```
daemon/       Go daemon (cmd/knobd + internal packages) — see daemon/README.md
ui/           Tauri + React + TypeScript configuration UI — see ui/README.md
packaging/    PKGBUILD (packaging/arch), systemd unit, udev rule, desktop file
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

## Installing

On Arch/CachyOS, `packaging/arch` builds and installs both `knobd` and
`knobd-ui` as a proper package — the daemon's systemd unit, udev rule,
and the UI's desktop entry are all placed and enabled automatically
(see `packaging/README.md` for exactly what that means and how to
uninstall):

```bash
cd packaging/arch && makepkg -si
journalctl --user -u knobd -f
```

For iterating on the daemon itself without a full package rebuild,
`make install-user` copies just the binary and the unit file into
`~/.local`:

```bash
make install-user
systemctl --user enable --now knobd.service
journalctl --user -u knobd -f
```

`make uninstall-user` removes what that installed. Once it's running:

```bash
curl --unix-socket $XDG_RUNTIME_DIR/knobd.sock http://localhost/config
curl --unix-socket $XDG_RUNTIME_DIR/knobd.sock http://localhost/state
curl -X PUT --unix-socket $XDG_RUNTIME_DIR/knobd.sock http://localhost/config -d @newconfig.json
```

`PUT /config` takes effect immediately, no restart needed. After a hand
edit to `~/.config/knobd/config.json` instead, `systemctl --user reload
knobd` picks it up.

It's normal for the daemon to start and sit there doing nothing if no
controller is plugged in or PipeWire isn't reachable yet — both are
discover-wait-reconnect, not startup failures. `GET /state` is the
place to check: it reports the MIDI device's and PipeWire's connection
status directly, rather than leaving "why isn't anything happening" a
guessing game.

## Where to start reading

1. `specs/README.md` — milestone index and how specs are written.
2. `specs/reference/xtouch-mini-midi-map.md` — the hardware protocol.
3. `specs/adr/` — the four architectural decisions behind the daemon.
4. `CLAUDE.md` — conventions for working in this repo.
