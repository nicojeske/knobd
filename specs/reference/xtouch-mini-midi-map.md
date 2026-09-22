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
left to right, one at a time). The bottom row was originally only
*partially* verified live during planning: positions 1, 6, 7, 8 (notes
87, 93, 94, 95) were captured directly; positions 2–5 (88, 91, 92, 86)
were filled in from the publicly documented Mackie Control button map
for this device. M02 re-verified this against the physical unit
(2026-09-21, via `knobd monitor --raw`; see
[`testdata/midi/live-verification-capture.txt`](../../testdata/midi/live-verification-capture.txt))
and confirmed positions 2, 3, 4, 5 (notes 88, 91, 92, 86) directly, each
by pressing the physical button and reading back its note number live —
all 8 bottom-row notes now have direct confirmation on this unit. M07's
MIDI-learn feature means end users never actually depend on this table
being letter-perfect regardless.

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

## LEDs (buttons and encoder rings) — confirmed 2026-09-22

Verified live against the physical unit via `knobd calibrate-leds` (M05)
— see [M05](../milestones/M05-led-feedback.md)'s Status section for the
session notes. Writing to the device changes its visible state, which is
why this table stayed unconfirmed through M02–M04's read-only planning.

- **Button LEDs** (grid buttons, side buttons): Note On, channel 1, same
  note numbers as the button itself (see the tables above — verified
  directly for button 1 (note 89), button 9 (note 87), and both side
  buttons (84/85); addressing generalizes to the rest by the same
  table). Velocity:
  - `0` = off
  - `1`–`2` = blinking
  - `3`–`127` = solid on

  This is the **opposite** of the publicly-documented guess this section
  used to carry (`0`/`1`/`2` = off/on/blinking) — the blink range is at
  the *bottom* of the scale, not velocity `2` alone. knobd uses velocity
  `0` for off and `127` for on; nothing currently uses blinking.

  Encoder pushes (notes 32–39) and the fader have **no LED at all** —
  confirmed by sending full-velocity Note On to note 32 (encoder 1's
  push) and observing no response anywhere, on the push or its ring.

- **Encoder rings**: CC **48–55**, channel 1 (encoder 1 → CC 48, ...,
  encoder 8 → CC 55 — verified directly on CC 48 and CC 55, the two
  ends of the range). `value = (mode << 4) | position`.

  The ring has 13 physical LED segments, but only **11 are individually
  addressable** (physical LEDs 2–12); the two end segments (1 and 13)
  never light from any position value — they read as fixed bezel marks,
  not real LEDs. So `position` is `0–11`: `0` is off, `1`–`11` address
  the 11 real LEDs in order, and any value `> 11` clamps at position 11
  rather than erroring or wrapping (verified: 11, 12, and 15 all look
  identical).

  `mode` (bits 4–5) is one of four values, all verified:

  | mode | name | appearance |
  |---|---|---|
  | `0` | single-dot | exactly one LED lit, at `position` |
  | `1` | pan | a 3-LED-wide bar centered near `position`, sliding along the ring |
  | `2` | fill | a growing bar from LED 2 through `position + 1` — `position` LEDs lit, starting at the addressable end. **This is the mode knobd uses for volume display**: `position = round(percent/100 * 11)` gives a natural 0–100% bar, `0` empty, `11` fully lit. |
  | `3` | spread | a bar that grows symmetrically outward from the ring's center |

  Modes 1 and 3 are documented here for completeness; knobd's `EncodeLED`
  only emits mode 2.

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
