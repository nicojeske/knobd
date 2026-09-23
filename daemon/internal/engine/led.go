package engine

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/device"
	"github.com/njeske/knobd/internal/model"
)

// ledFlushInterval bounds how often Engine pushes LED updates to the
// device: leading edge (see markLEDsDirty) plus trailing flush. MIDI
// runs at 31250 baud, ~347 three-byte messages/sec, and the fader alone
// was observed sending ~400 pitch-bend messages in a single free-play
// session (specs/reference/xtouch-mini-midi-map.md); without a floor, a
// continuous fader sweep would try to write a ring update per message.
// At 30ms (~33Hz) a sweep collapses to one write per interval instead,
// well within what the wire and the eye both consider "keeping up" --
// flush only ever writes controls that actually changed (see
// ledState.flush), so this interval is a worst-case bound, not the
// typical write rate.
const ledFlushInterval = 30 * time.Millisecond

// ledState is Engine's view of what it last pushed to the device, kept
// entirely on the run goroutine like every other piece of Engine's
// mutable state (see the package doc comment).
type ledState struct {
	// lastPushed is compared against the freshly recomputed desired
	// state on every flush; only a control whose LEDUpdate actually
	// changed gets written. This diff, not ledFlushInterval, is what
	// keeps the wire quiet during steady state.
	lastPushed map[model.Control]device.LEDUpdate
	// dirty means desired state may no longer match lastPushed and a
	// flush is owed, once ledFlushInterval allows one.
	dirty bool
	// lastPush is when flush last actually ran (successfully or not),
	// the reference point markLEDsDirty and Run's timer-arming both use.
	lastPush time.Time
	// override is a momentary confirmation flash (see FlashControl):
	// ledDesired renders it in place of a control's normally-computed
	// state until its deadline passes, at which point it reverts on its
	// own -- see Run's ledTimerC arming, which also wakes for this
	// deadline. Zero value (nil updates, zero until) means no active
	// override.
	override ledOverride
}

// ledOverride is a temporary rendering that wins over ledDesired's
// normally-computed state, for FlashControl's confirmation flash. Only
// one is ever active at a time -- a second FlashControl call replaces
// it wholesale, which is fine for what this exists for (a single
// deliberate user gesture at a time).
type ledOverride struct {
	updates map[model.Control]device.LEDUpdate
	until   time.Time
}

func newLEDState() *ledState {
	return &ledState{lastPushed: make(map[model.Control]device.LEDUpdate)}
}

// reset forgets every last-pushed value and marks dirty, forcing the
// next flush to rewrite every LED-bearing control from scratch. Used
// after a config reload (the set of bound controls may have changed)
// and after a reconnect (see Engine.RepaintLEDs) -- the rings and
// buttons don't remember anything across a power cycle.
func (s *ledState) reset() {
	s.lastPushed = make(map[model.Control]device.LEDUpdate)
	s.dirty = true
}

// markLEDsDirty flags LED state as possibly stale and, if the last
// flush was at least ledFlushInterval ago, flushes immediately -- the
// leading edge of the throttle, so a single deliberate knob turn lights
// its ring with no added latency. Otherwise it leaves dirty set for
// Run's own loop (mirroring how gestures.NextDeadline arms the gesture
// timer) to flush once the interval elapses -- the trailing edge, which
// is what collapses a fader burst to one write per interval instead of
// one per pitch-bend message.
func (e *Engine) markLEDsDirty(ctx context.Context, cfg model.Config, bindings *bindingIndex, res *resolver, layer int, leds *ledState) {
	leds.dirty = true
	if e.clk.Now().Sub(leds.lastPush) >= ledFlushInterval {
		e.flushLEDs(ctx, cfg, bindings, res, layer, leds)
	}
	// markLEDsDirty is called at exactly the set of moments api.State can
	// change (audio events, focus changes, config changes, resyncs, and
	// learn arm/disarm/expiry) -- deliberately reused as
	// OnStateChanged's wakeup seam rather than adding a second,
	// parallel set of call sites. See Deps.OnStateChanged's doc comment.
	if e.deps.OnStateChanged != nil {
		e.deps.OnStateChanged()
	}
}

