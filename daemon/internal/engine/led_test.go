package engine

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/device"
	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
)

// ringWrites filters port.Written() down to encoder-ring CC messages for
// a given encoder index (1-8), in order.
func ringWrites(port *midi.FakePort, encoderIndex int) []midi.Message {
	cc, ok := ringCCForTest(encoderIndex)
	if !ok {
		return nil
	}
	var out []midi.Message
	for _, m := range port.Written() {
		if m.Status == 0xB0 && m.Data1 == cc {
			out = append(out, m)
		}
	}
	return out
}

// ringCCForTest mirrors device's unexported ringCC (encoder 1-8 -> CC
// 48-55) -- duplicated here rather than exported from device purely for
// this test's convenience, since MaxRingPosition is the only constant
// that earns its keep as a cross-package export (engine's ledRingUpdate
// actually needs it at runtime; this is test-only arithmetic).
func ringCCForTest(index int) (byte, bool) {
	if index < 1 || index > 8 {
		return 0, false
	}
	return byte(47 + index), true
}

func buttonWrites(port *midi.FakePort, note byte) []midi.Message {
	var out []midi.Message
	for _, m := range port.Written() {
		if m.Status == 0x90 && m.Data1 == note {
			out = append(out, m)
		}
	}
	return out
}

func fillValue(percent float64) byte {
	pos := int(math.Round(percent / 100 * float64(device.MaxRingPosition)))
	if pos < 0 {
		pos = 0
	}
	if pos > device.MaxRingPosition {
		pos = device.MaxRingPosition
	}
	if pos == 0 {
		return 0x00 // off, per ledOffUpdate
	}
	return byte(device.LEDModeFill)<<4 | byte(pos)
}

// TestEngineRunLEDRingTracksVolume is acceptance criterion 2 minus
// physical hardware: turning a bound encoder updates its own ring, via
// the OnApplied local-echo path (see led.go's markLEDsDirty).
func TestEngineRunLEDRingTracksVolume(t *testing.T) {
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	ref := audio.Ref{Kind: audio.RefStream, ID: "1"}
	cfg := model.Config{
		ActiveProfileID: "default",
		AppMatchers:     []model.AppMatcher{{ID: "vesktop", AppNames: []string{"vesktop"}}},
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn,
					Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetApp, Ref: "vesktop"}, StepPercent: 10}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, backend := newTestEngine(cfg, clk)
	backend.Seed(nil, nil, []audio.Stream{{ID: "1", Direction: audio.StreamPlayback, Props: map[string]string{"application.name": "vesktop"}}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)

	waitForResolved(t, e, enc1, model.GestureTurn)

	// Nothing has read or written this stream ref's level yet (unlike a
	// sink/source target, an app target gets no startup refresh -- see
	// deviceRefsBoundToTargets), so the startup repaint shows it blank,
	// same as an unbound encoder, regardless of what Seed put in the
	// backend directly.
	waitFor(t, 2*time.Second, func() bool {
		w := ringWrites(port, 1)
		return len(w) > 0 && w[len(w)-1].Data2 == 0x00
	})

	// Turn counter-clockwise by 4 (Data2 65-127 = -(v-64)): the first
	// touch populates the level cache via getCached's GetVolume fallback
	// (Seed's default 100%), then steps it by StepPercent*Delta = 10*-4.
	mustInject(t, ctx, port, midi.Message{Status: 0xB0, Data1: 16, Data2: 68, Time: clk.Now()})

	waitFor(t, 2*time.Second, func() bool {
		st, err := backend.GetVolume(context.Background(), ref)
		return err == nil && st.Percent == 60
	})
	// OnApplied's markLEDsDirty only flushes immediately if enough
	// throttle-interval time has passed since the last flush; testClock
	// never advances on its own, so release the trailing flush
	// explicitly -- this is the "wire OnApplied for instant feedback"
	// path under test, not the throttle itself (see the rate-limiting
	// test for that).
	clk.Advance(ledFlushInterval)

	want := fillValue(60)
	waitFor(t, 2*time.Second, func() bool {
		w := ringWrites(port, 1)
		return len(w) > 0 && w[len(w)-1].Data2 == want
	})
}

