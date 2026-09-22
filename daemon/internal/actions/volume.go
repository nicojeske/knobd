package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/model"
)

// cachedLevel is one Ref's last-known volume/mute state, plus a count of
// writes this process has issued for it that haven't been confirmed yet.
type cachedLevel struct {
	state    audio.VolumeState
	known    bool
	inFlight int
}

// VolumeOptions configures NewVolumeHandlers. The zero value is sane
// defaults.
type VolumeOptions struct {
	// MaxPercent caps every write; <= 0 means audio.DefaultMaxPercent.
	MaxPercent float64
	// Logger receives clamp/error diagnostics. Nil means slog.Default().
	Logger *slog.Logger
	// OnApplied, if set, is called after every successful volume write
	// or mute change with the ref and the state actually applied --
	// M05's LED feedback seam, wired to the local echo of a write knobd
	// just made itself, in case driving the ring/button off
	// audio.Backend.Subscribe's own echo turns out to have visible
	// latency. Called from writeVolume, ensureUnmuted (an implicit
	// un-mute), and executeMuteToggle -- every place this file commits a
	// new VolumeState to the cache.
	OnApplied func(audio.Ref, audio.VolumeState)
}

func (o VolumeOptions) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

func (o VolumeOptions) maxPercent() float64 {
	if o.MaxPercent > 0 {
		return o.MaxPercent
	}
	return audio.DefaultMaxPercent
}

// VolumeHandlers is the volume action family: the model.ActionVolume*
// handlers plus the level cache they share.
//
// Read-modify-write serialization -- which audio.Backend's doc comment
// explicitly says it does not provide -- comes from the engine calling
// Execute for one ref at a time from a single dispatch goroutine (see
// specs/milestones/M04-mapping-engine-daemon.md); the mutex here guards
// only the cache itself, against the engine's concurrent ObserveState
// calls carrying externally-observed changes (e.g. from pavucontrol).
type VolumeHandlers struct {
	backend audio.Backend
	opts    VolumeOptions

	mu    sync.Mutex
	cache map[audio.Ref]*cachedLevel
}

// NewVolumeHandlers returns handlers that read/write through backend.
func NewVolumeHandlers(backend audio.Backend, opts VolumeOptions) *VolumeHandlers {
	return &VolumeHandlers{backend: backend, opts: opts, cache: make(map[audio.Ref]*cachedLevel)}
}

// Register wires every model.ActionVolume* type this file implements
// into r. model.ActionVolumeBalance is deliberately not registered:
// audio.Backend has no per-channel volume write (VolumeState.Channels is
// read-only) and stereo balance is not a planned feature -- see
// specs/reference/action-catalog.md. Registry.Execute's existing "no
// handler registered" error is the correct outcome for it.
func (v *VolumeHandlers) Register(r *Registry) {
	r.Register(model.ActionVolumeAdjust, HandlerFunc(v.executeAdjust))
	r.Register(model.ActionVolumeSet, HandlerFunc(v.executeSet))
	r.Register(model.ActionVolumeMuteToggle, HandlerFunc(v.executeMuteToggle))
	r.Register(model.ActionVolumeFollow, HandlerFunc(v.executeFollow))
}

// ObserveState folds an out-of-band volume/mute reading -- the engine's
// audio.Subscribe feed, which carries State for free on every change
// event -- into the cache, unless a write this process issued for ref is
// still outstanding. Without that guard, a late echo of our own write
// (carrying the pre-write value) would ratchet the cache backwards and
// the next detent would silently undo the previous one.
func (v *VolumeHandlers) ObserveState(ref audio.Ref, st audio.VolumeState) {
	v.mu.Lock()
	defer v.mu.Unlock()
	c, ok := v.cache[ref]
	if ok && c.inFlight > 0 {
		return
	}
	if !ok {
		c = &cachedLevel{}
		v.cache[ref] = c
	}
	c.state = st
	c.known = true
}

// CachedLevel returns ref's last-known volume/mute state without a
// backend round trip, for a caller (engine's GET /state snapshot) that
// wants a cheap best-effort read rather than a guaranteed-fresh one. ok
// is false if nothing has been observed or written for ref yet.
func (v *VolumeHandlers) CachedLevel(ref audio.Ref) (audio.VolumeState, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	c, ok := v.cache[ref]
	if !ok || !c.known {
		return audio.VolumeState{}, false
	}
	return c.state, true
}

