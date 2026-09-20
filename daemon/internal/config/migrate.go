package config

import (
	"fmt"

	"github.com/njeske/knobd/internal/model"
)

// currentSchemaVersion is model.CurrentSchemaVersion, indirected through
// a package-level var (rather than read directly in Migrate) so
// migrate_test.go can raise it to exercise the migrations loop and its
// "no path" error branch before there is a real schema bump to test
// against — model.CurrentSchemaVersion is a const and can't be swapped
// for a test.
var currentSchemaVersion = model.CurrentSchemaVersion

// migrations[i] upgrades a document from schema version i+1 to i+2 (i.e.
// migrations[0] takes version 1 to version 2). Each step operates on a
// generic JSON document (map[string]any), not a typed model.Config,
// because by the time a document has been decoded into the CURRENT
// model.Config, any field an old version had and the current struct
// doesn't is already gone — a migration that needs to read, rename, or
// restructure such a field has to run before that decode, not after.
// There is deliberately no migration registered yet, since
// model.CurrentSchemaVersion is 1 — this is where the first one goes the
// day that number becomes 2. Keep old migrations forever; a user's
// config.json may not have been opened by a newer knobd in a long time.
var migrations = []func(map[string]any) (map[string]any, error){}

// Migrate upgrades doc's "schemaVersion" field, if needed, to
// model.CurrentSchemaVersion, applying each step in migrations in order.
// A missing, zero, or unrecognized-type "schemaVersion" (meaning: absent
// from the file, which can only happen for a config written before
// versioning existed) is treated as version 1.
//
// If doc is already at or, unexpectedly, past CurrentSchemaVersion, it
// is returned unchanged — "past" can only mean the config was written by
// a newer knobd, which this build should not try to guess how to
// downgrade.
func Migrate(doc map[string]any) (map[string]any, error) {
	version := schemaVersionOf(doc)
	if version > currentSchemaVersion {
		return doc, nil
	}

	for version < currentSchemaVersion {
		idx := version - 1 // migrations[0] takes version 1 to version 2
		if idx < 0 || idx >= len(migrations) {
			return doc, fmt.Errorf("config: no migration path from schema version %d to %d", version, currentSchemaVersion)
		}
		next, err := migrations[idx](doc)
		if err != nil {
			return doc, fmt.Errorf("config: migrate schema version %d to %d: %w", version, version+1, err)
		}
		doc = next
		version++
	}

	doc["schemaVersion"] = version
	return doc, nil
}

// schemaVersionOf reads doc's "schemaVersion" field, treating it as 1 if
// absent, exactly zero, or not a JSON number — encoding/json decodes
// JSON numbers into map[string]any as float64, but a caller building a
// document by hand (see migrate_test.go) may reasonably use a plain int
// instead, so both are accepted.
func schemaVersionOf(doc map[string]any) int {
	version := 0
	switch v := doc["schemaVersion"].(type) {
	case float64:
		version = int(v)
	case int:
		version = v
	}
	if version == 0 {
		version = 1
	}
	return version
}
