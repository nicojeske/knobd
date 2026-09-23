package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/njeske/knobd/internal/actions"
	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/device"
	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
)

// inputRecorder collects OnInput calls under a mutex: OnInput fires on
// the engine's run goroutine, while the test goroutine reads the
// recorded events concurrently (via waitFor's polling), so an unguarded
// slice/counter here would itself be a data race in the test, not the
// engine.
type inputRecorder struct {
	mu     sync.Mutex
	events []device.Event
}

func (r *inputRecorder) record(ev device.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *inputRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func (r *inputRecorder) first() device.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.events[0]
}

// newLearnTestEngine mirrors newTestEngine but also wires OnInput, which
// newTestEngine's fixed signature doesn't expose -- kept as a separate
// helper rather than changing every existing caller of newTestEngine.
func newLearnTestEngine(cfg model.Config, clk *testClock, onInput func(device.Event)) (*Engine, *midi.FakePort, *audio.FakeBackend) {
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
		OnInput:  onInput,
	})
	return e, port, backend
}

func learnTestConfig(enc1 model.Control) model.Config {
	return model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn,
					Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetDefaultSink}, StepPercent: 2}},
			},
		}},
	}
}

// TestEngineLearnSuppressesDispatch is the central guarantee learn mode
// exists for: while armed, a decoded event reaches OnInput but the bound
// action never fires -- turning the encoder must not change the volume.
func TestEngineLearnSuppressesDispatch(t *testing.T) {
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	sinkRef := audio.Ref{Kind: audio.RefSink, ID: "sink1"}
	cfg := learnTestConfig(enc1)

	rec := &inputRecorder{}
	clk := newTestClock(time.Unix(0, 0))
	e, port, backend := newLearnTestEngine(cfg, clk, rec.record)
	backend.Seed([]audio.Device{{ID: "sink1", IsDefault: true}}, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)
	waitForResolved(t, e, enc1, model.GestureTurn)

	if err := e.SetLearnUntil(ctx, clk.Now().Add(10*time.Second)); err != nil {
		t.Fatalf("SetLearnUntil: %v", err)
	}

	mustInject(t, ctx, port, midi.Message{Status: 0xB0, Data1: 16, Data2: 3, Time: clk.Now()}) // encoder 1, +3 detents

	waitFor(t, 2*time.Second, func() bool { return rec.count() > 0 })
	first := rec.first()
	if first.Control != enc1 {
		t.Errorf("captured control = %+v, want %+v", first.Control, enc1)
	}
	if first.Kind != device.EventTurn {
		t.Errorf("captured kind = %v, want EventTurn", first.Kind)
	}

	// Give any (incorrect) dispatch a moment to land, then assert the
	// volume never moved off its seeded 100%.
	time.Sleep(20 * time.Millisecond)
	st, err := backend.GetVolume(context.Background(), sinkRef)
	if err != nil {
		t.Fatalf("GetVolume: %v", err)
	}
	if st.Percent != 100 {
		t.Errorf("volume = %v, want 100 (unchanged) -- learn did not suppress dispatch", st.Percent)
	}
}

// TestEngineLearnIsOneShot checks that a captured input disarms learn
// immediately, and that a second turn (after the capture) dispatches
// normally -- learn selects one control, then gets out of the way.
func TestEngineLearnIsOneShot(t *testing.T) {
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	sinkRef := audio.Ref{Kind: audio.RefSink, ID: "sink1"}
	cfg := learnTestConfig(enc1)

	rec := &inputRecorder{}
	clk := newTestClock(time.Unix(0, 0))
	e, port, backend := newLearnTestEngine(cfg, clk, rec.record)
	backend.Seed([]audio.Device{{ID: "sink1", IsDefault: true}}, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)
	waitForResolved(t, e, enc1, model.GestureTurn)

	if err := e.SetLearnUntil(ctx, clk.Now().Add(10*time.Second)); err != nil {
		t.Fatalf("SetLearnUntil: %v", err)
	}
	mustInject(t, ctx, port, midi.Message{Status: 0xB0, Data1: 16, Data2: 3, Time: clk.Now()})
	waitFor(t, 2*time.Second, func() bool { return rec.count() == 1 })

	waitFor(t, 2*time.Second, func() bool {
		snap, err := e.Snapshot(ctx)
		return err == nil && snap.LearnUntil.IsZero()
	})

	// A second turn, after the capture, must dispatch normally now that
	// learn has disarmed itself.
	mustInject(t, ctx, port, midi.Message{Status: 0xB0, Data1: 16, Data2: 3, Time: clk.Now()})
	waitFor(t, 2*time.Second, func() bool {
		st, err := backend.GetVolume(context.Background(), sinkRef)
		return err == nil && st.Percent == 106
	})
	if got := rec.count(); got != 1 {
		t.Errorf("captured = %d, want 1 (one-shot)", got)
	}
}

