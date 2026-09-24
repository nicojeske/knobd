package config

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/njeske/knobd/internal/model"
)

// defaultDoc returns model.Default() round-tripped through JSON into a
// generic document, i.e. exactly what Load's json.Unmarshal into
// map[string]any produces for a freshly-written config.
func defaultDoc(t *testing.T) map[string]any {
	t.Helper()
	data, err := json.Marshal(model.Default())
	if err != nil {
		t.Fatalf("marshal model.Default(): %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal into document: %v", err)
	}
	return doc
}

// decodeOrFatal is decode (config.go), for tests that want to assert on
// the resulting model.Config rather than the raw document — comparing
// decoded values sidesteps the fact that a hand-built map's
// "schemaVersion" may be an int while Migrate always stamps an int but a
// real JSON-decoded document holds a float64, which reflect.DeepEqual on
// the raw map would otherwise treat as a spurious difference.
func decodeOrFatal(t *testing.T, doc map[string]any) model.Config {
	t.Helper()
	cfg, err := decode(doc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return cfg
}

func TestMigrateMissingOrZeroVersionTreatedAsOne(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(doc map[string]any)
	}{
		{"absent", func(doc map[string]any) { delete(doc, "schemaVersion") }},
		{"explicit zero", func(doc map[string]any) { doc["schemaVersion"] = 0.0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := defaultDoc(t)
			tc.mutate(doc)

			got, err := Migrate(doc)
			if err != nil {
				t.Fatalf("Migrate: %v", err)
			}
			if cfg := decodeOrFatal(t, got); cfg.SchemaVersion != model.CurrentSchemaVersion {
				t.Errorf("SchemaVersion = %d, want %d", cfg.SchemaVersion, model.CurrentSchemaVersion)
			}
		})
	}
}

func TestMigrateAlreadyCurrentIsNoop(t *testing.T) {
	doc := defaultDoc(t)
	want := decodeOrFatal(t, doc)

	got, err := Migrate(doc)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if gotCfg := decodeOrFatal(t, got); !reflect.DeepEqual(gotCfg, want) {
		t.Errorf("Migrate(current) modified the config: got %#v, want %#v", gotCfg, want)
	}
}

func TestMigrateFutureVersionPassedThrough(t *testing.T) {
	doc := defaultDoc(t)
	doc["schemaVersion"] = float64(currentSchemaVersion + 1)
	// doc is a map (reference semantics): snapshot its decoded value now,
	// before Migrate runs, rather than comparing the map to itself after
	// — which would trivially pass even if Migrate mutated it in place.
	want := decodeOrFatal(t, doc)

	got, err := Migrate(doc)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	gotCfg := decodeOrFatal(t, got)
	if !reflect.DeepEqual(gotCfg, want) {
		t.Errorf("Migrate should not touch a document from a newer knobd; got %#v, want %#v", gotCfg, want)
	}
	if gotCfg.SchemaVersion != currentSchemaVersion+1 {
		t.Errorf("SchemaVersion = %d, want %d", gotCfg.SchemaVersion, currentSchemaVersion+1)
	}
}

// TestMigrateAppliesRegisteredSteps proves the migrations loop actually
// runs — not just that a config already at model.CurrentSchemaVersion
// round-trips — by temporarily raising currentSchemaVersion and
// registering a synthetic step. There is no real step to test yet (see
// migrate.go's doc comment); this stands in until the first one lands.
func TestMigrateAppliesRegisteredSteps(t *testing.T) {
	restoreVersion, restoreMigrations := currentSchemaVersion, migrations
	t.Cleanup(func() { currentSchemaVersion, migrations = restoreVersion, restoreMigrations })

	// Raise one past the real current version (2, as of M09) so this
	// proves the loop reaches a synthetic step 2 (index 1) beyond the
	// real v1->v2 step already registered, and threads its result
	// through, without depending on that real step's own behavior.
	currentSchemaVersion = 3
	migrations = append(migrations[:1:1], func(doc map[string]any) (map[string]any, error) {
		doc["migratedFrom2To3"] = true
		return doc, nil
	})

	doc := defaultDoc(t)
	got, err := Migrate(doc)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if got["schemaVersion"] != 3 {
		t.Errorf("schemaVersion = %v, want 3", got["schemaVersion"])
	}
	if got["migratedFrom2To3"] != true {
		t.Error("Migrate did not apply the registered step")
	}
}

// TestMigrateV1ToV2AddsMediaSettings exercises the real (not synthetic)
// v1->v2 step: a v1 document has no "media" key at all, and Migrate must
// add one with an empty IgnorePlayers so it decodes into a valid
// model.Config.
func TestMigrateV1ToV2AddsMediaSettings(t *testing.T) {
	doc := defaultDoc(t)
	doc["schemaVersion"] = 1.0
	delete(doc, "media")

	got, err := Migrate(doc)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	cfg := decodeOrFatal(t, got)
	if cfg.SchemaVersion != model.CurrentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", cfg.SchemaVersion, model.CurrentSchemaVersion)
	}
	if cfg.Media.IgnorePlayers == nil || len(cfg.Media.IgnorePlayers) != 0 {
		t.Errorf("Media.IgnorePlayers = %#v, want an empty (non-nil) slice", cfg.Media.IgnorePlayers)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("migrated config does not validate: %v", err)
	}
}

// TestMigrateNoPathReturnsError exercises the branch that was
// unreachable before this rework (model.CurrentSchemaVersion was always
// 1, so version was never less than it and no path could ever be
// missing).
func TestMigrateNoPathReturnsError(t *testing.T) {
	restoreVersion, restoreMigrations := currentSchemaVersion, migrations
	t.Cleanup(func() { currentSchemaVersion, migrations = restoreVersion, restoreMigrations })

	currentSchemaVersion = 3
	migrations = nil // no step registered to reach version 3

	doc := defaultDoc(t)
	if _, err := Migrate(doc); err == nil {
		t.Fatal("expected an error when no migration path exists")
	}
}