// TestEngineRunLEDUnboundRingIsBlank is acceptance criterion 5: an
// encoder with no binding shows a blank ring, painted by the initial
// repaint that runs even before any input arrives.
func TestEngineRunLEDUnboundRingIsBlank(t *testing.T) {
	cfg := model.Config{ActiveProfileID: "default", Profiles: []model.Profile{{ID: "default"}}}

	clk := newTestClock(time.Unix(0, 0))
	e, port, _ := newTestEngine(cfg, clk)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)

	waitFor(t, 2*time.Second, func() bool { return len(ringWrites(port, 3)) > 0 })

	w := ringWrites(port, 3)
	last := w[len(w)-1]
	if last.Data2 != 0x00 {
		t.Errorf("unbound encoder 3's last ring write = %#02x, want 0x00 (off)", last.Data2)
	}
}

// TestEngineRunLEDButtonLightsWhenMappedRegardlessOfAction is the
// 2026-09-25 revision: a button bound to any action -- not just
// volume.mute_toggle -- lights solid so a mapped control reads as
// mapped at a glance, and an unbound button stays off next to it.
func TestEngineRunLEDButtonLightsWhenMappedRegardlessOfAction(t *testing.T) {
	btn1 := model.Control{Kind: model.ControlButton, Index: 1} // note 89
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: btn1, Gesture: model.GesturePress, Action: model.KnobLockToggleAction{}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, _ := newTestEngine(cfg, clk)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)

	waitFor(t, 2*time.Second, func() bool {
		w := buttonWrites(port, 89)
		return len(w) > 0 && w[len(w)-1].Data2 == 127
	})

	// Button 2 (note 90) has no binding at all in this config -- it
	// must stay off, unlike button 1's mapped-but-otherwise-stateless
	// binding above.
	waitFor(t, 2*time.Second, func() bool { return len(buttonWrites(port, 90)) > 0 })
	w := buttonWrites(port, 90)
	if last := w[len(w)-1]; last.Data2 != 0 {
		t.Errorf("unbound button 2's last LED write = %#02x, want 0 (off)", last.Data2)
	}
}

// TestEngineRunLEDButtonTracksMute is acceptance criterion 4: pressing a
// mute-bound button toggles its LED, via executeMuteToggle's OnApplied
// call (the actions package fix this milestone also makes).
func TestEngineRunLEDButtonTracksMute(t *testing.T) {
	btn1 := model.Control{Kind: model.ControlButton, Index: 1} // note 89
	sinkRef := audio.Ref{Kind: audio.RefSink, ID: "sink1"}
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: btn1, Gesture: model.GesturePress,
					Action: model.VolumeMuteToggleAction{Target: model.Target{Kind: model.TargetDefaultSink}}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, backend := newTestEngine(cfg, clk)
	backend.Seed([]audio.Device{{ID: "sink1", IsDefault: true}}, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)

	waitForResolved(t, e, btn1, model.GesturePress)

	// Starts unmuted -> button LED starts off.
	waitFor(t, 2*time.Second, func() bool {
		w := buttonWrites(port, 89)
		return len(w) > 0 && w[len(w)-1].Data2 == 0
	})

	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 89, Data2: 127, Time: clk.Now()}) // down
	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 89, Data2: 0, Time: clk.Now()})   // up -> GesturePress

	waitFor(t, 2*time.Second, func() bool {
		st, err := backend.GetVolume(context.Background(), sinkRef)
		return err == nil && st.Muted
	})
	clk.Advance(ledFlushInterval) // release the trailing flush -- see TestEngineRunLEDRingTracksVolume
	waitFor(t, 2*time.Second, func() bool {
		w := buttonWrites(port, 89)
		return len(w) > 0 && w[len(w)-1].Data2 == 127 // ledButtonOnVelocity
	})

	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 89, Data2: 127, Time: clk.Now()})
	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 89, Data2: 0, Time: clk.Now()})

	waitFor(t, 2*time.Second, func() bool {
		st, err := backend.GetVolume(context.Background(), sinkRef)
		return err == nil && !st.Muted
	})
	clk.Advance(ledFlushInterval)
	waitFor(t, 2*time.Second, func() bool {
		w := buttonWrites(port, 89)
		return len(w) > 0 && w[len(w)-1].Data2 == 0
	})
}