// flushLEDs recomputes desired LED state and writes only the controls
// that changed since the last flush. It runs on the run goroutine
// rather than being handed to the dispatcher: midi.Port.Write is
// documented safe to call concurrently with Read (the dedicated reader
// goroutine already does so), and the dispatcher's single-goroutine
// serialization exists specifically for audio.Backend/Registry.Execute
// calls (see dispatch.go), neither of which this touches.
func (e *Engine) flushLEDs(ctx context.Context, cfg model.Config, bindings *bindingIndex, res *resolver, layer int, leds *ledState) {
	leds.dirty = false
	leds.lastPush = e.clk.Now()

	// Clear an expired override before computing desired state: once
	// its deadline has passed it must never win over the real
	// resolved state again, and clearing until back to its zero value
	// here is also what stops Run's timer-arming from busy-looping on
	// a deadline that's already behind it.
	if !leds.override.until.IsZero() && !e.clk.Now().Before(leds.override.until) {
		leds.override = ledOverride{}
	}

	for c, upd := range e.ledDesired(cfg, bindings, res, layer, leds) {
		if prev, ok := leds.lastPushed[c]; ok && prev == upd {
			continue
		}
		msgs, err := e.deps.Codec.EncodeLED(upd)
		if err != nil {
			if !errors.Is(err, device.ErrNoLED) {
				e.log.Warn("engine: encode LED update failed", "control", c, "err", err)
			}
			continue
		}
		wrote := true
		for _, msg := range msgs {
			if werr := e.deps.Port.Write(ctx, msg); werr != nil {
				// Not fatal -- see midi.Supervisor.Write's doc comment:
				// an LED update is a state push a caller can safely drop
				// and re-send. Leaving c out of lastPushed makes the
				// next flush retry it rather than believing a write
				// that never reached the device.
				e.log.Debug("engine: write LED update failed", "control", c, "err", werr)
				wrote = false
				break
			}
		}
		if wrote {
			leds.lastPushed[c] = upd
		}
	}
}

// ledControls is every physical control that has an LED on this unit --
// deliberately not just the currently-bound ones, so a control that's
// unbound (or was just unbound by a config change) still gets an
// explicit "off" write on the next flush, rather than being left
// showing whatever it happened to power on with.
var ledControls = buildLEDControls()

func buildLEDControls() []model.Control {
	const encoders, buttons, sideButtons = 8, 16, 2
	controls := make([]model.Control, 0, encoders+buttons+sideButtons)
	for i := 1; i <= encoders; i++ {
		controls = append(controls, model.Control{Kind: model.ControlEncoder, Index: i})
	}
	for i := 1; i <= buttons; i++ {
		controls = append(controls, model.Control{Kind: model.ControlButton, Index: i})
	}
	for i := 1; i <= sideButtons; i++ {
		controls = append(controls, model.Control{Kind: model.ControlSideButton, Index: i})
	}
	return controls
}

// ledDesired computes what every LED-bearing control should currently
// show: blank/off by default (ledControls), overridden by whatever the
// active bindings resolve to, finally overridden again by leds.override
// if one is active -- see FlashControl. It reuses buildSnapshot's
// existing (Control -> Target -> Refs -> cached volume) join rather than
// walking bindings a second way.
func (e *Engine) ledDesired(cfg model.Config, bindings *bindingIndex, res *resolver, layer int, leds *ledState) map[model.Control]device.LEDUpdate {
	desired := make(map[model.Control]device.LEDUpdate, len(ledControls))
	for _, c := range ledControls {
		desired[c] = ledOffUpdate(c)
	}

	// LearnUntil is irrelevant here -- ledDesired only reads snap.Controls
	// -- so pass the zero value rather than threading it through.
	snap := e.buildSnapshot(cfg, bindings, res, layer, time.Time{})
	for _, cs := range snap.Controls {
		switch cs.Control.Kind {
		case model.ControlEncoder:
			desired[cs.Control] = ledRingUpdate(cs.Control, cs.Volume)
		case model.ControlButton, model.ControlSideButton:
			// Only a mute-toggle binding drives a button's LED -- any
			// other action type bound to a button (e.g. a future
			// knob.assign_focused_app on a hold gesture) leaves it off.
			if cs.ActionType == model.ActionVolumeMuteToggle {
				desired[cs.Control] = ledButtonUpdate(cs.Control, cs.Volume)
			}
		}
	}

	if !leds.override.until.IsZero() && e.clk.Now().Before(leds.override.until) {
		for c, upd := range leds.override.updates {
			desired[c] = upd
		}
	}
	return desired
}

// ledOffUpdate is the blank/off rendering for c: no LED lit, for an
// encoder with no target or an unresolved target, or a button not bound
// to volume.mute_toggle.
func ledOffUpdate(c model.Control) device.LEDUpdate {
	if c.Kind == model.ControlEncoder {
		// LEDModeFill at Position 0 renders identically (confirmed
		// live), but LEDModeSingleDot's zero value is the exact byte
		// (0x00) confirmed as this unit's "off" -- pick it explicitly
		// rather than relying on LEDMode's zero value to keep meaning
		// what it means today if the mode constants are ever reordered.
		return device.LEDUpdate{Control: c, Mode: device.LEDModeSingleDot, Position: 0}
	}
	return device.LEDUpdate{Control: c, On: false}
}

