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

// fixturesDir is testdata/config relative to this package -- see its
// README for what each file contains.
const fixturesDir = "../../../testdata/config"

// copyFixture copies a testdata fixture into a fresh t.TempDir() as
// config.json, so a test can freely load/migrate/overwrite it without
// ever touching the checked-in original.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixturesDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture copy: %v", err)
	}
	return path
}

func TestLoadFixturesValidate(t *testing.T) {
	for _, name := range []string{"v1.json", "v0-unversioned.json"} {
		t.Run(name, func(t *testing.T) {
			path := copyFixture(t, name)
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load(%s): %v", name, err)
			}
			if err := cfg.Validate(); err != nil {
				t.Errorf("Load(%s) result failed Validate: %v", name, err)
			}
			if cfg.SchemaVersion != currentSchemaVersion {
				t.Errorf("Load(%s).SchemaVersion = %d, want %d", name, cfg.SchemaVersion, currentSchemaVersion)
			}
		})
	}
}

// TestLoadAndUpgradeRewritesFileAndBacksUpOriginal exercises the actual
// upgrade-on-startup path M12 adds: raise currentSchemaVersion past the
// fixtures' version 1, like TestMigrateAppliesRegisteredSteps does, and
// confirm LoadAndUpgrade (a) writes the migrated document back to path,
// (b) leaves a byte-for-byte backup of the pre-migration file at
// "<path>.v1.bak", and (c) is idempotent -- a second call neither
// changes config.json further nor touches the existing backup.
func TestLoadAndUpgradeRewritesFileAndBacksUpOriginal(t *testing.T) {
	restoreVersion, restoreMigrations := currentSchemaVersion, migrations
	t.Cleanup(func() { currentSchemaVersion, migrations = restoreVersion, restoreMigrations })

	currentSchemaVersion = 2
	migrations = []func(map[string]any) (map[string]any, error){
		func(doc map[string]any) (map[string]any, error) {
			doc["migratedFrom1To2"] = true
			return doc, nil
		},
	}

	for _, name := range []string{"v1.json", "v0-unversioned.json"} {
		t.Run(name, func(t *testing.T) {
			path := copyFixture(t, name)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read copied fixture: %v", err)
			}

			cfg, err := LoadAndUpgrade(path)
			if err != nil {
				t.Fatalf("LoadAndUpgrade: %v", err)
			}
			if cfg.SchemaVersion != 2 {
				t.Errorf("SchemaVersion = %d, want 2", cfg.SchemaVersion)
			}

			onDisk, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s after upgrade: %v", path, err)
			}
			reloaded, err := Load(path)
			if err != nil {
				t.Fatalf("Load rewritten %s: %v", path, err)
			}
			if reloaded.SchemaVersion != 2 {
				t.Errorf("config.json on disk is at schemaVersion %d, want 2 (rewrite didn't happen)", reloaded.SchemaVersion)
			}

			backupPath := path + ".v1.bak"
			backup, err := os.ReadFile(backupPath)
			if err != nil {
				t.Fatalf("read backup %s: %v", backupPath, err)
			}
			if !reflect.DeepEqual(backup, original) {
				t.Errorf("backup %s does not match the pre-migration file byte-for-byte", backupPath)
			}

			// Second call: config.json is now already at the current
			// version, so nothing further should change and the
			// backup should be left exactly as it was.
			if _, err := LoadAndUpgrade(path); err != nil {
				t.Fatalf("second LoadAndUpgrade: %v", err)
			}
			onDiskAgain, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s after second upgrade: %v", path, err)
			}
			if !reflect.DeepEqual(onDiskAgain, onDisk) {
				t.Error("second LoadAndUpgrade changed an already-current config.json")
			}
			backupAgain, err := os.ReadFile(backupPath)
			if err != nil {
				t.Fatalf("read backup after second upgrade: %v", err)
			}
			if !reflect.DeepEqual(backupAgain, backup) {
				t.Error("second LoadAndUpgrade modified the existing backup")
			}
		})
	}
}

func TestLoadAndUpgradeMissingFileWritesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg, err := LoadAndUpgrade(path)
	if err != nil {
		t.Fatalf("LoadAndUpgrade: %v", err)
	}
	if !reflect.DeepEqual(cfg, model.Default()) {
		t.Errorf("LoadAndUpgrade(missing) = %#v, want model.Default()", cfg)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("LoadAndUpgrade should not create a file when none existed")
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
