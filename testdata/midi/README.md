# MIDI capture fixtures

Raw captures from a Behringer X-Touch Mini in Mackie Control (MC) mode,
taken directly from `/dev/snd/midiC3D0` (also reachable via
`amidi -p hw:X-TOUCHMINI` or the stable path
`/dev/snd/by-id/usb-Behringer_X-TOUCH_MINI_1.0.1-00`, which resolves to
the *control* device — see `daemon/internal/midi`'s package doc comment
for why the rawmidi node itself has to be opened by its `midiC<N>D0`
path instead).

These back the parser tests in `daemon/internal/midi` and `daemon/internal/device`
(M02) and the authoritative map at `specs/reference/xtouch-mini-midi-map.md`.

## Files

- `free-play-capture.txt` — ~25s of `amidi -d` output (space-separated
  hex bytes, one 3-byte message per line), while freely operating every
  control at least once. Good for a "does the parser choke on anything"
  smoke test. Format: `status data1 data2`, all hex, one leading blank
  line, no trailing newline.
- `guided-capture.txt` — a scripted, timestamped capture
  (`seconds status data1 data2`, **status in hex, the two data bytes in
  decimal** — see Format notes below) following a fixed sequence: turn
  an encoder, push two encoder-pushes, press the full top row 1-8 left
  to right, press both side buttons. In practice this capture only
  exercises encoder 8, not encoder 1 as originally intended, and
  contains no bottom-row or fader traffic — see
  `daemon/internal/device`'s `TestFixtureGuidedCapture`, which asserts
  the sequence the file actually contains rather than the one originally
  planned. Still useful for validating control-index-to-message mapping
  since events are in a known order.
- `live-verification-capture.txt` — captured 2026-09-21 via
  `knobd monitor --raw` against the physical unit, specifically to
  re-verify the bottom-row button notes that `guided-capture.txt` never
  exercised (see the map's former Risk 2). All-hex `status data1 data2`,
  one message per line, no timestamp column. Exercises encoders 1, 2, 4,
  5; bottom-row buttons 9, 10, 11, 12, 13, 14, 16 (button 15/note 94
  wasn't pressed this time — everything else on the bottom row now has
  direct confirmation); top-row buttons 2, 3, 4, 5; encoder-push 6; both
  side buttons; and a full fader sweep down and back up. This capture
  also spans a live USB unplug/replug partway through (a manual
  hotplug-recovery check, not something the byte stream itself records)
  — the daemon reconnected and kept decoding correctly afterward.

## Format notes

- Encoder turns arrive as relative CC on channel 1, controllers 16-23:
  values 1-63 mean "clockwise by N", 65-127 mean "counter-clockwise by
  N-64". There is no absolute position.
- The fader sends pitch bend on channel 9 (status byte `0xE8`), 7-bit
  value in the data bytes with the LSB (`data1`) always observed as `0`.
- Buttons and encoder pushes are Note On/Off on channel 1; velocity 127
  on press, 0 on release.
- `guided-capture.txt`'s status column is **hex** (`90`, `B0`); its two
  data columns are **decimal**. A parser using the same base for all
  three columns will silently decode the wrong control.
- Neither fixture contains MIDI running status (every line/message
  carries its own status byte) — `amidi -d` and `knobd monitor --raw`
  both normalize it away. The running-status branch of
  `daemon/internal/midi`'s parser is exercised by synthetic byte-slice
  tests instead (`parser_test.go`), not by these fixtures.
