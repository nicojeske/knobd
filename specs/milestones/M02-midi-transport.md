# M02: MIDI transport

## Status

**Done** (as of 2026-09-21). Every acceptance criterion below is
checked.

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
`monitor`'s "(held Xms)" annotation on a button release is a display-only
after-the-fact string, not gesture detection — it does not decide
press-vs-hold and produces no `model.Gesture`; that state machine is
M04's, per Scope's own boundary.

## Design

Implemented per ADR 0001, with one correction to that ADR's original
framing: `/dev/snd/by-id/usb-Behringer_X-TOUCH_MINI_*` resolves to the
ALSA *control* device (`/dev/snd/controlC<N>`), not the rawmidi node —
opening it as if it were rawmidi doesn't error, `read(2)` on it just
blocks forever. `midi.Discover` (`discover.go`) follows the symlink one
step further to `/dev/snd/midiC<N>D0`, which is what `midi.Open`
(`rawmidi.go`) actually opens read-write. A background goroutine parses
the byte stream, including running status (`parser.go`) — a live capture
showed the fader's pitch-bend messages can arrive without a repeated
status byte — and delivers to `Read` over a bounded channel that drops
the oldest message rather than ever blocking the reader.

Hotplug uses inotify (`watcher.go`) on `/dev/snd/by-id`, debounced,
because this machine's 6 sound cards make that directory churn on
unrelated USB audio changes; a periodic backstop poll covers a missed or
unavailable inotify event. `midi.Supervisor` (`supervisor.go`) owns
discover → open → read → reconnect-with-backoff and itself implements
`Port`, so `engine` (M04) gets a self-healing connection for free; it
additionally exposes `Connected`/`Disconnected` for M05's LED
re-initialization after a reconnect, since the encoder rings don't
remember their state across a power cycle.

`device.NewXTouchMiniCodec`'s `Decode` (`xtouch.go`) is a pure, stateless
translation of one `midi.Message` at a time — no per-note timer, no
folding a note-on/note-off pair into a single gesture. That's the
resolution to this spec's original open question: gesture timing
(press vs. hold vs. double-press, against `engine.HoldThreshold`) is
`engine`'s job (M04), not `device`'s. Concretely this meant reshaping
`device.Event`: it no longer carries a `model.Gesture` (which has no
raw "down"/"up" value, and which `model.ControlFader.SupportsGesture`
rejects for every value including turn — a fader move literally cannot
be expressed as a `Gesture`). Instead `Event` carries an `EventKind`
(`EventTurn`/`EventButtonDown`/`EventButtonUp`/`EventFaderMove`) plus
separate `Delta` (relative encoder steps) and `Value` (absolute fader
position) fields. This keeps the change entirely inside `device` — no
`model` field, no config schema migration, confirmed by `make schema`
producing no diff.

Standard-mode detection, inline in `Decode`'s channel/controller checks,
is a whitelist, not a guess at Standard mode's exact numbers: any
channel-voice message on a channel other than 1 (9 for the fader), or a
CC outside controllers 16–23, cannot be this unit's MC-mode traffic and
returns `ErrStandardMode`. This catches a factory-default Standard-mode
unit's very first message (channel 11) without needing to have ever
captured Standard mode from this unit to know its exact layout.

## Data model changes

None to `model` (see above for why the `device.Event` reshape didn't
require one). Adds `device.EventKind`, changes `device.Event`'s shape,
adds `midi.DeviceInfo.StableID` (also not part of `model`) and the
sentinel errors in both packages. `midi.Discover`/`midi.Open` and
`device.NewXTouchMiniCodec`'s `Decode` are implemented for real, per
their original scaffolding.

## Acceptance criteria

- [x] `midi.Discover` finds the X-Touch Mini via `/dev/snd/by-id` on the
      development machine. (`TestDiscoverRealDevice`, and confirmed live:
      resolves to `/dev/snd/midiC3D0`.)
