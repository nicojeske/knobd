# packaging/

Files installed alongside the daemon, none of them wired up by any
install step yet (that's M12 — see
`specs/milestones/M12-packaging.md`).

- `systemd/knobd.service` — user service unit. Targets
  `graphical-session.target` rather than plain `default.target` because
  focus tracking (`daemon/internal/focus`, M06) needs a running Plasma
  session's D-Bus, not just a login session.
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
