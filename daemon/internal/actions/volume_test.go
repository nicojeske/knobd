package actions

import (
	"context"
	"testing"

	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/model"
)

func seedBackend(t *testing.T, refs map[audio.Ref]audio.VolumeState) *audio.FakeBackend {
	t.Helper()
	fb := audio.NewFakeBackend()
	var sinks, sources []audio.Device
	var streams []audio.Stream
	for ref := range refs {
		switch ref.Kind {
		case audio.RefStream:
			streams = append(streams, audio.Stream{ID: ref.ID, Direction: audio.StreamPlayback})
		case audio.RefRecord:
			streams = append(streams, audio.Stream{ID: ref.ID, Direction: audio.StreamRecord})
		case audio.RefSink:
			sinks = append(sinks, audio.Device{ID: ref.ID})
		case audio.RefSource:
			sources = append(sources, audio.Device{ID: ref.ID})
		}
	}
	fb.Seed(sinks, sources, streams)
	for ref, st := range refs {
		if err := fb.SetVolume(context.Background(), ref, st.Percent); err != nil {
			t.Fatalf("seed SetVolume(%+v): %v", ref, err)
		}
		if err := fb.SetMute(context.Background(), ref, st.Muted); err != nil {
			t.Fatalf("seed SetMute(%+v): %v", ref, err)
		}
	}
	return fb
}

func mustGetVolume(t *testing.T, fb *audio.FakeBackend, ref audio.Ref) audio.VolumeState {
	t.Helper()
	st, err := fb.GetVolume(context.Background(), ref)
	if err != nil {
		t.Fatalf("GetVolume(%+v): %v", ref, err)
	}
	return st
}

func TestVolumeAdjustSingleRef(t *testing.T) {
	ref := audio.Ref{Kind: audio.RefStream, ID: "1"}
	fb := seedBackend(t, map[audio.Ref]audio.VolumeState{ref: {Percent: 50}})
	v := NewVolumeHandlers(fb, VolumeOptions{})
	r := NewRegistry()
	v.Register(r)

	inv := Invocation{
		Action:  model.VolumeAdjustAction{StepPercent: 2},
		Gesture: model.GestureTurn,
		Delta:   3,
		Refs:    []audio.Ref{ref},
	}
	if err := r.Execute(context.Background(), inv); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := mustGetVolume(t, fb, ref)
	if got.Percent != 56 { // 50 + 2*3
		t.Errorf("Percent = %v, want 56", got.Percent)
	}
}

// TestVolumeAdjustCoalescingEquivalence pins the property the engine's
// dispatcher coalescing relies on: applying a step*N delta in one call
// must equal N sequential single-detent calls, because Curve.Adjust's
// exponent math is applied in normalized position space.
func TestVolumeAdjustCoalescingEquivalence(t *testing.T) {
	ref := audio.Ref{Kind: audio.RefStream, ID: "1"}

	fbOne := seedBackend(t, map[audio.Ref]audio.VolumeState{ref: {Percent: 30}})
	vOne := NewVolumeHandlers(fbOne, VolumeOptions{})
	for i := 0; i < 5; i++ {
		if err := vOne.executeAdjust(context.Background(), Invocation{
			Action: model.VolumeAdjustAction{StepPercent: 3, CurveExponent: 2}, Delta: 1, Refs: []audio.Ref{ref},
		}); err != nil {
			t.Fatalf("sequential adjust %d: %v", i, err)
		}
	}

	fbMerged := seedBackend(t, map[audio.Ref]audio.VolumeState{ref: {Percent: 30}})
	vMerged := NewVolumeHandlers(fbMerged, VolumeOptions{})
	if err := vMerged.executeAdjust(context.Background(), Invocation{
		Action: model.VolumeAdjustAction{StepPercent: 3, CurveExponent: 2}, Delta: 5, Refs: []audio.Ref{ref},
	}); err != nil {
		t.Fatalf("merged adjust: %v", err)
	}

	got1 := mustGetVolume(t, fbOne, ref).Percent
	got2 := mustGetVolume(t, fbMerged, ref).Percent
	// Floating point isn't associative, so five sequential math.Pow-based
	// steps and one merged step can differ in the last few bits; this is
	// still "equivalent" for every practical purpose (audio UIs round to
	// whole percent), so compare with a small epsilon rather than exact
	// equality.
	const epsilon = 1e-9
	if diff := got1 - got2; diff > epsilon || diff < -epsilon {
		t.Errorf("sequential result %v != merged result %v (diff %v)", got1, got2, diff)
	}
}

func TestVolumeAdjustMultiRefIndependent(t *testing.T) {
	ref1 := audio.Ref{Kind: audio.RefStream, ID: "1"}
	ref2 := audio.Ref{Kind: audio.RefStream, ID: "2"}
	fb := seedBackend(t, map[audio.Ref]audio.VolumeState{
		ref1: {Percent: 20},
		ref2: {Percent: 80},
	})
	v := NewVolumeHandlers(fb, VolumeOptions{})
	err := v.executeAdjust(context.Background(), Invocation{
		Action: model.VolumeAdjustAction{StepPercent: 5}, Delta: 1, Refs: []audio.Ref{ref1, ref2},
	})
	if err != nil {
		t.Fatalf("executeAdjust: %v", err)
	}
	if got := mustGetVolume(t, fb, ref1).Percent; got != 25 {
		t.Errorf("ref1 Percent = %v, want 25", got)
	}
	if got := mustGetVolume(t, fb, ref2).Percent; got != 85 {
		t.Errorf("ref2 Percent = %v, want 85", got)
	}
}

