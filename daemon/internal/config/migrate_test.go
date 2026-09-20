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

	currentSchemaVersion = 2
	migrations = []func(map[string]any) (map[string]any, error){
		func(doc map[string]any) (map[string]any, error) {
			// A real migration restructures/renames/drops a field the
			// current model.Config has no place for; this one just
			// proves the loop reaches step 0 and its result is threaded
			// through.
			doc["migratedFrom1To2"] = true
			return doc, nil
		},
	}

	doc := defaultDoc(t)
	got, err := Migrate(doc)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if got["schemaVersion"] != 2 {
		t.Errorf("schemaVersion = %v, want 2", got["schemaVersion"])
	}
	if got["migratedFrom1To2"] != true {
		t.Error("Migrate did not apply the registered step")
	}
}

// TestMigrateNoPathReturnsError exercises the branch that was
// unreachable before this rework (model.CurrentSchemaVersion was always
// 1, so version was never less than it and no path could ever be
// missing).
func TestMigrateNoPathReturnsError(t *testing.T) {
	restoreVersion, restoreMigrations := currentSchemaVersion, migrations
	t.Cleanup(func() { currentSchemaVersion, migrations = restoreVersion, restoreMigrations })

	currentSchemaVersion = 2
	migrations = nil // no step registered to reach version 2

	doc := defaultDoc(t)
	if _, err := Migrate(doc); err == nil {
		t.Fatal("expected an error when no migration path exists")
	}
}
