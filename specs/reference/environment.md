# Development environment

Facts verified live on the development machine on 2026-09-20, while
scaffolding this project (toolchain table re-verified 2026-09-23 for
M07). Every milestone's design should be checked against these rather
than assumed; re-verify anything here that a later session finds has
changed (a distro upgrade, a Plasma major version bump, etc.) and
update this file when it does.

## OS / desktop

- Arch-based (CachyOS), kernel 7.2.6-1-cachyos, x86_64.
- **KDE Plasma 6.7.5 on Wayland** (`XDG_SESSION_TYPE=wayland`,
  `XDG_CURRENT_DESKTOP=KDE`). Do not assume X11 tooling is available:
  `xdotool`, `wmctrl`, and `kdotool` are all **absent** from this system.
  This is why focus tracking goes through a KWin script rather than a
  window-manager-agnostic library — see
  [ADR 0003](../adr/0003-focus-tracking-via-kwin-script.md).
- KWin/plasmashell version: 6.7.5.

## Audio

- **PipeWire 1.6.8** with the `pipewire-pulse` compatibility server
  (`pactl info` reports `Server Name: PulseAudio (on PipeWire 1.6.8)`).
- Native protocol socket: `/run/user/1000/pulse/native` (i.e.
  `$XDG_RUNTIME_DIR/pulse/native`).
- `pactl`, `wpctl`, `pw-cli`, `pw-dump` are all present and were used to
  inspect the live audio graph; `pamixer` is not installed.
- See [`testdata/pipewire/`](../../testdata/pipewire/) for a curated
  sample of the live node graph and the edge cases it demonstrates.

## MIDI controller

- Behringer X-Touch Mini, USB vendor:product `1397:00b3` (from
  `lsusb`), currently in **Mackie Control (MC) mode**.
- Appears as ALSA card 3: `/dev/snd/midiC3D0`, and stably at
  `/dev/snd/by-id/usb-Behringer_X-TOUCH_MINI_1.0.1-00`.
- Readable/writable by the `audio` group with no extra udev rule needed
  on this system (see [`packaging/udev/99-knobd.rules`](../../packaging/udev/99-knobd.rules)
  for why one is shipped anyway).
- Full protocol details: [`xtouch-mini-midi-map.md`](xtouch-mini-midi-map.md).

## Toolchains

| Tool | Version | Notes |
|---|---|---|
| Go | 1.27.1 | Daemon language |
| Node | 26.8.2 | UI tooling |
| npm | 12.0.2 | |
| Rust / cargo | **1.98.1** (rustup 1.29.1) | Installed 2026-09-23 for M07, via pacman's `rustup` package (`rustup default stable`); shim at `/usr/bin/cargo` |
| golangci-lint | **not installed** | `make lint` falls back to `gofmt`+`go vet` when absent |
| git | 2.55.0 | |

Every Tauri v2 Linux system dependency was already present when Rust
was installed: `webkit2gtk-4.1` 2.52.6, `javascriptcoregtk-4.1` 2.52.6,
`libsoup-3.0` 3.6.6, `gtk+-3.0` 3.24.52, `openssl` 3.6.4, and
`ayatana-appindicator3-0.1` 0.6.0 (the tray icon's actual dependency on
this system — **not** `libappindicator-gtk3`, which the install
snippet below used to name; Arch/CachyOS packages this as
`libayatana-appindicator`). `patchelf` is **not installed**, which
blocks Tauri's AppImage bundler specifically (deb/rpm bundling is pure
Rust and unaffected) — see `specs/milestones/M07-config-ui.md`'s Risks.

Installing Rust (already done on this machine; kept here for a fresh
machine):

```bash
sudo pacman -S rustup
rustup default stable
# Tauri Linux prerequisites, if not already present:
sudo pacman -S webkit2gtk-4.1 libayatana-appindicator librsvg patchelf
```

## Permissions

- User is in the `audio` group (confirmed via `id -nG`), along with
  `wheel`, `docker`, `video`, `wireshark`, and others — no special
  privilege is needed for anything knobd does.
