package engine

import (
	"context"
	"testing"
	"time"

	"github.com/njeske/knobd/internal/actions"
	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/device"
	"github.com/njeske/knobd/internal/focus"
	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
)

// waitFor polls cond every millisecond until it returns true or timeout
// elapses -- the same bounded-poll shape as
// daemon/internal/midi/supervisor_test.go's waitFor. It is only ever
// used to wait for goroutine scheduling across the run/dispatcher
// handoff, never to encode a timing assumption under test (those are
// driven by testClock instead).
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func mustInject(t *testing.T, ctx context.Context, port *midi.FakePort, msg midi.Message) {
	t.Helper()
	if err := port.Inject(ctx, msg); err != nil {
		t.Fatalf("Inject(%+v): %v", msg, err)
	}
}

// newTestEngine wires a real device.Codec and the real volume handlers
// against fakes for everything hardware/PipeWire-facing, so Run is
// exercised end to end exactly as cmd/knobd wires it, just with
// midi.FakePort and audio.FakeBackend standing in for the real backends.
// OnApplied is wired to the engine's own NotifyLEDDirty, matching
// cmd/knobd/main.go's wiring (see led_test.go), via the same
// declare-before-construct pattern main.go uses: the closure captures e
// itself, not e's value at closure-creation time, and OnApplied is never
// actually called until well after e is assigned.
func newTestEngine(cfg model.Config, clk *testClock) (*Engine, *midi.FakePort, *audio.FakeBackend) {
	port := midi.NewFakePort(16)
	backend := audio.NewFakeBackend()
	registry := actions.NewRegistry()
	var e *Engine
	vh := actions.NewVolumeHandlers(backend, actions.VolumeOptions{
		OnApplied: func(audio.Ref, audio.VolumeState) {
			if e != nil {
				e.NotifyLEDDirty()
			}
		},
	})
	vh.Register(registry)

	e = New(Deps{
		Port:     port,
		Codec:    device.NewXTouchMiniCodec(),
		Audio:    backend,
		Config:   cfg,
		Registry: registry,
		Clock:    clk,
		Observer: vh,
	})
	return e, port, backend
}

// waitForResolved polls Snapshot until (control, gesture)'s target has
// resolved to at least one ref -- i.e. the run goroutine has processed
// the startup resync and populated its stream/device cache. Dispatching
// an event before this happens would legitimately resolve to nothing and
// be silently skipped (see dispatchGesture), which is correct engine
// behavior but would make a test racy if it didn't wait for it first.
func waitForResolved(t *testing.T, e *Engine, control model.Control, gesture model.Gesture) {
	t.Helper()
	waitFor(t, 2*time.Second, func() bool {
		snap, err := e.Snapshot(context.Background())
		if err != nil {
			return false
		}
		for _, cs := range snap.Controls {
			if cs.Control == control && cs.Gesture == gesture && len(cs.Refs) > 0 {
				return true
			}
		}
		return false
	})
}

func runEngine(t *testing.T, e *Engine, ctx context.Context) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	return done
}

