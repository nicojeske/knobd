# Config schema fixtures

Hand-built (well — generator-built, see below) `config.json` documents
used by `daemon/internal/config`'s migration tests (M12), rather than
the trimmed hardware/PipeWire captures the rest of `testdata/` holds.
Both files describe the same config: the `model.Default()` starter
config plus a few app matchers, an app group, a scene, and a second
profile — enough surface to exercise every top-level collection
`config.Migrate`/`config.Load` walk, not just an empty skeleton.

## Files

- `v1.json` — schema version 1 (`model.CurrentSchemaVersion` as of M12),
  produced by `model.Config.Validate`-passing Go values marshaled with
  `encoding/json`, matching exactly what `config.Save` would write.
- `v0-unversioned.json` — the same document with `schemaVersion` deleted
  entirely, standing in for a config written before schema versioning
  existed (`config.Migrate` treats a missing/zero `schemaVersion` as
  version 1 — see `daemon/internal/config/migrate.go`'s
  `schemaVersionOf`).

Consumed by `TestLoadAndUpgrade*` in `daemon/internal/config/config_test.go`,
which copies these into a `t.TempDir()` before touching them so the
originals here never change. If `model.CurrentSchemaVersion` moves past
1, regenerate `v1.json` from the new shape (or add a `v2.json` etc.) —
the test that raises `currentSchemaVersion` in-process to exercise a
*real* future migration doesn't need a fixture at the raised version,
only these two below-current ones.
