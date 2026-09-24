package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/model"
)

// MixOptions configures MixHandlers. The zero value is sane defaults.
type MixOptions struct {
	// Logger receives clamp/skip diagnostics. Nil means slog.Default().
	Logger *slog.Logger
}

func (o MixOptions) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

// soloSession is the one currently-active solo (see MixHandlers'
// doc comment: only one can be active at a time). prior holds every
// ref solo touched -- both the muted "everything else" and the
// unmuted target -- so a second press can restore each one exactly,
// not just unmute everything.
type soloSession struct {
	targetRefs map[audio.Ref]bool
	prior      map[audio.Ref]bool
}

// duckSession is one control's currently-active duck-while-held. prior
// holds only the refs it actually lowered (a ref already at or below
// DuckPercent when the hold started is left alone and so isn't
// recorded here -- see executeDuck).
type duckSession struct {
	prior map[audio.Ref]float64
}

// MixHandlers implements model.ActionAudioSoloToggle/
// ActionAudioDuckHold. Both read/write through VolumeHandlers' shared
// cache, so a mix change gets the same LED feedback as any other
// volume/mute write.
//
// Session state (currentSolo/ducks) is touched only from the
// dispatcher goroutine that calls Registry.Execute (see
// specs/milestones/M04-mapping-engine-daemon.md's Architecture
// section) -- the same single-goroutine-serialization argument
// VolumeHandlers' own cache mutex exists to guard against instead
// (concurrent ObserveState calls from the run goroutine), which
// doesn't apply here since nothing else ever touches this state.
type MixHandlers struct {
	vol  *VolumeHandlers
	opts MixOptions

	currentSolo *soloSession
	ducks       map[model.Control]*duckSession
}

// NewMixHandlers returns handlers that read/write volume levels through
// vol's shared cache.
func NewMixHandlers(vol *VolumeHandlers, opts MixOptions) *MixHandlers {
	return &MixHandlers{vol: vol, opts: opts, ducks: make(map[model.Control]*duckSession)}
}

// Register wires model.ActionAudioSoloToggle/ActionAudioDuckHold into r.
func (h *MixHandlers) Register(r *Registry) {
	r.Register(model.ActionAudioSoloToggle, HandlerFunc(h.executeSolo))
	r.Register(model.ActionAudioDuckHold, HandlerFunc(h.executeDuck))
}

// refSetEqual reports whether a and b name exactly the same set of
// refs, order-independent -- how executeSolo tells "this press is
// toggling the same solo off" from "this press wants a different
// target soloed".
func refSetEqual(a, b []audio.Ref) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[audio.Ref]bool, len(a))
	for _, r := range a {
		set[r] = true
	}
	for _, r := range b {
		if !set[r] {
			return false
		}
	}
	return true
}

// executeSolo implements the toggle: pressing a target already soloed
// restores every ref solo touched to its exact prior mute state (not
// just "unmute everything"); pressing a different target restores the
// old solo first, then solos the new one. inv.Refs/inv.Others are
// engine's dispatch-time resolution (see Invocation's doc comment) --
// this never returns early on an empty inv.Refs, since toggling a solo
// off must still work even if the target app has since exited.
func (h *MixHandlers) executeSolo(ctx context.Context, inv Invocation) error {
	if _, ok := inv.Action.(model.AudioSoloToggleAction); !ok {
		return fmt.Errorf("actions: audio.solo_toggle got %T", inv.Action)
	}

	var errs []error
	if h.currentSolo != nil {
		wasSameTarget := refSetEqual(mapKeys(h.currentSolo.targetRefs), inv.Refs)
		if err := h.restoreSolo(ctx, h.currentSolo); err != nil {
			errs = append(errs, err)
		}
		h.currentSolo = nil
		if wasSameTarget {
			return errors.Join(errs...)
		}
	}

	if len(inv.Refs) == 0 {
		h.opts.logger().Debug("actions: audio.solo_toggle: target resolved to nothing; nothing to solo", "control", inv.Control)
		return errors.Join(errs...)
	}

	targetRefs := make(map[audio.Ref]bool, len(inv.Refs))
	for _, ref := range inv.Refs {
		targetRefs[ref] = true
	}
	prior := make(map[audio.Ref]bool, len(inv.Refs)+len(inv.Others))
	for _, ref := range inv.Others {
		st, err := h.vol.getCached(ctx, ref)
		if err != nil {
			errs = append(errs, fmt.Errorf("actions: audio.solo_toggle: get mute state for %+v: %w", ref, err))
			continue
		}
		prior[ref] = st.Muted
		if !st.Muted {
			if err := h.vol.writeMute(ctx, ref, true); err != nil {
				errs = append(errs, err)
			}
		}
	}
	for _, ref := range inv.Refs {
		st, err := h.vol.getCached(ctx, ref)
		if err != nil {
			errs = append(errs, fmt.Errorf("actions: audio.solo_toggle: get mute state for %+v: %w", ref, err))
			continue
		}
		prior[ref] = st.Muted
		if st.Muted {
			if err := h.vol.writeMute(ctx, ref, false); err != nil {
				errs = append(errs, err)
			}
		}
	}
	h.currentSolo = &soloSession{targetRefs: targetRefs, prior: prior}
	return errors.Join(errs...)
}

