package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/njeske/knobd/internal/model"
)

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "does-not-exist.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg, model.Default()) {
		t.Errorf("Load(missing) = %#v, want model.Default()", cfg)
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.json") // exercises MkdirAll

	want := model.Default()
	want.AppMatchers = []model.AppMatcher{
		{ID: "vesktop", DisplayName: "Vesktop", Binaries: []string{"vesktop"}},
	}
	want.Profiles[0].Bindings = []model.Binding{
		{
			Control: model.Control{Kind: model.ControlEncoder, Index: 1},
			Gesture: model.GestureTurn,
			Action: model.VolumeAdjustAction{
				Target:      model.Target{Kind: model.TargetApp, Ref: "vesktop"},
				StepPercent: 2,
			},
		},
	}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.AppMatchers) != 1 || got.AppMatchers[0].ID != "vesktop" {
		t.Errorf("Load() AppMatchers = %#v", got.AppMatchers)
	}
	if len(got.Profiles[0].Bindings) != 1 {
		t.Fatalf("Load() Bindings = %#v", got.Profiles[0].Bindings)
	}
	if got.Profiles[0].Bindings[0].Action != want.Profiles[0].Bindings[0].Action {
		t.Errorf("Load() Action = %#v, want %#v",
			got.Profiles[0].Bindings[0].Action, want.Profiles[0].Bindings[0].Action)
	}
}

func TestSaveRejectsInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	bad := model.Default()
	bad.ActiveProfileID = "does-not-exist"

	if err := Save(path, bad); err == nil {
		t.Fatal("expected Save to reject an invalid config")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Save should not have left a file behind after rejecting an invalid config")
	}
}

func TestSaveIsAtomicNoStrayTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := Save(path, model.Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("directory contains %v, want exactly [config.json]", names)
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error loading malformed JSON")
	}
}

func TestPathHonorsXDGConfigHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	got, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join(dir, DirName, FileName)
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}
