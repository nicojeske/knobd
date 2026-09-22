# M06: Focus tracking

## Status

Done. Verified live against the real X-Touch Mini, real PipeWire, and a
real KWin session: focus switching (Konsole/KCalc/Brave/Dolphin/a Steam
game all reported correctly), the Brave PID-mismatch case, a KWin
restart auto-reinstalling the script, and a physical long-press
rebinding an encoder and persisting across a daemon restart.

## Depends on

M04 (a running engine for `TargetFocused`/`knob.assign_focused_app` to
plug into).

## Goal

`model.TargetFocused` resolves to whatever application currently owns
the focused window, live, and pressing-and-holding a knob
(`model.ActionKnobAssignFocusedApp`) rebinds that knob to it — the
feature specifically requested for this project.

## Scope

**In**: `focus.New`'s real KWin-backed implementation, the KWin script
(embedded at `daemon/internal/focus/script/knobd-focus.js` — see below;
not installed by packaging), the daemon's own D-Bus callback service,
resolving a `focus.AppInfo` to a `model.AppMatcher`/live `audio.Stream`s
(`audio.ResolveFocused`, a sibling of `audio.Resolve`), wiring
`ActionKnobAssignFocusedApp`'s handler into `actions.Registry`.

**Out**: any UI for reviewing/editing the resulting binding — that's
M07. Multi-monitor/multi-desktop nuances beyond "whatever KWin reports
as activated" are not a goal unless testing surfaces a real problem.

## Design

**First and most important task, done first**: resolve the open
question in ADR 0003 — can a KWin script actually `callDBus` *out* to an
arbitrary service, or only be called *into*? **Answer: yes** — confirmed
live with a throwaway script and a standalone Go D-Bus listener before
any production code was written. See ADR 0003's "Resolved" section for
the evidence; the FIFO/file-tail fallback it originally described was
never needed and no longer exists anywhere in the codebase.

`packaging/kwin/knobd-focus.js` as originally scoped doesn't exist:
the script is `go:embed`ded into the daemon binary from
`daemon/internal/focus/script/knobd-focus.js` and materialized to
`$XDG_RUNTIME_DIR` at startup instead, closing the packaging gap this
milestone would otherwise have inherited and eliminating any
script/daemon version-skew risk (see ADR 0003's Consequences section).
`focus.New` (`daemon/internal/focus/kwin.go`) loads it via
`org.kde.kwin.Scripting`, running the daemon's own D-Bus service
(`kwinProvider.FocusChanged`) to receive callbacks, exposing
`Watch`/`Current`, reinstalling the script if KWin itself restarts, and
warning (the only available liveness signal — `callDBus`/`loadScript`
failures are silent) if no handshake arrives within a few seconds.

Resolving a `focus.AppInfo` to actual audio streams
(`audio.ResolveFocused`, `daemon/internal/audio/focus.go`) turned out to
need a three-rung ladder, not a single property match: Brave's window
`resourceClass` (`brave-browser`) and its audio stream (no
`application.id` published at all) share no property the original
"match `ResourceClass` against `DesktopIDs`" plan would have caught.
Rung 1 matches identity tokens derived from `ResourceClass`/
`DesktopFileID` (`focus.AppInfo.NameCandidates`); rung 2 folds in a
`/proc/<pid>/exe`-derived binary name (what actually resolves Brave);
rung 3, `daemon/internal/proctree`'s ancestry walk, runs only as a
fallback when 1+2 find nothing — never unioned with them, since under
Flatpak/bubblewrap it can match the *wrong* application (see
`audio/focus.go`'s doc comment).

`ActionKnobAssignFocusedApp`'s handler (`daemon/internal/actions/assign.go`):
on `GestureHold` of an encoder-push, reads the focused app via
`focus.Provider.Current`, creates or reuses an `AppMatcher` for it
(reusing one whose criteria already overlap, never modifying it), and
rewrites the *paired encoder's* `GestureTurn` binding (not the push
itself, which stays available to reassign) to a fresh `VolumeAdjustAction`
— persisted synchronously via M04's config-save path for read-modify-write
atomicity across rapid assignments.

## Data model changes

None. Verified: `make schema` produces an identical
`docs/config.schema.json` after every change in this milestone,
including `model.Default()`'s new starter bindings (only default
*values* changed, not the shape).

