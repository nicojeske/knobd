package actions

import (
	"context"
	"testing"

	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/model"
)

func newMixHandlersFor(fb *audio.FakeBackend) *MixHandlers {
	return NewMixHandlers(NewVolumeHandlers(fb, VolumeOptions{}), MixOptions{})
}

// TestSoloMutesOthersAndRestoresMixedPriorStatesExactly is M08's fourth
// acceptance criterion: solo mutes everything else and a second press
// restores prior mute states exactly, not just "unmute everything" --
// one of the "everything else" streams starts already muted and must
// stay muted afterward.
func TestSoloMutesOthersAndRestoresMixedPriorStatesExactly(t *testing.T) {
	target := audio.Ref{Kind: audio.RefStream, ID: "target"}
	alreadyMuted := audio.Ref{Kind: audio.RefStream, ID: "already-muted"}
	unmuted := audio.Ref{Kind: audio.RefStream, ID: "unmuted"}
	fb := seedBackend(t, map[audio.Ref]audio.VolumeState{
		target:       {Percent: 50, Muted: false},
		alreadyMuted: {Percent: 60, Muted: true},
		unmuted:      {Percent: 70, Muted: false},
	})
	h := newMixHandlersFor(fb)

	inv := Invocation{
		Action: model.AudioSoloToggleAction{Target: model.Target{Kind: model.TargetApp, Ref: "target"}},
		Refs:   []audio.Ref{target},
		Others: []audio.Ref{alreadyMuted, unmuted},
	}

	if err := h.executeSolo(context.Background(), inv); err != nil {
		t.Fatalf("first press (solo on): %v", err)
	}
	if st := mustGetVolume(t, fb, target); st.Muted {
		t.Error("target must be unmuted while soloed")
	}
	if st := mustGetVolume(t, fb, alreadyMuted); !st.Muted {
		t.Error("already-muted must stay muted while soloed")
	}
	if st := mustGetVolume(t, fb, unmuted); !st.Muted {
		t.Error("unmuted must become muted while soloed")
	}

	if err := h.executeSolo(context.Background(), inv); err != nil {
		t.Fatalf("second press (solo off): %v", err)
	}
	if st := mustGetVolume(t, fb, target); st.Muted {
		t.Errorf("target should be restored to its prior unmuted state, got %+v", st)
	}
	if st := mustGetVolume(t, fb, alreadyMuted); !st.Muted {
		t.Errorf("already-muted should be restored to muted, got %+v", st)
	}
	if st := mustGetVolume(t, fb, unmuted); st.Muted {
		t.Errorf("unmuted should be restored to unmuted, got %+v", st)
	}
}

// TestSoloSwitchingTargetRestoresOldBeforeSoloingNew covers pressing a
// *different* solo target while one is already active.
func TestSoloSwitchingTargetRestoresOldBeforeSoloingNew(t *testing.T) {
	first := audio.Ref{Kind: audio.RefStream, ID: "first"}
	second := audio.Ref{Kind: audio.RefStream, ID: "second"}
	other := audio.Ref{Kind: audio.RefStream, ID: "other"}
	fb := seedBackend(t, map[audio.Ref]audio.VolumeState{
		first:  {Percent: 50, Muted: false},
		second: {Percent: 50, Muted: true}, // starts muted
		other:  {Percent: 50, Muted: false},
	})
	h := newMixHandlersFor(fb)

	soloFirst := Invocation{
		Action: model.AudioSoloToggleAction{Target: model.Target{Kind: model.TargetApp, Ref: "first"}},
		Refs:   []audio.Ref{first},
		Others: []audio.Ref{second, other},
	}
	if err := h.executeSolo(context.Background(), soloFirst); err != nil {
		t.Fatalf("solo first: %v", err)
	}

	soloSecond := Invocation{
		Action: model.AudioSoloToggleAction{Target: model.Target{Kind: model.TargetApp, Ref: "second"}},
		Refs:   []audio.Ref{second},
		Others: []audio.Ref{first, other},
	}
	if err := h.executeSolo(context.Background(), soloSecond); err != nil {
		t.Fatalf("solo second: %v", err)
	}

	// first is muted under the new solo, same as any other non-target
	// stream -- switching restores it to its pre-solo state only
	// transiently, before the new solo's own Others pass mutes it again.
	if st := mustGetVolume(t, fb, first); !st.Muted {
		t.Errorf("first should be muted, it's an \"other\" under the new solo, got %+v", st)
	}
	if st := mustGetVolume(t, fb, second); st.Muted {
		t.Error("second must be unmuted, it's now the solo target")
	}
	if st := mustGetVolume(t, fb, other); !st.Muted {
		t.Error("other must be muted under the new solo")
	}
}

