package config

import (
	"reflect"
	"testing"

	"github.com/njeske/knobd/internal/model"
)

func TestMigrateZeroVersionTreatedAsOne(t *testing.T) {
	cfg := model.Default()
	cfg.SchemaVersion = 0

	got, err := Migrate(cfg)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if got.SchemaVersion != model.CurrentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, model.CurrentSchemaVersion)
	}
}

func TestMigrateAlreadyCurrentIsNoop(t *testing.T) {
	cfg := model.Default()
	cfg.SchemaVersion = model.CurrentSchemaVersion

	got, err := Migrate(cfg)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Errorf("Migrate(current) modified the config: got %#v, want %#v", got, cfg)
	}
}

func TestMigrateFutureVersionPassedThrough(t *testing.T) {
	cfg := model.Default()
	cfg.SchemaVersion = model.CurrentSchemaVersion + 1

	got, err := Migrate(cfg)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if got.SchemaVersion != cfg.SchemaVersion {
		t.Errorf("Migrate should not touch a config from a newer knobd; got version %d, want %d",
			got.SchemaVersion, cfg.SchemaVersion)
	}
}