// TestEngineRunLEDRateLimitingCollapsesBurst is acceptance criterion 6:
// a burst of rapid external volume changes (the shape a continuous
// fader sweep or several quick pavucontrol adjustments produce)
// collapses to one leading-edge write plus one trailing flush, not one
// write per change -- see led.go's markLEDsDirty/ledFlushInterval.
func TestEngineRunLEDRateLimitingCollapsesBurst(t *testing.T) {
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	cfg := model.Config{
		ActiveProfileID: "default",
		AppMatchers:     []model.AppMatcher{{ID: "app", AppNames: []string{"app1"}}},
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn,
					Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetApp, Ref: "app"}, StepPercent: 1}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, backend := newTestEngine(cfg, clk)
	stream := audio.Stream{ID: "app1", Direction: audio.StreamPlayback, Props: map[string]string{"application.name": "app1"}}
	backend.Seed(nil, nil, []audio.Stream{stream})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)

	waitForResolved(t, e, enc1, model.GestureTurn)

	// Release the resync's own dirty flag (nothing to show yet -- app1's
	// level hasn't been observed) so it can't be mistaken for the
	// burst's leading-edge flush below, then capture the baseline. The
	// sleep is a real-time wait purely for goroutine handoff (like
	// TestEngineRunAudioSubscriptionClosedIsFatal's), not a timing
	// assumption under test: it guarantees the run goroutine has
	// actually executed this flush -- and so recorded lastPush as of
	// *this* Advance, not a later one -- before the next Advance below
	// races ahead of it.
	clk.Advance(ledFlushInterval)
	time.Sleep(20 * time.Millisecond)
	waitFor(t, 2*time.Second, func() bool { return len(ringWrites(port, 1)) > 0 })
	before := len(ringWrites(port, 1))

	// Move clear of *that* flush too: testClock never advances on its
	// own, so the burst's first change needs Now()-lastPush >=
	// ledFlushInterval to actually earn the leading edge, exactly as a
	// real clock would after however long it's been idle.
	clk.Advance(ledFlushInterval)
	time.Sleep(20 * time.Millisecond)

	for _, pct := range []float64{10, 20, 30, 40, 50, 60} {
		backend.Emit(audio.Event{Kind: audio.EventStreamChanged, Stream: &stream, State: &audio.VolumeState{Percent: pct}})
	}

	// The leading-edge write (for the first change) lands quickly...
	waitFor(t, 2*time.Second, func() bool { return len(ringWrites(port, 1)) > before })
	// ...then the trailing flush is held back until the interval
	// elapses, and carries the latest value (60%), not every
	// intermediate one.
	clk.Advance(ledFlushInterval)
	want := fillValue(60)
	waitFor(t, 2*time.Second, func() bool {
		w := ringWrites(port, 1)
		return len(w) > 0 && w[len(w)-1].Data2 == want
	})

	if got := len(ringWrites(port, 1)) - before; got != 2 {
		t.Errorf("6 rapid volume changes produced %d ring writes, want exactly 2 (leading edge + trailing flush)", got)
	}
}

