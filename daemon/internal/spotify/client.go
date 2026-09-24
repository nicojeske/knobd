package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultAPIBaseURL is Spotify's real Web API base. Client.BaseURL is
// overridable so tests can point it at an httptest.Server.
const DefaultAPIBaseURL = "https://api.spotify.com/v1"

// Client is a thin REST client over the handful of Spotify Web API
// endpoints actions.SpotifyHandlers needs -- not a general SDK (see
// specs/milestones/M10-spotify-web-api.md's Scope). Every request path
// reflects the February 2026 Web API changes: /me/library (not
// /me/tracks) for save/remove/check, and /playlists/{id}/items (not
// /playlists/{id}/tracks) for playlist membership.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	Tokens     *TokenManager
	// Account returns the current Spotify Client ID -- the TokenManager
	// account key. Read fresh on every call, the same live-config-read
	// pattern actions.MediaOptions.IgnorePlayers uses.
	Account func() string

	meMu sync.Mutex
	meID string // cached GET /me id, for Playlists' Editable field
}

// NewClient returns a Client pointed at the real Spotify Web API.
func NewClient(tokens *TokenManager, account func() string) *Client {
	return &Client{
		BaseURL:    DefaultAPIBaseURL,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		Tokens:     tokens,
		Account:    account,
	}
}

// apiError is Spotify's REST error body shape: {"error":{"status":404,
// "message":"...","reason":"NO_ACTIVE_DEVICE"}} -- the player endpoints
// add "reason"; other endpoints omit it.
type apiError struct {
	Error struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Reason  string `json:"reason"`
	} `json:"error"`
}

// APIError is returned for any non-2xx response client.go's do doesn't
// map to a package sentinel (ErrNotAuthorized, ErrNoActiveDevice,
// ErrNothingPlaying).
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("spotify: api error %d: %s", e.Status, e.Message)
}

// do issues one request to path (relative to BaseURL), retrying once
// after a fresh access token if the first attempt gets a 401. body, if
// non-nil, is JSON-marshaled as the request body; out, if non-nil,
// receives the JSON response body (skipped for 204/empty bodies).
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	account := c.Account()
	if account == "" {
		return ErrUnavailable
	}

	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("spotify: encode request body: %w", err)
		}
		bodyBytes = b
	}

	fullURL := c.BaseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	doOnce := func(accessToken string) (*http.Response, error) {
		var reader io.Reader
		if bodyBytes != nil {
			reader = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return c.HTTPClient.Do(req)
	}

	accessToken, err := c.Tokens.AccessToken(ctx, account)
	if err != nil {
		return err
	}
	resp, err := doOnce(accessToken)
	if err != nil {
		return fmt.Errorf("spotify: request %s %s: %w", method, path, err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		c.Tokens.Invalidate(account)
		accessToken, err = c.Tokens.AccessToken(ctx, account)
		if err != nil {
			return err
		}
		resp, err = doOnce(accessToken)
		if err != nil {
			return fmt.Errorf("spotify: retry %s %s: %w", method, path, err)
		}
	}
	defer resp.Body.Close()

	return c.handleResponse(resp, out)
}

func (c *Client) handleResponse(resp *http.Response, out any) error {
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("spotify: read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var ae apiError
		_ = json.Unmarshal(data, &ae)
		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			return ErrNotAuthorized
		case ae.Error.Reason == "NO_ACTIVE_DEVICE":
			return ErrNoActiveDevice
		}
		msg := ae.Error.Message
		if msg == "" {
			msg = strings.TrimSpace(string(data))
		}
		return &APIError{Status: resp.StatusCode, Message: msg}
	}

	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("spotify: decode response body: %w", err)
	}
	return nil
}

// CurrentTrack is the subset of GET /me/player/currently-playing this
// package needs.
type CurrentTrack struct {
	URI     string
	ID      string
	Name    string
	Artists []string
}

