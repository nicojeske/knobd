package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/njeske/knobd/internal/media"
	"github.com/njeske/knobd/internal/model"
	"github.com/njeske/knobd/internal/spotify"
)

// SpotifyAPI is the slice of spotify.Client this handler set needs --
// point-of-use, so actions never imports spotify.Client's full surface
// (mirrors MediaCommands' role for media.Backend). Satisfied by
// *spotify.Client in production and a fake in tests.
type SpotifyAPI interface {
	CurrentlyPlaying(ctx context.Context) (spotify.CurrentTrack, error)
	LibraryContains(ctx context.Context, uri string) (bool, error)
	SaveToLibrary(ctx context.Context, uri string) error
	RemoveFromLibrary(ctx context.Context, uri string) error
	AddPlaylistItems(ctx context.Context, playlistID, uri string) error
	RemovePlaylistItems(ctx context.Context, playlistID, uri string) error
	PlayContext(ctx context.Context, contextURI string) error
	Queue(ctx context.Context, trackURI string) error
	Transfer(ctx context.Context, deviceID string, play bool) error
	Devices(ctx context.Context) ([]spotify.Device, error)
	Playlists(ctx context.Context) ([]spotify.Playlist, error)
	CurrentVolume(ctx context.Context) (int, error)
	SetVolume(ctx context.Context, percent int) error
}

// spotifyQueueDepth bounds how many pending Spotify actions
// SpotifyHandlers will buffer before dropping the newest one. Spotify
// Web API calls are real network round-trips (unlike the MPRIS/D-Bus
// calls M09's actions make), so they cannot run on the engine's
// dispatch goroutine (see Invocation's doc comment on
// audio.Backend/Registry.Execute serialization) -- Execute below always
// returns immediately, handing the work to Run's own goroutine instead.
// A depth of 8 is generous for a control surface a human is pressing
// one button at a time; a full queue means something is stuck (a hung
// request), and dropping the newest press rather than blocking the
// dispatcher is the same tradeoff M09's "never block dispatch" rule
// makes elsewhere.
const spotifyQueueDepth = 8

// SpotifyOptions configures SpotifyHandlers. The zero value is sane
// defaults.
type SpotifyOptions struct {
	Logger *slog.Logger
	// Timeout bounds each queued job's context; zero means 10s.
	Timeout time.Duration
}

func (o SpotifyOptions) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

func (o SpotifyOptions) timeout() time.Duration {
	if o.Timeout > 0 {
		return o.Timeout
	}
	return 10 * time.Second
}

// spotifyJob is one queued Spotify action, captured at Execute time
// (Invocation is only valid for the duration of the dispatch call that
// produced it, so Run must not hold onto inv itself -- only what it
// needs from it).
type spotifyJob struct {
	action  model.Action
	control model.Control
}

// SpotifyHandlers implements model.ActionSpotifyLikeToggle/
// AddToPlaylist/RemoveFromPlaylist/StartPlaylist/QueueTrack/
// TransferPlayback (see specs/milestones/M10-spotify-web-api.md).
// Unlike every other handler set in this package, Execute never calls
// the backend directly -- see spotifyQueueDepth's doc comment -- so
// cmd/knobd must also start Run in its own goroutine alongside
// registering this handler set.
type SpotifyHandlers struct {
	api      SpotifyAPI
	notifier media.Notifier
	opts     SpotifyOptions

	jobs chan spotifyJob
}

// NewSpotifyHandlers builds a SpotifyHandlers. notifier is used for
// like_toggle/add_to_playlist/remove_from_playlist's result
// notifications (see specs/milestones/M10-spotify-web-api.md's Design
// decision to notify on those three, but not
// start_playlist/queue_track/transfer_playback).
func NewSpotifyHandlers(api SpotifyAPI, notifier media.Notifier, opts SpotifyOptions) *SpotifyHandlers {
	return &SpotifyHandlers{
		api:      api,
		notifier: notifier,
		opts:     opts,
		jobs:     make(chan spotifyJob, spotifyQueueDepth),
	}
}

