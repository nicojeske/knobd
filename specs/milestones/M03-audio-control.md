# M03: Audio control

## Status

Not started.

## Depends on

M01 (`daemon/internal/audio`'s `Backend` interface, `FakeBackend`,
`model.AppMatcher`/`model.Target`).

## Goal

Given a `model.Target`, resolve it to the live device(s)/stream(s) it
currently refers to and get/set their volume and mute state — including
correctly handling the two real edge cases already captured in
`testdata/pipewire/pw-dump-sample.json`.

## Scope

**In**: real `audio.Backend` implementation (sinks, sources, streams,
get/set volume, get/set mute, change subscription), `audio.Resolve`'s
real implementation (matching a `model.AppMatcher` against live
streams), `/proc/<pid>/exe` fallback resolution, a volume response
curve.

**Out**: wiring this up to actual MIDI input or the mapping engine
(M04). Scenes and solo/duck (M08) build on this but aren't part of it.

## Design

Per ADR 0002, implement `audio.New` against the PipeWire `pipewire-pulse`
compatibility socket via `github.com/jfreymuth/pulse`'s `proto`
subpackage. **First task of this milestone**: confirm that library
actually supports `SetSinkInputVolume`/`SetSinkInputMute`-equivalent
calls and subscription against this system's PipeWire version — if not,
implement the `pactl`-shell-out fallback behind the same `Backend`
interface instead (or as well, selected by a build-time or runtime
check) before building anything else on top of it.

Implement `audio.Resolve` (`daemon/internal/audio/backend.go`) to match
a `model.AppMatcher` against `[]Stream` using an OR across all of its
populated fields (`Binaries`, `AppNames`, `NodeNames`, `DesktopIDs`,
`MediaNameRx`) and OR within each field's list — see
`model.AppMatcher`'s doc comment. Two things to test directly against
`testdata/pipewire/pw-dump-sample.json`:

- The `java` stream (id 112) has no `application.*` properties at all —
  a matcher keyed only on `NodeNames: ["java"]` must still find it, and
  a matcher with no `NodeNames` entry must not accidentally match it via
  some other field.
- `vesktop` and `Pal` (ids 118/128, 132/146) each publish two streams —
  a matcher for either app must resolve to **both** of its streams, and
  a volume-adjust action must apply the same change to both.

When a `Stream`'s properties are ambiguous or minimal (only
`node.name`) but it does have `application.process.id`, fall back to
resolving `/proc/<pid>/exe`'s basename against `AppMatcher.Binaries`
before giving up.

Pick a response curve for `VolumeAdjustAction.StepPercent` (the plan
suggested cubic, "matching how KDE's own slider feels") and document the
chosen mapping in this file once decided, since it affects what
"StepPercent: 2" actually feels like turning the knob.

## Data model changes

None expected beyond what M01 already has (`model.AppMatcher`,
`model.Target`). If curve tuning needs a persisted parameter beyond
`VolumeAdjustAction.CurveExponent` (already scaffolded), add it here and
update `docs/config.schema.json` generation accordingly.

## Acceptance criteria

- [ ] Confirmed (and documented, updating ADR 0002 if the answer is
      "the fallback is needed") whether `pulse/proto` covers everything
      needed, before the rest of this milestone is built on top of it.
- [ ] `audio.Resolve` passes a unit test suite built directly from
      `testdata/pipewire/pw-dump-sample.json`, explicitly asserting on
      both edge cases named above.
- [ ] `SetVolume`/`SetMute` against a live PipeWire session are
      confirmed by hand: change a specific application's volume via
      knobd and see it reflected in `pavucontrol`/KDE's own volume
      mixer, and vice versa.
- [ ] `Subscribe` delivers an event within a reasonable time (no
      polling) when a stream's volume changes externally or a new
      stream appears/disappears.
- [ ] The chosen volume curve is documented in this file and covered by
      a unit test pinning specific input→output values.

## Verification

```bash
cd daemon && go test ./internal/audio/...
go run ./cmd/knobd monitor-audio   # or equivalent debug entry point added for this milestone
# play audio in two apps, change one's volume via knobd, confirm the
# other is unaffected; close an app mid-session and confirm its stream
# disappears from subsequent Streams() calls and a Subscribe event fires
```

## Risks & open questions

- `pulse/proto` API coverage is the single biggest unknown for this
  milestone — resolve it first, per Design above, not last.
- PipeWire-native properties not present through the Pulse compatibility
  layer are a possibility not yet tested; if `AppMatcher` resolution
  ever needs a property Pulse's protocol doesn't surface, that's a
  reason to revisit ADR 0002.