// TestEngineRunEncoderTurnAdjustsVolume is acceptance criterion 1 minus
// physical hardware: a binding from encoder 1's turn to
// volume.adjust{Target: {app, ...}} actually changes that app's volume.
func TestEngineRunEncoderTurnAdjustsVolume(t *testing.T) {
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	ref := audio.Ref{Kind: audio.RefStream, ID: "1"}
	cfg := model.Config{
		ActiveProfileID: "default",
		AppMatchers:     []model.AppMatcher{{ID: "vesktop", AppNames: []string{"vesktop"}}},
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn,
					Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetApp, Ref: "vesktop"}, StepPercent: 2}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, backend := newTestEngine(cfg, clk)
	// Seed before Run starts: the startup resync fires almost
	// immediately, and FakeBackend.Seed has no change-notification of
	// its own for a test to wait on.
	backend.Seed(nil, nil, []audio.Stream{{ID: "1", Direction: audio.StreamPlayback, Props: map[string]string{"application.name": "vesktop"}}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runEngine(t, e, ctx)

	waitForResolved(t, e, enc1, model.GestureTurn)

	mustInject(t, ctx, port, midi.Message{Status: 0xB0, Data1: 16, Data2: 3, Time: clk.Now()}) // encoder 1, +3 detents

	waitFor(t, 2*time.Second, func() bool {
		st, err := backend.GetVolume(context.Background(), ref)
		return err == nil && st.Percent == 106 // Seed's default 100% + 2*3
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil after ctx cancel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

// TestEngineRunFaderMoveSetsAbsoluteVolume exercises the fader's new
// GestureMove/volume.follow path end to end.
func TestEngineRunFaderMoveSetsAbsoluteVolume(t *testing.T) {
	fader := model.Control{Kind: model.ControlFader, Index: 1}
	sinkRef := audio.Ref{Kind: audio.RefSink, ID: "sink1"}
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: fader, Gesture: model.GestureMove,
					Action: model.VolumeFollowAction{Target: model.Target{Kind: model.TargetDefaultSink}}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, backend := newTestEngine(cfg, clk)
	backend.Seed([]audio.Device{{ID: "sink1", IsDefault: true}}, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)

	waitForResolved(t, e, fader, model.GestureMove)

	// Pitch bend channel 9 (status 0xE8): Data1=0, Data2=100 -> full =
	// 100<<7, Value = full>>7 = 100.
	mustInject(t, ctx, port, midi.Message{Status: 0xE8, Data1: 0, Data2: 100, Time: clk.Now()})

	wantPercent := 100.0 * float64(100) / 127 // default range [0,100]
	waitFor(t, 2*time.Second, func() bool {
		st, err := backend.GetVolume(context.Background(), sinkRef)
		if err != nil {
			return false
		}
		diff := st.Percent - wantPercent
		return diff < 1e-6 && diff > -1e-6
	})
}

// TestEngineRunPressVsHold is acceptance criterion 3, driven through the
// full Run loop (Port -> Codec -> gesture machine -> dispatch) with
// midi.FakePort injecting precisely-timed note on/off pairs, exactly as
// the criterion asks for -- the pure gestureMachine tests in
// gesture_test.go cover the state machine's boundary behavior
// exhaustively; this proves the whole pipeline wires it up correctly.
func TestEngineRunPressVsHold(t *testing.T) {
	push1 := model.Control{Kind: model.ControlEncoderPush, Index: 1}
	sinkRef := audio.Ref{Kind: audio.RefSink, ID: "sink1"}
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: push1, Gesture: model.GesturePress,
					Action: model.VolumeSetAction{Target: model.Target{Kind: model.TargetDefaultSink}, Percent: 10}},
				{Layer: 0, Control: push1, Gesture: model.GestureHold,
					Action: model.VolumeSetAction{Target: model.Target{Kind: model.TargetDefaultSink}, Percent: 90}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, backend := newTestEngine(cfg, clk)
	backend.Seed([]audio.Device{{ID: "sink1", IsDefault: true}}, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)

	waitForResolved(t, e, push1, model.GesturePress)

	t0 := clk.Now()

	// --- short press: released at 599ms, below HoldThreshold ---
	clk.drainResets()                                                                     // see drainResets' doc comment: M05's LED timer also resets on this clock
	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 32, Data2: 127, Time: t0}) // encoder-push 1 down
	if !clk.waitForReset(2 * time.Second) {
		t.Fatal("timed out waiting for the loop to arm its hold timer after the first down")
	}
	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 32, Data2: 0, Time: t0.Add(599 * time.Millisecond)}) // up

	waitFor(t, 2*time.Second, func() bool {
		st, err := backend.GetVolume(context.Background(), sinkRef)
		return err == nil && st.Percent == 10
	})

	// --- held past the threshold: HoldThreshold fires while still down ---
	downAt := t0.Add(2 * time.Second)
	clk.drainResets()
	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 32, Data2: 127, Time: downAt})
	if !clk.waitForReset(2 * time.Second) {
		t.Fatal("timed out waiting for the loop to arm its hold timer after the second down")
	}
	// clk.Now() is still t0 (Advance was never called): jumping forward
	// by exactly downAt.Sub(t0)+HoldThreshold lands the clock precisely
	// on the hold deadline the loop just armed.
	clk.Advance(downAt.Sub(t0) + HoldThreshold)

	waitFor(t, 2*time.Second, func() bool {
		st, err := backend.GetVolume(context.Background(), sinkRef)
		return err == nil && st.Percent == 90
	})

	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 32, Data2: 0, Time: downAt.Add(HoldThreshold).Add(10 * time.Millisecond)}) // release
}

