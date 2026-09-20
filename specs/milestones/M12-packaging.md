# M12: Packaging

## Status

Not started.

## Depends on

M02–M04 at minimum for a daemon worth packaging; realistically also
M07 for a UI worth shipping alongside it. Doesn't strictly need M05/M06/
M08–M11 to be "done," but should be revisited if any of them change
what needs installing (e.g. M06's KWin script, M10's OAuth app
registration step).

## Goal

`knobd` and `knobd-ui` install cleanly on Arch/CachyOS via a PKGBUILD (or
equivalent), with the systemd unit, udev rule, and KWin script all
placed and enabled automatically, config migrations proven safe across
an upgrade, and a documented uninstall path.

## Scope

**In**: static release build of the daemon (`go build` with appropriate
flags — no CGo per ADR 0001 means this should already produce a fully
static binary; confirm with `ldd`), `go:embed` of the built UI's static
assets if bundling the web-servable parts of the config UI alongside the
daemon binary makes sense (vs. Tauri shipping its own bundle
separately — decide during implementation which model fits better), a
PKGBUILD, an install script that places
`packaging/systemd/knobd.service`, `packaging/udev/99-knobd.rules`, and
`packaging/kwin/knobd-focus.js` in their correct locations and
enables/reloads what's needed (`systemctl --user enable --now
knobd.service`, `udevadm control --reload-rules`), an uninstall script,
and a config-migration smoke test (install an old-schema config, upgrade,
confirm it still loads).

**Out**: packaging for any distribution other than Arch-based ones
unless/until there's a reason to support one — don't build a generic
multi-distro packaging story speculatively.

## Design

Verify the static-binary claim first: `go build -o knobd ./cmd/knobd &&
ldd knobd` should report "not a dynamic executable" (or only libc, on
some builds) — if any CGo dependency snuck in via a transitive module
across M02–M11, this is where it'd be caught, and it's worth catching
before packaging rather than after.

PKGBUILD: standard Arch package build/install/package functions;
install the daemon binary to `/usr/bin/knobd` (or `/usr/lib/knobd/` if
`go:embed`'d UI assets make sense to keep separate from a user-facing
binary name), the systemd unit to
`/usr/lib/systemd/user/knobd.service`, the udev rule to
`/usr/lib/udev/rules.d/99-knobd.rules`, and the KWin script to a location
the daemon can find at a well-known path (update
`daemon/internal/focus`'s script-loading code, currently referencing a
relative/TODO path, to use the installed location).

Migration smoke test: since `model.CurrentSchemaVersion` may have moved
past 1 by the time this milestone runs, construct a fixture config at
schema version 1 (or whatever the oldest supported version is) and
assert `config.Load` upgrades it correctly — this exercises
`daemon/internal/config/migrate.go`'s chain for real, which M01 could
only scaffold without.

## Data model changes

None expected — this milestone packages what exists rather than
extending the model. If it surfaces a migration bug, fix it in
`daemon/internal/config`/`model`, not here.

## Acceptance criteria

- [ ] `ldd knobd` confirms no unexpected dynamic dependencies.
- [ ] `makepkg` builds a working package from the PKGBUILD.
- [ ] Installing the package places and enables the systemd unit, udev
      rule, and KWin script without manual steps.
- [ ] A fixture config at the oldest supported schema version loads
      correctly and is upgraded in place after an install/upgrade.
- [ ] Uninstalling removes everything the install script placed, and
      stops/disables the service.

## Verification

```bash
cd daemon && go build -o knobd ./cmd/knobd && ldd knobd
cd packaging && makepkg -si   # or wherever the PKGBUILD ends up living
systemctl --user status knobd.service
```

Then a full install → use → upgrade (with a schema-version bump
simulated via a fixture) → uninstall cycle, by hand, once.

## Risks & open questions

- Whether to embed the UI's static assets in the daemon binary
  (`go:embed`) or ship Tauri's own bundle as a fully separate package is
  not decided — Tauri produces its own installable artifact (AppImage/
  `.deb`/etc. via `tauri build`), which may make a second, UI-specific
  PKGBUILD the more natural fit than embedding. Decide once M07's Tauri
  build actually exists to look at.
- The KWin script's installed-path lookup in `daemon/internal/focus`
  needs updating to match wherever this milestone actually installs it
  — currently a TODO placeholder, not a real path.
