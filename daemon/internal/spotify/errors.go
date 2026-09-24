package spotify

import "errors"

// ErrUnavailable is returned when no Spotify integration is configured
// at all: no Client ID (see model.Config.Spotify) and/or no session bus
// to reach the Secret Service through -- mirrors media.ErrUnavailable's
// role for M09.
var ErrUnavailable = errors.New("spotify: not configured (set a Client ID in the Spotify tab)")

// ErrNotAuthorized is returned by every Client call, and by
// actions.SpotifyHandlers, when there is no valid (or refreshable)
// access token: first run before the OAuth flow completes, or after the
// user revokes access in their Spotify account (see Service's doc
// comment on invalid_grant handling).
var ErrNotAuthorized = errors.New("spotify: not authorized; connect via the Spotify tab")

// ErrNoActiveDevice is returned by PlayContext/Queue/Transfer when
// Spotify reports no Connect device is available to act on (its own
// 404 "NO_ACTIVE_DEVICE" player error).
var ErrNoActiveDevice = errors.New("spotify: no active Spotify Connect device")

// ErrNothingPlaying is returned by CurrentlyPlaying-dependent actions
// (like_toggle, add_to_playlist, remove_from_playlist) when Spotify
// reports nothing is currently playing.
var ErrNothingPlaying = errors.New("spotify: nothing is currently playing")