// Register implements the handler-set pattern shared with
// VolumeHandlers/MediaHandlers/SceneHandlers/MixHandlers.
func (h *SpotifyHandlers) Register(r *Registry) {
	r.Register(model.ActionSpotifyLikeToggle, HandlerFunc(h.enqueue))
	r.Register(model.ActionSpotifyAddToPlaylist, HandlerFunc(h.enqueue))
	r.Register(model.ActionSpotifyRemoveFromPlaylist, HandlerFunc(h.enqueue))
	r.Register(model.ActionSpotifyStartPlaylist, HandlerFunc(h.enqueue))
	r.Register(model.ActionSpotifyQueueTrack, HandlerFunc(h.enqueue))
	r.Register(model.ActionSpotifyTransferPlayback, HandlerFunc(h.enqueue))
	r.Register(model.ActionSpotifyVolumeAdjust, HandlerFunc(h.enqueue))
	r.Register(model.ActionSpotifyVolumeSet, HandlerFunc(h.enqueue))
}

// enqueue is every registered Handler: it captures what Run needs from
// inv and returns immediately, never touching the network on the
// engine's dispatch goroutine.
func (h *SpotifyHandlers) enqueue(ctx context.Context, inv Invocation) error {
	job := spotifyJob{action: inv.Action, control: inv.Control}
	select {
	case h.jobs <- job:
		return nil
	default:
		h.opts.logger().Warn("actions: spotify job queue full, dropping", "control", inv.Control, "type", inv.Action.ActionType())
		return fmt.Errorf("actions: spotify job queue full")
	}
}

// Run drains the job queue until ctx is canceled -- cmd/knobd starts
// this alongside eng.Run/srv.ListenAndServe/hub.Run (see
// specs/reference/codebase.md's Wiring section). It never returns an
// error worth racing the daemon's other goroutines over; ctx
// cancellation is the only exit.
func (h *SpotifyHandlers) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-h.jobs:
			h.runJob(ctx, job)
		}
	}
}

func (h *SpotifyHandlers) runJob(parent context.Context, job spotifyJob) {
	ctx, cancel := context.WithTimeout(parent, h.opts.timeout())
	defer cancel()

	var err error
	switch a := job.action.(type) {
	case model.SpotifyLikeToggleAction:
		err = h.likeToggle(ctx)
	case model.SpotifyAddToPlaylistAction:
		err = h.addToPlaylist(ctx, a)
	case model.SpotifyRemoveFromPlaylistAction:
		err = h.removeFromPlaylist(ctx, a)
	case model.SpotifyStartPlaylistAction:
		err = h.startPlaylist(ctx, a)
	case model.SpotifyQueueTrackAction:
		err = h.queueTrack(ctx, a)
	case model.SpotifyTransferPlaybackAction:
		err = h.transferPlayback(ctx, a)
	case model.SpotifyVolumeAdjustAction:
		err = h.volumeAdjust(ctx, a)
	case model.SpotifyVolumeSetAction:
		err = h.volumeSet(ctx, a)
	default:
		err = fmt.Errorf("actions: spotify handler got unexpected %T", job.action)
	}
	if err != nil {
		h.opts.logger().Warn("actions: spotify action failed", "control", job.control, "type", job.action.ActionType(), "err", err)
	}
}

func (h *SpotifyHandlers) likeToggle(ctx context.Context) error {
	track, err := h.api.CurrentlyPlaying(ctx)
	if err != nil {
		h.notifyError("Spotify", err)
		return err
	}
	contains, err := h.api.LibraryContains(ctx, track.URI)
	if err != nil {
		h.notifyError(track.Name, err)
		return err
	}
	if contains {
		if err := h.api.RemoveFromLibrary(ctx, track.URI); err != nil {
			h.notifyError(track.Name, err)
			return err
		}
		h.notify("Removed from Liked Songs", trackSummary(track))
		return nil
	}
	if err := h.api.SaveToLibrary(ctx, track.URI); err != nil {
		h.notifyError(track.Name, err)
		return err
	}
	h.notify("♥ Liked", trackSummary(track))
	return nil
}

func (h *SpotifyHandlers) addToPlaylist(ctx context.Context, a model.SpotifyAddToPlaylistAction) error {
	playlistID, err := spotify.ParseID("playlist", a.PlaylistID)
	if err != nil {
		return fmt.Errorf("actions: spotify.add_to_playlist: %w", err)
	}
	track, err := h.api.CurrentlyPlaying(ctx)
	if err != nil {
		h.notifyError("Spotify", err)
		return err
	}
	if err := h.api.AddPlaylistItems(ctx, playlistID, track.URI); err != nil {
		h.notifyError(track.Name, err)
		return err
	}
	h.notify("Added to playlist", trackSummary(track))
	return nil
}

