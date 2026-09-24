package actions

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/njeske/knobd/internal/media"
	"github.com/njeske/knobd/internal/model"
	"github.com/njeske/knobd/internal/spotify"
)

// fakeSpotifyAPI is a SpotifyAPI test double recording every call.
type fakeSpotifyAPI struct {
	mu sync.Mutex

	current     spotify.CurrentTrack
	currentErr  error
	contains    bool
	containsErr error
	saveErr     error
	removeErr   error
	addErr      error
	removePLErr error
	playErr     error
	queueErr    error
	transferErr error
	devices     []spotify.Device
	devicesErr  error
	playlists   []spotify.Playlist

	saved, removed                []string
	addedPlaylist, addedURI       string
	removedPlaylistID, removedURI string
	playedContext                 string
	queuedURI                     string
	transferredDevice             string
	transferredPlay               bool
}

func (f *fakeSpotifyAPI) CurrentlyPlaying(ctx context.Context) (spotify.CurrentTrack, error) {
	return f.current, f.currentErr
}
func (f *fakeSpotifyAPI) LibraryContains(ctx context.Context, uri string) (bool, error) {
	return f.contains, f.containsErr
}
func (f *fakeSpotifyAPI) SaveToLibrary(ctx context.Context, uri string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, uri)
	return f.saveErr
}
func (f *fakeSpotifyAPI) RemoveFromLibrary(ctx context.Context, uri string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, uri)
	return f.removeErr
}
func (f *fakeSpotifyAPI) AddPlaylistItems(ctx context.Context, playlistID, uri string) error {
	f.addedPlaylist, f.addedURI = playlistID, uri
	return f.addErr
}
func (f *fakeSpotifyAPI) RemovePlaylistItems(ctx context.Context, playlistID, uri string) error {
	f.removedPlaylistID, f.removedURI = playlistID, uri
	return f.removePLErr
}
func (f *fakeSpotifyAPI) PlayContext(ctx context.Context, contextURI string) error {
	f.playedContext = contextURI
	return f.playErr
}
func (f *fakeSpotifyAPI) Queue(ctx context.Context, trackURI string) error {
	f.queuedURI = trackURI
	return f.queueErr
}
func (f *fakeSpotifyAPI) Transfer(ctx context.Context, deviceID string, play bool) error {
	f.transferredDevice, f.transferredPlay = deviceID, play
	return f.transferErr
}
func (f *fakeSpotifyAPI) Devices(ctx context.Context) ([]spotify.Device, error) {
	return f.devices, f.devicesErr
}
func (f *fakeSpotifyAPI) Playlists(ctx context.Context) ([]spotify.Playlist, error) {
	return f.playlists, nil
}

var _ SpotifyAPI = (*fakeSpotifyAPI)(nil)

