package media

import (
	"context"
	"log/slog"
	"sort"
	"sync"
)

// TrackerOptions configures NewTracker.
type TrackerOptions struct {
	Logger *slog.Logger
	// OnChange, if set, fires after every Backend.Watch event Tracker
	// processes and after every Cycle -- knobd's SSE state-dirty seam
	// (api.Hub.NotifyStateDirty), mirroring actions.VolumeOptions.
	// OnApplied.
	OnChange func()
}

func (o TrackerOptions) logger() *slog.Logger {
	if o.Logger == nil {
		return slog.Default()
	}
	return o.Logger
}

// playerRecord is one tracked player plus the sequence number that
// decides "most recently active" (see Selected's doc comment).
type playerRecord struct {
	info PlayerInfo
	seq  uint64
}

// Tracker consumes a Backend's discovery/state events and answers
// "which player is currently selected" -- "most recent wins", the same
// rule playerctld uses: the selected player is whichever last either
// started Playing or was explicitly chosen via Cycle. If the selected
// player vanishes, the most recently active remaining one takes over.
//
// A player whose Ref (see PlayerInfo.Ref) is in the caller-supplied
// ignore list is invisible to Selected/Resolve/Cycle/Snapshot, though
// Tracker still tracks its state internally so un-ignoring it later
// doesn't need rediscovery.
type Tracker struct {
	log      *slog.Logger
	backend  Backend
	onChange func()

	mu       sync.Mutex
	players  map[string]*playerRecord // busName -> record
	nextSeq  uint64
	selected string // busName, "" if none selected yet
}

// NewTracker starts consuming backend.Watch(ctx) in the background,
// until ctx is canceled. backend is typically media.New's result, or
// media.Unavailable() when MPRIS startup failed -- either way Tracker
// just sees an empty or ever-silent event stream.
func NewTracker(ctx context.Context, backend Backend, opts TrackerOptions) (*Tracker, error) {
	t := &Tracker{
		log:      opts.logger(),
		backend:  backend,
		onChange: opts.OnChange,
		players:  make(map[string]*playerRecord),
	}

	events, err := backend.Watch(ctx)
	if err != nil {
		return nil, err
	}
	go t.consume(events)
	return t, nil
}

func (t *Tracker) consume(events <-chan Event) {
	for ev := range events {
		t.apply(ev)
		if t.onChange != nil {
			t.onChange()
		}
	}
}

func (t *Tracker) apply(ev Event) {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch ev.Kind {
	case PlayerVanished:
		delete(t.players, ev.Player.BusName)
		if t.selected == ev.Player.BusName {
			t.selected = ""
		}
	case PlayerAppeared, PlayerChanged:
		rec, ok := t.players[ev.Player.BusName]
		if !ok {
			// A newly-discovered player starts at seq 0 -- merely
			// appearing on the bus must not steal the selection from
			// whatever is actually playing (or was last target_cycle'd
			// to); see the type doc comment's "most recent wins" rule.
			// It's ordered among other never-activated players by name
			// (visible's tie-break), matching "players that are already
			// Playing first, then by name" for the very first batch a
			// fresh Tracker discovers via listPlayerNames.
			rec = &playerRecord{}
			t.players[ev.Player.BusName] = rec
		}
		wasPlaying := rec.info.Status == StatusPlaying
		rec.info = ev.Player
		if !wasPlaying && rec.info.Status == StatusPlaying {
			t.nextSeq++
			rec.seq = t.nextSeq
		}
	}
}

func ignored(ref string, ignore []string) bool {
	for _, r := range ignore {
		if r == ref {
			return true
		}
	}
	return false
}

// visible returns every tracked, non-ignored player, most recently
// active first.
func (t *Tracker) visible(ignore []string) []*playerRecord {
	out := make([]*playerRecord, 0, len(t.players))
	for _, rec := range t.players {
		if !ignored(rec.info.Ref(), ignore) {
			out = append(out, rec)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].seq != out[j].seq {
			return out[i].seq > out[j].seq
		}
		return out[i].info.BusName < out[j].info.BusName
	})
	return out
}

// Selected returns the currently selected player -- see the type doc
// comment for the rule. ok is false if no non-ignored player is
// currently known.
func (t *Tracker) Selected(ignore []string) (PlayerInfo, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	visible := t.visible(ignore)
	if len(visible) == 0 {
		return PlayerInfo{}, false
	}
	return visible[0].info, true
}

// Resolve returns the player matching ref (see MatchesRef), or, if ref
// is empty, the same result as Selected.
func (t *Tracker) Resolve(ref string, ignore []string) (PlayerInfo, bool) {
	if ref == "" {
		return t.Selected(ignore)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	for _, rec := range t.visible(ignore) {
		if MatchesRef(rec.info.BusName, ref) {
			return rec.info, true
		}
	}
	return PlayerInfo{}, false
}

// Cycle advances the selection to the next visible player after the
// currently selected one (wrapping), in the same most-recently-active
// order Selected reports, and returns it. If nothing is currently
// selected, it selects the most recently active one (same as Selected).
// ok is false if no non-ignored player is currently known.
func (t *Tracker) Cycle(ignore []string) (PlayerInfo, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	visible := t.visible(ignore)
	if len(visible) == 0 {
		return PlayerInfo{}, false
	}

	// curIdx defaults to 0 (the implicit current selection when nothing
	// has been Cycled yet is whatever Selected would report, i.e.
	// visible[0]) so the first Cycle call always advances to the next
	// player rather than re-picking the one already selected.
	curIdx := 0
	if t.selected != "" {
		for i, rec := range visible {
			if rec.info.BusName == t.selected {
				curIdx = i
				break
			}
		}
	}
	next := visible[(curIdx+1)%len(visible)]
	t.nextSeq++
	next.seq = t.nextSeq
	t.selected = next.info.BusName
	return next.info, true
}

// Snapshot returns every currently tracked, non-ignored player plus
// which one (if any) is selected, for api.State.Media.
func (t *Tracker) Snapshot(ignore []string) (players []PlayerInfo, selected string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	visible := t.visible(ignore)
	players = make([]PlayerInfo, len(visible))
	for i, rec := range visible {
		players[i] = rec.info
	}
	if len(visible) > 0 {
		selected = visible[0].info.BusName
	}
	return players, selected
}
