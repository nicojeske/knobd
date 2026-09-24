package api

import (
	"context"
	"errors"
	"net/http"
)

// ErrSpotifyNotConfigured is returned by SpotifyProvider.Login/Logout
// when Config.Spotify.ClientID is empty -- there is nothing to log into
// yet. cmd/knobd's adapter translates spotify.ErrUnavailable to this so
// this package never needs to import daemon/internal/spotify (see the
// package doc comment's dependency rule).
var ErrSpotifyNotConfigured = errors.New("api: spotify is not configured (set a Client ID in the Spotify tab)")

// ErrSpotifyNotAuthorized is returned by SpotifyProvider.
// Playlists/Devices when there is no valid access token yet -- the user
// hasn't completed the OAuth flow, or it was revoked.
var ErrSpotifyNotAuthorized = errors.New("api: spotify is not authorized; connect via the Spotify tab")

// SpotifyProvider is how the API drives the Spotify Web API OAuth flow
// and serves the playlist/device pickers the binding editor uses. See
// spotify.Service (daemon/internal/spotify) -- cmd/knobd's adapter is
// the only implementation.
type SpotifyProvider interface {
	// Login starts (or restarts) the OAuth PKCE flow and returns the
	// authorize URL for the daemon to try to open in a browser and the
	// UI to show as a fallback. Returns ErrSpotifyNotConfigured if no
	// Client ID is set.
	Login(ctx context.Context) (authorizeURL string, err error)
	// Logout deletes the stored refresh token and clears the connection
	// status.
	Logout() error
	// Playlists lists every playlist visible to the authorized user.
	// Returns ErrSpotifyNotAuthorized if not yet connected.
	Playlists(ctx context.Context) (SpotifyPlaylistList, error)
	// Devices lists currently available Spotify Connect devices.
	// Returns ErrSpotifyNotAuthorized if not yet connected.
	Devices(ctx context.Context) (SpotifyDeviceList, error)
}

// SpotifyLoginResponse is POST /spotify/login's response body.
type SpotifyLoginResponse struct {
	AuthorizeURL string `json:"authorizeUrl"`
}

// SpotifyPlaylist is one playlist from GET /spotify/playlists, for the
// binding editor's playlist picker (ui/src/components/binding).
type SpotifyPlaylist struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Owner string `json:"owner,omitempty"`
	// Editable is true if the authorized user can add/remove items
	// (owns it, or it's collaborative) -- the picker for
	// spotify.add_to_playlist/remove_from_playlist offers only these.
	Editable bool `json:"editable"`
}

// SpotifyDevice is one Spotify Connect device from GET /spotify/devices,
// for spotify.transfer_playback's device picker.
type SpotifyDevice struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type,omitempty"`
	Active bool   `json:"active"`
}

// SpotifyPlaylistList/SpotifyDeviceList are named (not bare []T) so
// daemon/internal/schema's OpenAPI component reflection -- which only
// produces a named $defs entry for a type with its own name, not an
// anonymous slice -- has something to reflect GET /spotify/playlists|
// devices' response body from.
type SpotifyPlaylistList []SpotifyPlaylist
type SpotifyDeviceList []SpotifyDevice

func (s *Server) handleSpotifyLogin(w http.ResponseWriter, r *http.Request) {
	if s.spotify == nil {
		writeError(w, s.log, http.StatusServiceUnavailable, CodeUnavailable, errNoSpotifyProvider())
		return
	}
	url, err := s.spotify.Login(r.Context())
	if err != nil {
		if errors.Is(err, ErrSpotifyNotConfigured) {
			writeError(w, s.log, http.StatusServiceUnavailable, CodeUnavailable, err)
			return
		}
		writeError(w, s.log, http.StatusInternalServerError, CodeInternal, err)
		return
	}
	writeJSON(w, s.log, http.StatusOK, SpotifyLoginResponse{AuthorizeURL: url})
}

func (s *Server) handleSpotifyLogout(w http.ResponseWriter, r *http.Request) {
	if s.spotify == nil {
		writeError(w, s.log, http.StatusServiceUnavailable, CodeUnavailable, errNoSpotifyProvider())
		return
	}
	if err := s.spotify.Logout(); err != nil {
		writeError(w, s.log, http.StatusInternalServerError, CodeInternal, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSpotifyPlaylists(w http.ResponseWriter, r *http.Request) {
	if s.spotify == nil {
		writeError(w, s.log, http.StatusServiceUnavailable, CodeUnavailable, errNoSpotifyProvider())
		return
	}
	playlists, err := s.spotify.Playlists(r.Context())
	if err != nil {
		if errors.Is(err, ErrSpotifyNotAuthorized) {
			writeError(w, s.log, http.StatusConflict, CodeSpotifyNotAuthorized, err)
			return
		}
		writeError(w, s.log, http.StatusInternalServerError, CodeInternal, err)
		return
	}
	if playlists == nil {
		playlists = SpotifyPlaylistList{}
	}
	writeJSON(w, s.log, http.StatusOK, playlists)
}

func (s *Server) handleSpotifyDevices(w http.ResponseWriter, r *http.Request) {
	if s.spotify == nil {
		writeError(w, s.log, http.StatusServiceUnavailable, CodeUnavailable, errNoSpotifyProvider())
		return
	}
	devices, err := s.spotify.Devices(r.Context())
	if err != nil {
		if errors.Is(err, ErrSpotifyNotAuthorized) {
			writeError(w, s.log, http.StatusConflict, CodeSpotifyNotAuthorized, err)
			return
		}
		writeError(w, s.log, http.StatusInternalServerError, CodeInternal, err)
		return
	}
	if devices == nil {
		devices = SpotifyDeviceList{}
	}
	writeJSON(w, s.log, http.StatusOK, devices)
}
