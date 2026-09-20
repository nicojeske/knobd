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
- `kwin/knobd-focus.js` — the KWin focus-tracking script; see its header
  comment and `specs/adr/0003-focus-tracking-via-kwin-script.md`. Not
  implemented yet (M06).