func (v *VolumeHandlers) setCacheLocked(ref audio.Ref, st audio.VolumeState) {
	c, ok := v.cache[ref]
	if !ok {
		c = &cachedLevel{}
		v.cache[ref] = c
	}
	c.state = st
	c.known = true
}

// getCached returns ref's cached level, seeding the cache with one
// GetVolume round trip the first time ref is seen.
func (v *VolumeHandlers) getCached(ctx context.Context, ref audio.Ref) (audio.VolumeState, error) {
	v.mu.Lock()
	if c, ok := v.cache[ref]; ok && c.known {
		st := c.state
		v.mu.Unlock()
		return st, nil
	}
	v.mu.Unlock()

	st, err := v.backend.GetVolume(ctx, ref)
	if err != nil {
		return audio.VolumeState{}, err
	}
	v.mu.Lock()
	v.setCacheLocked(ref, st)
	v.mu.Unlock()
	return st, nil
}

// ensureUnmuted un-mutes ref if current says it is muted. Turning a knob
// on a muted target and hearing nothing is a dead end -- this is what
// makes adjust/set/follow match KDE's behavior rather than pavucontrol's
// (see specs/milestones/M04-mapping-engine-daemon.md's decisions table).
//
// Commits the new Muted=false state to the cache and fires OnApplied
// itself, rather than leaving that to the writeVolume call that follows
// every caller here: if that write then fails, the mute state has still
// actually changed on the backend, and a caller relying on the cache
// (or a button LED relying on OnApplied) must not be left believing the
// target is still muted.
func (v *VolumeHandlers) ensureUnmuted(ctx context.Context, ref audio.Ref, current audio.VolumeState) error {
	if !current.Muted {
		return nil
	}
	if err := v.backend.SetMute(ctx, ref, false); err != nil {
		return fmt.Errorf("actions: un-mute %+v: %w", ref, err)
	}
	st := current
	st.Muted = false
	v.mu.Lock()
	v.setCacheLocked(ref, st)
	v.mu.Unlock()
	if v.opts.OnApplied != nil {
		v.opts.OnApplied(ref, st)
	}
	return nil
}

// writeVolume issues one SetVolume for ref, serialized against
// ObserveState via the in-flight counter, and commits the result to the
// cache on success. Every caller here writes with muted=false, since
// they all call ensureUnmuted first.
func (v *VolumeHandlers) writeVolume(ctx context.Context, ref audio.Ref, percent float64) error {
	v.mu.Lock()
	c, ok := v.cache[ref]
	if !ok {
		c = &cachedLevel{}
		v.cache[ref] = c
	}
	c.inFlight++
	v.mu.Unlock()

	err := v.backend.SetVolume(ctx, ref, percent)

	v.mu.Lock()
	if c.inFlight > 0 {
		c.inFlight--
	}
	if err == nil {
		c.state = audio.VolumeState{Percent: percent, Muted: false, Channels: c.state.Channels}
		c.known = true
	}
	v.mu.Unlock()

	if err != nil {
		return fmt.Errorf("actions: set volume for %+v: %w", ref, err)
	}
	if v.opts.OnApplied != nil {
		v.opts.OnApplied(ref, audio.VolumeState{Percent: percent, Muted: false})
	}
	return nil
}