// runOne enqueues inv's action and processes exactly one job, returning
// once runJob has finished -- so tests don't need to poll or sleep.
func runOne(t *testing.T, h *SpotifyHandlers, action model.Action) {
	t.Helper()
	if err := h.enqueue(context.Background(), Invocation{Action: action}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	select {
	case job := <-h.jobs:
		h.runJob(context.Background(), job)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for job to be queued")
	}
}

func TestSpotifyLikeToggleSavesWhenNotLiked(t *testing.T) {
	api := &fakeSpotifyAPI{current: spotify.CurrentTrack{URI: "spotify:track:abc", Name: "Song", Artists: []string{"Artist"}}, contains: false}
	notifier := &media.FakeNotifier{}
	h := NewSpotifyHandlers(api, notifier, SpotifyOptions{})

	runOne(t, h, model.SpotifyLikeToggleAction{})

	if len(api.saved) != 1 || api.saved[0] != "spotify:track:abc" {
		t.Errorf("saved = %v", api.saved)
	}
	if len(api.removed) != 0 {
		t.Errorf("removed = %v, want none", api.removed)
	}
	if len(notifier.Sent) != 1 || notifier.Sent[0].Summary != "♥ Liked" {
		t.Errorf("notifications = %+v", notifier.Sent)
	}
}

func TestSpotifyLikeToggleRemovesWhenAlreadyLiked(t *testing.T) {
	api := &fakeSpotifyAPI{current: spotify.CurrentTrack{URI: "spotify:track:abc", Name: "Song"}, contains: true}
	notifier := &media.FakeNotifier{}
	h := NewSpotifyHandlers(api, notifier, SpotifyOptions{})

	runOne(t, h, model.SpotifyLikeToggleAction{})

	if len(api.removed) != 1 || api.removed[0] != "spotify:track:abc" {
		t.Errorf("removed = %v", api.removed)
	}
	if len(api.saved) != 0 {
		t.Errorf("saved = %v, want none", api.saved)
	}
	if len(notifier.Sent) != 1 || notifier.Sent[0].Summary != "Removed from Liked Songs" {
		t.Errorf("notifications = %+v", notifier.Sent)
	}
}

func TestSpotifyLikeToggleNothingPlayingNotifiesError(t *testing.T) {
	api := &fakeSpotifyAPI{currentErr: spotify.ErrNothingPlaying}
	notifier := &media.FakeNotifier{}
	h := NewSpotifyHandlers(api, notifier, SpotifyOptions{})

	runOne(t, h, model.SpotifyLikeToggleAction{})

	if len(notifier.Sent) != 1 {
		t.Fatalf("notifications = %+v, want 1", notifier.Sent)
	}
	if notifier.Sent[0].Summary != "Spotify error" {
		t.Errorf("summary = %q", notifier.Sent[0].Summary)
	}
}

func TestSpotifyAddToPlaylistNormalizesID(t *testing.T) {
	api := &fakeSpotifyAPI{current: spotify.CurrentTrack{URI: "spotify:track:abc", Name: "Song"}}
	notifier := &media.FakeNotifier{}
	h := NewSpotifyHandlers(api, notifier, SpotifyOptions{})

	runOne(t, h, model.SpotifyAddToPlaylistAction{PlaylistID: "https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M"})

	if api.addedPlaylist != "37i9dQZF1DXcBWIGoYBM5M" || api.addedURI != "spotify:track:abc" {
		t.Errorf("AddPlaylistItems(%q, %q)", api.addedPlaylist, api.addedURI)
	}
}

func TestSpotifyRemoveFromPlaylist(t *testing.T) {
	api := &fakeSpotifyAPI{current: spotify.CurrentTrack{URI: "spotify:track:abc", Name: "Song"}}
	notifier := &media.FakeNotifier{}
	h := NewSpotifyHandlers(api, notifier, SpotifyOptions{})

	runOne(t, h, model.SpotifyRemoveFromPlaylistAction{PlaylistID: "37i9dQZF1DXcBWIGoYBM5M"})

	if api.removedPlaylistID != "37i9dQZF1DXcBWIGoYBM5M" || api.removedURI != "spotify:track:abc" {
		t.Errorf("RemovePlaylistItems(%q, %q)", api.removedPlaylistID, api.removedURI)
	}
}

func TestSpotifyStartPlaylist(t *testing.T) {
	api := &fakeSpotifyAPI{}
	h := NewSpotifyHandlers(api, &media.FakeNotifier{}, SpotifyOptions{})

	runOne(t, h, model.SpotifyStartPlaylistAction{PlaylistID: "37i9dQZF1DXcBWIGoYBM5M"})

	if api.playedContext != "spotify:playlist:37i9dQZF1DXcBWIGoYBM5M" {
		t.Errorf("PlayContext = %q", api.playedContext)
	}
}

func TestSpotifyQueueTrack(t *testing.T) {
	api := &fakeSpotifyAPI{}
	h := NewSpotifyHandlers(api, &media.FakeNotifier{}, SpotifyOptions{})

	runOne(t, h, model.SpotifyQueueTrackAction{TrackID: "spotify:track:37i9dQZF1DXcBWIGoYBM5M"})

	if api.queuedURI != "spotify:track:37i9dQZF1DXcBWIGoYBM5M" {
		t.Errorf("Queue = %q", api.queuedURI)
	}
}

func TestSpotifyTransferPlaybackMatchesByName(t *testing.T) {
	api := &fakeSpotifyAPI{devices: []spotify.Device{
		{ID: "d1", Name: "Living Room"},
		{ID: "d2", Name: "Office PC"},
	}}
	h := NewSpotifyHandlers(api, &media.FakeNotifier{}, SpotifyOptions{})

	runOne(t, h, model.SpotifyTransferPlaybackAction{DeviceName: "office pc", Play: true})

	if api.transferredDevice != "d2" || !api.transferredPlay {
		t.Errorf("Transfer(%q, %v)", api.transferredDevice, api.transferredPlay)
	}
}

func TestSpotifyTransferPlaybackUnknownDevice(t *testing.T) {
	api := &fakeSpotifyAPI{devices: []spotify.Device{{ID: "d1", Name: "Living Room"}}}
	h := NewSpotifyHandlers(api, &media.FakeNotifier{}, SpotifyOptions{})

	runOne(t, h, model.SpotifyTransferPlaybackAction{DeviceName: "Nonexistent"})

	if api.transferredDevice != "" {
		t.Errorf("Transfer should not have been called, got device %q", api.transferredDevice)
	}
}

func TestSpotifyEnqueueDropsWhenQueueFull(t *testing.T) {
	api := &fakeSpotifyAPI{}
	h := NewSpotifyHandlers(api, &media.FakeNotifier{}, SpotifyOptions{})

	// Fill the queue without draining it.
	for i := 0; i < spotifyQueueDepth; i++ {
		if err := h.enqueue(context.Background(), Invocation{Action: model.SpotifyQueueTrackAction{TrackID: fmt.Sprintf("t%d", i)}}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	err := h.enqueue(context.Background(), Invocation{Action: model.SpotifyQueueTrackAction{TrackID: "overflow"}})
	if err == nil {
		t.Fatal("expected an error when the queue is full")
	}
}

func TestSpotifyRunProcessesQueuedJobs(t *testing.T) {
	api := &fakeSpotifyAPI{}
	h := NewSpotifyHandlers(api, &media.FakeNotifier{}, SpotifyOptions{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		h.Run(ctx)
		close(done)
	}()

	if err := h.enqueue(context.Background(), Invocation{Action: model.SpotifyQueueTrackAction{TrackID: "abc123456789012345"}}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for api.queuedURI == "" {
		select {
		case <-deadline:
			t.Fatal("Run did not process the queued job in time")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

func TestUserFacingSpotifyErrorMapsSentinels(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{spotify.ErrNotAuthorized, "not connected (see the Spotify tab)"},
		{spotify.ErrNothingPlaying, "nothing is playing"},
		{spotify.ErrNoActiveDevice, "no active Spotify Connect device"},
		{errors.New("boom"), "boom"},
	}
	for _, tc := range cases {
		if got := userFacingSpotifyError(tc.err); got != tc.want {
			t.Errorf("userFacingSpotifyError(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