// TestEngineRunRepaintLEDsForcesFullRewrite is the reconnect story: the
// rings/buttons don't remember anything across a power cycle, so
// RepaintLEDs must rewrite every LED-bearing control even though
// nothing about the bound state actually changed.
func TestEngineRunRepaintLEDsForcesFullRewrite(t *testing.T) {
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn,
					Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetDefaultSink}, StepPercent: 1}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, backend := newTestEngine(cfg, clk)
	backend.Seed([]audio.Device{{ID: "sink1", IsDefault: true}}, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)

	waitForResolved(t, e, enc1, model.GestureTurn)
	waitFor(t, 2*time.Second, func() bool { return len(ringWrites(port, 1)) > 0 })
	before := len(port.Written())

	if err := e.RepaintLEDs(context.Background()); err != nil {
		t.Fatalf("RepaintLEDs: %v", err)
	}

	// Nothing about the bound state changed, so a plain markLEDsDirty
	// would have written nothing (the diff would be empty) -- a
	// non-empty write set here proves reset() actually forgot
	// lastPushed rather than RepaintLEDs being a no-op.
	if got := len(port.Written()); got <= before {
		t.Errorf("RepaintLEDs wrote %d new messages, want more than 0 (a full repaint, not a no-op diff)", got-before)
	}
}

// TestEngineFlashControlOverridesThenReverts is knob.assign_focused_app's
// confirmation flash (M06, closing the item M05 deferred): a bound
// encoder's ring briefly renders a full fill regardless of its actual
// resolved volume, then reverts on its own once the flash duration
// elapses -- with no further external trigger, proving the timer-arming
// logic (not just the leading-edge write) is what makes it revert.
func TestEngineFlashControlOverridesThenReverts(t *testing.T) {
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	ref := audio.Ref{Kind: audio.RefStream, ID: "1"}
	cfg := model.Config{
		ActiveProfileID: "default",
		AppMatchers:     []model.AppMatcher{{ID: "vesktop", AppNames: []string{"vesktop"}}},
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn,
					Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetApp, Ref: "vesktop"}, StepPercent: 10}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, backend := newTestEngine(cfg, clk)
	backend.Seed(nil, nil, []audio.Stream{{ID: "1", Direction: audio.StreamPlayback, Props: map[string]string{"application.name": "vesktop"}}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)

	waitForResolved(t, e, enc1, model.GestureTurn)

	// Same setup as TestEngineRunLEDRingTracksVolume: turn counter-
	// clockwise by 4 to bring the real, cached volume to 60% -- a value
	// distinguishable from the flash's own full fill (100% would render
	// identically).
	mustInject(t, ctx, port, midi.Message{Status: 0xB0, Data1: 16, Data2: 68, Time: clk.Now()})
	waitFor(t, 2*time.Second, func() bool {
		st, err := backend.GetVolume(context.Background(), ref)
		return err == nil && st.Percent == 60
	})
	clk.Advance(ledFlushInterval) // release the trailing flush of the real 60% state
	waitFor(t, 2*time.Second, func() bool {
		w := ringWrites(port, 1)
		return len(w) > 0 && w[len(w)-1].Data2 == fillValue(60)
	})

	const flashDuration = 400 * time.Millisecond
	e.FlashControl(enc1, flashDuration)
	clk.Advance(ledFlushInterval) // release the flash's own trailing flush, same throttle as any other LED write

	waitFor(t, 2*time.Second, func() bool {
		writes := ringWrites(port, 1)
		return len(writes) > 0 && writes[len(writes)-1].Data2 == fillValue(100)
	})

	// Nothing else touches the engine from here; only time passing
	// should revert this. Advancing past the flash duration must be
	// sufficient on its own.
	clk.Advance(flashDuration + time.Millisecond)

	waitFor(t, 2*time.Second, func() bool {
		writes := ringWrites(port, 1)
		return len(writes) > 0 && writes[len(writes)-1].Data2 == fillValue(60)
	})
}

// TestEngineFlashControlIsANoopWhenRunIsNotConsuming pins FlashControl's
// documented behavior (mirroring NotifyLEDDirty): calling it before Run
// starts, or after it has returned, must never block or panic.
func TestEngineFlashControlIsANoopWhenRunIsNotConsuming(t *testing.T) {
	clk := newTestClock(time.Unix(0, 0))
	e, _, _ := newTestEngine(model.Config{ActiveProfileID: "default", Profiles: []model.Profile{{ID: "default"}}}, clk)

	done := make(chan struct{})
	go func() {
		e.FlashControl(model.Control{Kind: model.ControlEncoder, Index: 1}, DefaultFlashDuration)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("FlashControl blocked with Run not yet started")
	}
}