// ledRingUpdate maps a resolved volume percentage onto the ring's fill
// mode (the "growing bar" display -- see
// specs/reference/xtouch-mini-midi-map.md's LED section). A muted
// target still shows its volume level: mute is shown only on the
// button LED (a deliberate M05 design decision -- blanking the ring on
// mute would be indistinguishable from an unbound encoder, which is
// also blank).
func ledRingUpdate(c model.Control, vol *audio.VolumeState) device.LEDUpdate {
	if vol == nil {
		return ledOffUpdate(c)
	}
	pos := int(math.Round(vol.Percent / 100 * float64(device.MaxRingPosition)))
	if pos < 0 {
		pos = 0
	}
	if pos > device.MaxRingPosition {
		pos = device.MaxRingPosition
	}
	if pos == 0 {
		return ledOffUpdate(c)
	}
	return device.LEDUpdate{Control: c, Mode: device.LEDModeFill, Position: pos}
}

// ledButtonUpdate maps a resolved mute state onto a button LED: on
// means muted, per the M05 design decision recorded on ledRingUpdate.
func ledButtonUpdate(c model.Control, vol *audio.VolumeState) device.LEDUpdate {
	if vol == nil {
		return ledOffUpdate(c)
	}
	return device.LEDUpdate{Control: c, On: vol.Muted}
}

// deviceRefsBoundToTargets returns every sink/source Ref that a
// currently-active binding's Target resolves to -- the set
// handleAudioEvent's stream-only ObserveState feed leaves stale after
// an external change, since an EventDeviceChanged carries no Ref (see
// that method's comment). Called from engine.go's streamsCh arm, once
// sinks/sources are fresh, to enqueue a work.refreshRefs follow-up.
func deviceRefsBoundToTargets(bindings *bindingIndex, res *resolver, layer int) []audio.Ref {
	seen := make(map[audio.Ref]bool)
	var refs []audio.Ref
	for _, ab := range bindings.activeBindings(layer) {
		target, ok := model.TargetOf(ab.Action)
		if !ok {
			continue
		}
		switch target.Kind {
		case model.TargetSink, model.TargetDefaultSink, model.TargetSource, model.TargetDefaultSource:
		default:
			continue
		}
		resolved, err := res.resolve(target)
		if err != nil {
			continue
		}
		for _, ref := range resolved {
			if !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
	}
	return refs
}

// DefaultFlashDuration is how long FlashControl's confirmation flash
// lasts before c's ring reverts to reflecting its actually-resolved
// state.
const DefaultFlashDuration = 400 * time.Millisecond

// flashRequest is FlashControl's payload to Run's flashCh.
type flashRequest struct {
	control  model.Control
	duration time.Duration
}

// FlashControl asks the run loop to render c as a momentary full ring
// fill for d (see flashUpdate), overriding whatever its bound target
// would otherwise show, until d elapses. This is
// knob.assign_focused_app's confirmation that a rebind actually took --
// see actions.AssignOptions.OnAssigned, wired from cmd/knobd/main.go to
// fire only after the new binding is durably persisted, so the flash
// means "saved", not "attempted" (closes the item M05 deferred to this
// milestone, specs/milestones/M05-led-feedback.md).
//
// Safe to call concurrently with Run, including from a different
// goroutine than Run's own, mirroring NotifyLEDDirty. A no-op if Run
// isn't consuming (not started yet, or already returned); a flash
// request while one is already pending is dropped rather than queued --
// two confirmation flashes this close together aren't worth a second
// wire write over each other, and the newer request would have nothing
// distinguishable to add.
func (e *Engine) FlashControl(c model.Control, d time.Duration) {
	select {
	case e.flashCh <- flashRequest{control: c, duration: d}:
	default:
	}
}

// flashUpdate is the rendering FlashControl asks for: a full ring fill
// for an encoder (the only control kind knob.assign_focused_app ever
// flashes -- its assign gesture is a hold on the encoder_push, but an
// encoder_push has no LED of its own, so the confirmation renders on
// its paired encoder's ring instead), or simply on for anything else.
func flashUpdate(c model.Control) device.LEDUpdate {
	if c.Kind == model.ControlEncoder {
		return device.LEDUpdate{Control: c, Mode: device.LEDModeFill, Position: device.MaxRingPosition}
	}
	return device.LEDUpdate{Control: c, On: true}
}