// TestEngineRunDecodeErrorIsNonFatal covers the Codec.Decode contract:
// a message legal-but-out-of-mode (here, channel 2 CC, which
// xtouchMiniCodec rejects as ErrStandardMode) must be logged and
// skipped, never stop the loop.
func TestEngineRunDecodeErrorIsNonFatal(t *testing.T) {
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	ref := audio.Ref{Kind: audio.RefStream, ID: "1"}
	cfg := model.Config{
		ActiveProfileID: "default",
		AppMatchers:     []model.AppMatcher{{ID: "vesktop", AppNames: []string{"vesktop"}}},
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn,
					Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetApp, Ref: "vesktop"}, StepPercent: 1}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, backend := newTestEngine(cfg, clk)
	backend.Seed(nil, nil, []audio.Stream{{ID: "1", Direction: audio.StreamPlayback, Props: map[string]string{"application.name": "vesktop"}}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runEngine(t, e, ctx)

	waitForResolved(t, e, enc1, model.GestureTurn)

	// Channel 2 CC (status 0xB1): xtouchMiniCodec.Decode rejects this as
	// ErrStandardMode. The loop must survive it and keep processing.
	mustInject(t, ctx, port, midi.Message{Status: 0xB1, Data1: 16, Data2: 3, Time: clk.Now()})
	mustInject(t, ctx, port, midi.Message{Status: 0xB0, Data1: 16, Data2: 5, Time: clk.Now()}) // a real turn, right after

	waitFor(t, 2*time.Second, func() bool {
		st, err := backend.GetVolume(context.Background(), ref)
		return err == nil && st.Percent == 105
	})

	select {
	case err := <-done:
		t.Fatalf("Run exited early with %v; a decode error must not be fatal", err)
	default:
	}
}

// TestEngineRunSetConfigTakesEffect is acceptance criterion 5's engine
// half: PUT /config's path into a running engine.
func TestEngineRunSetConfigTakesEffect(t *testing.T) {
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	sinkRef := audio.Ref{Kind: audio.RefSink, ID: "sink1"}
	sourceRef := audio.Ref{Kind: audio.RefSource, ID: "source1"}

	before := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn,
					Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetDefaultSink}, StepPercent: 1}},
			},
		}},
	}
	after := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn,
					Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetDefaultSource}, StepPercent: 1}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, backend := newTestEngine(before, clk)
	backend.Seed([]audio.Device{{ID: "sink1", IsDefault: true}}, []audio.Device{{ID: "source1", IsDefault: true}}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)

	waitForResolved(t, e, enc1, model.GestureTurn)

	if err := e.SetConfig(context.Background(), after); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		snap, err := e.Snapshot(context.Background())
		if err != nil {
			return false
		}
		for _, cs := range snap.Controls {
			if cs.Control == enc1 && len(cs.Refs) == 1 && cs.Refs[0] == sourceRef {
				return true
			}
		}
		return false
	})

	mustInject(t, ctx, port, midi.Message{Status: 0xB0, Data1: 16, Data2: 2, Time: clk.Now()})

	waitFor(t, 2*time.Second, func() bool {
		st, err := backend.GetVolume(context.Background(), sourceRef)
		return err == nil && st.Percent == 102
	})
	// The sink (the old binding's target) must be untouched.
	st, err := backend.GetVolume(context.Background(), sinkRef)
	if err != nil || st.Percent != 100 {
		t.Errorf("sink volume changed after SetConfig replaced its binding: %+v, %v", st, err)
	}
}