func (h *SpotifyHandlers) removeFromPlaylist(ctx context.Context, a model.SpotifyRemoveFromPlaylistAction) error {
	playlistID, err := spotify.ParseID("playlist", a.PlaylistID)
	if err != nil {
		return fmt.Errorf("actions: spotify.remove_from_playlist: %w", err)
	}
	track, err := h.api.CurrentlyPlaying(ctx)
	if err != nil {
		h.notifyError("Spotify", err)
		return err
	}
	if err := h.api.RemovePlaylistItems(ctx, playlistID, track.URI); err != nil {
		h.notifyError(track.Name, err)
		return err
	}
	h.notify("Removed from playlist", trackSummary(track))
	return nil
}

func (h *SpotifyHandlers) startPlaylist(ctx context.Context, a model.SpotifyStartPlaylistAction) error {
	playlistID, err := spotify.ParseID("playlist", a.PlaylistID)
	if err != nil {
		return fmt.Errorf("actions: spotify.start_playlist: %w", err)
	}
	return h.api.PlayContext(ctx, spotify.URI("playlist", playlistID))
}

func (h *SpotifyHandlers) queueTrack(ctx context.Context, a model.SpotifyQueueTrackAction) error {
	trackID, err := spotify.ParseID("track", a.TrackID)
	if err != nil {
		return fmt.Errorf("actions: spotify.queue_track: %w", err)
	}
	return h.api.Queue(ctx, spotify.URI("track", trackID))
}

// transferPlayback matches a.DeviceName against GET /me/player/devices
// case-insensitively -- see model.SpotifyTransferPlaybackAction's doc
// comment on why binding by name (not id) is the only usable option.
func (h *SpotifyHandlers) transferPlayback(ctx context.Context, a model.SpotifyTransferPlaybackAction) error {
	devices, err := h.api.Devices(ctx)
	if err != nil {
		return err
	}
	for _, d := range devices {
		if strings.EqualFold(d.Name, a.DeviceName) {
			return h.api.Transfer(ctx, d.ID, a.Play)
		}
	}
	names := make([]string, len(devices))
	for i, d := range devices {
		names[i] = d.Name
	}
	return fmt.Errorf("actions: spotify.transfer_playback: no device named %q (available: %s)",
		a.DeviceName, strings.Join(names, ", "))
}

// volumeAdjust reads the active Spotify Connect device's current volume
// fresh on every call (no local cache, unlike VolumeHandlers) so a level
// changed from elsewhere -- another client, or Spotify Connect itself --
// is always the adjustment's starting point rather than a stale echo of
// this process's last write.
func (h *SpotifyHandlers) volumeAdjust(ctx context.Context, a model.SpotifyVolumeAdjustAction) error {
	current, err := h.api.CurrentVolume(ctx)
	if err != nil {
		return err
	}
	next := clampPercent(float64(current) + a.StepPercent)
	return h.api.SetVolume(ctx, next)
}

func (h *SpotifyHandlers) volumeSet(ctx context.Context, a model.SpotifyVolumeSetAction) error {
	return h.api.SetVolume(ctx, clampPercent(a.Percent))
}

func clampPercent(v float64) int {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	default:
		return int(v + 0.5)
	}
}

func (h *SpotifyHandlers) notify(summary, body string) {
	if err := h.notifier.Notify(summary, body); err != nil {
		h.opts.logger().Warn("actions: spotify notification failed", "err", err)
	}
}

func (h *SpotifyHandlers) notifyError(subject string, err error) {
	msg := userFacingSpotifyError(err)
	if nerr := h.notifier.Notify("Spotify error", fmt.Sprintf("%s: %s", subject, msg)); nerr != nil {
		h.opts.logger().Warn("actions: spotify error notification failed", "err", nerr)
	}
}

// userFacingSpotifyError renders the package sentinels as something
// worth putting in a notification bubble rather than a raw Go error
// string.
func userFacingSpotifyError(err error) string {
	switch {
	case errors.Is(err, spotify.ErrNotAuthorized):
		return "not connected (see the Spotify tab)"
	case errors.Is(err, spotify.ErrNothingPlaying):
		return "nothing is playing"
	case errors.Is(err, spotify.ErrNoActiveDevice):
		return "no active Spotify Connect device"
	default:
		return err.Error()
	}
}

func trackSummary(t spotify.CurrentTrack) string {
	if len(t.Artists) == 0 {
		return t.Name
	}
	return fmt.Sprintf("%s — %s", t.Name, strings.Join(t.Artists, ", "))
}
