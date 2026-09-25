package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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

// spotifyVolumeCacheTTL bounds how long volumeAdjust trusts its cached
// baseline before re-fetching CurrentVolume: long enough that a burst of
// knob turns (the case that actually matters for feel) never re-reads,
// short enough that the level a phone or another client changed while
// the knob sat untouched isn't stale for long once it's picked up again.
const spotifyVolumeCacheTTL = 60 * time.Second

// SpotifyOptions configures SpotifyHandlers. The zero value is sane
// defaults.
type SpotifyOptions struct {
	Logger *slog.Logger
	// Timeout bounds each queued job's context; zero means 10s.
	Timeout time.Duration
	// OnVolumeApplied, if set, is called with the new cached percent
	// every time volumeAdjust/volumeSet update it -- optimistically,
	// before the corresponding Web API write actually completes (see
	// publishVolume's doc comment) -- so the ring can update the instant
	// a knob turn is processed rather than waiting on a network round
	// trip. Mirrors VolumeOptions.OnApplied's role for local audio.
	OnVolumeApplied func(percent int)
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
// needs from it). delta carries inv.Delta -- the signed detent count for
// a GestureTurn firing (see Invocation's doc comment) -- since
// volumeAdjust needs it to know which way the knob turned; every other
// action this handler set processes ignores it.
type spotifyJob struct {
	action  model.Action
	control model.Control
	delta   int
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

	// volMu guards the volume cache below -- read/written from runJob's
	// goroutine (volumeAdjust/volumeSet) and from CachedVolumePercent
	// (engine's run goroutine, via SetSpotifySource), so unlike the rest
	// of this type's state it needs a real mutex rather than relying on
	// runJob's own serialization.
	volMu       sync.Mutex
	volKnown    bool
	volPercent  int
	volCachedAt time.Time
	// volPending is a single-slot, latest-wins handoff to
	// runVolumeWriter: only the most recent percent a knob turn settled
	// on is worth actually sending to Spotify, since every earlier one
	// in a fast burst is already obsolete by the time it would go out
	// (see runVolumeWriter's doc comment).
	volPending chan int
}

// NewSpotifyHandlers builds a SpotifyHandlers. notifier is used for
// like_toggle/add_to_playlist/remove_from_playlist's result
// notifications (see specs/milestones/M10-spotify-web-api.md's Design
// decision to notify on those three, but not
// start_playlist/queue_track/transfer_playback).
func NewSpotifyHandlers(api SpotifyAPI, notifier media.Notifier, opts SpotifyOptions) *SpotifyHandlers {
	return &SpotifyHandlers{
		api:        api,
		notifier:   notifier,
		opts:       opts,
		jobs:       make(chan spotifyJob, spotifyQueueDepth),
		volPending: make(chan int, 1),
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
	job := spotifyJob{action: inv.Action, control: inv.Control, delta: inv.Delta}
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
	go h.runVolumeWriter(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-h.jobs:
			h.runJob(ctx, job)
		}
	}
}

// runVolumeWriter drains volPending and issues the actual
// PUT /me/player/volume for each value, one at a time. It runs
// independently of the main job queue specifically so a burst of
// spotify.volume_adjust turns never queues up multiple real HTTP round
// trips behind each other -- volumeAdjust/volumeSet only ever update the
// cache and hand off the latest target here, returning to runJob
// immediately (see spotifyQueueDepth's doc comment on why runJob itself
// must never block on the network). If a new value arrives while a
// write is still in flight, it simply waits in the size-1 volPending
// slot and is picked up as soon as the current write finishes -- any
// value that arrives after that point silently supersedes it.
func (h *SpotifyHandlers) runVolumeWriter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case pct := <-h.volPending:
			writeCtx, cancel := context.WithTimeout(ctx, h.opts.timeout())
			err := h.api.SetVolume(writeCtx, pct)
			cancel()
			if err != nil {
				h.opts.logger().Warn("actions: spotify volume write failed", "percent", pct, "err", err)
			}
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
		err = h.volumeAdjust(ctx, a, job.delta)
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

// volumeAdjust adjusts the cached baseline by a.StepPercent scaled by
// delta -- the signed detent count for the GestureTurn that produced
// this job (see spotifyJob's doc comment) -- and hands the result to
// publishVolume. It never itself waits on the PUT /me/player/volume
// round trip (see runVolumeWriter), only (occasionally) on the
// GET /me/player that seeds/refreshes the cache. The baseline is
// re-fetched when unknown or stale (see spotifyVolumeCacheTTL) rather
// than on every call, unlike CurrentlyPlaying-backed actions
// (like_toggle etc.), which always read fresh -- a knob spun quickly
// needs each detent to feel instant far more than it needs to react to a
// volume change made from elsewhere mid-spin.
func (h *SpotifyHandlers) volumeAdjust(ctx context.Context, a model.SpotifyVolumeAdjustAction, delta int) error {
	if delta == 0 {
		return nil
	}
	current, ok := h.cachedOrFetchVolume(ctx)
	if !ok {
		fresh, err := h.api.CurrentVolume(ctx)
		if err != nil {
			return err
		}
		current = fresh
	}
	h.publishVolume(clampPercent(float64(current) + a.StepPercent*float64(delta)))
	return nil
}

func (h *SpotifyHandlers) volumeSet(ctx context.Context, a model.SpotifyVolumeSetAction) error {
	h.publishVolume(clampPercent(a.Percent))
	return nil
}

// cachedOrFetchVolume returns the cached percent if known and not older
// than spotifyVolumeCacheTTL; ok is false if volumeAdjust must fall back
// to a fresh CurrentVolume call.
func (h *SpotifyHandlers) cachedOrFetchVolume(_ context.Context) (int, bool) {
	h.volMu.Lock()
	defer h.volMu.Unlock()
	if h.volKnown && time.Since(h.volCachedAt) < spotifyVolumeCacheTTL {
		return h.volPercent, true
	}
	return 0, false
}

// publishVolume commits percent to the cache, fires OnVolumeApplied
// (the ring's update seam) immediately, and hands percent to
// runVolumeWriter as the latest write target -- optimistically, ahead
// of that write actually succeeding. If the write later fails (no
// active device, a network error), the cache and the ring stay on a
// value Spotify never actually applied until the next adjust re-fetches
// past spotifyVolumeCacheTTL; this trades a rare, self-correcting
// mismatch for every knob turn feeling as immediate as a local one.
func (h *SpotifyHandlers) publishVolume(percent int) {
	h.volMu.Lock()
	h.volKnown = true
	h.volPercent = percent
	h.volCachedAt = time.Now()
	h.volMu.Unlock()

	if h.opts.OnVolumeApplied != nil {
		h.opts.OnVolumeApplied(percent)
	}

	for {
		select {
		case h.volPending <- percent:
			return
		default:
		}
		select {
		case <-h.volPending:
		default:
		}
	}
}

// CachedVolumePercent implements engine.SpotifyVolumeObserver: a cheap,
// non-blocking read of the last percent volumeAdjust/volumeSet
// committed, for the ring. ok is false until the first adjust/set call
// (nothing has ever been cached yet).
func (h *SpotifyHandlers) CachedVolumePercent() (float64, bool) {
	h.volMu.Lock()
	defer h.volMu.Unlock()
	if !h.volKnown {
		return 0, false
	}
	return float64(h.volPercent), true
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
