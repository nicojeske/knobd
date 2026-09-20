# Behringer X-Touch Mini — MIDI protocol reference

Captured live from the physical unit on 2026-09-20, in its factory
default **Mackie Control (MC) mode** (the mode selector switch on the
back of the unit was not touched). Raw captures backing this table live
in [`testdata/midi/`](../../testdata/midi/). This is the authoritative
source `daemon/internal/device`'s codec (M02, M05) implements against —
if you find a discrepancy by testing the real hardware, fix this file in
the same change.

## Encoders (turn) — relative CC

| Control | Message |
|---|---|
| Encoder 1–8, turn | `Bn 1(0+i) vv`, channel 1, controller **16–23** (i = encoder index 0–7) |

**Values are relative, not absolute** — there is no "current position"
to read back from the wire:

- `1`–`63` = clockwise, magnitude = value (i.e. `3` means "3 detents
  clockwise since the last message").
- `65`–`127` = counter-clockwise, magnitude = `value − 64`.
- `64` and `0` were not observed and should be treated as "no movement"
  if they ever appear.

This is exactly what's wanted for volume control: the encoder is
endless (no mechanical end stops) and reports *change*, not position, so
there's no pickup/takeover problem to solve the way there would be with
absolute CC.

## Encoder pushes (each encoder is also a button)

| Control | Message |
|---|---|
| Encoder-push 1–8 | Note On/Off, channel 1, note **32–39** (encoder 1 → note 32, ..., encoder 8 → note 39) |

Verified directly: pushing encoder 1 sent note 32, encoder 8 sent note
39, velocity 127 on press / 0 on release, confirming the 1:1 offset for
the ones in between.

## Grid buttons (16, in two rows of 8)

| Control | Note |
|---|---|
| Top row, left→right (1–8) | 89, 90, 40, 41, 42, 43, 44, 45 |
| Bottom row, left→right (9–16) | 87, 88, 91, 92, 86, 93, 94, 95 |

**Non-contiguous — do not assume `note = 40 + index`.** All 8 top-row
notes were verified directly in a guided, timestamped capture (pressed
left to right, one at a time). The bottom row was only *partially*
verified live: positions 1, 6, 7, 8 (notes 87, 93, 94, 95) were captured
directly; positions 2–5 (88, 91, 92, 86) are filled in from the publicly
documented Mackie Control button map for this device, consistent with
what was captured but not independently confirmed on this unit. **M02
should re-verify the middle four before relying on them**, and M07's
MIDI-learn feature means end users never actually depend on this table
being letter-perfect.

## Side buttons

| Control | Note |
|---|---|
| Upper side button | 84 |
| Lower side button | 85 |

Both on channel 1. Reserved by default for
[`model.ActionLayerMomentary`](../../daemon/internal/model/action.go).

## Fader

| Control | Message |
|---|---|
| Fader | Pitch bend, **channel 9** (status byte `0xE8`) |

7-bit value carried in the message's data bytes (the second/LSB byte was
always observed as `0`), giving an effective range of 0–127, absolute
(unlike the encoders). 404 pitch-bend messages were captured during a
single free-play session — expect a high message rate from fader moves
and rate-limit LED/UI updates accordingly rather than acting on every
one.

## LEDs (buttons and encoder rings) — not yet verified

Writing to the device changes its visible state, so this was
deliberately not tested live while only planning the project (see the
plan's stated risk). The following is the documented MC-mode encoding
for this device family and needs empirical confirmation in
[M05](../milestones/M05-led-feedback.md) before anything depends on it:

- **Button LEDs**: Note On, channel 1, same note numbers as the button
  itself, velocity `0` = off, `1` = on, `2` = blinking.
- **Encoder rings**: CC **48–55**, channel 1 (encoder 1 → CC 48, ...,
  encoder 8 → CC 55). Value encodes both a display mode and a ring
  position: `value = (mode << 4) | position`, with `position` in
  `0–12` (13 LEDs around the ring) and `mode` one of single-dot, pan,
  fan, or spread (the exact 2-bit mode values are the first thing M05's
  calibration step should pin down).

## Standard mode (not currently in use)

The unit was found in MC mode; it also supports a "Standard" mode
(selected via a physical DIP switch / button combo, differs by exact
model revision) that sends **absolute** CC instead of relative — turning
an encoder jumps straight to a value rather than reporting a delta. If
this project ever needs to support a unit in Standard mode, the codec
needs pickup/takeover logic to avoid the value jumping when a knob's
physical position doesn't match the last-known logical value; see
[ADR 0001](../adr/0001-pure-go-alsa-rawmidi.md)'s note on this and
`daemon/internal/device`'s package doc comment.
