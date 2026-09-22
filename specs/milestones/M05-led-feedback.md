# M05: LED feedback

## Status

**Done** (as of 2026-09-22). Every acceptance criterion below is
checked except the one that depends on M06 (which doesn't exist yet).
Everything else — the calibration sweep, the startup repaint, a live
knob turn, an external `pactl`/pavucontrol-style change, mute toggling,
an unbound ring staying blank, a rapid fader sweep, and an unplug/replug
reconnect — was verified against the physical X-Touch Mini and a real
PipeWire session on this machine, not just fakes.

A few things changed from this spec's original shape, decided during
implementation:

- **Calibration is a permanent `knobd calibrate-leds` subcommand**, not
  the "tiny throwaway script" originally planned. It sends one raw
  CC/Note message and exits (LED state latches on the device, so it
  doesn't need to hold a connection open) — a drop-in sibling of
  `monitor`/`monitor-audio` that stays useful for re-verifying against a
  firmware update or a second unit, rather than being written once and
  discarded.
- **The documented byte encoding was wrong in one place**: button LED
  velocity 0/1/2 is not off/on/blinking as publicly documented — it's
  0 = off, 1–2 = blinking, 3–127 = solid on. See
  `specs/reference/xtouch-mini-midi-map.md`'s LED section for the full
  confirmed table (all four ring display modes, the addressable
  11-of-13 LED range, etc.).
- **`model.Default()` now ships a starter binding set** (encoder/button
  1 → `default_sink`, encoder/button 2 → `default_source`, the fader
  following `default_sink`) instead of zero bindings. Strictly this is
  M07/config-UI territory, but M05's own acceptance criteria need
  something bound to demonstrate against on a freshly-installed system,
  and it's what made the hardware checklist below possible to run at
  all without hand-writing a config first.
- **A second, related gap got fixed alongside the LED work**: a
  `default_sink`/`default_source`-bound ring would have gone stale after
  an external change, since `engine.handleAudioEvent` only ever fed the
  level cache from stream events (see that method's comment — an
  `EventDeviceChanged` carries no `Ref`). `dispatch.go` gained a
  `refreshRefs` work item so a resync also `GetVolume`s every sink/
  source ref an active binding targets. Verified live: a `pactl
  set-sink-volume` from outside knobd updated the ring within about a
  second.
- **`actions.VolumeOptions.OnApplied` needed two fixes** to actually
  serve as the "instant local echo" seam it was built for in M04: it
  only fired from `writeVolume`, never from `executeMuteToggle` (a
  button LED would have waited for the `Subscribe` echo to show a mute)
  or from `ensureUnmuted`'s implicit un-mute (a correctness gap
  independent of LEDs — the cache could stay stale if the write that
  followed then failed).

## Depends on

M04 (a running engine with resolved volume state to display).

## Goal

The X-Touch Mini's own LEDs become the mixer's display: each bound
encoder's ring shows its target's current volume, each mute-capable
button's LED shows mute state, an unbound encoder's ring is blank, and
assigning a knob to the focused app (M06's `knob.assign_focused_app`)
flashes a confirmation.

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

**Started with calibration, not implementation**, as planned. Per
`specs/reference/xtouch-mini-midi-map.md`'s LED section (now confirmed),
the byte-level addressing (button LEDs: Note On, same note numbers as
input; rings: CC 48–55, `value = mode<<4 | position`) matched the
public documentation, but the *meaning* of the values didn't fully —
see the Status section's note on button velocity. Calibration used a
new `knobd calibrate-leds` subcommand (`daemon/cmd/knobd/calibrate.go`)
rather than a throwaway script: it sends one raw CC/Note message and
exits (LED state latches on the device, so nothing needs to hold a
connection open and watch), which turned out to be a better fit than an
interactive sweep for driving from an agent asking a human "what do you
see" between each value.

`xtouchMiniCodec.EncodeLED` (`daemon/internal/device/led.go`) is
implemented against the confirmed values. `engine` (`led.go`) pushes an
LED update whenever resolved volume/mute state changes, from either
source the Design originally called for:

- **A local action just executed**: `actions.VolumeOptions.OnApplied`
  (an M04 seam, previously unused) now fires from every place
  `daemon/internal/actions/volume.go` commits a new state to the level
  cache, and is wired to `Engine.NotifyLEDDirty` — a coalescing wakeup,
  not a value carried on the channel, since the run loop just re-reads
  whatever's now in the cache.
- **An `audio.Event` arriving via `Subscribe`**: `handleAudioEvent`
  already updated the cache for stream refs; a resync now also
  refreshes any bound sink/source ref (see the Status section's note on
  the gap that closed).

Rather than a `Target -> []Control` reverse index, `engine` recomputes
every LED-bearing control's desired state on each flush by reusing
`buildSnapshot`'s existing `(Control -> Target -> Refs -> cached
volume)` join, then diffs against what it last pushed and writes only
what changed — the diff, not the flush interval, is what keeps the wire
quiet. Rate limiting is a leading-edge-plus-trailing-flush throttle at
30ms (~33Hz): a deliberate change flushes immediately if enough time has
passed since the last flush, otherwise it's folded into one write at
the next interval boundary, so a continuous fader sweep collapses to a
bounded write rate instead of one write per pitch-bend message. See the
Risks section for why this was chosen over a fixed-interval flush.

## Data model changes

None to `model`. `device.LEDMode`/`LEDUpdate` were adjusted to match
confirmed reality as expected: `LEDMode`'s constants now name the four
real display styles (single-dot, pan, fill, spread — only `fill` is
used, for volume's natural "growing bar" reading), and `LEDUpdate
.Position`'s range is corrected from the pre-verification 0–12 guess to
the confirmed 0–11 (11 of the ring's 13 physical segments are
individually addressable).

## Acceptance criteria

- [x] Ring/button LED encoding confirmed against the physical unit and
      written down in `specs/reference/xtouch-mini-midi-map.md`,
      replacing its "not yet verified" section. Done via `knobd
      calibrate-leds`, 2026-09-22.
- [x] Turning a bound encoder updates its own ring in real time to
      reflect the new volume. Verified on the physical unit: turning
      encoder 1 tracked smoothly with no perceptible lag.
- [x] Changing an app's volume from *outside* knobd (e.g. via
      `pavucontrol`) updates the corresponding ring within a short,
      perceptible delay. Verified with `pactl set-sink-volume` against
      a real PipeWire session — the ring updated within about a second.
      (Tested against a sink, not an app stream specifically, since the
      default starter config binds encoder 1 to `default_sink` — the
      underlying path, `handleAudioEvent`'s `ObserveState` feed, is the
      same one a stream-bound encoder uses.)
- [x] Muting a target via a knobd button toggles that button's LED.
      Verified: pressing button 1 lit its LED and actually muted the
      output; the ring kept showing volume throughout, per this
      milestone's mute-display design decision.
- [x] An encoder with no target shows a blank/off ring. Verified:
      encoders/buttons 3–8 (unbound in the default config) were blank
      from the very first startup repaint.
- [x] LED updates don't visibly lag or drop frames during rapid fader
      movement (rate-limiting works). Verified: sweeping the fader
      end to end kept the bound ring's LEDs visibly smooth, no
      stutter.
- [x] Assigning a knob to the focused app (`knob.assign_focused_app`)
      flashes a confirmation — done in M06:
      `engine.Engine.FlashControl` (`daemon/internal/engine/led.go`)
      renders a 400ms full ring fill, wired from
      `actions.AssignOptions.OnAssigned` so it fires only once the new
      binding is durably persisted.

## Verification

Physical-hardware verification was unavoidable for this milestone — LED
state can't be usefully asserted in a unit test — and was done in full
against the physical X-Touch Mini and this machine's real PipeWire
session (not fakes): the calibration sweep; the startup repaint reading
real sink/source volume and mute state; a live knob turn; an external
`pactl` change (standing in for pavucontrol); pressing the mute button
and confirming both the LED and the actual mute; unbound encoders
staying blank; a rapid fader sweep; and unplugging/replugging the
controller to confirm `RepaintLEDs` repaints rather than leaving the
unit dark.

Unit tests cover everything that doesn't need the physical device:
`device`'s `EncodeLED` byte-level encoding (table-driven, every control
kind), and `engine`'s LED state/throttling against `midi.FakePort
.Written()` and the deterministic `testClock` (ring tracks volume, an
unbound ring is blank, a button tracks mute, a rate-limited burst
collapses to a leading-edge write plus one trailing flush, and
`RepaintLEDs` forces a full rewrite).

## Risks & open questions

- ~~The documented encoding might not match this exact unit/firmware
  revision~~ — confirmed by calibration; it *did* differ from the
  public documentation in one place (button LED velocity — see the
  Status section), which is exactly why this was step 1 and not an
  assumption baked into `EncodeLED` directly.
- **Rate-limiting strategy: leading edge + trailing flush**, chosen
  over a fixed-interval flush or a trailing-only debounce. A fixed
  interval adds latency to every single deliberate change, even one
  that isn't part of a burst — a needless wait for a one-off knob turn,
  even at only 30ms. A trailing-only debounce is worse for a continuous
  gesture
  specifically: the ring would stay frozen for the entire duration of
  a fader sweep and only catch up once movement stopped, which reads
  as broken rather than merely throttled. Leading-edge-plus-trailing
  gives immediate feedback for the common case (an isolated turn or
  press) while still bounding the worst case (a continuous sweep) to
  one write per `ledFlushInterval` (30ms, ~33Hz).
