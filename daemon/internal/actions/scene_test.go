package actions

import (
	"context"
	"testing"

	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/model"
)

func newSceneHandlersFor(fb *audio.FakeBackend, cfg model.Config) (*SceneHandlers, *fakeConfigStore) {
	vh := NewVolumeHandlers(fb, VolumeOptions{})
	store := &fakeConfigStore{cfg: cfg}
	return NewSceneHandlers(vh, store, SceneOptions{}), store
}

// TestSceneApplyRestoresLevelsAndMuteAcrossEntries is M08's third
// acceptance criterion: recalling a scene restores every entry's saved
// level/mute state in one action, in both mute directions at once.
func TestSceneApplyRestoresLevelsAndMuteAcrossEntries(t *testing.T) {
	musicRef := audio.Ref{Kind: audio.RefStream, ID: "music"}
	micRef := audio.Ref{Kind: audio.RefSource, ID: "mic"}
	fb := seedBackend(t, map[audio.Ref]audio.VolumeState{
		musicRef: {Percent: 5, Muted: true},   // scene wants it up and unmuted
		micRef:   {Percent: 90, Muted: false}, // scene wants it down and muted
	})
	scene := model.Scene{
		ID: "meeting",
		Entries: []model.SceneEntry{
			{Target: model.Target{Kind: model.TargetApp, Ref: "music"}, VolumePercent: 80, Muted: false},
			{Target: model.Target{Kind: model.TargetDefaultSource}, VolumePercent: 20, Muted: true},
		},
	}
	h, _ := newSceneHandlersFor(fb, baseConfig())

	inv := Invocation{
		Action:    model.SceneApplyAction{SceneID: "meeting"},
		Scene:     &scene,
		SceneRefs: [][]audio.Ref{{musicRef}, {micRef}},
	}
	if err := h.executeApply(context.Background(), inv); err != nil {
		t.Fatalf("executeApply: %v", err)
	}

	music := mustGetVolume(t, fb, musicRef)
	if music.Percent != 80 || music.Muted {
		t.Errorf("music = %+v, want {Percent:80 Muted:false}", music)
	}
	mic := mustGetVolume(t, fb, micRef)
	if mic.Percent != 20 || !mic.Muted {
		t.Errorf("mic = %+v, want {Percent:20 Muted:true}", mic)
	}
}

// TestSceneApplySkipsEntryThatDidNotResolve: an entry engine couldn't
// resolve to any ref at dispatch time (SceneRefs[i] empty) is a no-op,
// not an error, and every other entry still applies.
func TestSceneApplySkipsEntryThatDidNotResolve(t *testing.T) {
	musicRef := audio.Ref{Kind: audio.RefStream, ID: "music"}
	fb := seedBackend(t, map[audio.Ref]audio.VolumeState{musicRef: {Percent: 5}})
	scene := model.Scene{
		ID: "meeting",
		Entries: []model.SceneEntry{
			{Target: model.Target{Kind: model.TargetApp, Ref: "discord"}, VolumePercent: 100, Muted: false}, // not running
			{Target: model.Target{Kind: model.TargetApp, Ref: "music"}, VolumePercent: 80, Muted: false},
		},
	}
	h, _ := newSceneHandlersFor(fb, baseConfig())

	inv := Invocation{
		Action:    model.SceneApplyAction{SceneID: "meeting"},
		Scene:     &scene,
		SceneRefs: [][]audio.Ref{nil, {musicRef}},
	}
	if err := h.executeApply(context.Background(), inv); err != nil {
		t.Fatalf("executeApply: %v", err)
	}
	music := mustGetVolume(t, fb, musicRef)
	if music.Percent != 80 {
		t.Errorf("music.Percent = %v, want 80", music.Percent)
	}
}

func TestSceneApplyWrongActionTypeErrors(t *testing.T) {
	h, _ := newSceneHandlersFor(audio.NewFakeBackend(), baseConfig())
	err := h.executeApply(context.Background(), Invocation{Action: model.VolumeSetAction{}})
	if err == nil {
		t.Fatal("expected an error for the wrong action type")
	}
}

// TestSceneSaveUpdatesOnlyExistingResolvedEntries is M08's third
// acceptance criterion's other half: saving a scene captures the
// current live state of its existing entries, leaves an unresolved
// entry untouched, and never grows the entry list.
func TestSceneSaveUpdatesOnlyExistingResolvedEntries(t *testing.T) {
	musicRef := audio.Ref{Kind: audio.RefStream, ID: "music"}
	fb := seedBackend(t, map[audio.Ref]audio.VolumeState{musicRef: {Percent: 42, Muted: true}})

	cfg := baseConfig()
	cfg.Scenes = []model.Scene{{
		ID: "meeting",
		Entries: []model.SceneEntry{
			{Target: model.Target{Kind: model.TargetApp, Ref: "music"}, VolumePercent: 0, Muted: false},
			{Target: model.Target{Kind: model.TargetApp, Ref: "discord"}, VolumePercent: 5, Muted: true}, // not running
		},
	}}
	originalEntries := append([]model.SceneEntry(nil), cfg.Scenes[0].Entries...)

	h, store := newSceneHandlersFor(fb, cfg)
	scene := cfg.Scenes[0]
	inv := Invocation{
		Action:    model.SceneSaveAction{SceneID: "meeting"},
		Scene:     &scene,
		SceneRefs: [][]audio.Ref{{musicRef}, nil},
	}
	if err := h.executeSave(context.Background(), inv); err != nil {
		t.Fatalf("executeSave: %v", err)
	}

	if len(store.saved) != 1 {
		t.Fatalf("SetConfig called %d times, want 1", len(store.saved))
	}
	got := store.saved[0].Scenes[0].Entries
	if len(got) != 2 {
		t.Fatalf("scene has %d entries, want 2 (save must never grow/shrink the entry list)", len(got))
	}
	if got[0].VolumePercent != 42 || !got[0].Muted {
		t.Errorf("entry 0 = %+v, want {VolumePercent:42 Muted:true}", got[0])
	}
	if got[1] != originalEntries[1] {
		t.Errorf("entry 1 = %+v, want untouched %+v (target did not resolve)", got[1], originalEntries[1])
	}

	// The caller's original config (and the scene value dispatch handed
	// in) must not have been mutated in place.
	if cfg.Scenes[0].Entries[0].VolumePercent != 0 {
		t.Errorf("caller's original config was mutated: entry 0 VolumePercent = %v, want 0", cfg.Scenes[0].Entries[0].VolumePercent)
	}
}

func TestSceneSaveUnknownSceneErrors(t *testing.T) {
	h, _ := newSceneHandlersFor(audio.NewFakeBackend(), baseConfig())
	scene := model.Scene{ID: "ghost"}
	err := h.executeSave(context.Background(), Invocation{
		Action: model.SceneSaveAction{SceneID: "ghost"},
		Scene:  &scene,
	})
	if err == nil {
		t.Fatal("expected an error for a scene that no longer exists in the live config")
	}
}