func TestSoloWrongActionTypeErrors(t *testing.T) {
	h := newMixHandlersFor(audio.NewFakeBackend())
	if err := h.executeSolo(context.Background(), Invocation{Action: model.VolumeSetAction{}}); err == nil {
		t.Fatal("expected an error for the wrong action type")
	}
}

// TestSoloToggleOffWithTargetGoneStillRestoresOthers: the target app
// exited while soloed (Refs now resolves to nothing), but toggling
// solo off must still restore everything else -- solo/duck never skip
// dispatch just because a target resolved to nothing (see
// engine.dispatchWithOthers' doc comment).
func TestSoloToggleOffWithTargetGoneStillRestoresOthers(t *testing.T) {
	target := audio.Ref{Kind: audio.RefStream, ID: "target"}
	other := audio.Ref{Kind: audio.RefStream, ID: "other"}
	fb := seedBackend(t, map[audio.Ref]audio.VolumeState{
		target: {Percent: 50, Muted: false},
		other:  {Percent: 50, Muted: false},
	})
	h := newMixHandlersFor(fb)

	on := Invocation{
		Action: model.AudioSoloToggleAction{Target: model.Target{Kind: model.TargetApp, Ref: "target"}},
		Refs:   []audio.Ref{target},
		Others: []audio.Ref{other},
	}
	if err := h.executeSolo(context.Background(), on); err != nil {
		t.Fatalf("solo on: %v", err)
	}

	// The target app exits: engine now resolves Refs to nothing, but
	// this is still logically "toggle the same solo off" from the
	// user's perspective (same binding, second press).
	off := Invocation{
		Action: model.AudioSoloToggleAction{Target: model.Target{Kind: model.TargetApp, Ref: "target"}},
		Refs:   nil,
		Others: []audio.Ref{other, target},
	}
	if err := h.executeSolo(context.Background(), off); err != nil {
		t.Fatalf("solo off: %v", err)
	}
	if st := mustGetVolume(t, fb, other); st.Muted {
		t.Errorf("other should be restored to unmuted, got %+v", st)
	}
}

// TestDuckLowersOthersToPercentAndRestoresExactLevelsOnRelease is M08's
// fifth acceptance criterion: duck-while-held reduces everything else's
// volume for the hold duration and restores exact prior levels on
// release, leaving an already-quieter stream untouched in both
// directions.
func TestDuckLowersOthersToPercentAndRestoresExactLevelsOnRelease(t *testing.T) {
	target := audio.Ref{Kind: audio.RefStream, ID: "target"}
	loud := audio.Ref{Kind: audio.RefStream, ID: "loud"}
	quiet := audio.Ref{Kind: audio.RefStream, ID: "quiet"} // already below DuckPercent
	fb := seedBackend(t, map[audio.Ref]audio.VolumeState{
		target: {Percent: 80},
		loud:   {Percent: 90},
		quiet:  {Percent: 5},
	})
	h := newMixHandlersFor(fb)
	push1 := model.Control{Kind: model.ControlEncoderPush, Index: 1}

	hold := Invocation{
		Action:  model.AudioDuckHoldAction{Target: model.Target{Kind: model.TargetApp, Ref: "target"}, DuckPercent: 20},
		Control: push1,
		Gesture: model.GestureHold,
		Refs:    []audio.Ref{target},
		Others:  []audio.Ref{loud, quiet},
	}
	if err := h.executeDuck(context.Background(), hold); err != nil {
		t.Fatalf("hold: %v", err)
	}
	if st := mustGetVolume(t, fb, loud); st.Percent != 20 {
		t.Errorf("loud.Percent = %v, want 20 (ducked)", st.Percent)
	}
	if st := mustGetVolume(t, fb, quiet); st.Percent != 5 {
		t.Errorf("quiet.Percent = %v, want 5 (already below duck level, untouched)", st.Percent)
	}

	release := hold
	release.Gesture = model.GestureRelease
	if err := h.executeDuck(context.Background(), release); err != nil {
		t.Fatalf("release: %v", err)
	}
	if st := mustGetVolume(t, fb, loud); st.Percent != 90 {
		t.Errorf("loud.Percent = %v, want 90 (restored)", st.Percent)
	}
	if st := mustGetVolume(t, fb, quiet); st.Percent != 5 {
		t.Errorf("quiet.Percent = %v, want 5 (never touched)", st.Percent)
	}
}

