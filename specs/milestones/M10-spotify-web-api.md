# M10: Spotify Web API

## Status

Not started.

## Depends on

M09 (this extends the same "media" surface with Spotify-specific
capability MPRIS doesn't have; sharing the player-selection concept from
M09 rather than duplicating it).

## Goal

A button likes/unlikes the currently-playing Spotify track, another adds
it to a specific playlist, and a few other Spotify-only actions
(playlist start, queue, transfer playback) become available —
everything from `specs/reference/action-catalog.md`'s Spotify section
that genuinely needs the Web API rather than MPRIS.

## Scope

**In**: OAuth 2.0 **PKCE** flow (no client secret needed/stored,
appropriate for a desktop app) with a **loopback redirect**
(`http://127.0.0.1:<port>/callback`, a locally-bound one-shot HTTP
listener during the auth flow only), refresh-token storage via the
Secret Service D-Bus API (**KWallet**, confirmed as the standard secret
store on this KDE system) rather than a plaintext file, handlers for
`spotify.like_toggle`/`add_to_playlist`/`remove_from_playlist`/
`start_playlist`/`queue_track`/`transfer_playback`.

**Out**: any Spotify feature not in the action catalog (this is not a
general Spotify API client). Re-implementing anything MPRIS already
covers (play/pause/next/previous/seek) — that stays on the M09 path even
for a Spotify-selected player, so Spotify-as-a-player and
Spotify-as-Web-API-target don't diverge into two separate mental models
for the same app.

## Design

Implement the model.Action types for this milestone first — none exist
yet (see `specs/reference/action-catalog.md`'s Spotify table, all
"Planned"). Follow `model.Action`'s established registration pattern
(constant in `action.go`, struct, `actionRegistry` entry,
`TestActionRegistryComplete` will catch a missed entry) rather than
inventing a different shape for this one integration.

OAuth: register a Spotify Developer application (manual, one-time setup
— document the redirect URI and required scopes once decided, in this
file, since it affects what the user needs to configure in Spotify's
developer dashboard), implement the PKCE code-verifier/challenge dance,
and store the resulting refresh token via `github.com/godbus/dbus/v5`
against the `org.freedesktop.secrets` (Secret Service) API — KWallet
implements this interface, so no KWallet-specific code is needed beyond
speaking the standard interface.

Every action handler calls the Spotify Web API's REST endpoints
directly (there's no need for a full SDK for six endpoints); refresh the
access token transparently on a 401 using the stored refresh token.

## Data model changes

New `model.ActionType` constants and structs for each action in Scope
(e.g. `ActionSpotifyLikeToggle`, `ActionSpotifyAddToPlaylist{PlaylistID
string}`, etc.) — add them following the exact pattern of the existing
media actions in `daemon/internal/model/action.go`.

## Acceptance criteria

- [ ] First-run OAuth flow completes via loopback redirect with no
      manual token copy-pasting required.
- [ ] Refresh token is stored via the Secret Service D-Bus API, not a
      plaintext file, and survives a daemon restart without
      re-prompting for auth.
- [ ] A button toggles like/unlike on the currently-playing Spotify
      track, confirmed to actually change the track's saved status in
      Spotify.
- [ ] A button adds the current track to a specific, pre-configured
      playlist.
- [ ] Token refresh on expiry is transparent — no user-visible failure
      when an access token expires mid-session.

## Verification

Manual, requires a Spotify account and a registered developer
application: complete the OAuth flow, then exercise each action while
Spotify is playing, confirming the effect in the actual Spotify app/web
player.

## Risks & open questions

- Requires the user (you) to register a Spotify Developer application
  before this milestone can be tested at all — a one-time manual
  prerequisite to flag clearly when this milestone starts, not discover
  partway through.
- Secret Service API availability/behavior across KWallet versions is
  unverified — confirm early rather than assuming.