func TestVolumeAdjustUnmutesOnChange(t *testing.T) {
	ref := audio.Ref{Kind: audio.RefStream, ID: "1"}
	fb := seedBackend(t, map[audio.Ref]audio.VolumeState{ref: {Percent: 40, Muted: true}})
	v := NewVolumeHandlers(fb, VolumeOptions{})
	err := v.executeAdjust(context.Background(), Invocation{
		Action: model.VolumeAdjustAction{StepPercent: 2}, Delta: 1, Refs: []audio.Ref{ref},
	})
	if err != nil {
		t.Fatalf("executeAdjust: %v", err)
	}
	got := mustGetVolume(t, fb, ref)
	if got.Muted {
		t.Error("expected un-muted after volume.adjust")
	}
	if got.Percent != 42 {
		t.Errorf("Percent = %v, want 42", got.Percent)
	}
}

func TestVolumeSetClampsToMax(t *testing.T) {
	ref := audio.Ref{Kind: audio.RefStream, ID: "1"}
	fb := seedBackend(t, map[audio.Ref]audio.VolumeState{ref: {Percent: 10}})
	v := NewVolumeHandlers(fb, VolumeOptions{MaxPercent: 120})
	err := v.executeSet(context.Background(), Invocation{
		Action: model.VolumeSetAction{Percent: 500}, Refs: []audio.Ref{ref},
	})
	if err != nil {
		t.Fatalf("executeSet: %v", err)
	}
	if got := mustGetVolume(t, fb, ref).Percent; got != 120 {
		t.Errorf("Percent = %v, want clamped to 120", got)
	}
}

func TestVolumeFollowMapsValueRange(t *testing.T) {
	cases := []struct {
		name     string
		min, max float64
		value    int
		wantPct  float64
	}{
		{"default range at 0", 0, 0, 0, 0},
		{"default range at max", 0, 0, 127, 100},
		{"default range at midpoint", 0, 0, 64, 100 * 64.0 / 127},
		{"custom range at 0", 10, 90, 0, 10},
		{"custom range at max", 10, 90, 127, 90},
		{"value clamped above 127", 0, 0, 200, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := audio.Ref{Kind: audio.RefSink, ID: "sink1"}
			fb := seedBackend(t, map[audio.Ref]audio.VolumeState{ref: {Percent: 0}})
			v := NewVolumeHandlers(fb, VolumeOptions{})
			err := v.executeFollow(context.Background(), Invocation{
				Action: model.VolumeFollowAction{MinPercent: tc.min, MaxPercent: tc.max},
				Value:  tc.value,
				Refs:   []audio.Ref{ref},
			})
			if err != nil {
				t.Fatalf("executeFollow: %v", err)
			}
			got := mustGetVolume(t, fb, ref).Percent
			if diff := got - tc.wantPct; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("Percent = %v, want %v", got, tc.wantPct)
			}
		})
	}
}

func TestVolumeMuteToggleMatrix(t *testing.T) {
	ref1 := audio.Ref{Kind: audio.RefStream, ID: "1"}
	ref2 := audio.Ref{Kind: audio.RefStream, ID: "2"}

	cases := []struct {
		name      string
		seed      map[audio.Ref]audio.VolumeState
		wantMuted map[audio.Ref]bool
	}{
		{
			"all unmuted -> mute all",
			map[audio.Ref]audio.VolumeState{ref1: {Percent: 50}, ref2: {Percent: 50}},
			map[audio.Ref]bool{ref1: true, ref2: true},
		},
		{
			"all muted -> unmute all",
			map[audio.Ref]audio.VolumeState{ref1: {Percent: 50, Muted: true}, ref2: {Percent: 50, Muted: true}},
			map[audio.Ref]bool{ref1: false, ref2: false},
		},
		{
			"mixed -> mute all (converge, don't oscillate)",
			map[audio.Ref]audio.VolumeState{ref1: {Percent: 50, Muted: true}, ref2: {Percent: 50, Muted: false}},
			map[audio.Ref]bool{ref1: true, ref2: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fb := seedBackend(t, tc.seed)
			v := NewVolumeHandlers(fb, VolumeOptions{})
			err := v.executeMuteToggle(context.Background(), Invocation{
				Action: model.VolumeMuteToggleAction{}, Refs: []audio.Ref{ref1, ref2},
			})
			if err != nil {
				t.Fatalf("executeMuteToggle: %v", err)
			}
			for ref, want := range tc.wantMuted {
				if got := mustGetVolume(t, fb, ref).Muted; got != want {
					t.Errorf("%+v Muted = %v, want %v", ref, got, want)
				}
			}
		})
	}
}

func TestVolumeBalanceHasNoHandler(t *testing.T) {
	r := NewRegistry()
	v := NewVolumeHandlers(audio.NewFakeBackend(), VolumeOptions{})
	v.Register(r)
	err := r.Execute(context.Background(), Invocation{Action: model.VolumeBalanceAction{}})
	if err == nil {
		t.Fatal("expected an error: volume.balance has no registered handler")
	}
}

func TestVolumeAdjustPropagatesBackendError(t *testing.T) {
	v := NewVolumeHandlers(audio.NewFakeBackend(), VolumeOptions{})

	// A ref the fake backend never seeded errors deterministically from
	// GetVolume, which executeAdjust must propagate rather than panic on.
	unknown := audio.Ref{Kind: audio.RefStream, ID: "does-not-exist"}
	err := v.executeAdjust(context.Background(), Invocation{
		Action: model.VolumeAdjustAction{StepPercent: 1}, Delta: 1, Refs: []audio.Ref{unknown},
	})
	if err == nil {
		t.Fatal("expected an error for an unknown ref")
	}
}
