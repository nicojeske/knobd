// Package engine is knobd's core event loop: it turns decoded
// device.Event values into (Control, Gesture) lookups against the
// active model.Profile's bindings, resolves each binding's model.Target
// against the audio/focus backends, and dispatches the resulting
// model.Action to daemon/internal/actions.
//
// Every piece of mutable engine state (the gesture machine, the binding
// index, the target resolver's stream/device cache) lives on the
// goroutine Run runs on. There are no mutexes in this package: SetConfig
// and Snapshot are channel round trips served by that same goroutine,
// and every blocking audio.Backend call is made from a second,
// dedicated dispatcher goroutine (see dispatch.go) so a stuck PipeWire
// connection can never stall gesture timing.
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/njeske/knobd/internal/actions"
	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/device"
	"github.com/njeske/knobd/internal/focus"
	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
	"github.com/njeske/knobd/internal/proctree"
)

// HoldThreshold is the minimum duration a button/encoder-push must be
// held before it produces a GestureHold instead of a GesturePress.
// TODO(M07): consider exposing this as a per-user preference if it turns
// out to need tuning.
const HoldThreshold = 600 * time.Millisecond

// DoublePressWindow is how long after a press's release a second press
// still counts as a GestureDoublePress rather than a new GesturePress.
// 350ms sits just under KDE's own ~400ms double-click interval; it is
// also the latency added to a plain press on any control that actually
// has a double_press binding (see gestureMachine's doc comment), so it
// is deliberately at the short end of the usual 300-500ms range.
const DoublePressWindow = 350 * time.Millisecond

// StateObserver receives volume/mute readings the engine observes from
// audio.Backend.Subscribe (so a level cache stays fresh with changes
// made outside knobd too, e.g. in pavucontrol) and answers cheap,
// non-blocking reads of that cache for Snapshot. *actions.VolumeHandlers
// implements this; it is a separate interface here, defined at the
// point of use, so engine never needs to import a concrete handler type.
type StateObserver interface {
	ObserveState(ref audio.Ref, st audio.VolumeState)
	CachedLevel(ref audio.Ref) (audio.VolumeState, bool)
}

// Deps bundles the backends Engine needs. Port, Codec, Audio, and Config
// are required; Focus/Registry/Logger/Clock/Observer fall back to a
// working default (focus.Unavailable(), an empty actions.Registry,
// slog.Default(), the real clock, and no observer) so tests only need to
// set what they're exercising.
type Deps struct {
	Port   midi.Port
	Codec  device.Codec
	Audio  audio.Backend
	Focus  focus.Provider
	Config model.Config

	// ProcRoot overrides where the focus resolver's process-tree
	// fallback rung reads pid ancestry from (see
	// daemon/internal/proctree); "" means the real /proc. Tests set a
	// t.TempDir() fabricated tree.
	ProcRoot string

	Registry *actions.Registry
	Logger   *slog.Logger
	Clock    Clock
	Observer StateObserver

	// OnInput, if non-nil, receives every decoded device.Event while
	// MIDI learn is armed (see SetLearnUntil) -- and only then; it is
	// never called while learn is disarmed. Called on the run goroutine
	// and must never block: daemon/internal/api's hub implementation is
	// a non-blocking send into a single-slot, latest-wins buffer.
	OnInput func(ev device.Event)
	// OnStateChanged, if non-nil, is called (on the run goroutine, and
	// must therefore never block) whenever engine state that Snapshot
	// reflects may have changed. It is markLEDsDirty's wakeup seam,
	// deliberately reusing that function's trigger set (every audio
	// event, focus change, config change, resync, and learn arm/disarm/
	// expiry already call it) -- see led.go. daemon/internal/api's hub
	// implementation is a non-blocking send into a coalescing channel,
	// exactly like NotifyLEDDirty's own caller.
	OnStateChanged func()
}