func TestEngineRunContextCancelReturnsNil(t *testing.T) {
	clk := newTestClock(time.Unix(0, 0))
	e, _, _ := newTestEngine(model.Config{ActiveProfileID: "default", Profiles: []model.Profile{{ID: "default"}}}, clk)

	ctx, cancel := context.WithCancel(context.Background())
	done := runEngine(t, e, ctx)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

// newTestEngineWithFocus is newTestEngine plus a focus.Provider, for
// tests exercising the M06 focus-events wiring end to end (Watch ->
// resolver.setFocused -> TargetFocused resolution -> Snapshot). Kept
// separate from newTestEngine, whose 12 other call sites have no
// reason to specify one.
func newTestEngineWithFocus(cfg model.Config, clk *testClock, fp focus.Provider) (*Engine, *midi.FakePort, *audio.FakeBackend) {
	port := midi.NewFakePort(16)
	backend := audio.NewFakeBackend()
	registry := actions.NewRegistry()
	e := New(Deps{
		Port:     port,
		Codec:    device.NewXTouchMiniCodec(),
		Audio:    backend,
		Focus:    fp,
		Config:   cfg,
		Registry: registry,
		Clock:    clk,
	})
	return e, port, backend
}

// TestEngineRunFocusChangeUpdatesTargetFocusedRefs exercises the M06
// wiring end to end: Engine.Run starts Watch, a focus.FakeProvider
// event flows through the run loop's select arm into
// resolver.setFocused, and a TargetFocused binding's Snapshot refs
// (and Snapshot.Focused) reflect it -- all without dispatching any
// gesture, proving the watch/cache path independently of dispatch.
func TestEngineRunFocusChangeUpdatesTargetFocusedRefs(t *testing.T) {
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn,
					Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetFocused}, StepPercent: 2}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	fp := focus.NewFakeProvider()
	e, _, backend := newTestEngineWithFocus(cfg, clk, fp)
	backend.Seed(nil, nil, []audio.Stream{
		{ID: "1", Direction: audio.StreamPlayback, Props: map[string]string{"application.name": "vesktop"}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runEngine(t, e, ctx)

	// Before any focus is reported, TargetFocused resolves to nothing.
	waitFor(t, 2*time.Second, func() bool {
		snap, err := e.Snapshot(context.Background())
		if err != nil {
			return false
		}
		// Wait for the startup resync (the stream cache) to be ready,
		// evidenced by Controls being populated at all, then confirm no
		// refs yet.
		for _, cs := range snap.Controls {
			if cs.Control == enc1 && cs.Gesture == model.GestureTurn {
				return len(cs.Refs) == 0
			}
		}
		return false
	})

	fp.SetFocused(focus.AppInfo{ResourceClass: "vesktop"})

	waitForResolved(t, e, enc1, model.GestureTurn)

	snap, err := e.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.Focused.ResourceClass != "vesktop" {
		t.Errorf("Snapshot.Focused = %+v, want ResourceClass %q", snap.Focused, "vesktop")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil after ctx cancel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

// TestEngineRunMomentaryLayerSwitchesOnDownAndRevertsOnRelease is M08's
// first acceptance criterion: holding a side button switches layers for
// as long as it's held, taking effect on the raw button-down (not
// HoldThreshold later) and reverting the instant it's released --
// including a gesture (the encoder turn) dispatched entirely between
// the down and up, pinned to the layer active when the button that
// triggered it went down (see downLayer's doc comment).
func TestEngineRunMomentaryLayerSwitchesOnDownAndRevertsOnRelease(t *testing.T) {
	side1 := model.Control{Kind: model.ControlSideButton, Index: 1}
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	sinkRef := audio.Ref{Kind: audio.RefSink, ID: "sink1"}
	sourceRef := audio.Ref{Kind: audio.RefSource, ID: "source1"}
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: side1, Gesture: model.GestureHold, Action: model.LayerMomentaryAction{Layer: 1}},
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn,
					Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetDefaultSink}, StepPercent: 2}},
				{Layer: 1, Control: enc1, Gesture: model.GestureTurn,
					Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetDefaultSource}, StepPercent: 2}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, backend := newTestEngine(cfg, clk)
	backend.Seed([]audio.Device{{ID: "sink1", IsDefault: true}}, []audio.Device{{ID: "source1", IsDefault: true}}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)

	waitForResolved(t, e, enc1, model.GestureTurn) // layer 0's binding, resolved against the sink

	// Hold side 1 down, with no release for a long while -- well short
	// of HoldThreshold (600ms), the switch must already be in effect.
	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 84, Data2: 127, Time: clk.Now()})

	waitFor(t, 2*time.Second, func() bool {
		snap, err := e.Snapshot(context.Background())
		return err == nil && snap.ActiveLayer == 1
	})

	// The side button's own LED lights while its layer is active.
	clk.Advance(ledFlushInterval) // release the trailing flush -- see TestEngineRunLEDRingTracksVolume
	waitFor(t, 2*time.Second, func() bool {
		w := buttonWrites(port, 84)
		return len(w) > 0 && w[len(w)-1].Data2 == 127
	})

	// A turn now hits layer 1's binding (the source), not layer 0's.
	mustInject(t, ctx, port, midi.Message{Status: 0xB0, Data1: 16, Data2: 3, Time: clk.Now()})
	waitFor(t, 2*time.Second, func() bool {
		st, err := backend.GetVolume(context.Background(), sourceRef)
		return err == nil && st.Percent == 106
	})
	if st, err := backend.GetVolume(context.Background(), sinkRef); err != nil || st.Percent != 100 {
		t.Errorf("sink volume changed while layer 1 was active: %+v, %v", st, err)
	}

	// Release side 1 -- reverts to layer 0 instantly.
	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 84, Data2: 0, Time: clk.Now().Add(50 * time.Millisecond)})
	waitFor(t, 2*time.Second, func() bool {
		snap, err := e.Snapshot(context.Background())
		return err == nil && snap.ActiveLayer == 0
	})
	clk.Advance(ledFlushInterval)
	waitFor(t, 2*time.Second, func() bool {
		w := buttonWrites(port, 84)
		return len(w) > 0 && w[len(w)-1].Data2 == 0
	})

	// A further turn now hits layer 0's binding (the sink) again.
	mustInject(t, ctx, port, midi.Message{Status: 0xB0, Data1: 16, Data2: 2, Time: clk.Now()})
	waitFor(t, 2*time.Second, func() bool {
		st, err := backend.GetVolume(context.Background(), sinkRef)
		return err == nil && st.Percent == 104
	})
}

