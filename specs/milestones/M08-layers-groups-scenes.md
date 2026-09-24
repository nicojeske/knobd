# M08: Layers, groups, scenes

## Status

Done (hand verification with hardware still open). Implemented
2026-09-24 across four commits (layers, groups, scenes, solo/duck) plus
one UI commit; see `git log --grep '^M08:'`.

## Depends on

M04 (engine/binding lookup), M07 (a UI to manage groups/scenes/layers
without hand-editing JSON — technically buildable against the API
directly first, but not pleasant to use without it).

## Goal

The two side buttons switch software layers (momentary and latching),
turning every other control into effectively 2-3x as many controls
without needing a mode indicator to avoid getting lost; user-defined app
groups let one binding control several apps as a unit; scenes let one
button recall (or, long-pressed, save) a complete saved mix; solo and
duck-while-held round out the audio-macro set from
`specs/reference/action-catalog.md`.

## Scope

**In**: real layer-switch semantics in `engine` (previously only
"layer 0 is always active" from M04) driven by
`model.ActionLayerMomentary`/`LayerLatch`/`LayerCycle`, `AppGroup`
resolution (already validated in `model.Config.Validate` — this
milestone makes it functional at dispatch time, resolving a `TargetGroup`
to the union of its matchers' streams), `Scene` apply/save handlers
(`model.ActionSceneApply`/`SceneSave`), `AudioSoloToggleAction`/
`AudioDuckHoldAction` handlers.

**Out**: any new UI screens beyond what's needed to create/edit groups
and scenes (basic CRUD, not a polished experience) — deeper UI/UX work
can follow later without blocking the underlying functionality.

## Design

Layer resolution: `engine.layerState` (`daemon/internal/engine/layers.go`)
tracks a base "latched" layer plus a stack of currently-held momentary
layers; `active()` is the top of that stack, or the latched layer if
nothing's held. `bindingIndex.lookup` (from M04) already had the
layer-0-fallback shape this needed — layers overlay the base rather than
replacing it wholesale, per `Binding.Layer`'s doc comment in
`daemon/internal/model/binding.go`.

Two behaviors that weren't obvious from the acceptance criteria alone,
worked out during implementation:

- **Momentary switches on the control's raw button-down, not
  `GestureHold` 600ms later.** `Run`'s `EventButtonDown` handling
  checks the (yet-to-switch) active layer's binding for the control's
  `GestureHold`; if it's a `LayerMomentaryAction`, it pushes the switch
  immediately, so "hold a side button, turn a knob on the new layer"
  has no added latency. `EventButtonUp` pops it. `layer.latch` toggles
  the layer it's already on back to 0 (pressing an already-latched
  layer's control again) rather than staying latched forever, since a
  2-side-button unit has no dedicated "back to 0" control.
- **A press/hold/release/double_press gesture resolves against the
  layer active when its control physically went down**, not whatever's
  active by the time the gesture actually fires (`downLayer` in
  `engine.go`). Without this, releasing a side button *before* a
  control it's covering (e.g. a duck bound on a different layer) would
  send that control's release to the wrong layer and never restore it.
  Turn/move gestures have no "down" of their own and always use
  whatever's active right now.
- `layer.momentary`/`layer.latch`/`layer.cycle` execute inline in
  `engine.dispatchGesture` rather than through `actions.Registry`, since
  they mutate `layerState`, which only the run goroutine owns.
  `engine.InlineActionTypes()` exposes them so `cmd/knobd`'s
  capabilities adapter can still report them as implemented.
- `Binding.Validate` requires `GestureHold` for `layer.momentary` and
  `audio.duck_hold` — both are meaningless on any other gesture, and a
  binding on the wrong one previously just silently never fired.

`TargetGroup` resolution (`resolver.resolveGroup`): unions
`resolveApp` across every `AppMatcher` referenced by the group's
`MatcherIDs` (already validated to exist by `model.Config.Validate`),
de-duplicating by `audio.Ref`.

Scenes (`actions.SceneHandlers`): `SceneApplyAction` restores each
`SceneEntry`'s saved `VolumePercent` then `Muted` to its `Target`,
unconditionally (a restore always wins, not a diff against current
state); an entry whose target doesn't currently resolve to anything is
skipped, not an error. `SceneSaveAction` overwrites a scene's entries
with the current live value for each target the scene already
references — saving doesn't invent new entries (a scene's target list
is set when the scene is created/edited); an entry that doesn't
currently resolve is left untouched rather than zeroed out. Target
resolution for a scene's several entries needs engine's own state (the
config's `AppMatchers`/`AppGroups`, the live stream graph), so engine
resolves at dispatch time (`dispatchScene`) and hands the handler both
the scene and its per-entry `[]audio.Ref` via new
`actions.Invocation.Scene`/`SceneRefs` fields — the handler must use
these, never re-resolve.

Solo/duck (`actions.MixHandlers`): solo mutes every currently-known
stream except the target's, remembering prior mute state per stream
(both the muted "everything else" and the unmuted target) so a second
press restores it rather than just unmuting everything; pressing a
*different* target restores the old solo first, then solos the new
one. Duck drops every stream except the target's to `DuckPercent`
while held (a stream already quieter is left alone in both
directions), restoring exact prior levels on `GestureRelease` rather
than a second press — sessions are keyed by `Control`, so independent
duck bindings never interfere. Neither ever skips dispatch just
because its target resolved to nothing: toggling solo off, or
releasing a duck, must still restore state even if the target app has
since exited. This needed a new dispatch path, `dispatchWithOthers`,
alongside `dispatchScene`, since resolving `Invocation.Others` (every
currently-known playback stream not in the action's own `Target`) is
also dispatch-time work only engine can do. A duck's `GestureRelease`
fires even with no explicit `Release` binding — `holdPaired` pairs it
with the same control's `GestureHold` binding automatically, the
natural way to configure a hold-style action.

## Data model changes

None — `model.AppGroup`, `model.Scene`/`SceneEntry`, and the four
action types this milestone implements handlers for already existed
from M01. `model.Default()` now also seeds side button 1 (hold =
`layer.momentary{1}`, press = `layer.latch{1}`) and side button 2 (same
for layer 2) on a first-run config, so a freshly-installed system has
layers to try immediately; existing configs are untouched (no
migration needed).

## Acceptance criteria

- [x] Holding a side button switches to layer 1 for as long as it's
      held, reverting to layer 0 on release; latching a layer keeps it
      active until explicitly switched. (`engine.layerState`,
      `TestEngineRunMomentaryLayerSwitchesOnDownAndRevertsOnRelease`,
      `TestEngineRunLatchTogglesLayer`)
- [x] A `TargetGroup` binding controls every stream belonging to every
      matcher in that group simultaneously. (`resolver.resolveGroup`,
      `TestEngineRunGroupBindingControlsEveryMatcherSimultaneously`)
- [x] Recalling a scene restores every entry's saved level/mute state in
      one action; saving a scene captures the current live state of its
      existing entries. (`actions.SceneHandlers`,
      `TestEngineRunSceneApplyAndSaveEndToEnd`)
- [x] Solo mutes everything else and a second press restores prior mute
      states exactly (not just "unmute everything"). (`actions.MixHandlers`,
      `TestSoloMutesOthersAndRestoresMixedPriorStatesExactly`)
- [x] Duck-while-held reduces everything else's volume for the hold
      duration and restores exact prior levels on release.
      (`TestDuckLowersOthersToPercentAndRestoresExactLevelsOnRelease`)

## Verification

Automated: `daemon/internal/engine`, `daemon/internal/actions`, and
`daemon/internal/model` each have table-driven/fake-backend tests for
every criterion above, run end to end through `Engine.Run` with
`midi.FakePort`/`audio.FakeBackend` where it matters (layer timing,
LED state, the full press/turn/release pipeline).

Manual (still open): with at least three simultaneous audio streams
playing and the physical X-Touch Mini attached, exercise layer
switching, a group binding, scene save/apply, solo, and duck, in each
case confirming the *other* streams' levels are correctly restored
afterward, not just muted-and-forgotten — and confirm the side-button
and layer-bound button LEDs track the active layer on real hardware
(`ledDesired`'s layer-light pass has no hardware-verification pass
yet, only `midi.FakePort` assertions).

## Risks & open questions

- Whether scene-save should be able to add new entries (vs. only
  updating existing ones) is a real product decision, not just an
  implementation detail — the Design section above picks "no, entries
  are fixed at scene-creation time" as a starting assumption, now
  implemented; revisit if it feels wrong once used for real.
- **Solo/duck button LEDs are not implemented.** `actions.MixHandlers`'
  session state isn't visible to `engine`, so no button lights up while
  a solo or duck is active — the underlying audio behavior is correct
  and tested, but there's no visual confirmation on the device. A
  follow-up could add an `OnSoloChanged`/`OnDuckChanged` seam mirroring
  `actions.VolumeOptions.OnApplied`.
- **Solo vs. scene apply interaction is unresolved.** A scene applied
  while a solo is active gets its mute states overwritten again once
  the solo is toggled off (solo's restore-exactly rule doesn't know
  about the scene apply that happened in between). Not fixed; only
  documented here.
- **Duck still starts at `GestureHold`'s 600ms threshold**
  (`engine.HoldThreshold`), unlike momentary layers' raw-button-down
  latency fix. `model.AudioDuckHoldAction`'s doc comment documents
  Hold…Release semantics, so this matches the type as specified; move
  it to raw-down like momentary if 600ms feels laggy in practice.
- ~~Interaction between solo/duck and the dynamic app pool~~ — moot; M04
  dropped the dynamic app pool entirely (mappings are explicit-only; see
  `specs/milestones/M04-mapping-engine-daemon.md`'s Status). Solo/duck
  only ever need to consider explicitly-bound and `all_streams` targets.