func (d *Deps) setDefaults() {
	if d.Focus == nil {
		d.Focus = focus.Unavailable()
	}
	if d.Registry == nil {
		d.Registry = actions.NewRegistry()
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Clock == nil {
		d.Clock = realClock{}
	}
}

// Engine runs the main event loop described in the package doc comment.
type Engine struct {
	deps Deps
	log  *slog.Logger
	clk  Clock

	configCh   chan configRequest
	snapshotCh chan chan Snapshot
	// ledCh is woken (coalescing, non-blocking) whenever LED state might
	// have changed outside a MIDI/audio event Run already watches --
	// currently, only actions.VolumeOptions.OnApplied's local echo of a
	// write knobd just made itself (see NotifyLEDDirty and
	// cmd/knobd/main.go's wiring).
	ledCh chan struct{}
	// repaintCh serves RepaintLEDs: a request to forget every
	// last-pushed LED value and rewrite everything from scratch, for
	// cmd/knobd/state.go's watchConnections to call after a device
	// reconnect (see that file's doc comment for why it's the sole
	// consumer of midi.Supervisor.Connected).
	repaintCh chan chan struct{}
	// flashCh serves FlashControl -- see its doc comment in led.go.
	flashCh chan flashRequest
	// learnCh serves SetLearnUntil -- see learn.go.
	learnCh chan learnRequest
}

type configRequest struct {
	cfg   model.Config
	reply chan error
}

// New constructs an Engine. It does not start it — call Run for that.
func New(deps Deps) *Engine {
	deps.setDefaults()
	return &Engine{
		deps:       deps,
		log:        deps.Logger,
		clk:        deps.Clock,
		configCh:   make(chan configRequest),
		snapshotCh: make(chan chan Snapshot),
		ledCh:      make(chan struct{}, 1),
		repaintCh:  make(chan chan struct{}),
		flashCh:    make(chan flashRequest, 1),
		learnCh:    make(chan learnRequest),
	}
}

// SetConfig replaces the configuration Run resolves bindings against.
// Safe to call concurrently with Run (this is PUT /config's path into
// the running engine); the new config takes effect for the next event,
// never mid-dispatch. Returns ctx.Err() if Run isn't consuming (not
// started yet, or already returned) before ctx is done.
func (e *Engine) SetConfig(ctx context.Context, cfg model.Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("engine: SetConfig: %w", err)
	}
	reply := make(chan error, 1)
	select {
	case e.configCh <- configRequest{cfg: cfg, reply: reply}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Snapshot returns engine's current state (GET /state's payload). Safe
// to call concurrently with Run; never blocks on audio.Backend.
func (e *Engine) Snapshot(ctx context.Context) (Snapshot, error) {
	reply := make(chan Snapshot, 1)
	select {
	case e.snapshotCh <- reply:
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
	select {
	case snap := <-reply:
		return snap, nil
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}

// NotifyLEDDirty wakes the run loop to recompute and (throttled) flush
// LED state at the next opportunity. Safe to call concurrently with
// Run, including from a different goroutine than Run's own -- it's the
// seam actions.VolumeOptions.OnApplied uses (see cmd/knobd/main.go) so
// a ring/button LED updates the instant knobd's own write to PipeWire
// succeeds, rather than waiting for audio.Backend.Subscribe's echo of
// it. Multiple calls before Run next looks coalesce into one wakeup;
// a no-op if Run isn't consuming (not started yet, or already returned).
func (e *Engine) NotifyLEDDirty() {
	select {
	case e.ledCh <- struct{}{}:
	default:
	}
}

// RepaintLEDs forces every LED-bearing control to be rewritten at the
// next flush, regardless of what Engine believes it last pushed. Call
// after the MIDI device reconnects -- the X-Touch Mini's rings and
// buttons don't remember anything across a power cycle. Returns
// ctx.Err() if Run isn't consuming before ctx is done.
func (e *Engine) RepaintLEDs(ctx context.Context) error {
	reply := make(chan struct{}, 1)
	select {
	case e.repaintCh <- reply:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-reply:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Run drives the event loop until ctx is canceled or an unrecoverable
// error occurs (see the fatal/non-fatal table in
// specs/milestones/M04-mapping-engine-daemon.md's Architecture section).
func (e *Engine) Run(ctx context.Context) error {
	cfg := e.deps.Config

	bindings := newBindingIndex(cfg, e.log)
	// activeProfileID tracks which profile layerState belongs to: a
	// layer switch is a live-session concern, not config, so an
	// unrelated config edit (SetConfig with the same active profile)
	// must never snap the user back to layer 0 -- only actually
	// switching profiles does (see the configCh case below).
	activeProfileID := cfg.ActiveProfileID
	gestures := newGestureMachine(HoldThreshold, DoublePressWindow, bindings.deferPress)
	res := newResolver(proctree.Walker{Root: e.deps.ProcRoot}, e.log)
	res.setConfig(cfg)
	layers := &layerState{}
	// downLayer pins the layer a control's press/hold/release/
	// double_press gesture resolves against to whatever was active when
	// the control physically went down (see the EventButtonDown case
	// below and dispatchGesture), independent of any layer switch that
	// happens while it's still held. Turn/move gestures have no "down"
	// of their own and always resolve against whatever's active right
	// now instead.
	downLayer := make(map[model.Control]int)
	// learnUntil is the zero time.Time when learn mode is disarmed, or
	// the deadline it's armed until -- see learn.go's SetLearnUntil and
	// the msgs/timerC cases below for how it's read and cleared.
	var learnUntil time.Time

	// Watch is started once, here, and never retried: per
	// focus.Provider.Watch's doc comment, an implementation is
	// responsible for its own reconnects (kwinProvider reinstalls its
	// KWin script after a compositor restart internally), so the only
	// thing that should ever close this channel is ctx being canceled.
	// A Watch failure here (as opposed to the channel later closing) is
	// non-fatal -- unlike the audio subscription below, focus is a
	// best-effort hint the engine can run entirely without (both
	// focus.Unavailable() and a not-yet-verified real Provider are
	// expected states, not engine bugs).
	focusEvents, ferr := e.deps.Focus.Watch(ctx)
	if ferr != nil {
		e.log.Warn("engine: focus watch unavailable; 'focused' targets will not resolve", "err", ferr)
		focusEvents = nil
	}
	// Seed the cache from Current so a target resolves against the
	// already-focused window rather than only the next change --
	// Current is documented to answer from cache with no I/O, so this
	// is as cheap as Watch's first delivery would have been anyway.
	if info, cerr := e.deps.Focus.Current(ctx); cerr == nil {
		res.setFocused(info)
	}

	dispatchCh := make(chan work, dispatchQueueDepth)
	streamsCh := make(chan streamsResult, 1)
	dispatchDone := make(chan struct{})
	go func() {
		defer close(dispatchDone)
		e.runDispatcher(ctx, dispatchCh, streamsCh)
	}()
	defer func() {
		close(dispatchCh)
		<-dispatchDone
	}()

	type readResult struct {
		msg midi.Message
		err error
	}
	msgs := make(chan readResult)
	go func() {
		for {
			msg, err := e.deps.Port.Read(ctx)
			select {
			case msgs <- readResult{msg, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	// Subscribe before any enumeration: a stream created between the two
	// calls would otherwise never be seen. The corresponding first
	// enumeration is requested as an ordinary resync job below, not run
	// inline, so knobd processes MIDI even while PipeWire is still
	// coming up.
	events, err := e.deps.Audio.Subscribe(ctx)
	if err != nil {
		return fmt.Errorf("engine: subscribe to audio events: %w", err)
	}
	e.enqueueDispatch(dispatchCh, work{resync: true}, "startup resync")

	timer := e.clk.NewTimer(time.Hour)
	timer.Stop()

	// leds paints once the config/bindings above are in place, even
	// before the startup resync completes -- everything currently
	// unresolved (no streams enumerated yet) renders as blank, which is
	// what an encoder with an as-yet-unresolved target should show
	// anyway; the resync's own streamsCh arm below repaints once real
	// state arrives.
	leds := newLEDState()
	ledTimer := e.clk.NewTimer(time.Hour)
	ledTimer.Stop()
	e.markLEDsDirty(ctx, cfg, bindings, res, layers.active(), leds)

	var lastDecodeLog time.Time
	var decodeLogSuppressed int

	for {
		var timerC <-chan time.Time
		// Wake at the earlier of the gesture machine's own next deadline
		// and learn mode's expiry, so learn disarms itself (and pushes
		// that state change) even if no further input ever arrives after
		// it's armed -- see the case below for both checks it performs
		// once woken.
		deadline, haveDeadline := gestures.NextDeadline()
		if !learnUntil.IsZero() && (!haveDeadline || learnUntil.Before(deadline)) {
			deadline, haveDeadline = learnUntil, true
		}
		if haveDeadline {
			d := deadline.Sub(e.clk.Now())
			if d < 0 {
				d = 0
			}
			timer.Reset(d)
			timerC = timer.C()
		}

		var ledTimerC <-chan time.Time
		now := e.clk.Now()
		var ledWake time.Duration
		haveLEDWake := false
		if leds.dirty {
			d := ledFlushInterval - now.Sub(leds.lastPush)
			if d < 0 {
				d = 0
			}
			ledWake, haveLEDWake = d, true
		}
		// An active override also needs its own wake, independent of
		// leds.dirty: nothing else would otherwise notice that its
		// deadline passed and flush the reverted state -- see
		// FlashControl and ledDesired's override pass.
		if !leds.override.until.IsZero() {
			d := leds.override.until.Sub(now)
			if d < 0 {
				d = 0
			}
			if !haveLEDWake || d < ledWake {
				ledWake, haveLEDWake = d, true
			}
		}
		if haveLEDWake {
			ledTimer.Reset(ledWake)
			ledTimerC = ledTimer.C()
		}

		select {
		case <-ctx.Done():
			return nil

		case rr := <-msgs:
			if rr.err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("engine: read from device: %w", rr.err)
			}
			ev, ok, decErr := e.deps.Codec.Decode(rr.msg)
			if decErr != nil {
				e.logDecodeError(decErr, &lastDecodeLog, &decodeLogSuppressed)
				continue
			}
			if !ok {
				continue
			}

			if !learnUntil.IsZero() {
				if ev.Time.After(learnUntil) {
					// Expired since this event's own timestamp but before
					// the timerC arm below noticed -- disarm now and let
					// this event dispatch normally, below.
					learnUntil = time.Time{}
					gestures.Reset()
					e.markLEDsDirty(ctx, cfg, bindings, res, layers.active(), leds)
				} else {
					// Learn is armed: every event is suppressed from
					// dispatch unconditionally. Only a turn, a fader move,
					// or a button going down counts as a capture -- never
					// EventButtonUp, so one physical press is exactly one
					// learn_input, not two (see specs/milestones/M07-config-ui.md).
					// Anything else here (e.g. a button-up with no
					// matching captured down, because Reset just cleared
					// it below on a capture, or because it was already
					// down when learn armed) is silently swallowed.
					switch ev.Kind {
					case device.EventTurn, device.EventFaderMove, device.EventButtonDown:
						if e.deps.OnInput != nil {
							e.deps.OnInput(ev)
						}
						// One-shot: the first captured input disarms
						// learn immediately rather than waiting for its
						// own deadline (the timerC arm above).
						learnUntil = time.Time{}
						gestures.Reset()
						e.markLEDsDirty(ctx, cfg, bindings, res, layers.active(), leds)
					}
					continue
				}
			}

			// Pin the layer this control's press/hold/release/
			// double_press gesture(s) will resolve against to whatever's
			// active right now, before a momentary switch below (if any)
			// changes it -- see downLayer's doc comment above and
			// dispatchGesture. Set unconditionally on every down, even
			// for a control with no such binding, since there's no
			// cheaper way to know in advance which controls will need it,
			// and an unused entry is just as harmless as one gestures.Handle
			// never turns into a dispatched gesture at all.
			if ev.Kind == device.EventButtonDown {
				downLayer[ev.Control] = layers.active()
				// A momentary layer switch takes effect on the raw
				// button-down, not the eventual GestureHold 600ms later
				// (see HoldThreshold) -- "hold the side button, turn a
				// knob on the new layer" must work immediately.
				if a, ok := bindings.lookup(layers.active(), ev.Control, model.GestureHold); ok {
					if lm, ok := a.(model.LayerMomentaryAction); ok {
						layers.pushMomentary(ev.Control, lm.Layer)
						e.markLEDsDirty(ctx, cfg, bindings, res, layers.active(), leds)
					}
				}
			} else if ev.Kind == device.EventButtonUp {
				// Unconditional and harmless if ev.Control was never
				// pushed: popMomentary is a no-op search-and-remove.
				before := layers.active()
				layers.popMomentary(ev.Control)
				if layers.active() != before {
					e.markLEDsDirty(ctx, cfg, bindings, res, layers.active(), leds)
				}
			}

			for _, g := range gestures.Handle(ev) {
				e.dispatchGesture(ctx, cfg, bindings, res, layers, downLayer, g, dispatchCh, leds)
			}

		case now := <-timerC:
			for _, g := range gestures.Tick(now) {
				e.dispatchGesture(ctx, cfg, bindings, res, layers, downLayer, g, dispatchCh, leds)
			}
			if !learnUntil.IsZero() && !now.Before(learnUntil) {
				learnUntil = time.Time{}
				gestures.Reset()
				e.markLEDsDirty(ctx, cfg, bindings, res, layers.active(), leds)
			}

		case <-ledTimerC:
			e.flushLEDs(ctx, cfg, bindings, res, layers.active(), leds)

		case ev, open := <-events:
			if !open {
				if ctx.Err() != nil {
					return nil
				}
				return errAudioSubscriptionClosed
			}
			e.handleAudioEvent(ev, res, dispatchCh)
			e.markLEDsDirty(ctx, cfg, bindings, res, layers.active(), leds)

		case info, open := <-focusEvents:
			if !open {
				if ctx.Err() != nil {
					return nil
				}
				// Deliberately non-fatal, asymmetric with
				// errAudioSubscriptionClosed above: focus is a
				// best-effort hint, not something the engine depends on
				// to keep running. The last known app stays cached and
				// TargetFocused keeps resolving against it; a control
				// bound to it just stops following further focus
				// changes until knobd is restarted.
				e.log.Warn("engine: focus event stream closed; 'focused' targets will use the last known app")
				focusEvents = nil // a nil channel blocks forever: this arm is now permanently disabled
				continue
			}
			res.setFocused(info)
			// An encoder bound to TargetFocused must have its ring
			// follow the newly-focused app; nothing else in this loop
			// would repaint it on a focus change alone.
			e.markLEDsDirty(ctx, cfg, bindings, res, layers.active(), leds)

		case sr := <-streamsCh:
			res.setSinks(sr.sinks)
			res.setSources(sr.sources)
			res.setStreams(sr.streams)
			if refs := deviceRefsBoundToTargets(bindings, res, layers.active()); len(refs) > 0 {
				e.enqueueDispatch(dispatchCh, work{refreshRefs: refs}, "device level refresh")
			}
			e.markLEDsDirty(ctx, cfg, bindings, res, layers.active(), leds)

		case req := <-e.configCh:
			cfg = req.cfg
			bindings = newBindingIndex(cfg, e.log)
			gestures.deferPress = bindings.deferPress
			res.setConfig(cfg)
			// A layer switch is a live-session concern, not config (see
			// layerState's doc comment) -- an unrelated edit to the same
			// active profile must not snap the user back to layer 0. Only
			// actually switching the active profile resets it, since the
			// new profile's layers mean something different. Any
			// in-progress momentary hold is dropped either way: the
			// bindings it referenced may no longer exist.
			if cfg.ActiveProfileID != activeProfileID {
				layers.latched = 0
				activeProfileID = cfg.ActiveProfileID
			}
			layers.held = nil
			// The set of bound controls may have changed; forget every
			// last-pushed LED value rather than diffing against a
			// binding index that no longer applies.
			leds.reset()
			e.markLEDsDirty(ctx, cfg, bindings, res, layers.active(), leds)
			select {
			case req.reply <- nil:
			default:
			}

		case reply := <-e.snapshotCh:
			reply <- e.buildSnapshot(cfg, bindings, res, layers.active(), learnUntil)

		case <-e.ledCh:
			e.markLEDsDirty(ctx, cfg, bindings, res, layers.active(), leds)

		case reply := <-e.repaintCh:
			leds.reset()
			e.flushLEDs(ctx, cfg, bindings, res, layers.active(), leds)
			select {
			case reply <- struct{}{}:
			default:
			}

		case req := <-e.flashCh:
			leds.override = ledOverride{
				updates: map[model.Control]device.LEDUpdate{req.control: flashUpdate(req.control)},
				until:   e.clk.Now().Add(req.duration),
			}
			e.markLEDsDirty(ctx, cfg, bindings, res, layers.active(), leds)

		case req := <-e.learnCh:
			// Reset on every transition, arming or disarming: a control
			// physically down when learn arms would otherwise have its
			// matching up swallowed by the suppression above (since it's
			// not one of the three capture kinds), leaving m.down
			// populated forever and eventually producing a spurious
			// GestureHold/GestureRelease pair once learn disarms. The
			// same reasoning applies in reverse when disarming.
			learnUntil = req.deadline
			gestures.Reset()
			e.markLEDsDirty(ctx, cfg, bindings, res, layers.active(), leds)
			select {
			case req.reply <- nil:
			default:
			}
		}
	}
}

// logDecodeError logs a Codec.Decode error, rate-limited to once per 5s:
// a controller left in Standard mode (see device.ErrStandardMode) emits
// one such error per detent, which would otherwise flood the log.
func (e *Engine) logDecodeError(err error, last *time.Time, suppressed *int) {
	now := e.clk.Now()
	if now.Sub(*last) < 5*time.Second {
		*suppressed++
		return
	}
	if *suppressed > 0 {
		e.log.Warn("engine: decode error (repeated)", "err", err, "suppressedSince", *last, "suppressedCount", *suppressed)
	} else {
		e.log.Warn("engine: decode error", "err", err)
	}
	*last = now
	*suppressed = 0
}

// holdPaired reports whether action is one whose GestureHold binding
// must also receive the control's eventual GestureRelease, even when
// nothing is bound to Release directly -- audio.duck_hold's whole
// point is "restore on release" (see its doc comment), and a user
// binding only its Hold gesture (the natural, and only sensible, way
// to configure it) must not need a second, redundant Release binding
// to make that work.
func holdPaired(action model.Action) bool {
	switch action.(type) {
	case model.AudioDuckHoldAction:
		return true
	default:
		return false
	}
}

// dispatchGesture looks up g's binding -- against the layer pinned at
// the control's button-down for a press/hold/release/double_press
// gesture (see downLayer's doc comment in Run), or whatever's active
// right now for a turn/move, which has no "down" of its own -- and
// either executes it inline (the three layer.* actions, which mutate
// layers, state Run's goroutine alone owns) or resolves its target and
// enqueues the resulting Invocation to the dispatcher. Resolution
// happens here, inline on the run goroutine, rather than in the
// dispatcher: every target.Kind resolver.resolve handles is a pure,
// non-blocking cache read (see resolveFocused's doc comment for what
// changed in M06 to make that true of TargetFocused too — it no longer
// takes a context.Context at all, for the same reason).
func (e *Engine) dispatchGesture(ctx context.Context, cfg model.Config, bindings *bindingIndex, res *resolver, layers *layerState, downLayer map[model.Control]int, g gesture, dispatchCh chan<- work, leds *ledState) {
	layer := layers.active()
	switch g.Gesture {
	case model.GesturePress, model.GestureHold, model.GestureRelease, model.GestureDoublePress:
		if l, ok := downLayer[g.Control]; ok {
			layer = l
		}
	}
	// A press/hold/release/double_press gesture is done needing its
	// pinned layer once it produces its terminal outcome -- everything
	// except GestureHold, which a GestureRelease may still follow.
	defer func() {
		switch g.Gesture {
		case model.GesturePress, model.GestureDoublePress, model.GestureRelease:
			delete(downLayer, g.Control)
		}
	}()

	action, ok := bindings.lookup(layer, g.Control, g.Gesture)
	if !ok && g.Gesture == model.GestureRelease {
		// See holdPaired: a Release with no binding of its own still
		// fires if the same control's Hold binding wants one.
		if holdAction, holdOK := bindings.lookup(layer, g.Control, model.GestureHold); holdOK && holdPaired(holdAction) {
			action, ok = holdAction, true
		}
	}
	if !ok {
		return
	}

	switch a := action.(type) {
	case model.LayerMomentaryAction:
		// Momentary switching happens at the raw-event level (see Run's
		// EventButtonDown/Up handling) so it takes effect instantly
		// rather than waiting for HoldThreshold. The Hold/Release
		// gestures dispatchGesture sees for this binding are just the
		// side button behaving like any other button as far as the
		// gesture machine is concerned -- nothing left to do here.
		return
	case model.LayerLatchAction:
		layers.latch(a.Layer)
	case model.LayerCycleAction:
		layers.cycle(a.LayerOrder, bindings.maxLayer)
	default:
		e.dispatchResolvedAction(cfg, action, g, layer, res, dispatchCh)
		return
	}
	// Only the two layer-mutating cases above fall through to here.
	e.markLEDsDirty(ctx, cfg, bindings, res, layers.active(), leds)
}

// dispatchResolvedAction enqueues the resulting Invocation for every
// action type that isn't handled inline by dispatchGesture. Most
// actions resolve a single Target the ordinary way; scene.apply/
// scene.save and audio.solo_toggle/audio.duck_hold need extra
// dispatch-time resolution engine alone can do (a scene's several
// entry targets; solo/duck's "everything else"), so those are broken
// out into their own helpers below.
func (e *Engine) dispatchResolvedAction(cfg model.Config, action model.Action, g gesture, layer int, res *resolver, dispatchCh chan<- work) {
	switch action.(type) {
	case model.SceneApplyAction, model.SceneSaveAction:
		e.dispatchScene(cfg, action, g, layer, res, dispatchCh)
		return
	case model.AudioSoloToggleAction, model.AudioDuckHoldAction:
		e.dispatchWithOthers(action, g, layer, res, dispatchCh)
		return
	}

	var refs []audio.Ref
	target, hasTarget := model.TargetOf(action)
	if hasTarget {
		resolved, err := res.resolve(target)
		if err != nil {
			e.log.Warn("engine: resolve target failed", "control", g.Control, "gesture", g.Gesture, "target", target, "err", err)
			return
		}
		if len(resolved) == 0 {
			e.log.Debug("engine: target resolved to nothing", "control", g.Control, "gesture", g.Gesture, "target", target)
			return
		}
		refs = resolved
	}

	inv := &actions.Invocation{
		Action:  action,
		Control: g.Control,
		Gesture: g.Gesture,
		Layer:   layer,
		Delta:   g.Delta,
		Value:   g.Value,
		At:      g.At,
		Refs:    refs,
		Target:  target,
	}
	e.enqueueDispatch(dispatchCh, work{inv: inv}, "invocation")
}

// sceneByID finds id in cfg.Scenes, returning a copy (nil if not
// found) so the caller can safely hold a pointer to it independent of
// cfg's own backing array.
func sceneByID(cfg model.Config, id string) *model.Scene {
	for i := range cfg.Scenes {
		if cfg.Scenes[i].ID == id {
			s := cfg.Scenes[i]
			return &s
		}
	}
	return nil
}

// dispatchScene resolves every entry in the scene action names against
// the live audio graph and enqueues an Invocation carrying both the
// scene and that per-entry resolution (actions.SceneHandlers must use
// Invocation.SceneRefs, never re-resolve -- see its doc comment). A
// scene that no longer exists (e.g. deleted from config after the
// binding was made) is logged and skipped; an individual entry whose
// target doesn't currently resolve to anything gets a nil refs slice
// at its index rather than failing the whole scene -- apply/save must
// each decide what "not currently resolvable" means for their entry.
func (e *Engine) dispatchScene(cfg model.Config, action model.Action, g gesture, layer int, res *resolver, dispatchCh chan<- work) {
	sceneID, _ := model.SceneRefOf(action) // both callers (see the switch above) always carry one
	scene := sceneByID(cfg, sceneID)
	if scene == nil {
		e.log.Warn("engine: scene not found", "control", g.Control, "gesture", g.Gesture, "sceneId", sceneID)
		return
	}

	sceneRefs := make([][]audio.Ref, len(scene.Entries))
	for i, entry := range scene.Entries {
		refs, err := res.resolve(entry.Target)
		if err != nil {
			e.log.Warn("engine: resolve scene entry target failed", "sceneId", sceneID, "target", entry.Target, "err", err)
			continue
		}
		sceneRefs[i] = refs
	}

	inv := &actions.Invocation{
		Action:    action,
		Control:   g.Control,
		Gesture:   g.Gesture,
		Layer:     layer,
		Delta:     g.Delta,
		Value:     g.Value,
		At:        g.At,
		Scene:     scene,
		SceneRefs: sceneRefs,
	}
	e.enqueueDispatch(dispatchCh, work{inv: inv}, "scene invocation")
}

// dispatchWithOthers resolves action's Target the ordinary way, plus
// Others (every currently-known playback stream not in that
// resolution) for audio.solo_toggle/audio.duck_hold. Unlike
// dispatchResolvedAction's default path, an empty (or failed) Target
// resolution here does not skip the dispatch: toggling solo off, or
// releasing a duck, must still restore prior state even if the app it
// targeted has since exited -- MixHandlers (M08) decides what "no
// target refs" means for the gesture it's handling.
func (e *Engine) dispatchWithOthers(action model.Action, g gesture, layer int, res *resolver, dispatchCh chan<- work) {
	target, _ := model.TargetOf(action) // both types always carry one
	refs, err := res.resolve(target)
	if err != nil {
		e.log.Warn("engine: resolve target failed", "control", g.Control, "gesture", g.Gesture, "target", target, "err", err)
		refs = nil
	}

	inv := &actions.Invocation{
		Action:  action,
		Control: g.Control,
		Gesture: g.Gesture,
		Layer:   layer,
		Delta:   g.Delta,
		Value:   g.Value,
		At:      g.At,
		Refs:    refs,
		Target:  target,
		Others:  othersExcluding(res.allPlaybackStreams(), refs),
	}
	e.enqueueDispatch(dispatchCh, work{inv: inv}, "solo/duck invocation")
}

// othersExcluding returns every ref in all that isn't in exclude.
func othersExcluding(all, exclude []audio.Ref) []audio.Ref {
	excl := make(map[audio.Ref]bool, len(exclude))
	for _, r := range exclude {
		excl[r] = true
	}
	var out []audio.Ref
	for _, r := range all {
		if !excl[r] {
			out = append(out, r)
		}
	}
	return out
}

// handleAudioEvent updates the resolver's cache from one audio.Event and
// feeds StateObserver, all inline on the run goroutine -- audio.Event
// carries no blocking work, only data.
func (e *Engine) handleAudioEvent(ev audio.Event, res *resolver, dispatchCh chan<- work) {
	switch ev.Kind {
	case audio.EventStreamChanged:
		if ev.Stream == nil {
			return
		}
		res.upsertStream(*ev.Stream)
		if ev.State != nil && e.deps.Observer != nil {
			e.deps.Observer.ObserveState(ev.Stream.Ref(), *ev.State)
		}
	case audio.EventStreamRemoved:
		if ev.Stream != nil {
			res.removeStream(ev.Stream.ID)
		}
	case audio.EventDeviceChanged, audio.EventDeviceRemoved, audio.EventDefaultChanged, audio.EventResync:
		// Device volume echoes are not fed to Observer here: an
		// EventDeviceChanged doesn't say whether the changed Device is a
		// sink or a source, so there's no way to build the audio.Ref
		// ObserveState needs without guessing. The level cache falls
		// back to one GetVolume call the first time a sink/source ref is
		// touched instead (see actions.VolumeHandlers.getCached).
		e.enqueueDispatch(dispatchCh, work{resync: true}, "audio graph refresh")
	}
}