// restoreSolo restores every ref s touched to its recorded prior mute
// state. A ref that no longer resolves to anything real (its stream
// exited while solo was active) is skipped via getCached's own error,
// not treated as a failure -- there's nothing left to restore.
func (h *MixHandlers) restoreSolo(ctx context.Context, s *soloSession) error {
	var errs []error
	for ref, wasMuted := range s.prior {
		st, err := h.vol.getCached(ctx, ref)
		if err != nil {
			continue
		}
		if st.Muted == wasMuted {
			continue
		}
		if err := h.vol.writeMute(ctx, ref, wasMuted); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// mapKeys returns m's keys as a slice, for feeding refSetEqual.
func mapKeys(m map[audio.Ref]bool) []audio.Ref {
	out := make([]audio.Ref, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// executeDuck implements GestureHold (start ducking) / GestureRelease
// (restore) -- see model.AudioDuckHoldAction's doc comment. Sessions
// are keyed by Control so several duck bindings on different controls
// never interfere with each other.
func (h *MixHandlers) executeDuck(ctx context.Context, inv Invocation) error {
	a, ok := inv.Action.(model.AudioDuckHoldAction)
	if !ok {
		return fmt.Errorf("actions: audio.duck_hold got %T", inv.Action)
	}

	switch inv.Gesture {
	case model.GestureHold:
		if _, active := h.ducks[inv.Control]; active {
			// Already ducking (e.g. a duplicate Hold with no Release in
			// between) -- a no-op, not a second, independent duck.
			return nil
		}
		duckPercent := h.vol.clamp(a.DuckPercent, "audio.duck_hold")
		prior := make(map[audio.Ref]float64, len(inv.Others))
		var errs []error
		for _, ref := range inv.Others {
			st, err := h.vol.getCached(ctx, ref)
			if err != nil {
				errs = append(errs, fmt.Errorf("actions: audio.duck_hold: get level for %+v: %w", ref, err))
				continue
			}
			if st.Percent <= duckPercent {
				// Already quieter than the duck level -- leave it alone
				// so releasing never raises it.
				continue
			}
			prior[ref] = st.Percent
			if err := h.vol.writeVolume(ctx, ref, duckPercent); err != nil {
				errs = append(errs, err)
			}
		}
		h.ducks[inv.Control] = &duckSession{prior: prior}
		return errors.Join(errs...)

	case model.GestureRelease:
		session, active := h.ducks[inv.Control]
		if !active {
			return nil
		}
		delete(h.ducks, inv.Control)
		var errs []error
		for ref, percent := range session.prior {
			if err := h.vol.writeVolume(ctx, ref, percent); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)

	default:
		return fmt.Errorf("actions: audio.duck_hold got unexpected gesture %q", inv.Gesture)
	}
}
