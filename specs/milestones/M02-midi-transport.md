# M02: MIDI transport

## Status

Not started.

## Depends on

M01 (`daemon/internal/midi`'s `Port` interface and `FakePort`,
`daemon/internal/model`'s `Control`/`Gesture` types).

## Goal

`knobd monitor` prints a live, correctly-decoded stream of every turn,
press, hold, and release on the physical X-Touch Mini, reconnecting
automatically if the device is unplugged and replugged.

## Scope

**In**: real device discovery, opening the rawmidi character device,
running-status-aware message parsing, hotplug detection and reconnect,
the X-Touch Mini MC-mode codec (`daemon/internal/device`) turning raw
messages into `device.Event`, a `knobd monitor` debug subcommand.

**Out**: doing anything with the decoded events beyond printing them
(that's M04's engine) and LED output (M05). Standard-mode (absolute CC)
support is not required — detect it and fail clearly rather than
silently misinterpreting it (see
`specs/reference/xtouch-mini-midi-map.md`'s Standard-mode note).

## Design

Implement `midi.Discover` and `midi.Open`
(`daemon/internal/midi/port.go`) per ADR 0001: scan
`/dev/snd/by-id/usb-Behringer_X-TOUCH_MINI_*`, open with `os.OpenFile`
in read-write mode, and run a background goroutine parsing the byte
stream — including running status, since a live capture showed the
fader's pitch-bend messages can arrive without a repeated status byte.
Hotplug: watch `/dev/snd` with `inotify` (or poll every second or two as
a simpler first cut) and reconnect with backoff when the device
reappears.

Implement `device.NewXTouchMiniCodec`'s `Decode`
(`daemon/internal/device/codec.go`) against
`specs/reference/xtouch-mini-midi-map.md`. Two things the map calls out
that the codec must get right:

- Encoder turns are **relative** — `Event.Delta` is the signed
  detent count, there's no absolute position to track per encoder.
- Encoder-push / button note-offs should generally fold into a single
  `device.Event` per gesture (see `model.Gesture`'s doc comment: a short
  press delivers only `GesturePress`, not a separate `GestureRelease`) —
  this requires the codec (or a thin layer above it) to hold a per-note
  timer, which is really the seed of M04's gesture detection. Decide
  during implementation whether that timer lives in `device` or is
  pushed up into `engine` as a "raw press/release" event with `engine`
  doing the press/hold/release timing — the latter keeps `device` a
  pure, stateless codec, which is probably right.

## Data model changes

None to `model`. Adds `device.Event`, `device.LEDUpdate` (already
scaffolded, M02 only needs the `Decode` half) and `midi.DeviceInfo`
(already scaffolded).

## Acceptance criteria

- [ ] `midi.Discover` finds the X-Touch Mini via `/dev/snd/by-id` on the
      development machine.
- [ ] `midi.Open` + `Read` yields correctly-parsed `midi.Message`s for
      every control, verified against `testdata/midi/guided-capture.txt`
      as a parser unit test (no hardware required for this part).
- [ ] Unplugging and replugging the physical device is detected and
      recovered from without restarting the daemon.
- [ ] `device.xtouchMiniCodec.Decode` correctly turns raw messages into
      `Control`+`Gesture` for every control on the map, re-verifying in
      particular the four bottom-row button notes
      (`specs/reference/xtouch-mini-midi-map.md`'s Risk 2) that were
      only inferred, not directly captured, during planning.
- [ ] A unit passing a Standard-mode-style absolute CC stream to the
      codec fails with a clear, specific error rather than silently
      producing wrong deltas.
- [ ] `knobd monitor` (wired up in `cmd/knobd`) prints a readable line
      per event when run against the physical controller.

## Verification

```bash
cd daemon && go test ./internal/midi/... ./internal/device/...
go run ./cmd/knobd monitor
# turn every encoder, press every button, move the fader — confirm each
# produces exactly the expected line, then unplug/replug the USB cable
# and confirm it recovers without restarting the process
```

## Risks & open questions

- Confirm the bottom-row button notes for positions 2–5 (currently
  inferred; see `specs/reference/xtouch-mini-midi-map.md`) against the
  physical unit as an early step, not an afterthought.
- Decide where gesture timing (press vs. hold) actually lives — see
  Design above. Whichever way it goes, keep `device`'s `Codec` interface
  honest about what it does and doesn't decide.
- Hotplug detection approach (inotify vs. polling) is an implementation
  detail worth a quick spike before committing — inotify is more
  correct but `/dev/snd`'s churn on unrelated device changes (this
  machine has 6 sound cards) needs filtering to avoid needless
  re-scans.
