# M03: Audio control

## Status

Done, 2026-09-21. `daemon/internal/audio` now has a real `Backend`
(`pulse.go` + `subscribe.go`, against `github.com/jfreymuth/pulse`'s
`proto` subpackage — no `pactl` fallback was needed, see ADR 0002), a
real `Resolve` (`matcher.go`), a resilient `audio.Supervisor`
(`supervisor.go`, mirroring `midi.Supervisor`), and a `knobd
monitor-audio` debug command. All confirmed against this system's live
PipeWire session, not just the fixture — see Verification.

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

**Done — see ADR 0002's M03 update.** `pulse/proto` v0.1.3 covers every
operation needed; no `pactl` fallback exists or is needed. `audio.Backend`
picked up three changes beyond the original scaffold, made while it still
had zero non-test consumers (`engine.Deps` names the type but
`Engine.Run` remains an M04 stub): `GetVolume`/`SetVolume`/`SetMute` take
a typed `audio.Ref{Kind, ID}` instead of a bare `id string` (a bare string
would force the backend to guess whether an ID names a sink, a source, or
a stream index — the caller's resolved `model.Target` already knows);
`VolumeState` gained `Channels []float64` (per-channel percent, so
balance is preserved rather than flattened — see below); and `Event`
gained `State *VolumeState` plus `EventDeviceRemoved`/
`EventDefaultChanged`/`EventResync`, so a subscriber never needs a
round-trip per change event. `audio.Supervisor` (`supervisor.go`) wraps
the real backend with the same discover/reconnect-with-backoff shape
`midi.Supervisor` uses, since a PipeWire restart is exactly as routine as
a MIDI device replug and shouldn't require restarting knobd — confirmed
by hand with `systemctl --user restart pipewire` while `knobd
monitor-audio` was running.

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

Implemented as `AnnotateProcessBinaries` (`pulse.go`), applied at
enumeration time (`Streams()`) rather than inside `Resolve`, so `Resolve`
stays a pure function testable against the fixture with no filesystem
access. It writes the fallback to a synthesized `knobd.process.binary`
property rather than overwriting `application.process.binary` — the real
key is reserved for what PipeWire actually published, so the config UI
(M07) can always tell a resolved binary from a fabricated one.
`Resolve`/`matcher.go` checks the real key first, the synthesized one
second. **Known limitation, not solvable from here**: for a Flatpak/
bubblewrap-sandboxed app, `application.process.id` is a pid in the app's
own pid namespace, not ours — the fallback can silently resolve to an
unrelated process or nothing. Any `/proc` read error (including that one)
is treated as "no answer" and never written to `Props`.

Matching decision, also settled during implementation: `Binaries`,
`AppNames`, `NodeNames`, and `DesktopIDs` all match **case-insensitively**
(only `AppNames` was originally specified as such). The fixture alone
has three different casing conventions across otherwise-identical
properties (`application.name: "Brave"` vs.
`application.process.binary: "brave"`; `"vesktop"`; `"Pal"`), and a
matcher is hand-written against whatever a user happens to see in
`pactl`/`pw-dump` — an exact-match rule would silently fail for anyone
who typed the "wrong" casing. `MediaNameRx` stays case-sensitive (prefix
a pattern with `(?i)` for the same effect) and unanchored — write
`^...$` to avoid a substring match. This is a one-way door (widening to
case-insensitive later would be a no-op for existing configs; narrowing
it back would break some), accepted deliberately.

Pick a response curve for `VolumeAdjustAction.StepPercent` (the plan
suggested cubic, "matching how KDE's own slider feels") and document the
chosen mapping in this file once decided, since it affects what
"StepPercent: 2" actually feels like turning the knob.

**Decided: linear (`Curve{Exponent: 1}`), not cubic.** The plan's guess
was based on a misreading of what "matching KDE's slider" means: KDE's
and pavucontrol's sliders are linear *in PulseAudio's percent scale*
(`volume / PA_VOLUME_NORM`) — the perceptual cubic curve is already
baked into PipeWire's percent-to-gain conversion
(`proto.Volume.Linear()`, confusingly named — see `pulse.go`), not
something the UI adds on top. So `StepPercent: 2` at the default
`Exponent: 1` means exactly what it says: turning one detent moves the
number pavucontrol shows by 2 percentage points, same as dragging its
slider. `VolumeAdjustAction.CurveExponent` remains available per-binding
for anyone who wants an old-fashioned fader feel (fine at the bottom,
coarse at the top) — `daemon/internal/audio/curve.go`'s doc comment has
the details, including the one non-obvious consequence: with
`Exponent != 1`, `StepPercent` stops meaning "percentage points of
volume" and starts meaning "percent of knob travel" — the two coincide
only at the default. Pinned by `curve_test.go`.

## Data model changes

None expected beyond what M01 already has (`model.AppMatcher`,
`model.Target`). If curve tuning needs a persisted parameter beyond
`VolumeAdjustAction.CurveExponent` (already scaffolded), add it here and
update `docs/config.schema.json` generation accordingly.

## Acceptance criteria

- [x] Confirmed (and documented, updating ADR 0002 if the answer is
      "the fallback is needed") whether `pulse/proto` covers everything
      needed, before the rest of this milestone is built on top of it.
      Confirmed sufficient; see ADR 0002's M03 update.
- [x] `audio.Resolve` passes a unit test suite built directly from
      `testdata/pipewire/pw-dump-sample.json`, explicitly asserting on
      both edge cases named above. `matcher_test.go`.
- [x] `SetVolume`/`SetMute` against a live PipeWire session are
      confirmed by hand: change a specific application's volume via
      knobd and see it reflected in `pavucontrol`/KDE's own volume
      mixer, and vice versa. Confirmed both directions with two live
      `paplay` streams: `SetVolume` via a direct `audio.New`/`SetVolume`
      call showed up in `pactl list sink-inputs` at the exact percent
      set, and an external `pactl set-sink-input-volume`/
      `set-sink-input-mute` showed up in `knobd monitor-audio`'s output.
- [x] `Subscribe` delivers an event within a reasonable time (no
      polling) when a stream's volume changes externally or a new
      stream appears/disappears. Confirmed live — an external `pactl`
      change appeared in `monitor-audio`'s output in well under a
      second.
- [x] The chosen volume curve is documented in this file and covered by
      a unit test pinning specific input→output values. `curve_test.go`.

## Verification

```bash
cd daemon && go test -race ./internal/audio/...
cd .. && make schema && git diff --exit-code -- docs/config.schema.json  # unchanged
make build
./daemon/knobd monitor-audio -once   # enumerate current sinks/sources/streams and exit
./daemon/knobd monitor-audio         # watch for live change events
# play audio in two apps (e.g. `paplay` two WAV files), change one's
# volume via knobd, confirm the other is unaffected; close an app
# mid-session and confirm its stream disappears from the next -once
# dump and a Subscribe event fires; systemctl --user restart
# pipewire-pulse mid-session and confirm knobd reconnects on its own
```

All of the above were run against this machine's live PipeWire 1.6.8
session during implementation, not just against the fixture: two `paplay`
streams were used to confirm multi-stream resolution and independent
volume control, `pactl`/knobd cross-checks confirmed `SetVolume`/`GetVolume`
agree in both directions, and `systemctl --user restart pipewire-pulse`
was actually run while `knobd monitor-audio` was watching — it printed
`-- disconnected: audio: backend connection lost`, reconnected within
~30ms, and delivered `EventResync`, after which enumeration and further
Subscribe events worked normally.

## Risks & open questions

- `pulse/proto` API coverage turned out not to be a risk in practice —
  confirmed sufficient, see ADR 0002.
- PipeWire-native properties came through the Pulse compatibility layer
  intact for everything `AppMatcher` needs, confirmed against both the
  fixture and a live session; no reason yet to revisit ADR 0002 on that
  front.
- **Deferred, not solved, by this milestone** (recorded here rather than
  silently dropped):
  - `VolumeBalanceAction` still has no `Backend` method or registry
    handler. `VolumeState.Channels` was added specifically so this is
    possible without another interface change, but wiring it up (and
    deciding what a "balance step" means for a mono stream) is for
    whichever milestone actually implements the action — not yet
    scheduled.
  - Whether `SetVolume` should un-mute an already-muted target (KDE's
    behavior) or leave mute alone (pavucontrol's behavior) is
    deliberately left to the caller: `Backend.SetVolume` only ever
    changes volume. M04's engine has to pick one when it builds the
    `ActionVolumeAdjust`/`ActionVolumeSet` handlers.
  - The `/proc/<pid>/exe` fallback cannot see through a Flatpak/
    bubblewrap pid namespace — `application.process.id` there belongs to
    the sandbox's own namespace, not knobd's. No fix is possible from
    this side; documented in `pulse.go` and here so nobody re-discovers
    it as a bug.
  - `Supervisor.Subscribe`/`Supervisor.SetVolume`/etc. share one
    connect-and-subscribe goroutine, so a `Subscribe` call issued during
    the brief window of an active (re)connect attempt is served slightly
    late rather than instantly. Not on any latency-sensitive path today;
    revisit only if that changes.
