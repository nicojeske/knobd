# M05: LED feedback

## Status

Not started.

## Depends on

M04 (a running engine with resolved volume state to display).

## Goal

The X-Touch Mini's own LEDs become the mixer's display: each bound
encoder's ring shows its target's current volume, each mute-capable
button's LED shows mute state, an unbound (pooled or genuinely unbound)
encoder's ring is blank, and assigning a knob to the focused app
(M06's `knob.assign_focused_app`) flashes a confirmation.

## Scope

**In**: empirically calibrating and implementing `device.Codec.
EncodeLED`, wiring `engine` to push LED updates whenever resolved volume/
mute state changes (both from local actions and from external changes
picked up via `audio.Backend.Subscribe`), rate-limiting updates against
the fader's high message rate observed during planning (404 pitch-bend
messages in one session) so LED writes don't become a bottleneck.

**Out**: any new `model.Action` types — this milestone is pure output,
driven by state M04 already computes.

## Design

**Start with calibration, not implementation.** Per
`specs/reference/xtouch-mini-midi-map.md`'s LED section, the ring/button
LED byte encoding is *documented* (button LEDs: Note On, velocity
0/1/2; rings: CC 48–55, `value = mode<<4 | position`) but **not verified
against this physical unit** — writing to the device is a state change
that was deliberately not exercised during read-only planning. Before
writing `xtouchMiniCodec.EncodeLED`'s real logic:

1. Write a tiny throwaway script that sweeps CC 48 (encoder 1's ring)
   through its full value range while watching the physical ring.
2. Record which values map to which visible position/mode in this
   file, replacing the "to be verified" language in the reference doc
   with confirmed values.
3. Repeat for a button LED's velocity values.

Then implement `EncodeLED` against confirmed values, and wire `engine`
to call it: on every resolved volume/mute change (both from an action
just executed and from an `audio.Event` arriving via `Subscribe`), map
the affected `Target`'s bound `Control`(s) — a target can have more than
one control bound to it — to an `LEDUpdate` and write it via `midi.Port
.Write`.

## Data model changes

None to `model`. `device.LEDMode`/`LEDUpdate` (already scaffolded)
likely need adjustment once real values are confirmed in step 2 above —
update `daemon/internal/device/codec.go`'s comments to match reality
rather than leaving the pre-verification guess in place.

## Acceptance criteria

- [ ] Ring/button LED encoding confirmed against the physical unit and
      written down in `specs/reference/xtouch-mini-midi-map.md`,
      replacing its "not yet verified" section.
- [ ] Turning a bound encoder updates its own ring in real time to
      reflect the new volume.
- [ ] Changing an app's volume from *outside* knobd (e.g. via
      `pavucontrol`) updates the corresponding ring within a short,
      perceptible delay.
- [ ] Muting a target via a knobd button toggles that button's LED.
- [ ] An encoder with no target shows a blank/off ring.
- [ ] LED updates don't visibly lag or drop frames during rapid fader
      movement (rate-limiting works).

## Verification

Physical-hardware verification is unavoidable for this milestone — LED
state can't be usefully asserted in a unit test. Manual checklist:
turn a bound encoder and watch its ring; move the volume slider in
`pavucontrol` for a bound app and watch the corresponding ring update;
press a mute button and watch its LED; assign a knob to the focused app
and confirm the flash.

## Risks & open questions

- The documented encoding might not match this exact unit/firmware
  revision — that's the entire reason calibration is step 1, not an
  assumption to implement against directly.
- Rate-limiting strategy (debounce vs. fixed-interval flush) isn't
  decided yet — pick one during implementation and record it here.
