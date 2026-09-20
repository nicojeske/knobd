# MIDI capture fixtures

Raw captures from a Behringer X-Touch Mini in Mackie Control (MC) mode,
taken directly from `/dev/snd/midiC3D0` (also reachable via
`amidi -p hw:X-TOUCHMINI` or the stable path
`/dev/snd/by-id/usb-Behringer_X-TOUCH_MINI_1.0.1-00`).

These back the parser tests in `daemon/internal/device` (M02) and the
authoritative map at `specs/reference/xtouch-mini-midi-map.md`.

## Files

- `free-play-capture.txt` — ~25s of `amidi -d` output (space-separated hex
  bytes, one 3-byte message per line except running pitch-bend) while
  freely operating every control at least once. Good for a "does the
  parser choke on anything" smoke test.
- `guided-capture.txt` — a scripted, timestamped capture
  (`seconds status data1 data2`, decimal) following a fixed sequence:
  turn knob 1 then knob 8, push knob 1 then knob 8, press top row 1-8
  left to right, press bottom row 1-8 left to right (bottom row cut
  short — see `specs/reference/xtouch-mini-midi-map.md` risk notes),
  side buttons upper then lower. Use this one to validate
  control-index-to-message mapping since events are in a known order.

## Format notes

- Encoder turns arrive as relative CC on channel 1, controllers 16-23:
  values 1-63 mean "clockwise by N", 65-127 mean "counter-clockwise by
  N-64". There is no absolute position.
- The fader sends pitch bend on channel 9 (status byte `0xE8`), 7-bit
  value in the data bytes with the LSB always 0.
- Buttons and encoder pushes are Note On/Off on channel 1; velocity 127
  on press, 0 on release.
