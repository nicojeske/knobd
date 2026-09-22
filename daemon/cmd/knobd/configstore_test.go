package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/njeske/knobd/internal/config"
	"github.com/njeske/knobd/internal/model"
)

type stubApplier struct {
	err   error
	calls []model.Config
}

func (s *stubApplier) SetConfig(_ context.Context, cfg model.Config) error {
	s.calls = append(s.calls, cfg)
	if s.err != nil {
		return s.err
	}
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestConfigStoreSetConfigSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	applier := &stubApplier{}
	store := newConfigStore(path, model.Default(), applier, discardLogger())

	next := model.Default()
	next.Profiles[0].DisplayName = "Renamed"
	if err := store.SetConfig(context.Background(), next); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	if got := store.Config(); got.Profiles[0].DisplayName != "Renamed" {
		t.Errorf("Config() = %+v, want the new config in memory", got)
	}
	onDisk, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if onDisk.Profiles[0].DisplayName != "Renamed" {
		t.Errorf("on-disk config = %+v, want the new config persisted", onDisk)
	}
	if len(applier.calls) != 1 {
		t.Errorf("engine.SetConfig called %d times, want 1", len(applier.calls))
	}
}

func TestConfigStoreSetConfigRejectsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	applier := &stubApplier{}
	original := model.Default()
	store := newConfigStore(path, original, applier, discardLogger())

	invalid := model.Config{ActiveProfileID: "nonexistent"} // Validate fails: no such profile
	if err := store.SetConfig(context.Background(), invalid); err == nil {
		t.Fatal("expected an error for an invalid config")
	}
	if got := store.Config(); got.ActiveProfileID != original.ActiveProfileID {
		t.Errorf("Config() changed despite validation failure: %+v", got)
	}
	if len(applier.calls) != 0 {
		t.Errorf("engine.SetConfig called %d times, want 0 (validation should fail before it)", len(applier.calls))
	}
}

func TestConfigStoreSetConfigApplierFailureLeavesDiskUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	applier := &stubApplier{err: errors.New("engine refused it")}
	original := model.Default()
	store := newConfigStore(path, original, applier, discardLogger())

	next := model.Default()
	next.Profiles[0].DisplayName = "Should Not Land"
	if err := store.SetConfig(context.Background(), next); err == nil {
		t.Fatal("expected an error when the engine applier fails")
	}

	if got := store.Config(); got.Profiles[0].DisplayName != original.Profiles[0].DisplayName {
		t.Errorf("Config() changed despite the applier failing: %+v", got)
	}
	// config.Save is only ever reached after the applier succeeds, so
	// the file must never have been created at all.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no file at %s (applier failure must precede any disk write), stat err = %v", path, err)
	}
}

// TestConfigStoreSetConfigSaveFailureRollsEngineBack points path at a
// directory (config.Save's os.MkdirAll/CreateTemp will fail against it)
// to force a save failure after the engine has already accepted the new
// config, and asserts the engine is rolled back to the previous value.
func TestConfigStoreSetConfigSaveFailureRollsEngineBack(t *testing.T) {
	// A path whose parent directory cannot be created: pointing "config
	// dir" at a file (not a directory) makes os.MkdirAll fail.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("blocking"), 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}
	path := filepath.Join(blocker, "config.json")

	applier := &stubApplier{}
	original := model.Default()
	store := newConfigStore(path, original, applier, discardLogger())

	next := model.Default()
	next.Profiles[0].DisplayName = "Rolled Back"
	if err := store.SetConfig(context.Background(), next); err == nil {
		t.Fatal("expected a save error")
	}

	if got := store.Config(); got.Profiles[0].DisplayName != original.Profiles[0].DisplayName {
		t.Errorf("Config() = %+v, want unchanged after a save failure", got)
	}
	// The applier should have been called twice: once with the new
	// config, once to roll back to the original.
	if len(applier.calls) != 2 {
		t.Fatalf("engine.SetConfig called %d times, want 2 (apply + rollback)", len(applier.calls))
	}
	if applier.calls[1].Profiles[0].DisplayName != original.Profiles[0].DisplayName {
		t.Errorf("rollback call = %+v, want the original config", applier.calls[1])
	}
}
