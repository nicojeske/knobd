package config

import (
	"fmt"

	"github.com/njeske/knobd/internal/model"
)

// migrations[i] upgrades a config from schema version i+1 to i+2 (i.e.
// migrations[0] takes version 1 to version 2). There is deliberately no
// migration registered yet, since model.CurrentSchemaVersion is 1 — this
// is where the first one goes the day that number becomes 2. Keep old
// migrations forever; a user's config.json may not have been opened by a
// newer knobd in a long time.
var migrations = []func(map[string]any) (map[string]any, error){}

// Migrate upgrades cfg's SchemaVersion, if needed, to
// model.CurrentSchemaVersion, applying each step in migrations in order.
// A SchemaVersion of 0 (meaning: absent from the file, which can only
// happen for a config written before versioning existed) is treated as
// version 1.
//
// If cfg is already at or, unexpectedly, past CurrentSchemaVersion, it
// is returned unchanged — "past" can only mean the config was written by
// a newer knobd, which this build should not try to guess how to
// downgrade.
func Migrate(cfg model.Config) (model.Config, error) {
	version := cfg.SchemaVersion
	if version == 0 {
		version = 1
	}

	if version > model.CurrentSchemaVersion {
		return cfg, nil
	}
	if version == model.CurrentSchemaVersion {
		cfg.SchemaVersion = version
		return cfg, nil
	}

	// A real migration chain re-decodes into map[string]any so it can
	// add/rename/restructure fields the current model.Config struct no
	// longer has a field for, then re-decodes the result into
	// model.Config once it reaches CurrentSchemaVersion. Since there are
	// no migrations yet, this branch is unreachable until
	// CurrentSchemaVersion is bumped past 1 — see
	// specs/milestones/M01-foundations.md's "config migration chain"
	// acceptance criterion for the first real one to add here.
	return cfg, fmt.Errorf("config: no migration path from schema version %d to %d", version, model.CurrentSchemaVersion)
}
