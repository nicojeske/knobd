# Config schema fixtures

Hand-built (well — generator-built, see below) `config.json` documents
used by `daemon/internal/config`'s migration tests (M12), rather than
the trimmed hardware/PipeWire captures the rest of `testdata/` holds.
Both files describe the same config: the `model.Default()` starter
config plus a few app matchers, an app group, a scene, and a second
profile — enough surface to exercise every top-level collection
`config.Migrate`/`config.Load` walk, not just an empty skeleton.

## Files

- `v1.json` — schema version 1 (the shape before M09's `Config.Media`
  and M10's `Config.Spotify`), produced by `model.Config.Validate`-
  passing Go values marshaled with `encoding/json`, matching exactly
  what `config.Save` would write at that version.
- `v2.json` — schema version 2: `v1.json` plus `Config.Media` (M09), but
  still missing `Config.Spotify` (M10) -- exercises the real v2->v3
  migration step in isolation via `TestLoadFixturesValidate`.
- `v0-unversioned.json` — the same document as `v1.json` with
  `schemaVersion` deleted entirely, standing in for a config written
  before schema versioning existed (`config.Migrate` treats a
  missing/zero `schemaVersion` as version 1 — see
  `daemon/internal/config/migrate.go`'s `schemaVersionOf`).

Consumed by `TestLoadAndUpgrade*`/`TestLoadFixturesValidate` in
`daemon/internal/config/config_test.go`, which copies these into a
`t.TempDir()` before touching them so the originals here never change.
If `model.CurrentSchemaVersion` moves past 3, regenerate/add a fixture
at the new below-current version the same way `v2.json` was added here
— the test that raises `currentSchemaVersion` in-process to exercise a
*real* future migration doesn't need a fixture at the raised version,
only these below-current ones. `v1.json` deliberately has no `"media"`
or `"spotify"` key at all, so it also exercises the real v1->v2->v3
migration chain (`migrateV1toV2`/`migrateV2toV3` in
`daemon/internal/config/migrate.go`), which add `Config.Media` and
`Config.Spotify` with their empty defaults.
