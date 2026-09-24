package main

import (
	"testing"
	"time"

	"github.com/njeske/knobd/internal/api"
	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/engine"
	"github.com/njeske/knobd/internal/focus"
	"github.com/njeske/knobd/internal/model"
)

func TestSnapshotToState(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	enc1 := model.Control{Kind: model.ControlEncoder, Index: 1}
	push2 := model.Control{Kind: model.ControlEncoderPush, Index: 2}
	target := model.Target{Kind: model.TargetApp, Ref: "vesktop"}

	snap := engine.Snapshot{
		ActiveProfileID: "default",
		ActiveLayer:     0,
		Focused:         focus.AppInfo{ResourceClass: "brave-browser", Caption: "some page"},
		Controls: []engine.ControlSnapshot{
			{
				Control:    enc1,
				Gesture:    model.GestureTurn,
				ActionType: model.ActionVolumeAdjust,
				Target:     &target,
				Refs:       []audio.Ref{{Kind: audio.RefStream, ID: "118"}, {Kind: audio.RefStream, ID: "128"}},
				Volume:     &audio.VolumeState{Percent: 42, Muted: false},
			},
			{
				// A layer action: carries no target at all.
				Control:    push2,
				Gesture:    model.GestureHold,
				ActionType: model.ActionLayerMomentary,
			},
		},
	}
	device := api.DeviceState{Connected: true, Name: "X-TOUCH MINI"}
	audioState := api.AudioState{Connected: false, LastError: "connection refused"}

	got := snapshotToState(snap, device, audioState, false, api.MediaState{}, now)

	if got.Now != now {
		t.Errorf("Now = %v, want %v", got.Now, now)
	}
	if got.Device != device {
		t.Errorf("Device = %+v, want %+v", got.Device, device)
	}
	if got.Audio != audioState {
		t.Errorf("Audio = %+v, want %+v", got.Audio, audioState)
	}
	if got.Focus.Available {
		t.Error("Focus.Available = true, want false")
	}
	if got.Focus.ResourceClass != "brave-browser" {
		t.Errorf("Focus.ResourceClass = %q, want %q", got.Focus.ResourceClass, "brave-browser")
	}
	if got.Profile != (api.ProfileState{ActiveProfileID: "default", ActiveLayer: 0}) {
		t.Errorf("Profile = %+v", got.Profile)
	}
	if len(got.Controls) != 2 {
		t.Fatalf("Controls = %+v, want 2 entries", got.Controls)
	}

	c0 := got.Controls[0]
	if c0.Control != enc1 || c0.Gesture != model.GestureTurn || c0.ActionType != model.ActionVolumeAdjust {
		t.Errorf("Controls[0] = %+v", c0)
	}
	if c0.Target == nil || *c0.Target != target {
		t.Errorf("Controls[0].Target = %v, want %+v", c0.Target, target)
	}
	if c0.Resolved == nil {
		t.Fatal("Controls[0].Resolved is nil, want a resolved target")
	}
	wantRefs := []string{"stream:118", "stream:128"}
	if len(c0.Resolved.Refs) != 2 || c0.Resolved.Refs[0] != wantRefs[0] || c0.Resolved.Refs[1] != wantRefs[1] {
		t.Errorf("Controls[0].Resolved.Refs = %v, want %v", c0.Resolved.Refs, wantRefs)
	}
	if c0.Resolved.VolumePercent != 42 {
		t.Errorf("Controls[0].Resolved.VolumePercent = %v, want 42", c0.Resolved.VolumePercent)
	}

	c1 := got.Controls[1]
	if c1.Control != push2 || c1.ActionType != model.ActionLayerMomentary {
		t.Errorf("Controls[1] = %+v", c1)
	}
	if c1.Target != nil {
		t.Errorf("Controls[1].Target = %v, want nil (layer actions carry no target)", c1.Target)
	}
	if c1.Resolved != nil {
		t.Errorf("Controls[1].Resolved = %v, want nil", c1.Resolved)
	}
	if got.Learn.Active {
		t.Error("Learn.Active = true, want false (snap.LearnUntil is the zero time.Time)")
	}
}

func TestSnapshotToStateLearnActive(t *testing.T) {
	learnUntil := time.Date(2026, 9, 22, 12, 0, 15, 0, time.UTC)
	snap := engine.Snapshot{ActiveProfileID: "default", LearnUntil: learnUntil}

	got := snapshotToState(snap, api.DeviceState{}, api.AudioState{}, false, api.MediaState{}, time.Time{})

	if !got.Learn.Active {
		t.Fatal("Learn.Active = false, want true")
	}
	if got.Learn.ExpiresAt == nil || !got.Learn.ExpiresAt.Equal(learnUntil) {
		t.Errorf("Learn.ExpiresAt = %v, want %v", got.Learn.ExpiresAt, learnUntil)
	}
}

func TestSnapshotToStateEmptyControls(t *testing.T) {
	got := snapshotToState(engine.Snapshot{ActiveProfileID: "default"}, api.DeviceState{}, api.AudioState{}, true, api.MediaState{}, time.Time{})
	if len(got.Controls) != 0 {
		t.Errorf("Controls = %+v, want empty", got.Controls)
	}
	if !got.Focus.Available {
		t.Error("Focus.Available = false, want true")
	}
}
