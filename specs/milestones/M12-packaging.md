# M12: Packaging

## Status

In progress (2026-09-24). Everything below is implemented and
automatically verified (`make check-static`, `daemon/internal/config`'s
migration tests, a full `makepkg` build); the by-hand install → use →
upgrade → uninstall cycle against a real machine (this milestone's last
acceptance criterion) is still open.

## Depends on

M02–M04 at minimum for a daemon worth packaging; realistically also
M07 for a UI worth shipping alongside it. Doesn't strictly need M05/M06/
M08–M11 to be "done," but should be revisited if any of them change
what needs installing (e.g. M10's OAuth app registration step).

## Goal

`knobd` and `knobd-ui` install cleanly on Arch/CachyOS via a PKGBUILD,
with the systemd unit and udev rule placed and enabled automatically,
config migrations proven safe across an upgrade, and a documented
uninstall path.

## Scope

**In**: static release build of the daemon (confirmed with `ldd` via
`make check-static`), a split PKGBUILD (`packaging/arch`) producing
`knobd` and `knobd-ui` packages, an install hook
(`packaging/arch/knobd.install`) that enables/starts/restarts/stops the
systemd user unit for live sessions (pacman runs as root and has no
`--user` session of its own), a desktop entry + icons for the UI, and a
config-migration test that persists the upgrade to disk (not just in
memory) and proves it with real fixtures.

**Out**: packaging for any distribution other than Arch-based ones
unless/until there's a reason to support one — don't build a generic
multi-distro packaging story speculatively. Systemd socket activation
(`knobd.socket`, replacing the dial-then-unlink dance in
`daemon/internal/api/server.go`'s `listen`) is deferred to a future
follow-up rather than folded into this milestone.

## Design

**Static binary.** `make release` builds with `CGO_ENABLED=0`,
`-trimpath`, and a stripped `-ldflags "-s -w"`; `make check-static`
builds it and fails unless `ldd` reports "not a dynamic executable" —
wired into CI right after the plain `go build` step, since a CGo
dependency sneaking in via a transitive module wouldn't otherwise be
caught until someone tried to package it.

**Split package, not `go:embed`.** The config UI only ever talks to the
daemon through Tauri's own Rust bridge
(`ui/src-tauri/src/{socket,commands,events}.rs`), never as a browser
page — there's nothing web-servable to `go:embed` into the daemon
binary. `packaging/arch/PKGBUILD` is one `pkgbase=knobd` producing two
packages from one checkout: `knobd` (the daemon: binary, systemd unit,
udev rule) and `knobd-ui` (`ui/src-tauri/target/release/knobd-ui`, built
with `tauri build --no-bundle` since this PKGBUILD does its own install
instead of using Tauri's `.deb` bundler, plus a desktop entry and
hicolor icons). `knobd-ui` depends on `knobd`.

**KWin script needs nothing here.** M06 already solved this: the script
is `go:embed`ded into the daemon from
`daemon/internal/focus/script/knobd-focus.js` and materialized to
`$XDG_RUNTIME_DIR` at startup (see that file's header comment and ADR
0003's Consequences section). There is no `packaging/kwin/` directory
and no installed-path lookup to update — the version this spec
originally described (a static install path for the script) was never
built; the go:embed approach replaced it during M06.

**Enabling without manual steps.** pacman's transaction runs as root
with no `systemctl --user` session to talk to, so
`packaging/arch/knobd.install` does two things: `systemctl --global
enable knobd.service` in `post_install` (covers every user at their next
login), and, for every currently logged-in user (`loginctl list-users`),
starts/restarts/stops the unit directly via `systemctl --user -M
"$user@"`. `post_upgrade` restarts (not full-cycles) the unit for live
sessions — that's also the point at which the on-disk config actually
gets migrated, see below. Every call is best-effort (`|| true`); a
session pacman can't reach is skipped, never a transaction failure.
Reloading udev rules and doing a plain `systemd --user daemon-reload`
are already handled by pacman's own hooks
(`35-systemd-udev-reload.hook`, `30-systemd-daemon-reload-user.hook`),
so the install script doesn't duplicate them.

**Migration persists to disk.** `config.Load` (pre-M12) migrated a
document in memory but never wrote the result back — so "upgraded in
place" wasn't actually true. `daemon/internal/config` now has
`LoadAndUpgrade`, which does what `Load` does but also `Save`s the
migrated result and leaves a byte-for-byte backup of the pre-migration
file at `<path>.v<N>.bak` (skipped if that backup already exists, so
repeated calls before the backup is ever cleaned up don't clobber it).
knobd's startup (`cmd/knobd/main.go`) uses `LoadAndUpgrade`; the SIGHUP
reload path keeps using plain `Load`, since a hand-edited file on disk
shouldn't be silently rewritten by a reload.
`testdata/config/{v1,v0-unversioned}.json` are realistic fixtures
(several profiles, matchers, a group, a scene) exercising every
top-level collection the migration path walks, not just
`model.Default()`'s skeleton — see that directory's README.

## Data model changes

None — `model.CurrentSchemaVersion` is still 1 and no migration steps
exist yet. `config_test.go`'s upgrade test exercises the migration loop
for real (not just a no-op) the same way `migrate_test.go` already did:
by temporarily raising `currentSchemaVersion` and registering a
synthetic step, since there's no real schema bump to test against.

## Acceptance criteria

- [x] `ldd knobd` confirms no unexpected dynamic dependencies
      (`make check-static`, in CI).
- [x] `makepkg` builds a working package from the PKGBUILD.
- [x] Installing the package places and enables the systemd unit and
      udev rule without manual steps (KWin needs no install step — see
      Design).
- [x] A fixture config at the oldest supported schema version loads
      correctly and is upgraded in place after an install/upgrade
      (`TestLoadAndUpgradeRewritesFileAndBacksUpOriginal`).
- [ ] Uninstalling removes everything the install script placed, and
      stops/disables the service — verified against `makepkg`'s output
      package contents; the by-hand `pacman -R` cycle on a real machine
      is still open.

## Verification

```bash
make check-static                 # ldd knobd -> "not a dynamic executable"
cd packaging/arch && makepkg -si
systemctl --user status knobd.service
```

Then a full install → use → upgrade (with a schema-version bump
simulated via a fixture) → uninstall cycle, by hand, once:

```bash
# after a normal install, simulate an old config found on upgrade:
cp testdata/config/v0-unversioned.json ~/.config/knobd/config.json
# bump pkgrel in the PKGBUILD, `makepkg -si` again -- knobd.install's
# post_upgrade restarts the live unit, which is when LoadAndUpgrade
# runs; confirm ~/.config/knobd/config.json now has a "schemaVersion"
# field and a config.json.v1.bak sits next to it with the original
# unversioned content.
sudo pacman -R knobd-ui knobd
# confirm: service inactive, /etc/systemd/user/*.wants/knobd.service
# gone, all files pacman -Ql listed are gone, ~/.config/knobd/ untouched
```

## Risks & open questions

- Systemd socket activation (a `knobd.socket` unit, replacing the
  dial-then-unlink race documented in `daemon/internal/api/server.go`'s
  `listen`) was considered for this milestone and deliberately deferred
  — it's a real improvement but orthogonal to "does the package
  install," and folding it in here would have coupled two independently
  reviewable changes.
- `packaging/arch/PKGBUILD`'s `pkgver()` falls back to a git revision
  count + short hash (`r<N>.<hash>`) since there are no release tags
  yet; switch to a tag-based scheme once tags exist.
- The by-hand end-to-end cycle (install → use → upgrade → uninstall on
  a real Arch/CachyOS machine, per this milestone's own Testing-without-
  hardware carve-out in `CLAUDE.md`) hasn't been run yet — everything
  else here is automatically verified, but this is the one step that
  genuinely needs a human at the keyboard.