// CurrentlyPlaying returns the track currently playing on the user's
// account (not scoped to a device -- Spotify's own endpoint is
// account-wide). Returns ErrNothingPlaying if playback is stopped
// (Spotify's 204 response) or the current item isn't a track (an
// episode, which library/playlist actions don't apply to).
func (c *Client) CurrentlyPlaying(ctx context.Context) (CurrentTrack, error) {
	var body struct {
		Item *struct {
			ID      string `json:"id"`
			URI     string `json:"uri"`
			Name    string `json:"name"`
			Type    string `json:"type"`
			Artists []struct {
				Name string `json:"name"`
			} `json:"artists"`
		} `json:"item"`
	}
	if err := c.do(ctx, http.MethodGet, "/me/player/currently-playing", nil, nil, &body); err != nil {
		return CurrentTrack{}, err
	}
	if body.Item == nil || body.Item.Type != "track" {
		return CurrentTrack{}, ErrNothingPlaying
	}
	track := CurrentTrack{ID: body.Item.ID, URI: body.Item.URI, Name: body.Item.Name}
	for _, a := range body.Item.Artists {
		track.Artists = append(track.Artists, a.Name)
	}
	return track, nil
}

// LibraryContains reports whether uri is already saved to the user's
// library (GET /me/library/contains). uris is a query parameter (a
// comma-separated list of Spotify URIs, not a JSON body), same as
// SaveToLibrary/RemoveFromLibrary -- confirmed against Spotify's own
// reference docs after the original "ids" JSON-body shape (carried over
// from the pre-February-2026 /me/tracks* family) turned out to silently
// no-op against the real API: it fails validation and never reaches the
// library at all.
func (c *Client) LibraryContains(ctx context.Context, uri string) (bool, error) {
	var result []bool
	q := url.Values{"uris": {uri}}
	if err := c.do(ctx, http.MethodGet, "/me/library/contains", q, nil, &result); err != nil {
		return false, err
	}
	return len(result) > 0 && result[0], nil
}

// SaveToLibrary saves uri to the user's library (PUT /me/library).
func (c *Client) SaveToLibrary(ctx context.Context, uri string) error {
	q := url.Values{"uris": {uri}}
	return c.do(ctx, http.MethodPut, "/me/library", q, nil, nil)
}

// RemoveFromLibrary removes uri from the user's library
// (DELETE /me/library).
func (c *Client) RemoveFromLibrary(ctx context.Context, uri string) error {
	q := url.Values{"uris": {uri}}
	return c.do(ctx, http.MethodDelete, "/me/library", q, nil, nil)
}

// AddPlaylistItems adds uri to playlistID (POST /playlists/{id}/items).
func (c *Client) AddPlaylistItems(ctx context.Context, playlistID, uri string) error {
	path := fmt.Sprintf("/playlists/%s/items", playlistID)
	return c.do(ctx, http.MethodPost, path, nil, map[string]any{"uris": []string{uri}}, nil)
}

// RemovePlaylistItems removes uri from playlistID
// (DELETE /playlists/{id}/items).
func (c *Client) RemovePlaylistItems(ctx context.Context, playlistID, uri string) error {
	path := fmt.Sprintf("/playlists/%s/items", playlistID)
	body := map[string]any{"tracks": []map[string]string{{"uri": uri}}}
	return c.do(ctx, http.MethodDelete, path, nil, body, nil)
}

// PlayContext starts playback of contextURI (a playlist/album URI) on
// the currently active device (PUT /me/player/play).
func (c *Client) PlayContext(ctx context.Context, contextURI string) error {
	return c.do(ctx, http.MethodPut, "/me/player/play", nil, map[string]any{"context_uri": contextURI}, nil)
}

// Queue adds trackURI to the playback queue on the currently active
// device (POST /me/player/queue).
func (c *Client) Queue(ctx context.Context, trackURI string) error {
	q := url.Values{"uri": {trackURI}}
	return c.do(ctx, http.MethodPost, "/me/player/queue", q, nil, nil)
}

// Transfer moves playback to deviceID (PUT /me/player).
func (c *Client) Transfer(ctx context.Context, deviceID string, play bool) error {
	body := map[string]any{"device_ids": []string{deviceID}, "play": play}
	return c.do(ctx, http.MethodPut, "/me/player", nil, body, nil)
}

