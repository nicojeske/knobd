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
	volume      int
	volumeErr   error
	setVolErr   error

	saved, removed                []string
	addedPlaylist, addedURI       string
	removedPlaylistID, removedURI string
	playedContext                 string
	queuedURI                     string
	transferredDevice             string
	transferredPlay               bool
	setVolume                     int
	setVolumeCalled               bool
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
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queuedURI = trackURI
	return f.queueErr
}
func (f *fakeSpotifyAPI) queuedURICopy() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queuedURI
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
func (f *fakeSpotifyAPI) CurrentVolume(ctx context.Context) (int, error) {
	return f.volume, f.volumeErr
}
func (f *fakeSpotifyAPI) SetVolume(ctx context.Context, percent int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setVolume, f.setVolumeCalled = percent, true
	return f.setVolErr
}

var _ SpotifyAPI = (*fakeSpotifyAPI)(nil)

// runOne enqueues inv's action and processes exactly one job, returning
// once runJob has finished -- so tests don't need to poll or sleep.
func runOne(t *testing.T, h *SpotifyHandlers, action model.Action) {
	t.Helper()
	runOneDelta(t, h, action, 0)
}

// runOneDelta is runOne with an explicit Invocation.Delta -- the signed
// detent count a real GestureTurn carries -- for the one action
// (SpotifyVolumeAdjustAction) that actually reads it.
func runOneDelta(t *testing.T, h *SpotifyHandlers, action model.Action, delta int) {
	t.Helper()
	if err := h.enqueue(context.Background(), Invocation{Action: action, Delta: delta}); err != nil {
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

// TestSpotifyVolumeAdjustAddsStepToCurrent checks volumeAdjust's own
// synchronous effect: the cache (and thus CachedVolumePercent, the
// ring's source) updates immediately, without waiting for
// runVolumeWriter (started only by Run, not by runOne) to actually issue
// the PUT. See TestSpotifyVolumeWriterAppliesLatestPending for that
// asynchronous half.
func TestSpotifyVolumeAdjustAddsStepToCurrent(t *testing.T) {
	api := &fakeSpotifyAPI{volume: 40}
	h := NewSpotifyHandlers(api, &media.FakeNotifier{}, SpotifyOptions{})

	runOneDelta(t, h, model.SpotifyVolumeAdjustAction{StepPercent: 5}, 1)

	if pct, ok := h.CachedVolumePercent(); !ok || pct != 45 {
		t.Errorf("CachedVolumePercent = %v, %v, want 45, true", pct, ok)
	}
}

// TestSpotifyVolumeAdjustHonorsTurnDirection is a regression test for a
// bug where volumeAdjust always added StepPercent regardless of which
// way the knob turned (Invocation.Delta was captured at enqueue time but
// never read), so the level only ever climbed to 100 and stuck there.
// A negative Delta (the CCW direction) must subtract, not add.
func TestSpotifyVolumeAdjustHonorsTurnDirection(t *testing.T) {
	api := &fakeSpotifyAPI{volume: 40}
	h := NewSpotifyHandlers(api, &media.FakeNotifier{}, SpotifyOptions{})

	runOneDelta(t, h, model.SpotifyVolumeAdjustAction{StepPercent: 5}, -1)

	if pct, ok := h.CachedVolumePercent(); !ok || pct != 35 {
		t.Errorf("CachedVolumePercent = %v, %v, want 35, true", pct, ok)
	}
}

// TestSpotifyVolumeAdjustZeroDeltaIsNoop guards against a GestureTurn
// job that somehow carries a zero Delta ever nudging the volume.
func TestSpotifyVolumeAdjustZeroDeltaIsNoop(t *testing.T) {
	api := &fakeSpotifyAPI{volume: 40}
	h := NewSpotifyHandlers(api, &media.FakeNotifier{}, SpotifyOptions{})

	runOneDelta(t, h, model.SpotifyVolumeAdjustAction{StepPercent: 5}, 0)

	if _, ok := h.CachedVolumePercent(); ok {
		t.Errorf("CachedVolumePercent became known on a zero-delta adjust, want untouched")
	}
}

func TestSpotifyVolumeAdjustClampsToRange(t *testing.T) {
	api := &fakeSpotifyAPI{volume: 98}
	h := NewSpotifyHandlers(api, &media.FakeNotifier{}, SpotifyOptions{})

	runOneDelta(t, h, model.SpotifyVolumeAdjustAction{StepPercent: 10}, 1)

	if pct, ok := h.CachedVolumePercent(); !ok || pct != 100 {
		t.Errorf("CachedVolumePercent = %v, %v, want 100, true", pct, ok)
	}
}

// TestSpotifyVolumeAdjustReusesCacheWithoutRefetching checks the whole
// point of the cache: a second adjust right after the first must not
// call CurrentVolume again (that's the extra network round trip that
// made every detent feel slow before the cache existed).
func TestSpotifyVolumeAdjustReusesCacheWithoutRefetching(t *testing.T) {
	api := &fakeSpotifyAPI{volume: 40}
	h := NewSpotifyHandlers(api, &media.FakeNotifier{}, SpotifyOptions{})

	runOneDelta(t, h, model.SpotifyVolumeAdjustAction{StepPercent: 5}, 1)
	api.volume = 999 // if a second adjust re-fetches, it'll pick this up
	runOneDelta(t, h, model.SpotifyVolumeAdjustAction{StepPercent: 5}, 1)

	if pct, ok := h.CachedVolumePercent(); !ok || pct != 50 {
		t.Errorf("CachedVolumePercent = %v, %v, want 50 (cache reused, not refetched)", pct, ok)
	}
}

func TestSpotifyVolumeSet(t *testing.T) {
	api := &fakeSpotifyAPI{}
	h := NewSpotifyHandlers(api, &media.FakeNotifier{}, SpotifyOptions{})

	runOne(t, h, model.SpotifyVolumeSetAction{Percent: 30})

	if pct, ok := h.CachedVolumePercent(); !ok || pct != 30 {
		t.Errorf("CachedVolumePercent = %v, %v, want 30, true", pct, ok)
	}
}

// TestSpotifyVolumeWriterAppliesLatestPending exercises the async half
// runOne skips: with Run (and therefore runVolumeWriter) actually
// started, a burst of adjusts converges on the last one's target, not
// every intermediate value.
func TestSpotifyVolumeWriterAppliesLatestPending(t *testing.T) {
	api := &fakeSpotifyAPI{volume: 0}
	h := NewSpotifyHandlers(api, &media.FakeNotifier{}, SpotifyOptions{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.Run(ctx)

	for i := 0; i < 5; i++ {
		if err := h.enqueue(context.Background(), Invocation{Action: model.SpotifyVolumeAdjustAction{StepPercent: 10}, Delta: 1}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		api.mu.Lock()
		got := api.setVolume
		called := api.setVolumeCalled
		api.mu.Unlock()
		if called && got == 50 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("SetVolume never converged to 50, last seen %d (called=%v)", got, called)
		}
		time.Sleep(5 * time.Millisecond)
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
	for api.queuedURICopy() == "" {
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