// TestEngineRunMomentaryPinsGestureToDownTimeLayer proves the pinning
// downLayer exists for: releasing the side button *before* the control
// whose gesture is mid-way through (a Hold ... Release pair that spans
// the side button's own release) must still resolve that gesture's
// Release against the layer active when it went down, not whatever's
// active by the time the release actually arrives.
func TestEngineRunMomentaryPinsGestureToDownTimeLayer(t *testing.T) {
	side1 := model.Control{Kind: model.ControlSideButton, Index: 1}
	push1 := model.Control{Kind: model.ControlEncoderPush, Index: 1}
	sourceRef := audio.Ref{Kind: audio.RefSource, ID: "source1"}
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: side1, Gesture: model.GestureHold, Action: model.LayerMomentaryAction{Layer: 1}},
				// Layer 1's push1 hold/release drops then restores the
				// source; nothing is bound to push1 on layer 0.
				{Layer: 1, Control: push1, Gesture: model.GestureHold,
					Action: model.VolumeSetAction{Target: model.Target{Kind: model.TargetDefaultSource}, Percent: 10}},
				{Layer: 1, Control: push1, Gesture: model.GestureRelease,
					Action: model.VolumeSetAction{Target: model.Target{Kind: model.TargetDefaultSource}, Percent: 100}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, backend := newTestEngine(cfg, clk)
	backend.Seed(nil, []audio.Device{{ID: "source1", IsDefault: true}}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)

	t0 := clk.Now()
	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 84, Data2: 127, Time: t0}) // side1 down
	waitFor(t, 2*time.Second, func() bool {
		snap, err := e.Snapshot(context.Background())
		return err == nil && snap.ActiveLayer == 1
	})

	clk.drainResets()
	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 32, Data2: 127, Time: t0.Add(10 * time.Millisecond)}) // push1 down
	if !clk.waitForReset(2 * time.Second) {
		t.Fatal("timed out waiting for the hold timer to arm after push1 down")
	}
	clk.Advance(HoldThreshold + 20*time.Millisecond) // push1's GestureHold fires

	waitFor(t, 2*time.Second, func() bool {
		st, err := backend.GetVolume(context.Background(), sourceRef)
		return err == nil && st.Percent == 10
	})

	// Release side1 first -- layer reverts to 0 -- while push1 is still
	// held.
	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 84, Data2: 0, Time: t0.Add(HoldThreshold + 30*time.Millisecond)})
	waitFor(t, 2*time.Second, func() bool {
		snap, err := e.Snapshot(context.Background())
		return err == nil && snap.ActiveLayer == 0
	})

	// Now release push1. Its Release must still resolve against layer 1
	// (where its Hold binding lived, pinned at push1's own down time)
	// and restore the source's volume -- not silently do nothing because
	// layer 0 has no push1 binding at all.
	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 32, Data2: 0, Time: t0.Add(HoldThreshold + 40*time.Millisecond)})
	waitFor(t, 2*time.Second, func() bool {
		st, err := backend.GetVolume(context.Background(), sourceRef)
		return err == nil && st.Percent == 100
	})
}