- [x] `midi.Open` + `Read` yields correctly-parsed `midi.Message`s for
      every control, verified against `testdata/midi/guided-capture.txt`
      as a parser unit test (no hardware required for this part).
      (`device.TestFixtureGuidedCapture`; the parser itself has its own
      dense synthetic suite in `midi/parser_test.go` since neither
      fixture contains running status — see `testdata/midi/README.md`.)
- [x] Unplugging and replugging the physical device is detected and
      recovered from without restarting the daemon. Verified live: a
      `knobd monitor` session survived a real USB unplug/replug
      mid-capture and kept decoding correctly afterward (see
      `testdata/midi/live-verification-capture.txt`'s provenance note);
      also covered without hardware by
      `midi.TestSupervisorReconnectsAfterDeviceLoss`.
- [x] `device.xtouchMiniCodec.Decode` correctly turns raw messages into
      `Control` + `EventKind` (see Design above for why this is
      `EventKind`, not `Control`+`Gesture` as originally written here —
      gesture detection is `engine`'s job, M04) for every control on the
      map, re-verifying in particular the four bottom-row button notes
      (`specs/reference/xtouch-mini-midi-map.md`'s bottom-row note, née
      "Risk 2" — that cross-reference was stale, the map has no numbered
      risks) that were only inferred, not directly captured, during
      planning. Re-verified live against the physical unit
      (`testdata/midi/live-verification-capture.txt`,
      `device.TestFixtureLiveVerificationCapture`): all four
      (notes 88, 91, 92, 86 → buttons 10–13) confirmed correct.
- [x] A unit passing a Standard-mode-style absolute CC stream to the
      codec fails with a clear, specific error rather than silently
      producing wrong deltas. (`device.TestDecodeStandardMode`.)
- [x] `knobd monitor` (wired up in `cmd/knobd`) prints a readable line
      per event when run against the physical controller. Confirmed
      live for every control family (encoders, encoder-pushes, top and
      bottom row buttons, both side buttons, fader) plus the
      disconnect/reconnect messages.

## Verification

```bash
cd daemon && go test ./internal/midi/... ./internal/device/...
go run ./cmd/knobd monitor
# turn every encoder, press every button, move the fader — confirm each
# produces exactly the expected line, then unplug/replug the USB cable
# and confirm it recovers without restarting the process
```

Run 2026-09-21 against the physical unit: every control family produced
correctly-labeled events (including a full fader sweep and the
previously-unverified bottom-row buttons), and a real unplug/replug
mid-session was detected and recovered without restarting `knobd`.

## Risks & open questions

- ~~Confirm the bottom-row button notes for positions 2–5~~ Resolved:
  confirmed directly against the physical unit, see the acceptance
  criteria above.
- ~~Decide where gesture timing (press vs. hold) actually lives~~
  Resolved: `engine` (M04), not `device` — see Design above. `device`'s
  `Codec.Decode` doc comment now states this explicitly as part of its
  contract, including that a non-nil `Decode` error is never fatal to a
  caller's event loop.
- ~~Hotplug detection approach (inotify vs. polling)~~ Resolved: inotify
  on `/dev/snd/by-id`, debounced, with a periodic poll as a backstop —
  see Design above.
- Not a risk carried forward, but worth a note for M04: `model.Binding`
  cannot express *any* fader binding today, since
  `model.ControlFader.SupportsGesture` returns false for every
  `model.Gesture` including `GestureTurn`. `device.Event`'s
  `EventFaderMove` now exists on the input side; M04 needs to decide how
  a fader binds (most likely: no gesture at all, a continuous
  `Value`-driven target write, per `model.ControlFader.SupportsGesture`'s
  own doc comment) rather than rediscovering this as a surprise.
- Not attempted, and deliberately out of scope: a statistical
  relative-vs-absolute heuristic that could catch a *remapped*
  Standard-mode unit sending absolute values on the same channel/CC
  range MC mode uses. `ErrStandardMode`'s whitelist only catches
  traffic outside that range/channel (which covers this unit's factory
  default). Building the statistical version would require `Decode` to
  hold state, undoing the pure-stateless-codec property the rest of this
  design depends on, to protect against a configuration no one has
  observed on this hardware.
