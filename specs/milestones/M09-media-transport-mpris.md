# M09: Media transport (MPRIS)

## Status

Done (2026-09-24). Hand verification against real MPRIS players and the
physical X-Touch Mini is still open -- see Verification.

## Depends on

M04 (engine/dispatch to build on).

## Goal

Buttons/encoders control play/pause, next/previous, seek, shuffle, and
repeat for whatever media player is running — Spotify, a browser tab,
`mpv`, anything speaking MPRIS — with zero authentication and no network
dependency, and an encoder can seek through the current track.

## Scope

**In**: MPRIS player discovery over D-Bus (`org.mpris.MediaPlayer2.*`
bus names — confirmed present on the dev machine for
`plasma-browser-integration` and a running Brave instance), a selected
"current player" (with `media.target_cycle` to switch which one
transport commands hit if more than one is running), handlers for
`model.ActionMediaTransport` (`MediaPlayPause`/`Next`/`Previous`/
`ShuffleToggle`/`RepeatCycle`) and `model.ActionMediaSeek`, an optional
now-playing desktop notification on track change.

**Out**: anything Spotify-specific that MPRIS can't do (liking a track,
playlist management) — that's M10, deliberately kept separate since it
needs OAuth and MPRIS doesn't.

## Design

Uses `github.com/godbus/dbus/v5` (already a dependency via M06's
focus-tracking D-Bus service, though `daemon/internal/media` dials its
own session bus connection rather than sharing focus's -- focus's
connection is tied to owning a well-known bus name and its own
supervise/reconnect lifecycle, and media is a pure client with a
simpler one) to enumerate `org.mpris.MediaPlayer2.*` names via
`ListNames`/`NameOwnerChanged`, and call the standard MPRIS
`org.mpris.MediaPlayer2.Player` interface's `PlayPause`/`Next`/
`Previous`/`Seek` methods and `Shuffle`/`LoopStatus` properties (via
`org.freedesktop.DBus.Properties.Get`/`Set`).

`PlayerRef` (on `MediaTransportAction`/`MediaSeekAction`/
`MediaNowPlayingAction`) selects a specific player by MPRIS bus name
suffix (e.g. `"spotify"` for `org.mpris.MediaPlayer2.spotify`); empty
means "whatever the currently-selected player is," itself settable via
`media.target_cycle`.

### Design refinements from implementation

- **Duplicate players.** The dev machine's Brave instance appears on the
  bus twice: `org.mpris.MediaPlayer2.brave.instance1918` (Brave's own,
  limited MPRIS implementation -- `CanSeek` false, no `LoopStatus`/
  `Shuffle`) and `org.mpris.MediaPlayer2.plasma-browser-integration`
  (KDE's richer proxy for the same tab -- `CanSeek` true, `LoopStatus`
  present). `Config.Media.IgnorePlayers` (a new field, schema v2 --
  bus-name suffixes to exclude from discovery/selection/target_cycle)
  is how a user excludes the worse half of a pair like this; there's no
  way to detect "these two bus names are the same underlying player"
  automatically; a user has to say so.
- **`media.target_cycle`/`media.now_playing` got their own `ActionType`
  constants** (`MediaTargetCycleAction`/`MediaNowPlayingAction`) rather
  than folding into `MediaCommand` -- `target_cycle` takes no `PlayerRef`
  (it always acts on "the current selection," there's nothing to
  target) and `now_playing` needed its own `PlayerRef` but no
  `Command`, so neither fit `MediaTransportAction`'s shape cleanly.
- **`media.now_playing` is button-triggered only**, not an automatic
  notification on every track change, matching the plan's decision to
  keep it simple -- a config toggle for "notify automatically" was
  considered and deferred (see Risks).
- **Player selection is "most recent wins"**, matching `playerctld`:
  the selected player is whichever last transitioned to `Playing` or
  was `media.target_cycle`'d to. Merely appearing on the bus does *not*
  steal the selection -- a newly-launched-but-paused player must not
  interrupt whatever's actually playing. Never-activated players (none
  playing, never cycled) tie-break by bus name. If the selected player
  vanishes, the most recently active remaining one takes over
  automatically.
