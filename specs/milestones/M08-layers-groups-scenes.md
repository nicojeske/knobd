# M08: Layers, groups, scenes

## Status

Not started.

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

**In**: real layer-switch semantics in `engine` (currently only
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

Layer resolution: extend `engine`'s binding lookup (from M04) to track
an "active layer" that momentary/latch/cycle actions mutate, checked
before falling back to layer 0 for any `(Control, Gesture)` not bound on
the active layer — i.e. layers overlay the base rather than replacing it
wholesale, per `Binding.Layer`'s doc comment in
`daemon/internal/model/binding.go`.

`TargetGroup` resolution: union `audio.Resolve` across every
`AppMatcher` referenced by the group's `MatcherIDs` (already validated
to exist by `model.Config.Validate`), de-duplicating by stream ID in
case two matchers in the same group somehow match the same stream.

Scenes: `SceneApplyAction` restores each `SceneEntry`'s saved
`VolumePercent`/`Muted` to its `Target`; `SceneSaveAction` overwrites a
scene's entries with the current live value for each target the scene
already references (i.e. saving doesn't invent new entries — a scene's
target list is set when the scene is created/edited, not implicitly
grown by saving over it; if that's the wrong call once this is actually
used, revise it here and update `model.Scene`'s doc comment to match).

Solo: mute every currently-known stream except the target's, remembering
prior mute state per stream so a second press restores it rather than
just unmuting everything. Duck: same idea but a temporary volume
reduction (`DuckPercent`) rather than a full mute, restored on
`GestureRelease` rather than a second press.

## Data model changes

None expected beyond what M01 scaffolded — `model.AppGroup`,
`model.Scene`/`SceneEntry`, and the four action types this milestone
implements handlers for already exist.

## Acceptance criteria

- [ ] Holding a side button switches to layer 1 for as long as it's
      held, reverting to layer 0 on release; latching a layer keeps it
      active until explicitly switched.
- [ ] A `TargetGroup` binding controls every stream belonging to every
      matcher in that group simultaneously.
- [ ] Recalling a scene restores every entry's saved level/mute state in
      one action; saving a scene captures the current live state of its
      existing entries.
- [ ] Solo mutes everything else and a second press restores prior mute
      states exactly (not just "unmute everything").
- [ ] Duck-while-held reduces everything else's volume for the hold
      duration and restores exact prior levels on release.

## Verification

Manual, with at least three simultaneous audio streams playing: exercise
layer switching, a group binding, scene save/apply, solo, and duck, in
each case confirming the *other* streams' levels are correctly restored
afterward, not just muted-and-forgotten.

## Risks & open questions

- Whether scene-save should be able to add new entries (vs. only
  updating existing ones) is a real product decision, not just an
  implementation detail — the Design section above picks "no, entries
  are fixed at scene-creation time" as a starting assumption; revisit if
  it feels wrong once built.
- Interaction between solo/duck and the dynamic app pool (M04) needs a
  decision: does a duck-while-held affect newly-arriving pooled streams
  too, or only streams that existed when the hold started?
