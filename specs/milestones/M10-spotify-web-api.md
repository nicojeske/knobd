# M10: Spotify Web API

## Status

Done (2026-09-24). Hand verification with a real Spotify account/OAuth
flow and the physical X-Touch Mini is still open — see Verification.

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
(`http://127.0.0.1:48721/callback`, a locally-bound one-shot HTTP
listener during the auth flow only — see Design refinements for why the
port is fixed rather than dynamically assigned), refresh-token storage
via the Secret Service D-Bus API rather than a plaintext file, handlers
for `spotify.like_toggle`/`add_to_playlist`/`remove_from_playlist`/
`start_playlist`/`queue_track`/`transfer_playback`/`volume_adjust`/
`volume_set`. The volume actions control the active Spotify Connect
device's volume via the Web API (`PUT /me/player/volume`), not the local
PipeWire mixer — the point is that it stays in sync with Spotify Connect
itself (and whatever device the user actually plays through), which a
local mixer write cannot do.

**Out**: any Spotify feature not in the action catalog (this is not a
general Spotify API client). Re-implementing anything MPRIS already
covers (play/pause/next/previous/seek) — that stays on the M09 path even
for a Spotify-selected player, so Spotify-as-a-player and
Spotify-as-Web-API-target don't diverge into two separate mental models
for the same app.

## Design

`model.Action` types: `SpotifyLikeToggleAction{}`,
`SpotifyAddToPlaylistAction{PlaylistID}`,
`SpotifyRemoveFromPlaylistAction{PlaylistID}`,
`SpotifyStartPlaylistAction{PlaylistID}`,
`SpotifyQueueTrackAction{TrackID}`,
`SpotifyTransferPlaybackAction{DeviceName, Play}`,
`SpotifyVolumeAdjustAction{StepPercent}` (fired on `GestureTurn`, reads
`Client.CurrentVolume` fresh on every call — no local cache, unlike
`VolumeAdjustAction`, since the whole point is staying in sync with a
level that can change from elsewhere), `SpotifyVolumeSetAction{Percent}`
— following
`model.Action`'s established registration pattern (constant in
`action.go`, struct, `actionRegistry` entry, `TestActionRegistryComplete`
catches a missed entry).

OAuth/token storage/REST client live in a new `daemon/internal/spotify`
package (`auth.go`, `token.go`, `client.go`, `secret.go`, `service.go`,
`id.go`), following the hardware-facing-package pattern (small
interfaces at the point of use, a `Fake*` for tests) even though nothing
here is hardware: `SecretStore` is the D-Bus Secret Service boundary,
`SpotifyAPI` (in `daemon/internal/actions`) is what
`actions.SpotifyHandlers` needs from `*spotify.Client`.

