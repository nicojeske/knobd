package actions

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/njeske/knobd/internal/media"
	"github.com/njeske/knobd/internal/model"
)

// MediaPlayers is the slice of media.Tracker this handler needs --
// point-of-use, so actions never imports the concrete *media.Tracker
// type's full surface.
type MediaPlayers interface {
	Resolve(ref string, ignore []string) (media.PlayerInfo, bool)
	Cycle(ignore []string) (media.PlayerInfo, bool)
}

// MediaCommands is the slice of media.Backend this handler needs to
// send commands -- discovery (Watch) belongs to media.Tracker, not
// here.
type MediaCommands interface {
	PlayPause(busName string) error
	Next(busName string) error
	Previous(busName string) error
	Seek(busName string, offset time.Duration) error
	SetShuffle(busName string, shuffle bool) error
	SetLoopStatus(busName string, status string) error
}

// MediaOptions configures MediaHandlers. The zero value is sane
// defaults.
type MediaOptions struct {
	// Logger receives per-command diagnostics (unsupported command, no
	// matching player). Nil means slog.Default().
	Logger *slog.Logger
	// IgnorePlayers is read fresh on every dispatch (Config.Media.
	// IgnorePlayers, via cmd/knobd's ConfigMutator) rather than cached,
	// so an edit through the UI's Media tab applies immediately with no
	// separate push path.
	IgnorePlayers func() []string
}

func (o MediaOptions) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

func (o MediaOptions) ignorePlayers() []string {
	if o.IgnorePlayers == nil {
		return nil
	}
	return o.IgnorePlayers()
}

// repeatCycleOrder is the fixed LoopStatus rotation
// model.MediaRepeatCycle advances through, per the MPRIS spec's three
// valid values.
var repeatCycleOrder = []string{"None", "Track", "Playlist"}

// MediaHandlers implements model.ActionMediaTransport/ActionMediaSeek/
// ActionMediaTargetCycle/ActionMediaNowPlaying (see
// specs/milestones/M09-media-transport-mpris.md). Every command is
// resolved against players (a media.Tracker's cached state), never a
// live D-Bus property read -- see internal/media's package doc comment
// for why.
type MediaHandlers struct {
	players  MediaPlayers
	backend  MediaCommands
	notifier media.Notifier
	opts     MediaOptions
}

// NewMediaHandlers builds a MediaHandlers. notifier is used only by
// media.now_playing; pass media.Unavailable-backed values (or a nil-safe
// stand-in) if MPRIS/notifications aren't available -- see cmd/knobd's
// wiring, which always provides a real or Unavailable() backend/notifier
// rather than a nil one.
func NewMediaHandlers(players MediaPlayers, backend MediaCommands, notifier media.Notifier, opts MediaOptions) *MediaHandlers {
	return &MediaHandlers{players: players, backend: backend, notifier: notifier, opts: opts}
}

// Register implements the handler-set pattern shared with
// VolumeHandlers/SceneHandlers/MixHandlers.
func (h *MediaHandlers) Register(r *Registry) {
	r.Register(model.ActionMediaTransport, HandlerFunc(h.executeTransport))
	r.Register(model.ActionMediaSeek, HandlerFunc(h.executeSeek))
	r.Register(model.ActionMediaTargetCycle, HandlerFunc(h.executeTargetCycle))
	r.Register(model.ActionMediaNowPlaying, HandlerFunc(h.executeNowPlaying))
}

// resolve looks up ref (PlayerRef, possibly empty) against the
// currently known, non-ignored players, returning a legible error
// naming ref if nothing matches.
func (h *MediaHandlers) resolve(ref string) (media.PlayerInfo, error) {
	p, ok := h.players.Resolve(ref, h.opts.ignorePlayers())
	if !ok {
		if ref == "" {
			return media.PlayerInfo{}, fmt.Errorf("actions: %w", media.ErrNoPlayer)
		}
		return media.PlayerInfo{}, fmt.Errorf("actions: %w: %q", media.ErrNoPlayer, ref)
	}
	return p, nil
}

