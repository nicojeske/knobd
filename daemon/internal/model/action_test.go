package model

import (
	"encoding/json"
	"testing"
)

// TestActionRegistryComplete fails the build if a new ActionType
// constant is ever added to action.go without a matching entry in
// actionRegistry — otherwise DecodeAction would silently reject every
// config that uses the new action at runtime instead of at compile/test
// time.
func TestActionRegistryComplete(t *testing.T) {
	allTypes := []ActionType{
		ActionVolumeAdjust, ActionVolumeSet, ActionVolumeMuteToggle, ActionVolumeBalance, ActionVolumeFollow,
		ActionAudioSoloToggle, ActionAudioDuckHold, ActionSceneApply, ActionSceneSave,
		ActionLayerMomentary, ActionLayerLatch, ActionLayerCycle,
		ActionKnobAssignFocusedApp, ActionKnobClear, ActionKnobLockToggle,
		ActionMediaTransport, ActionMediaSeek, ActionMediaTargetCycle, ActionMediaNowPlaying,
		ActionSpotifyLikeToggle, ActionSpotifyAddToPlaylist, ActionSpotifyRemoveFromPlaylist,
		ActionSpotifyStartPlaylist, ActionSpotifyQueueTrack, ActionSpotifyTransferPlayback,
		ActionSinkCycleDefault, ActionMicPushToTalk, ActionMicPushToMute, ActionShellRun,
	}
	for _, at := range allTypes {
		if _, ok := actionRegistry[at]; !ok {
			t.Errorf("action type %q has no actionRegistry entry", at)
		}
	}
	if len(actionRegistry) != len(allTypes) {
		t.Errorf("actionRegistry has %d entries but test lists %d action types; keep both lists in sync",
			len(actionRegistry), len(allTypes))
	}
}

// TestActionTypesSortedAndComplete guards ActionTypes() and ZeroAction(),
// which daemon/internal/schema relies on to enumerate and reflect over
// every registered action.
func TestActionTypesSortedAndComplete(t *testing.T) {
	types := ActionTypes()
	if len(types) != len(actionRegistry) {
		t.Fatalf("ActionTypes() returned %d types, actionRegistry has %d", len(types), len(actionRegistry))
	}
	for i := 1; i < len(types); i++ {
		if types[i-1] >= types[i] {
			t.Fatalf("ActionTypes() not strictly sorted: %q before %q", types[i-1], types[i])
		}
	}
	for _, at := range types {
		zero, ok := ZeroAction(at)
		if !ok {
			t.Errorf("ZeroAction(%q) ok = false, want true", at)
			continue
		}
		if zero.ActionType() != at {
			t.Errorf("ZeroAction(%q).ActionType() = %q", at, zero.ActionType())
		}
	}
	if _, ok := ZeroAction(ActionType("nonexistent.action")); ok {
		t.Error("ZeroAction of an unregistered type should return ok = false")
	}
}

func TestMediaCommandsComplete(t *testing.T) {
	want := []MediaCommand{MediaPlayPause, MediaNext, MediaPrevious, MediaShuffleToggle, MediaRepeatCycle}
	got := MediaCommands()
	if len(got) != len(want) {
		t.Fatalf("MediaCommands() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("MediaCommands()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestEncodeDecodeActionRoundTrip(t *testing.T) {
	actions := []Action{
		VolumeAdjustAction{Target: Target{Kind: TargetFocused}, StepPercent: 2},
		VolumeSetAction{Target: Target{Kind: TargetApp, Ref: "vesktop"}, Percent: 50},
		VolumeMuteToggleAction{Target: Target{Kind: TargetDefaultSource}},
		VolumeFollowAction{Target: Target{Kind: TargetDefaultSink}, MinPercent: 10, MaxPercent: 90},
		AudioDuckHoldAction{Target: Target{Kind: TargetFocused}, DuckPercent: 20},
		SceneApplyAction{SceneID: "meeting"},
		LayerMomentaryAction{Layer: 1},
		KnobAssignFocusedAppAction{StepPercent: 3},
		KnobClearAction{},
		MediaTransportAction{Command: MediaPlayPause},
		ShellRunAction{Command: "notify-send hi"},
	}

	for _, want := range actions {
		encoded, err := EncodeAction(want)
		if err != nil {
			t.Fatalf("EncodeAction(%#v): %v", want, err)
		}
		got, err := DecodeAction(encoded)
		if err != nil {
			t.Fatalf("DecodeAction(%s): %v", encoded, err)
		}
		if got != want {
			t.Errorf("round trip mismatch: got %#v, want %#v (json: %s)", got, want, encoded)
		}
	}
}

func TestDecodeActionUnknownType(t *testing.T) {
	_, err := DecodeAction([]byte(`{"type":"nonexistent.action","params":{}}`))
	if err == nil {
		t.Fatal("expected error for unknown action type, got nil")
	}
}

func TestEncodeActionNil(t *testing.T) {
	if _, err := EncodeAction(nil); err == nil {
		t.Fatal("expected error encoding a nil action, got nil")
	}
}

func TestActionEnvelopeShape(t *testing.T) {
	encoded, err := EncodeAction(VolumeSetAction{Target: Target{Kind: TargetDefaultSink}, Percent: 75})
	if err != nil {
		t.Fatalf("EncodeAction: %v", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if _, ok := envelope["type"]; !ok {
		t.Error("envelope missing \"type\" key")
	}
	if _, ok := envelope["params"]; !ok {
		t.Error("envelope missing \"params\" key")
	}
	var typ string
	if err := json.Unmarshal(envelope["type"], &typ); err != nil {
		t.Fatalf("unmarshal type: %v", err)
	}
	if typ != string(ActionVolumeSet) {
		t.Errorf("type = %q, want %q", typ, ActionVolumeSet)
	}
}
