# ADR 0005: JSON Schema generation via `github.com/invopop/jsonschema`

**Status**: Accepted

## Context

`docs/config.schema.json` is the contract `ui/`'s TypeScript types are
generated from (see `docs/README.md` and `CLAUDE.md`'s "API types are
generated from the daemon's Go structs, not hand duplicated"). It needs
to be produced from `daemon/internal/model`'s Go types, with one real
wrinkle: `model.Action` is a 20-member tagged union (`action.go`'s
`actionRegistry`), and the schema needs an explicit `oneOf` branch per
registered type, each with a `const` discriminator — not something a
plain struct-to-schema reflection pass produces on its own.

Every other dependency in `daemon/go.mod` is either absent (the module
had zero external dependencies before this) or CGo-free per ADR 0001.
Adding a schema library is the module's first external dependency and
deserves the same "why" as the other structural decisions here.

## Decision

Generate the schema by reflecting over `daemon/internal/model` with
`github.com/invopop/jsonschema`, driven by a `Reflector.Mapper` callback
(`daemon/internal/schema/schema.go`) rather than by adding `JSONSchema()`
methods to `model` types — this keeps `model` dependency-free, as its own
package doc comment requires, since only `daemon/internal/schema`
imports `jsonschema`.

The emitter lives in its own command, `daemon/cmd/schemagen`, **not** in
`cmd/knobd`. `invopop/jsonschema` pulls in four transitive dependencies
(`bahlo/generic-list-go`, `buger/jsonparser`, `pb33f/ordered-map`,
`go.yaml.in/yaml`); schema generation is a build-time, developer-facing
step, not something the daemon needs to do at runtime, so none of that
should link into the binary that actually ships and runs as a systemd
service. `make schema` runs `go run ./cmd/schemagen`, not
`./knobd --emit-schema`.

`daemon/internal/model` itself imports nothing beyond the standard
library — the constraint this decision extends, not relaxes.

## Alternatives considered

- **Hand-rolled reflection**, walking `model`'s types with the standard
  `reflect` package: keeps the module dependency-free entirely, but
  means writing and maintaining `oneOf`/`$ref`/`enum` construction,
  ordered-property output, and `additionalProperties: false` handling
  from scratch — exactly the machinery `invopop/jsonschema` already gets
  right (RFC draft-bhutton / 2020-12 compliant), for a one-time, low-risk
  build-time tool. Not worth owning.
- **Hand-written `docs/config.schema.json`**, checked by a reflection-based
  test rather than generated: rejected outright — this is precisely the
  "hand duplicated" pattern `CLAUDE.md` rules out for API types, for the
  same reason `ui/src/types/config.ts` isn't hand-maintained forever.

## Consequences

- `daemon/go.sum` now exists; `daemon/go.mod` has one direct dependency
  (`invopop/jsonschema`) plus its four transitive ones.
- `daemon/internal/schema` maintains small shadow types (`Config`,
  `Profile`, `Binding`) mirroring `model.Config`/`model.Profile`'s JSON
  shape and `model.Binding`'s custom-marshaled shape (see
  `binding.go`'s `bindingJSON`) — reflection can't see through
  `model.Binding`'s custom `MarshalJSON`/`UnmarshalJSON` or its `Action`
  interface field, so something has to stand in. `schema_test.go`'s
  structural-coverage check guards these against drifting from the real
  model types.
- `cmd/schemagen` is a second `cmd/` binary purely for local/CI use; it
  is never packaged or installed (see `specs/milestones/M12-packaging.md`).
- `docs/config.schema.json` is committed and gated in CI (`make schema`
  regenerated, then diffed) rather than left a purely local build
  artifact, so drift between the model and the schema fails a build
  instead of silently reaching `ui/`.