func (h *MediaHandlers) executeTransport(ctx context.Context, inv Invocation) error {
	a, ok := inv.Action.(model.MediaTransportAction)
	if !ok {
		return fmt.Errorf("actions: media.transport handler got %T", inv.Action)
	}
	p, err := h.resolve(a.PlayerRef)
	if err != nil {
		h.opts.logger().Warn("actions: media.transport: no player", "control", inv.Control, "command", a.Command, "err", err)
		return err
	}

	switch a.Command {
	case model.MediaPlayPause:
		if !p.CanControl {
			return h.unsupported(p, "play/pause")
		}
		return h.backend.PlayPause(p.BusName)
	case model.MediaNext:
		if !p.CanControl {
			return h.unsupported(p, "next")
		}
		return h.backend.Next(p.BusName)
	case model.MediaPrevious:
		if !p.CanControl {
			return h.unsupported(p, "previous")
		}
		return h.backend.Previous(p.BusName)
	case model.MediaShuffleToggle:
		if p.Shuffle == nil {
			return h.unsupported(p, "shuffle")
		}
		return h.backend.SetShuffle(p.BusName, !*p.Shuffle)
	case model.MediaRepeatCycle:
		if p.LoopStatus == "" {
			return h.unsupported(p, "repeat")
		}
		return h.backend.SetLoopStatus(p.BusName, nextRepeatStatus(p.LoopStatus))
	default:
		return fmt.Errorf("actions: media.transport: unknown command %q", a.Command)
	}
}

// nextRepeatStatus advances status through repeatCycleOrder, treating
// any value it doesn't recognize (a player-specific extension) as if it
// were "None".
func nextRepeatStatus(status string) string {
	for i, s := range repeatCycleOrder {
		if s == status {
			return repeatCycleOrder[(i+1)%len(repeatCycleOrder)]
		}
	}
	return repeatCycleOrder[0]
}

func (h *MediaHandlers) unsupported(p media.PlayerInfo, what string) error {
	err := fmt.Errorf("actions: %w: %s does not support %s", media.ErrUnsupported, p.Identity, what)
	h.opts.logger().Warn("actions: media command unsupported", "player", p.BusName, "command", what)
	return err
}

// executeSeek implements model.ActionMediaSeek, fired on GestureTurn.
// Offset scales SeekMs by inv.Delta -- see Invocation.Delta's doc
// comment: a fast spin coalesces several detents into one dispatch with
// Delta > 1, and dispatch.go's mergeable/merge (M09 addition) sums Delta
// across coalesced media.seek firings the same way it already does for
// volume.adjust, so a fast spin still seeks smoothly rather than in
// coarse jumps.
func (h *MediaHandlers) executeSeek(ctx context.Context, inv Invocation) error {
	a, ok := inv.Action.(model.MediaSeekAction)
	if !ok {
		return fmt.Errorf("actions: media.seek handler got %T", inv.Action)
	}
	p, err := h.resolve(a.PlayerRef)
	if err != nil {
		h.opts.logger().Warn("actions: media.seek: no player", "control", inv.Control, "err", err)
		return err
	}
	if !p.CanSeek {
		return h.unsupported(p, "seek")
	}
	offset := time.Duration(a.SeekMs*int64(inv.Delta)) * time.Millisecond
	return h.backend.Seek(p.BusName, offset)
}

// executeTargetCycle implements model.ActionMediaTargetCycle.
func (h *MediaHandlers) executeTargetCycle(ctx context.Context, inv Invocation) error {
	if _, ok := inv.Action.(model.MediaTargetCycleAction); !ok {
		return fmt.Errorf("actions: media.target_cycle handler got %T", inv.Action)
	}
	p, ok := h.players.Cycle(h.opts.ignorePlayers())
	if !ok {
		err := fmt.Errorf("actions: %w", media.ErrNoPlayer)
		h.opts.logger().Warn("actions: media.target_cycle: no player", "control", inv.Control, "err", err)
		return err
	}
	h.opts.logger().Info("actions: media.target_cycle selected", "player", p.BusName, "identity", p.Identity)
	return nil
}

// executeNowPlaying implements model.ActionMediaNowPlaying: a desktop
// notification naming the resolved player's current track.
func (h *MediaHandlers) executeNowPlaying(ctx context.Context, inv Invocation) error {
	a, ok := inv.Action.(model.MediaNowPlayingAction)
	if !ok {
		return fmt.Errorf("actions: media.now_playing handler got %T", inv.Action)
	}
	p, err := h.resolve(a.PlayerRef)
	if err != nil {
		h.opts.logger().Warn("actions: media.now_playing: no player", "control", inv.Control, "err", err)
		return err
	}

	summary := p.Track.Title
	if summary == "" {
		summary = "Nothing playing"
	}
	var bodyParts []string
	if len(p.Track.Artists) > 0 {
		bodyParts = append(bodyParts, strings.Join(p.Track.Artists, ", "))
	}
	if p.Track.Album != "" {
		bodyParts = append(bodyParts, p.Track.Album)
	}
	body := strings.Join(bodyParts, " — ")
	if p.Identity != "" {
		if body != "" {
			body += " "
		}
		body += fmt.Sprintf("(%s)", p.Identity)
	}

	return h.notifier.Notify(summary, body)
}