func TestDuckRepeatHoldWithNoInterveningReleaseIsNoop(t *testing.T) {
	loud := audio.Ref{Kind: audio.RefStream, ID: "loud"}
	fb := seedBackend(t, map[audio.Ref]audio.VolumeState{loud: {Percent: 90}})
	h := newMixHandlersFor(fb)
	push1 := model.Control{Kind: model.ControlEncoderPush, Index: 1}

	hold := Invocation{
		Action:  model.AudioDuckHoldAction{DuckPercent: 20},
		Control: push1,
		Gesture: model.GestureHold,
		Others:  []audio.Ref{loud},
	}
	if err := h.executeDuck(context.Background(), hold); err != nil {
		t.Fatalf("first hold: %v", err)
	}
	// Manually raise the level, as if the user turned a knob mid-duck --
	// a second Hold with no Release in between must not re-capture this
	// as the new "prior" level.
	if err := fb.SetVolume(context.Background(), loud, 33); err != nil {
		t.Fatalf("SetVolume: %v", err)
	}
	if err := h.executeDuck(context.Background(), hold); err != nil {
		t.Fatalf("second hold (no-op): %v", err)
	}

	release := hold
	release.Gesture = model.GestureRelease
	if err := h.executeDuck(context.Background(), release); err != nil {
		t.Fatalf("release: %v", err)
	}
	if st := mustGetVolume(t, fb, loud); st.Percent != 90 {
		t.Errorf("loud.Percent = %v, want 90 (restored from the first hold's capture)", st.Percent)
	}
}

func TestDuckReleaseWithNoActiveSessionIsNoop(t *testing.T) {
	h := newMixHandlersFor(audio.NewFakeBackend())
	release := Invocation{
		Action:  model.AudioDuckHoldAction{},
		Control: model.Control{Kind: model.ControlEncoderPush, Index: 1},
		Gesture: model.GestureRelease,
	}
	if err := h.executeDuck(context.Background(), release); err != nil {
		t.Fatalf("release with no session: %v", err)
	}
}

func TestDuckWrongActionTypeErrors(t *testing.T) {
	h := newMixHandlersFor(audio.NewFakeBackend())
	if err := h.executeDuck(context.Background(), Invocation{Action: model.VolumeSetAction{}}); err == nil {
		t.Fatal("expected an error for the wrong action type")
	}
}

func TestDuckIndependentPerControl(t *testing.T) {
	loud := audio.Ref{Kind: audio.RefStream, ID: "loud"}
	fb := seedBackend(t, map[audio.Ref]audio.VolumeState{loud: {Percent: 90}})
	h := newMixHandlersFor(fb)
	push1 := model.Control{Kind: model.ControlEncoderPush, Index: 1}
	push2 := model.Control{Kind: model.ControlEncoderPush, Index: 2}

	hold1 := Invocation{Action: model.AudioDuckHoldAction{DuckPercent: 20}, Control: push1, Gesture: model.GestureHold, Others: []audio.Ref{loud}}
	if err := h.executeDuck(context.Background(), hold1); err != nil {
		t.Fatalf("hold1: %v", err)
	}
	// A different control's release must not affect push1's session.
	release2 := Invocation{Action: model.AudioDuckHoldAction{}, Control: push2, Gesture: model.GestureRelease}
	if err := h.executeDuck(context.Background(), release2); err != nil {
		t.Fatalf("release2: %v", err)
	}
	if st := mustGetVolume(t, fb, loud); st.Percent != 20 {
		t.Errorf("loud.Percent = %v, want 20 (push1's duck still active)", st.Percent)
	}
}