// TestEngineRunLatchTogglesLayer is M08's first acceptance criterion's
// other half: a latched layer stays active until explicitly switched
// again -- tapping the same control latches back to layer 0 (per the
// M08 design decision to toggle, since a two-side-button unit has no
// dedicated "back to 0" control).
func TestEngineRunLatchTogglesLayer(t *testing.T) {
	side1 := model.Control{Kind: model.ControlSideButton, Index: 1}
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: side1, Gesture: model.GesturePress, Action: model.LayerLatchAction{Layer: 1}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, _ := newTestEngine(cfg, clk)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)

	tap := func(at time.Time) {
		mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 84, Data2: 127, Time: at})
		mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 84, Data2: 0, Time: at.Add(50 * time.Millisecond)})
	}

	tap(clk.Now())
	waitFor(t, 2*time.Second, func() bool {
		snap, err := e.Snapshot(context.Background())
		return err == nil && snap.ActiveLayer == 1
	})
	clk.Advance(ledFlushInterval)
	waitFor(t, 2*time.Second, func() bool {
		w := buttonWrites(port, 84)
		return len(w) > 0 && w[len(w)-1].Data2 == 127
	})

	tap(clk.Now().Add(2 * time.Second))
	waitFor(t, 2*time.Second, func() bool {
		snap, err := e.Snapshot(context.Background())
		return err == nil && snap.ActiveLayer == 0
	})
	clk.Advance(ledFlushInterval)
	waitFor(t, 2*time.Second, func() bool {
		w := buttonWrites(port, 84)
		return len(w) > 0 && w[len(w)-1].Data2 == 0
	})
}