## Acceptance criteria

- [x] Confirmed whether KWin's `callDBus` supports calling out to an
      arbitrary service in this Plasma version; ADR 0003 updated with
      the answer either way. Confirmed yes, live: a throwaway KWin
      script + a standalone Go D-Bus listener exchanged real focus
      events (Konsole/KCalc) end to end. See ADR 0003's "Resolved"
      section.
- [x] `focus.Provider.Watch` delivers an event when switching focus
      between two different applications' windows. Verified live via
      `knobd monitor-focus` and `GET /state`: Konsole, a Steam game,
      Brave, and Dolphin all reported correctly on every switch.
- [x] Long-pressing an encoder-push bound to nothing (or to something
      else) rebinds it to the focused application and that binding
      persists across a daemon restart. Verified live (physical
      long-press on encoder-push 3 while Konsole was focused): the
      daemon logged `"assigned focused app to a knob"`, encoder 3's turn
      binding and the new `AppMatcher` landed in `config.json`, and both
      were still there after restarting `knobd` against the same config
      file.
- [x] The browser-audio-PID-mismatch case (focused window's PID differs
      from its audio stream's PID) is handled via the process-tree
      fallback and verified against Brave (installed on the dev
      machine) specifically. Verified live: focusing Brave reported
      `resourceClass="brave-browser"` with `binary="brave"` (rung 2's
      `/proc/<pid>/exe` annotation), matching the audio stream's
      `application.process.binary` even though the stream publishes no
      `application.id` at all. Rung 3 (the process-tree walk) is
      exercised by `audio/focus_test.go` against a fixture pinning that
      exact shape; the live session didn't independently need it since
      rung 2 already resolves Brave.

## Verification

Physical/manual, all done: focused Konsole, a Steam game, Brave, and
Dolphin in turn and confirmed `GET /state`'s `FocusState.resourceClass`
and `knobd monitor-focus` reported each correctly, including Brave's
`/proc`-derived binary annotation; restarted KWin (`kwin_wayland
--replace`) and confirmed the daemon's supervise loop reinstalled the
script and resumed reporting within ~500ms with no manual intervention;
crashed and restarted the daemon process itself and confirmed the
unload-then-reload path (stale script from a dead bus name, cleanly
replaced); physically long-pressed encoder-push 3 while Konsole was
focused and confirmed it now controls Konsole's volume, with the
binding and the new `AppMatcher` surviving a `knobd` restart against the
same config file.

## Risks & open questions

- The `callDBus` direction question (see Design) was the load-bearing
  risk for this entire milestone — resolved first, before any
  production code, per the Design section above.
- KWin scripting API stability across Plasma versions is unverified
  beyond 6.7.5 (the dev machine's version). The script's property reads
  degrade to empty/zero values rather than throwing if a future Plasma
  renames something, but a rename would then produce a script that
  loads and runs while reporting nothing useful — watch for this if
  upgrading Plasma major versions (see ADR 0003's Consequences section).
- The default `ignoredResourceClasses` list (`plasmashell`, `krunner`,
  `kwin_wayland`) in `daemon/internal/focus/payload.go` is a small,
  empirically-grown allow-list, not exhaustively validated against
  every shell surface KDE can activate.
- Flatpak/bubblewrap pid-namespace sandboxing can make rung 3's
  process-tree fallback match the wrong application (the pid it sees is
  namespace-local, not the host pid) — documented alongside M03's
  analogous `/proc`-lookup sharp edge; not solvable from outside the
  sandbox.
