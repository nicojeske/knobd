# packaging/

Everything M12 wires up into an installable Arch package, plus the raw
files the package installs.

## Layout

- `arch/` — `PKGBUILD` (`pkgbase=knobd`, producing two packages: `knobd`
  and `knobd-ui`) and `knobd.install`, the post-install/upgrade/remove
  hooks. Build and install with:

  ```bash
  cd packaging/arch
  makepkg -si
  ```

  This builds a static daemon binary (`make release`; see ADR 0001 and
  `make check-static`) and the Tauri UI (`npx tauri build --no-bundle`)
  from a clean git checkout of the repository, then installs:

  | File | Destination |
  |---|---|
  | `daemon/knobd` | `/usr/bin/knobd` |
  | `ui/src-tauri/target/release/knobd-ui` | `/usr/bin/knobd-ui` |
  | `systemd/knobd.service` | `/usr/lib/systemd/user/knobd.service` |
  | `udev/99-knobd.rules` | `/usr/lib/udev/rules.d/99-knobd.rules` |
  | `desktop/knobd-ui.desktop` | `/usr/share/applications/knobd-ui.desktop` |
  | UI icons | `/usr/share/icons/hicolor/*/apps/knobd.png` |

  `knobd.install`'s `post_install` runs `systemctl --global enable
  knobd.service` (so it starts at every future login) and also starts it
  immediately for any user with a running session — `pacman` runs as
  root and has no session of its own to enable/start a `--user` unit in,
  so this is what "enabled without manual steps" (the M12 acceptance
  criterion) means in practice. `post_upgrade` restarts the unit for
  live sessions, which is also when `config.LoadAndUpgrade`
  (`daemon/internal/config/config.go`) actually migrates an on-disk
  config to the current schema version — see that package's doc comment.
  Reloading udev rules and doing a plain (non-restarting) `systemctl
  --user daemon-reload` are already handled by pacman's own
  `35-systemd-udev-reload.hook` / `30-systemd-daemon-reload-user.hook`,
  so `knobd.install` doesn't duplicate them.

- `desktop/knobd-ui.desktop` — the UI's application-menu entry.
- `systemd/knobd.service` — user service unit. Targets
  `graphical-session.target` rather than plain `default.target` because
  focus tracking (`daemon/internal/focus`, M06) needs a running Plasma
  session's D-Bus, not just a login session. The dev-only `make
  install-user` target rewrites its `ExecStart=` line for a non-`/usr`
  `$PREFIX`; the packaged install ships it unmodified.
- `udev/99-knobd.rules` — grants `audio`-group access to the X-Touch
  Mini's rawmidi device by USB vendor/product ID (`1397:00b3`, confirmed
  via `lsusb`). Likely redundant with distro defaults (confirmed
  unnecessary on the development machine) but documents the requirement
  explicitly for distros where it isn't.

There is no `kwin/knobd-focus.js` here (M06): the KWin focus-tracking
script is `go:embed`ded into the `knobd` binary from
`daemon/internal/focus/script/knobd-focus.js` and materialized to
`$XDG_RUNTIME_DIR` at startup rather than installed by packaging — see
that file's header comment and
`specs/adr/0003-focus-tracking-via-kwin-script.md`'s Consequences
section for why (in short: the script/daemon wire contract has no
error-visible failure mode if the two drift, so a stale on-disk copy
from an older build is worse than an install step is worth avoiding).

## Uninstall

```bash
sudo pacman -R knobd-ui knobd
```

`knobd.install`'s `pre_remove` stops the running service (which also
unloads the KWin script — see above) and disables the global enablement
before pacman removes the files. `~/.config/knobd/config.json` and any
`*.bak` files `LoadAndUpgrade` left behind are deliberately **not**
removed — they're per-user data, not package-owned files — so a
reinstall picks the same config back up. Remove them by hand if you
actually want a clean slate: `rm -rf ~/.config/knobd`.

## Non-Arch distributions

Out of scope for M12 (see its spec's Scope section) — there is no
generic multi-distro packaging story here yet. `cd ui && npm run tauri
build` still produces a `.deb` on its own if that's useful in the
meantime; the daemon has no package of its own outside `packaging/arch`.