func (v *VolumeHandlers) executeAdjust(ctx context.Context, inv Invocation) error {
	a, ok := inv.Action.(model.VolumeAdjustAction)
	if !ok {
		return fmt.Errorf("actions: volume.adjust got %T", inv.Action)
	}
	if inv.Delta == 0 || len(inv.Refs) == 0 {
		return nil
	}
	step := a.StepPercent * float64(inv.Delta)
	curve := audio.Curve{Exponent: a.CurveExponent, MaxPercent: v.opts.maxPercent()}

	var errs []error
	// Each ref is stepped independently rather than computing one shared
	// target percent: an app's several streams (see audio's package doc
	// comment) may legitimately sit at different levels, and applying
	// the same step to each preserves their relative balance.
	for _, ref := range inv.Refs {
		current, err := v.getCached(ctx, ref)
		if err != nil {
			errs = append(errs, fmt.Errorf("actions: get volume for %+v: %w", ref, err))
			continue
		}
		if err := v.ensureUnmuted(ctx, ref, current); err != nil {
			errs = append(errs, err)
			continue
		}
		next := curve.Adjust(current.Percent, step)
		if err := v.writeVolume(ctx, ref, next); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (v *VolumeHandlers) executeSet(ctx context.Context, inv Invocation) error {
	a, ok := inv.Action.(model.VolumeSetAction)
	if !ok {
		return fmt.Errorf("actions: volume.set got %T", inv.Action)
	}
	if len(inv.Refs) == 0 {
		return nil
	}
	percent := v.clamp(a.Percent, "volume.set")

	var errs []error
	for _, ref := range inv.Refs {
		current, err := v.getCached(ctx, ref)
		if err != nil {
			errs = append(errs, fmt.Errorf("actions: get volume for %+v: %w", ref, err))
			continue
		}
		if err := v.ensureUnmuted(ctx, ref, current); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := v.writeVolume(ctx, ref, percent); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (v *VolumeHandlers) executeFollow(ctx context.Context, inv Invocation) error {
	a, ok := inv.Action.(model.VolumeFollowAction)
	if !ok {
		return fmt.Errorf("actions: volume.follow got %T", inv.Action)
	}
	if len(inv.Refs) == 0 {
		return nil
	}
	min, max := a.MinPercent, a.MaxPercent
	if min == 0 && max == 0 {
		max = 100
	}
	value := inv.Value
	if value < 0 {
		value = 0
	} else if value > 127 {
		value = 127
	}
	// No curve here, deliberately: a fader is a physical position, and a
	// response curve between position and level is exactly what would
	// make a motorless fader feel disconnected from where it's sitting.
	percent := min + (max-min)*float64(value)/127

	var errs []error
	for _, ref := range inv.Refs {
		current, err := v.getCached(ctx, ref)
		if err != nil {
			errs = append(errs, fmt.Errorf("actions: get volume for %+v: %w", ref, err))
			continue
		}
		if err := v.ensureUnmuted(ctx, ref, current); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := v.writeVolume(ctx, ref, percent); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// executeMuteToggle implements the multi-stream disagreement rule: if
// any ref is currently unmuted, mute all of them; only if every ref is
// already muted does it unmute all of them. Mute is a safety action --
// "silence this" rather than "flip each stream" -- so this converges
// instead of oscillating a multi-stream app's streams out of phase with
// each other.
func (v *VolumeHandlers) executeMuteToggle(ctx context.Context, inv Invocation) error {
	if _, ok := inv.Action.(model.VolumeMuteToggleAction); !ok {
		return fmt.Errorf("actions: volume.mute_toggle got %T", inv.Action)
	}
	if len(inv.Refs) == 0 {
		return nil
	}

	states := make(map[audio.Ref]audio.VolumeState, len(inv.Refs))
	anyUnmuted := false
	for _, ref := range inv.Refs {
		st, err := v.getCached(ctx, ref)
		if err != nil {
			return fmt.Errorf("actions: get mute state for %+v: %w", ref, err)
		}
		states[ref] = st
		if !st.Muted {
			anyUnmuted = true
		}
	}
	target := anyUnmuted

	var errs []error
	for _, ref := range inv.Refs {
		if states[ref].Muted == target {
			continue
		}
		if err := v.backend.SetMute(ctx, ref, target); err != nil {
			errs = append(errs, fmt.Errorf("actions: set mute for %+v: %w", ref, err))
			continue
		}
		st := states[ref]
		st.Muted = target
		v.mu.Lock()
		v.setCacheLocked(ref, st)
		v.mu.Unlock()
		if v.opts.OnApplied != nil {
			v.opts.OnApplied(ref, st)
		}
	}
	return errors.Join(errs...)
}

func (v *VolumeHandlers) clamp(percent float64, action string) float64 {
	max := v.opts.maxPercent()
	switch {
	case percent < 0:
		v.opts.logger().Warn("clamping negative percent to 0", "action", action, "percent", percent)
		return 0
	case percent > max:
		v.opts.logger().Warn("clamping percent to max", "action", action, "percent", percent, "max", max)
		return max
	default:
		return percent
	}
}
