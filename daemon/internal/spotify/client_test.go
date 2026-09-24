package spotify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// farFuture is a fixed, generously-future expiry so a primed test token
// is never treated as expired by TokenManager.AccessToken's own clock
// check.
var farFuture = time.Now().Add(24 * time.Hour)

// newTestClient wires a Client against srv with a pre-primed, always-
// valid access token, so client tests exercise request shapes without
// needing a working token endpoint too.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	tm, _ := newTestTokenManager(t, nil)
	if err := tm.SetTokens("client-1", Tokens{AccessToken: "at1", ExpiresAt: farFuture}); err != nil {
		t.Fatalf("SetTokens: %v", err)
	}
	c := NewClient(tm, func() string { return "client-1" })
	c.BaseURL = srv.URL
	return c
}

func TestClientSaveToLibraryUsesURIsAndNewEndpoint(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if r.URL.Query().Get("uris") != "spotify:track:abc123" {
			t.Errorf("uris query = %q, want spotify:track:abc123", r.URL.Query().Get("uris"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.SaveToLibrary(context.Background(), "spotify:track:abc123"); err != nil {
		t.Fatalf("SaveToLibrary: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/me/library" {
		t.Errorf("request = %s %s, want PUT /me/library", gotMethod, gotPath)
	}
}

func TestClientLibraryContains(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/library/contains" {
			t.Errorf("path = %s, want /me/library/contains", r.URL.Path)
		}
		if r.URL.Query().Get("uris") != "spotify:track:abc123" {
			t.Errorf("uris query = %q", r.URL.Query().Get("uris"))
		}
		json.NewEncoder(w).Encode([]bool{true})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	ok, err := c.LibraryContains(context.Background(), "spotify:track:abc123")
	if err != nil {
		t.Fatalf("LibraryContains: %v", err)
	}
	if !ok {
		t.Error("LibraryContains = false, want true")
	}
}

func TestClientRemoveFromLibraryUsesURIsQueryParam(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if r.URL.Query().Get("uris") != "spotify:track:abc123" {
			t.Errorf("uris query = %q, want spotify:track:abc123", r.URL.Query().Get("uris"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.RemoveFromLibrary(context.Background(), "spotify:track:abc123"); err != nil {
		t.Fatalf("RemoveFromLibrary: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/me/library" {
		t.Errorf("request = %s %s, want DELETE /me/library", gotMethod, gotPath)
	}
}

func TestClientCurrentVolume(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/player" {
			t.Errorf("path = %s, want /me/player", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"device": map[string]any{"volume_percent": 42},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.CurrentVolume(context.Background())
	if err != nil {
		t.Fatalf("CurrentVolume: %v", err)
	}
	if got != 42 {
		t.Errorf("CurrentVolume = %d, want 42", got)
	}
}

func TestClientCurrentVolumeNoActiveDevice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.CurrentVolume(context.Background()); err != ErrNoActiveDevice {
		t.Fatalf("err = %v, want ErrNoActiveDevice", err)
	}
}

func TestClientSetVolume(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if r.URL.Query().Get("volume_percent") != "37" {
			t.Errorf("volume_percent query = %q, want 37", r.URL.Query().Get("volume_percent"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.SetVolume(context.Background(), 37); err != nil {
		t.Fatalf("SetVolume: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/me/player/volume" {
		t.Errorf("request = %s %s, want PUT /me/player/volume", gotMethod, gotPath)
	}
}

func TestClientAddPlaylistItemsUsesItemsPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.AddPlaylistItems(context.Background(), "playlist123", "spotify:track:abc"); err != nil {
		t.Fatalf("AddPlaylistItems: %v", err)
	}
	if gotPath != "/playlists/playlist123/items" {
		t.Errorf("path = %s, want /playlists/playlist123/items", gotPath)
	}
}

func TestClientCurrentlyPlayingNothingPlaying(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.CurrentlyPlaying(context.Background())
	if err != ErrNothingPlaying {
		t.Fatalf("err = %v, want ErrNothingPlaying", err)
	}
}

func TestClientPlayerErrorNoActiveDevice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"status":  404,
				"message": "Device not found",
				"reason":  "NO_ACTIVE_DEVICE",
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.Transfer(context.Background(), "device1", true)
	if err != ErrNoActiveDevice {
		t.Fatalf("err = %v, want ErrNoActiveDevice", err)
	}
}

func TestClientRetriesOnceOn401(t *testing.T) {
	tm, store := newTestTokenManager(t, nil)
	store.Set("client-1", "refresh-1")

	var tokenCalls, apiCalls int
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at-new", "expires_in": 3600})
	}))
	defer tokenSrv.Close()
	tm.auth.TokenURL = tokenSrv.URL

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		if apiCalls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer apiSrv.Close()

	// Prime with an already-expired cached token so the first call uses
	// it (triggering the server's 401) rather than refreshing first.
	if err := tm.SetTokens("client-1", Tokens{AccessToken: "stale", ExpiresAt: farFuture}); err != nil {
		t.Fatalf("SetTokens: %v", err)
	}

	c := NewClient(tm, func() string { return "client-1" })
	c.BaseURL = apiSrv.URL

	if err := c.SaveToLibrary(context.Background(), "spotify:track:abc"); err != nil {
		t.Fatalf("SaveToLibrary: %v", err)
	}
	if apiCalls != 2 {
		t.Errorf("api server called %d times, want 2 (one 401, one retry)", apiCalls)
	}
	if tokenCalls != 1 {
		t.Errorf("token server called %d times, want 1", tokenCalls)
	}
}

func TestClientPlaylistsEditableFlag(t *testing.T) {
	meCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/me":
			meCalls++
			json.NewEncoder(w).Encode(map[string]any{"id": "me-id", "display_name": "Me"})
		case "/me/playlists":
			json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{"id": "p1", "name": "Mine", "collaborative": false, "owner": map[string]any{"id": "me-id", "display_name": "Me"}},
					{"id": "p2", "name": "Theirs", "collaborative": false, "owner": map[string]any{"id": "other-id", "display_name": "Other"}},
					{"id": "p3", "name": "Shared", "collaborative": true, "owner": map[string]any{"id": "other-id", "display_name": "Other"}},
				},
				"next": "",
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	playlists, err := c.Playlists(context.Background())
	if err != nil {
		t.Fatalf("Playlists: %v", err)
	}
	want := map[string]bool{"p1": true, "p2": false, "p3": true}
	if len(playlists) != 3 {
		t.Fatalf("got %d playlists, want 3", len(playlists))
	}
	for _, p := range playlists {
		if p.Editable != want[p.ID] {
			t.Errorf("playlist %s Editable = %v, want %v", p.ID, p.Editable, want[p.ID])
		}
	}
	if meCalls != 1 {
		t.Errorf("/me called %d times, want 1 (cached)", meCalls)
	}
}
