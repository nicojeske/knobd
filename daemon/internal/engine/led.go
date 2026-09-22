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

	for c, upd := range e.ledDesired(cfg, bindings, res, layer) {
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
// active bindings resolve to. It reuses buildSnapshot's existing
// (Control -> Target -> Refs -> cached volume) join rather than walking
// bindings a second way.
func (e *Engine) ledDesired(cfg model.Config, bindings *bindingIndex, res *resolver, layer int) map[model.Control]device.LEDUpdate {
	desired := make(map[model.Control]device.LEDUpdate, len(ledControls))
	for _, c := range ledControls {
		desired[c] = ledOffUpdate(c)
	}

	snap := e.buildSnapshot(cfg, bindings, res, layer)
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
		resolved, err := res.resolve(context.Background(), target)
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
