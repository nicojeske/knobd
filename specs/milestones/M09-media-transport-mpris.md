# M09: Media transport (MPRIS)

## Status

Not started.

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

Use a Go D-Bus library (`github.com/godbus/dbus/v5`, confirmed on the
module proxy — likely already a dependency via M06's focus-tracking
D-Bus service, in which case share it) to enumerate
`org.mpris.MediaPlayer2.*` names via the standard D-Bus
`ListNames`/`NameOwnerChanged`, and call the standard MPRIS
`org.mpris.MediaPlayer2.Player` interface's `PlayPause`/`Next`/
`Previous`/`Seek` methods and `Shuffle`/`LoopStatus` properties.

`PlayerRef` (already on both `MediaTransportAction` and `MediaSeekAction`
in `model/action.go`) selects a specific player by MPRIS bus name
suffix (e.g. `"spotify"` for `org.mpris.MediaPlayer2.spotify`); empty
means "whatever the currently-selected player is," itself settable via
`media.target_cycle`.

## Data model changes

Possibly a new `ActionType` for `media.target_cycle` and
`media.now_playing` if they end up warranting their own action rather
than being folded into `MediaTransportAction`'s `MediaCommand` enum —
decide during implementation and update
`specs/reference/action-catalog.md`'s status column accordingly.

## Acceptance criteria

- [ ] A button bound to `MediaPlayPause` toggles playback in whatever
      MPRIS player is currently selected.
- [ ] With two players running simultaneously (e.g. Spotify + a browser
      tab), `media.target_cycle` switches which one subsequent commands
      affect.
- [ ] An encoder bound to `MediaSeekAction` seeks the current track
      smoothly (not in overly coarse jumps) in both directions.
- [ ] Player disappearing (app closed) is handled gracefully — no crash,
      falls back to another running player or a clear "no player" state.

## Verification

Manual: run Spotify (or any MPRIS-compatible player) and a second player
simultaneously, bind buttons/an encoder per Design, and exercise every
transport command against each, confirming target-cycling actually
switches which player responds.

## Risks & open questions

- Player selection UX (which one is "current" by default — most
  recently active? explicit user choice only?) needs a decision;
  "most recently active, per MPRIS's `PlaybackStatus`" is a reasonable
  default to start from.