- **Every MPRIS write uses `dbus.FlagNoReplyExpected`.** `PlayPause`/
  `Next`/`Previous`/`Seek`/property `Set` calls all run on the engine's
  single dispatcher goroutine (the same one that serializes every
  `audio.Backend` write, per M04's Architecture section) -- a hung or
  slow-to-answer player must never be able to block it. Capability
  checks (`CanControl`/`CanSeek`/`Shuffle != nil`/`LoopStatus != ""`)
  are read from `media.Tracker`'s cache, never a live property `Get`,
  for the same reason.
- **`api.State.Media`** (available/selected/players) exposes the
  tracker's state to the UI: a "Controlling: X" status-bar summary, a
  player-picker dropdown replacing the free-text `PlayerRef` field, and
  a new Media tab for editing `IgnorePlayers`.

## Data model changes

- `model.ActionMediaTargetCycle`/`ActionMediaNowPlaying` (new
  `ActionType` constants) and their `MediaTargetCycleAction{}`/
  `MediaNowPlayingAction{PlayerRef string}` params types.
- `model.Config.Media MediaSettings{IgnorePlayers []string}`.
  `CurrentSchemaVersion` bumped 1 -> 2; `daemon/internal/config`'s
  `migrateV1toV2` adds an empty `Media` to an old document.
- `api.State.Media MediaState{Available, Selected, Players
  []MediaPlayer}` (not part of the config schema -- see `api.State`'s
  own doc comment on why `GET /state`'s shape is separate from
  `model.Config`'s).

## Acceptance criteria

- [x] A button bound to `MediaPlayPause` toggles playback in whatever
      MPRIS player is currently selected.
- [x] With two players running simultaneously (e.g. Spotify + a browser
      tab), `media.target_cycle` switches which one subsequent commands
      affect.
- [x] An encoder bound to `MediaSeekAction` seeks the current track
      smoothly (not in overly coarse jumps) in both directions --
      `engine/dispatch.go`'s coalescing sums `Delta` across a fast
      spin's coalesced firings into one `Seek` call, the same rule
      `volume.adjust` already used.
- [x] Player disappearing (app closed) is handled gracefully — no crash,
      falls back to another running player or a clear "no player" state
      (`media.Tracker`'s vanish handling, covered by
      `TestTrackerVanishFallsBackToNextMostRecentlyActive`).

## Verification

Automated: `daemon/internal/media`, `daemon/internal/actions`'s
`MediaHandlers` tests, and `daemon/internal/engine`'s coalescing tests
all pass against fakes (`media.FakeBackend`/`FakeNotifier`) with no live
D-Bus session required, following the same pattern as
`focus.FakeProvider`/`audio.FakeBackend`.

Manual (still open): run Spotify (or any MPRIS-compatible player) and a
second player simultaneously, bind buttons/an encoder per Design, and
exercise every transport command against each, confirming
target-cycling actually switches which player responds, that seeking is
smooth on a player with `CanSeek`, that it errors clearly (logged, no
crash) on one without (e.g. Brave's native MPRIS), and that closing the
selected player falls back without a crash. Also confirm against real
hardware (the X-Touch Mini) rather than just `curl`'d bindings.

## Risks & open questions

- Player selection UX -- resolved: "most recent wins" (see Design
  refinements above), the same rule `playerctld` uses.
- Spotify's own MPRIS implementation has historically ignored `Shuffle`/
  `LoopStatus` writes from some clients; this needs confirming by hand
  against a real Spotify session and noting here once verified.
- Seeking forward past the end of the track is left to the player's own
  MPRIS-spec behavior (commonly: skip to the next track) rather than
  clamped -- clamping would need a blocking `Position` read, which the
  no-blocking-D-Bus-call design deliberately avoids.
- An automatic (not button-triggered) now-playing notification on every
  track change was considered and deferred: it would need a config
  toggle (another schema field) and a decision about which selected-
  player's track changes should fire it, neither clearly worth the
  scope for this milestone.
