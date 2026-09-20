# ADR 0001: MIDI I/O via pure-Go ALSA rawmidi, not CGo

**Status**: Accepted

## Context

The daemon needs to read from and write to the X-Touch Mini's MIDI
port. The obvious library choice, `librtmidi` (and its Go bindings,
`gitlab.com/gomidi/rtmididrv`), requires CGo and a system install of
`librtmidi` — confirmed absent from the development machine
(`pacman -Q rtmidi` failed; only `alsa-lib` is present).

During planning, `/dev/snd/midiC3D0` (the X-Touch Mini's ALSA rawmidi
device, confirmed via `amidi -l` and `lsusb`) was read directly with a
20-line Python script doing plain `open()`/`read()` on the character
device, no library at all. It produced clean, correctly-framed 3-byte
MIDI messages.

## Decision

Implement `daemon/internal/midi`'s real backend as a pure-Go reader/
writer against the ALSA rawmidi character device
(`/dev/snd/by-id/usb-Behringer_X-TOUCH_MINI_1.0.1-00`, resolved via
`/dev/snd/by-id` rather than a card number — see
`specs/reference/environment.md`), with no CGo and no dependency on
`librtmidi` being installed.

`gitlab.com/gomidi/midi/v2` (pure Go, confirmed reachable on the module
proxy) is an acceptable dependency for message *parsing/encoding
helpers* if it pulls its weight, but the transport itself — opening and
reading/writing the device file — is deliberately kept in-house rather
than taken from a library, since it's a small amount of code and the
project's static-binary property (see the "Consequences" section)
depends on nobody upstream deciding to require CGo later.

## Alternatives considered

- **CGo + librtmidi**: the "standard" cross-platform MIDI library, but
  requires an external system dependency and breaks single-binary
  distribution. Rejected — this project targets exactly one platform
  (Linux/ALSA) and gets nothing from RtMidi's Windows/macOS backends.
- **`gitlab.com/gomidi/midi/v2`'s own ALSA driver** (`imidi`/`rtmididrv`
  backends): still CGo-based for the ALSA driver specifically. Kept as a
  documented fallback in `daemon/internal/midi`'s package doc comment in
  case the direct rawmidi approach hits a wall (e.g. multi-port devices,
  SysEx handling) M02 didn't anticipate, but not the default.

## Consequences

- The daemon stays a single static binary with no `.so` dependencies
  beyond libc, simplifying packaging (M12) to "copy one file."
- knobd owns MIDI running-status parsing itself rather than getting it
  for free from a library — a real, if small, implementation burden for
  M02.
- If a future controller needs SysEx or multi-port MIDI in ways the
  rawmidi character device doesn't expose cleanly, this decision may
  need revisiting. Not a concern for the X-Touch Mini today.
- Standard-mode absolute CC (see
  `specs/reference/xtouch-mini-midi-map.md`'s note on it) would need
  pickup/takeover logic in `daemon/internal/device`'s codec — orthogonal
  to this ADR, but worth remembering it isn't handled by "using rawmidi
  directly" on its own.