// TestEngineRunLayerCycleAdvancesWithNoExplicitOrder covers
// layer.cycle's default 0..maxLayer wraparound end to end.
func TestEngineRunLayerCycleAdvancesWithNoExplicitOrder(t *testing.T) {
	btn1 := model.Control{Kind: model.ControlButton, Index: 1} // note 89
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: btn1, Gesture: model.GesturePress, Action: model.LayerCycleAction{}},
				// A layer-2 binding exists purely to give maxLayer a
				// value > 1, so cycling actually visits layer 2 too.
				{Layer: 2, Control: enc1, Gesture: model.GestureTurn,
					Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetDefaultSink}, StepPercent: 1}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, _ := newTestEngine(cfg, clk)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)

	press := func(at time.Time) {
		mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 89, Data2: 127, Time: at})
		mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 89, Data2: 0, Time: at.Add(50 * time.Millisecond)})
	}

	wantLayer := func(want int) {
		t.Helper()
		waitFor(t, 2*time.Second, func() bool {
			snap, err := e.Snapshot(context.Background())
			return err == nil && snap.ActiveLayer == want
		})
	}

	press(clk.Now())
	wantLayer(1)
	press(clk.Now().Add(2 * time.Second))
	wantLayer(2)
	press(clk.Now().Add(4 * time.Second))
	wantLayer(0) // wraps
}

// TestEngineRunGroupBindingControlsEveryMatcherSimultaneously is M08's
// second acceptance criterion, driven through the full Run loop: a
// TargetGroup binding's turn adjusts every stream belonging to every
// matcher in that group, in one dispatch.
func TestEngineRunGroupBindingControlsEveryMatcherSimultaneously(t *testing.T) {
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	vesktopRef := audio.Ref{Kind: audio.RefStream, ID: "1"}
	javaRef := audio.Ref{Kind: audio.RefStream, ID: "2"}
	cfg := model.Config{
		ActiveProfileID: "default",
		AppMatchers: []model.AppMatcher{
			{ID: "vesktop", AppNames: []string{"vesktop"}},
			{ID: "java-app", NodeNames: []string{"java"}},
		},
		AppGroups: []model.AppGroup{
			{ID: "voice", MatcherIDs: []string{"vesktop", "java-app"}},
		},
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn,
					Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetGroup, Ref: "voice"}, StepPercent: 2}},
			},
		}},
	}

	clk := newTestClock(time.Unix(0, 0))
	e, port, backend := newTestEngine(cfg, clk)
	backend.Seed(nil, nil, []audio.Stream{
		{ID: "1", Direction: audio.StreamPlayback, Props: map[string]string{"application.name": "vesktop"}},
		{ID: "2", Direction: audio.StreamPlayback, Props: map[string]string{"node.name": "java"}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)

	waitForResolved(t, e, enc1, model.GestureTurn)

	mustInject(t, ctx, port, midi.Message{Status: 0xB0, Data1: 16, Data2: 3, Time: clk.Now()}) // +3 detents

	waitFor(t, 2*time.Second, func() bool {
		v, err := backend.GetVolume(context.Background(), vesktopRef)
		j, jerr := backend.GetVolume(context.Background(), javaRef)
		return err == nil && jerr == nil && v.Percent == 106 && j.Percent == 106
	})
}

func TestEngineRunAudioSubscriptionClosedIsFatal(t *testing.T) {
	clk := newTestClock(time.Unix(0, 0))
	e, _, backend := newTestEngine(model.Config{ActiveProfileID: "default", Profiles: []model.Profile{{ID: "default"}}}, clk)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runEngine(t, e, ctx)

	// Give Subscribe a moment to register before killing it.
	time.Sleep(10 * time.Millisecond)
	backend.Fail()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil; want the audio subscription-closed error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the audio backend failed")
	}
}