`actions.SpotifyHandlers` never calls the network from
`Registry.Execute` (that runs on the engine's single dispatch
goroutine, which every other handler set in this codebase also never
blocks — see `engine`'s Architecture section): `Execute` enqueues a job
onto a small buffered channel and returns immediately; `Run` (started by
`cmd/knobd` in its own goroutine) drains it. A full queue drops the
newest press with a logged warning rather than blocking.

## Data model changes

- `model.ActionSpotifyLikeToggle`/`AddToPlaylist`/`RemoveFromPlaylist`/
  `StartPlaylist`/`QueueTrack`/`TransferPlayback` (new `ActionType`
  constants) and their params structs, in `daemon/internal/model/action.go`.
- `model.Config.Spotify SpotifySettings{ClientID string}`.
  `CurrentSchemaVersion` bumped 2 -> 3; `daemon/internal/config`'s
  `migrateV2toV3` adds an empty `Spotify` to an older document.
- `api.State.Spotify SpotifyState{Configured, Authorized,
  LoginInProgress, LoginURL, User, LastError}` (not part of the config
  schema, same reasoning as `api.State.Media`).
- New routes: `POST`/`DELETE /spotify/login`, `GET /spotify/playlists`,
  `GET /spotify/devices` (`daemon/internal/api/spotify.go`).

## Acceptance criteria

- [x] First-run OAuth flow completes via loopback redirect with no
      manual token copy-pasting required. (Automated: `TestStartLoginFullFlow`
      drives the whole PKCE exchange against a fake token endpoint and a
      real loopback listener. Manual confirmation against the real
      Spotify accounts.spotify.com endpoints is still open.)
- [x] Refresh token is stored via the Secret Service D-Bus API, not a
      plaintext file, and survives a daemon restart without
      re-prompting for auth. (`spotify.NewSecretService`, backed by
      `org.freedesktop.secrets`; `Service.ValidateStoredToken` checks a
      stored token on startup. Manual confirmation against a real
      KWallet/gnome-keyring session is still open.)
- [x] A button toggles like/unlike on the currently-playing Spotify
      track. (`actions.SpotifyHandlers.likeToggle`, covered by
      `TestSpotifyLikeToggle*`; confirming the actual effect in Spotify
      is manual.)
- [x] A button adds the current track to a specific, pre-configured
      playlist. (`addToPlaylist`, `TestSpotifyAddToPlaylistNormalizesID`;
      manual confirmation open.)
- [x] Token refresh on expiry is transparent. (`TokenManager.AccessToken`
      refreshes ahead of expiry and on a 401 with one retry, covered by
      `TestTokenManagerRefreshesWhenExpired`/`TestClientRetriesOnceOn401`.)
- [x] A knob adjusts, and a button sets, the active Spotify Connect
      device's volume via the Web API, without each turn waiting on a
      network round trip, and the turned encoder's LED ring reflects the
      level. (`actions.SpotifyHandlers.volumeAdjust`/`volumeSet`/
      `runVolumeWriter`/`CachedVolumePercent`, `spotify.Client.
      CurrentVolume`/`SetVolume`, `engine.SpotifyVolumeObserver`/
      `SetSpotifySource`; covered by `TestSpotifyVolumeAdjust*`/
      `TestSpotifyVolumeSet`/`TestSpotifyVolumeWriterAppliesLatestPending`/
      `TestClientCurrentVolume*`/`TestClientSetVolume`/
      `TestEngineRunLEDRingTracksSpotifyVolume`. Manual confirmation that
      a real Spotify Connect device's volume actually moves, feels
      responsive under a fast knob spin, and stays in sync when changed
      from elsewhere, is open.)

## Verification

Automated: `daemon/internal/spotify` (PKCE vector, the full loopback
login flow, token refresh/rotation/`invalid_grant` handling, every REST
endpoint's request shape against `httptest`) and
`daemon/internal/actions`'s `SpotifyHandlers` tests (against a fake
`SpotifyAPI` and `media.FakeNotifier`) all pass with no live network or
D-Bus session required.

Manual (still open, requires a Spotify Premium account and a registered
developer application — see Risks):

1. In the [Spotify dashboard](https://developer.spotify.com/dashboard),
   create an app, add `http://127.0.0.1/callback` as a Redirect URI (no
   port — see Design refinements), and enable the Web API.
2. Run the daemon and UI; in the Spotify tab, enter the Client ID and
   click Connect. A browser should open with no copy-pasting required;
   after consenting, the tab should show "Connected".
3. Restart the daemon; the tab should still show "Connected" with no
   re-prompt. Confirm `secret-tool search application knobd` (or
   KWallet Manager) shows the stored item, and that no token appears in
   `~/.config/knobd/config.json`.
4. Bind buttons to `spotify.like_toggle` and `spotify.add_to_playlist`
   (pick a playlist from the picker) while Spotify is playing; confirm
   the effect in the actual Spotify app, and the resulting desktop
   notification.
5. Exercise `spotify.start_playlist`, `spotify.queue_track`, and
   `spotify.transfer_playback` (pick a device from the picker; try both
   with and without "resume playback").
6. Leave the daemon running past the access token's ~1 hour expiry, then
   press `spotify.like_toggle` again — should work with no visible
   failure (transparent refresh).
7. Confirm against real hardware (the X-Touch Mini), not just curl'd
   bindings.

## Design refinements from implementation

- **Secret Service backend is gnome-keyring on the dev machine, not
  KWallet** — `org.freedesktop.secrets` is owned by
  `gnome-keyring-daemon`, with `ksecretd` (KWallet's KDE 6 successor)
  only providing the separate `org.kde.secretservicecompat`/portal
  interfaces. Both speak the same `org.freedesktop.Secret.Service`
  API this package uses, so no code changed, but the spec's original
  "KWallet, confirmed as the standard secret store" framing was wrong
  and is corrected here — confirm the actual owner with
  `busctl --user list | grep -i secret` on whatever system this runs
  on next, rather than assuming KWallet.
- **`OpenSession("plain")`**, not the Diffie-Hellman-negotiated
  algorithm: the session bus is already private to the user's own
  processes (AF_UNIX + peer credentials), so the extra negotiation buys
  nothing here and would need a crypto dependency this project doesn't
  otherwise have (see `secret.go`'s doc comment on `NewSecretService`).
- **Spotify's February 2026 Web API changes** are load-bearing for this
  milestone, not incidental: `PUT`/`DELETE /me/library` +
  `GET /me/library/contains` (URI-based) replace the old
  `/me/tracks*` family, and `/playlists/{id}/items` replaces
  `/playlists/{id}/tracks`. `daemon/internal/spotify/client.go` only
  implements the current shapes.
- **Loopback redirect URI uses a fixed port, registered exactly**:
  `http://127.0.0.1:48721/callback` (`spotify.LoopbackPort`). Spotify's
  own documentation says a registered loopback redirect URI may omit
  the port, with the authorize request supplying whatever port was
  actually bound (this package's original design: `Auth.StartLogin`
  binding `127.0.0.1:0` and letting the kernel pick one) — but the
  Developer Dashboard's redirect URI validator rejects a portless
  loopback URI outright ("needs a port") as of September 2026,
  contradicting that documented behavior. `localhost` is separately,
  and consistently, rejected (must be a literal loopback IP). A fixed
  port means only one login flow can be in progress at a time system-
  wide; `StartLogin` synchronously tears down (not gracefully — see
  `bindLoopbackPort`'s retry-on-`EADDRINUSE` doc comment for why a
  short retry budget still exists) any flow it supersedes before
  binding, since two flows can no longer listen side by side even
  briefly.
- **Spotify actions run on their own worker goroutine**
  (`actions.SpotifyHandlers.Run`, started alongside `eng.Run`/
  `srv.ListenAndServe`/`hub.Run` in `cmd/knobd`), not inline in
  `Registry.Execute` — unlike M09's MPRIS calls (fire-and-forget D-Bus,
  `FlagNoReplyExpected`), a Spotify Web API call is a real blocking
  HTTPS round-trip, and the engine's dispatch goroutine must never
  block on one.
- **`transfer_playback` matches by device name, case-insensitively**,
  not id — Spotify Connect device ids aren't stable across client
  restarts, so a name is the only binding-time value that stays valid.
- **Access tokens are held in memory only**, never persisted; only the
  refresh token goes through the Secret Service. `TokenManager` refreshes
  ahead of a 30s-skew expiry window and once more on an unexpected 401
  from a live call.
- **`invalid_grant` on refresh means revoked access**: `TokenManager`
  deletes the stored refresh token and reports `ErrNotAuthorized`, so the
  Spotify tab shows "Connect" again instead of repeating the same error
  forever.
- **The `/me/library` endpoints (save/remove/check) take `uris` as a
  query string parameter, not an `"ids"` JSON request body** — found by
  hand-testing `spotify.like_toggle` against a real account: it silently
  failed validation and never touched the library. `client.go`'s
  original shape (a JSON body keyed `"ids"`) was carried over from the
  pre-February-2026 `/me/tracks*` family's request shape by mistake, not
  from the new endpoints' actual documented shape (confirmed against
  Spotify's own reference docs: `PUT`/`DELETE /me/library?uris=...` and
  `GET /me/library/contains?uris=...`, all query-parameter based, a
  comma-separated list of up to 40 URIs). Fixed in `SaveToLibrary`/
  `RemoveFromLibrary`/`LibraryContains`; `GET /me/library/contains`'s
  `ids`-vs-`uris` param name and bare-JSON-array response shape were
  otherwise already correct. This resolves the "Risks & open questions"
  entry about the `/playlists/{id}/items` DELETE body shape being
  unconfirmed for the sibling `/me/library` endpoints specifically (the
  playlist-items body shape itself is still unconfirmed — see Risks).
- **`spotify.volume_adjust`/`volume_set` cache the volume locally and
  never let a knob turn wait on the network** -- found by hand-testing:
  the original implementation did a `GET /me/player` (to read the
  current level) followed by a `PUT /me/player/volume` on every single
  detent, both awaited synchronously inside the same job the queue
  processes one at a time, so a fast spin of the knob felt clearly
  laggy (each detent paid two real HTTPS round trips, serialized).
  `SpotifyHandlers` now keeps a local `percent`/`known`/`cachedAt` cache
  (`spotifyVolumeCacheTTL` = 60s), refetched via `CurrentVolume` only
  when unknown or stale, and hands each new target to a dedicated
  `runVolumeWriter` goroutine over a single-slot, latest-wins channel
  (`volPending`) instead of writing inline -- `volumeAdjust`/
  `volumeSet` return to the job queue immediately, so a burst of turns
  collapses to one in-flight `PUT` plus whatever the knob settled on
  last, the same "coalesce a burst to the latest value" tradeoff
  `engine`'s LED flush throttle already makes for a different reason.
  The ring/cache update optimistically, before that `PUT` is confirmed
  (see `publishVolume`'s doc comment) -- a knob turn that's about to
  fail (no active device, a dropped connection) still shows the turned-
  to value until the next stale-cache refetch corrects it, trading a
  rare, self-correcting mismatch for every turn feeling as immediate as
  a local `volume.adjust` binding.
- **The LED ring for `spotify.volume_adjust`/`volume_set` needed a
  second source of truth, not `buildSnapshot`'s usual one** -- these
  actions carry no `model.Target` (there is exactly one account-wide
  Spotify Connect volume, not one per resolved `audio.Ref`), so
  `engine.ledDesired`'s existing Target->Refs->`StateObserver.
  CachedLevel` join always produced a nil `Volume` and rendered the
  ring blank, same as an unbound encoder -- LED feedback was simply
  never wired for these two actions, not merely stale. Fixed with a
  parallel, minimal seam: `engine.SpotifyVolumeObserver` (one method,
  `CachedVolumePercent() (float64, bool)`), implemented by
  `*actions.SpotifyHandlers` off the same cache the speed fix above
  introduced; wired via `Engine.SetSpotifySource` (a setter, not a
  `Deps` field, since `spotifyHandlers` can't be constructed until
  after the config store exists, which itself needs `Engine` to already
  exist -- see `cmd/knobd/main.go`'s construction-order comment) and
  `actions.SpotifyOptions.OnVolumeApplied` (mirrors `VolumeOptions.
  OnApplied`'s role, calling `eng.NotifyLEDDirty()` on the same
  optimistic cache update, not on the `PUT`'s eventual completion).
- **`spotify.volume_adjust` only ever increased, regardless of which way
  the knob turned, until it clamped at 100 and stuck** -- found by
  hand-testing. `SpotifyHandlers` doesn't execute on the engine's
  dispatch goroutine like every other handler set (see
  `spotifyQueueDepth`'s doc comment): `enqueue` captures what `Run`
  needs from the `Invocation` into a `spotifyJob` and returns
  immediately, and that capture simply dropped `Invocation.Delta` --
  the signed detent count a `GestureTurn` carries, which every other
  turn-driven handler (`VolumeHandlers.executeAdjust`) multiplies its
  own step by. `volumeAdjust` was always adding the raw, usually-
  positive `StepPercent` with no sign applied. `spotify.volume_set`
  (a button binding, no `Delta` involved) was unaffected, which is why
  it kept working as a workaround. Fixed by adding `spotifyJob.delta`,
  threading `inv.Delta` through `enqueue`, and scaling
  `StepPercent*float64(delta)` in `volumeAdjust`, matching
  `executeAdjust`'s pattern (plus a `delta == 0` no-op guard). See
  `TestSpotifyVolumeAdjustHonorsTurnDirection`/
  `TestSpotifyVolumeAdjustZeroDeltaIsNoop`.
- **`spotify.Service.Login` takes no `ctx` parameter, deliberately** --
  found by hand-testing the real flow: `api`'s `handleSpotifyLogin`
  originally threaded `r.Context()` (POST /spotify/login's own request
  context) all the way into `Auth.StartLogin`, which derives the login
  flow's lifetime from whatever context it's given. Since Go cancels a
  request's context the moment its handler returns, and
  `handleSpotifyLogin` returns as soon as the authorize URL is built
  (well before the user has even seen the browser tab), the loopback
  listener was being torn down within milliseconds of `POST
  /spotify/login` answering -- Firefox then couldn't connect when
  Spotify redirected back, seconds later. `spotify.Service` now stores
  the daemon's own lifetime context (`ctx` in `cmd/knobd`'s
  `runDaemon`, canceled on SIGINT/SIGTERM) at construction and always
  drives `Login`'s flow from that, never from a caller's; the caller-
  facing `api.SpotifyProvider.Login(ctx)`/`cmd/knobd`'s
  `spotifyProvider.Login(ctx)` still take a `ctx` parameter (interface
  consistency with `Playlists`/`Devices`, which do need one for their
  own request-scoped calls), but it is unused by `Login` specifically,
  and documented as such.

## Risks & open questions

- Requires a Spotify Developer application (Client ID) and, per
  Spotify's February 2026 Developer Mode changes, a Premium account for
  the app owner — a one-time manual prerequisite, still not exercised
  against the real Spotify endpoints by this session.
- Secret Service behavior when the collection is locked (a fresh login
  session before the keyring is unlocked) is implemented
  (`secret.go`'s `unlock`/`runPrompt`) but not exercised by an automated
  test (needs a real, lockable collection) or by hand yet.
- The February 2026 `DELETE /playlists/{id}/items` request body shape
  (`{"tracks":[{"uri":...}]}`) is carried over from the pre-February
  `/tracks` endpoint's documented shape since Spotify's changelog didn't
  spell out the new endpoint's body explicitly; confirm against a real
  account during manual verification and fix `client.go` if wrong.