// TestEngineLearnExpiresWithNoInput checks that learn disarms itself
// purely from the passage of time, with nothing ever touched -- a UI
// that armed learn and then crashed or was closed must not leave the
// controller's normal dispatch suppressed forever.
func TestEngineLearnExpiresWithNoInput(t *testing.T) {
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	cfg := learnTestConfig(enc1)

	clk := newTestClock(time.Unix(0, 0))
	e, _, backend := newLearnTestEngine(cfg, clk, func(device.Event) {})
	backend.Seed([]audio.Device{{ID: "sink1", IsDefault: true}}, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)
	waitForResolved(t, e, enc1, model.GestureTurn)

	deadline := clk.Now().Add(5 * time.Second)
	clk.drainResets()
	if err := e.SetLearnUntil(ctx, deadline); err != nil {
		t.Fatalf("SetLearnUntil: %v", err)
	}
	if !clk.waitForReset(2 * time.Second) {
		t.Fatal("timed out waiting for the loop to arm its learn-expiry timer")
	}
	clk.Advance(6 * time.Second)

	waitFor(t, 2*time.Second, func() bool {
		snap, err := e.Snapshot(ctx)
		return err == nil && snap.LearnUntil.IsZero()
	})
}

// TestEngineLearnResetsGestureStateOnArm checks the gestures.Reset() gate
// documented on the learnCh case: a button physically down when learn
// arms must not leave stale press state around once learn disarms and
// dispatch resumes -- otherwise the eventual up (or a Tick) could
// synthesize a spurious hold/release for a press learn never saw start.
func TestEngineLearnResetsGestureStateOnArm(t *testing.T) {
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
	e, port, backend := newLearnTestEngine(cfg, clk, func(device.Event) {})
	backend.Seed([]audio.Device{{ID: "sink1", IsDefault: true}}, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEngine(t, e, ctx)
	waitForResolved(t, e, push1, model.GesturePress)

	t0 := clk.Now()
	// drainResets/waitForReset (the same pattern TestEngineRunPressVsHold
	// uses) confirms the down event has actually been processed -- and
	// m.down[push1] populated -- before arming learn, closing the race
	// between the buffered inject and SetLearnUntil's own round trip.
	clk.drainResets()
	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 32, Data2: 127, Time: t0}) // push1 down, physically held
	if !clk.waitForReset(2 * time.Second) {
		t.Fatal("timed out waiting for the loop to arm its hold timer after the down")
	}

	// Arm learn while the button is still down. Without gestures.Reset()
	// on this transition, m.down[push1] would stay populated forever
	// (learn suppresses the eventual up below, since it's not a capture
	// kind), and a later Tick could fire a spurious GestureHold.
	if err := e.SetLearnUntil(ctx, clk.Now().Add(10*time.Second)); err != nil {
		t.Fatalf("SetLearnUntil: %v", err)
	}
	mustInject(t, ctx, port, midi.Message{Status: 0x90, Data1: 32, Data2: 0, Time: t0.Add(50 * time.Millisecond)}) // up, while armed

	// Disarm, and drive the clock well past HoldThreshold: if the down
	// state had survived, Tick would now fire GestureHold and set the
	// volume to 90.
	if err := e.SetLearnUntil(ctx, time.Time{}); err != nil {
		t.Fatalf("SetLearnUntil (disarm): %v", err)
	}
	clk.drainResets()
	clk.Advance(2 * HoldThreshold)
	time.Sleep(20 * time.Millisecond)

	st, err := backend.GetVolume(context.Background(), sinkRef)
	if err != nil {
		t.Fatalf("GetVolume: %v", err)
	}
	if st.Percent != 100 {
		t.Errorf("volume = %v, want 100 (unchanged) -- a stale press survived learn arming and fired a spurious gesture", st.Percent)
	}
}