// CurrentVolume returns the active Spotify Connect device's volume, 0-100
// (GET /me/player's device.volume_percent). Returns ErrNoActiveDevice if
// there is no active device (Spotify's own 204 response, the same
// "nothing to act on" case Transfer/PlayContext/Queue report for the
// player-mutation endpoints).
func (c *Client) CurrentVolume(ctx context.Context) (int, error) {
	var body struct {
		Device *struct {
			VolumePercent *int `json:"volume_percent"`
		} `json:"device"`
	}
	if err := c.do(ctx, http.MethodGet, "/me/player", nil, nil, &body); err != nil {
		return 0, err
	}
	if body.Device == nil || body.Device.VolumePercent == nil {
		return 0, ErrNoActiveDevice
	}
	return *body.Device.VolumePercent, nil
}

// SetVolume sets the active Spotify Connect device's volume to percent
// (0-100), via PUT /me/player/volume?volume_percent=N -- this moves the
// level Spotify Connect itself tracks, so it stays in sync across
// whatever device is actually playing, unlike a local PipeWire mixer
// change.
func (c *Client) SetVolume(ctx context.Context, percent int) error {
	q := url.Values{"volume_percent": {fmt.Sprintf("%d", percent)}}
	return c.do(ctx, http.MethodPut, "/me/player/volume", q, nil, nil)
}

// Device is one Spotify Connect device from GET /me/player/devices.
type Device struct {
	ID       string
	Name     string
	Type     string
	IsActive bool
}

// Devices lists currently available Spotify Connect devices.
func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	var body struct {
		Devices []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Type     string `json:"type"`
			IsActive bool   `json:"is_active"`
		} `json:"devices"`
	}
	if err := c.do(ctx, http.MethodGet, "/me/player/devices", nil, nil, &body); err != nil {
		return nil, err
	}
	devices := make([]Device, 0, len(body.Devices))
	for _, d := range body.Devices {
		devices = append(devices, Device{ID: d.ID, Name: d.Name, Type: d.Type, IsActive: d.IsActive})
	}
	return devices, nil
}

// Playlist is one playlist from GET /me/playlists, with Editable
// resolved from ownership/collaborative status against the
// authorized user's own id (GET /me, cached).
type Playlist struct {
	ID       string
	Name     string
	Owner    string
	Editable bool
}

// Playlists lists every playlist visible to the authorized user,
// paginating through GET /me/playlists.
func (c *Client) Playlists(ctx context.Context) ([]Playlist, error) {
	meID, err := c.currentUserID(ctx)
	if err != nil {
		return nil, err
	}

	var playlists []Playlist
	path := "/me/playlists"
	q := url.Values{"limit": {"50"}}
	for path != "" {
		var body struct {
			Items []struct {
				ID            string `json:"id"`
				Name          string `json:"name"`
				Collaborative bool   `json:"collaborative"`
				Owner         struct {
					ID          string `json:"id"`
					DisplayName string `json:"display_name"`
				} `json:"owner"`
			} `json:"items"`
			Next string `json:"next"`
		}
		if err := c.do(ctx, http.MethodGet, path, q, nil, &body); err != nil {
			return nil, err
		}
		for _, it := range body.Items {
			playlists = append(playlists, Playlist{
				ID:       it.ID,
				Name:     it.Name,
				Owner:    it.Owner.DisplayName,
				Editable: it.Owner.ID == meID || it.Collaborative,
			})
		}
		if body.Next == "" {
			break
		}
		next, err := url.Parse(body.Next)
		if err != nil {
			break
		}
		path = strings.TrimPrefix(next.Path, "/v1")
		q = next.Query()
	}
	return playlists, nil
}

// User is the subset of GET /me this package needs.
type User struct {
	ID          string
	DisplayName string
}

// Me returns the authorized user's own profile, also used to validate
// that a stored token is actually good (Service's startup check).
func (c *Client) Me(ctx context.Context) (User, error) {
	var body struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	}
	if err := c.do(ctx, http.MethodGet, "/me", nil, nil, &body); err != nil {
		return User{}, err
	}
	return User{ID: body.ID, DisplayName: body.DisplayName}, nil
}

func (c *Client) currentUserID(ctx context.Context) (string, error) {
	c.meMu.Lock()
	cached := c.meID
	c.meMu.Unlock()
	if cached != "" {
		return cached, nil
	}
	me, err := c.Me(ctx)
	if err != nil {
		return "", err
	}
	c.meMu.Lock()
	c.meID = me.ID
	c.meMu.Unlock()
	return me.ID, nil
}
