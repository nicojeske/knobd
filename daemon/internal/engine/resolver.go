package engine

import (
	"fmt"
	"log/slog"

	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/focus"
	"github.com/njeske/knobd/internal/model"
	"github.com/njeske/knobd/internal/proctree"
)

// resolver turns a model.Target into the live audio.Refs it currently
// names. It lives in engine, not audio: resolving app/group/focused
// targets needs the config's AppMatchers and the currently-focused
// application, neither of which belongs in audio (which deliberately
// keeps audio.Resolve/audio.ResolveFocused pure functions over a
// matcher/request and a stream slice, testable with no I/O — see
// specs/milestones/M03-audio-control.md and
// specs/milestones/M06-focus-tracking.md).
//
// resolve reads a cache the engine's run goroutine maintains from
// audio.Backend.Subscribe events (see engine.go), never calling
// Backend.Streams/Sinks/Sources itself: that keeps a blocking
// enumeration off the hot dispatch path, and audio.Supervisor's reads
// wait across a reconnect, which would otherwise stall gesture timing
// for the whole reconnect window.
//
// resolve takes no context.Context: every target kind it handles is now
// a pure, non-blocking cache read (see resolveFocused's doc comment for
// what changed here in M06 — a focus.Provider used to be called inline,
// which was the entire reason ctx existed on this method).
type resolver struct {
	matchers map[string]model.AppMatcher
	// groups backs resolveGroup: a TargetGroup unions resolveApp over
	// every matcher the group names (see model.AppGroup's doc comment
	// and specs/milestones/M08-layers-groups-scenes.md's Design
	// section).
	groups map[string]model.AppGroup

	streams map[string]audio.Stream
	sinks   []audio.Device
	sources []audio.Device
	// streamGen counts every streams mutation, so resolveFocused's memo
	// (below) can tell "the stream graph hasn't changed since I last
	// computed this" from "it has" without a deep comparison.
	streamGen uint64

	// focused is the last AppInfo the engine's run loop received from
	// focus.Provider.Watch (or seeded once from Current at startup) —
	// see engine.go's focus-events select arm. resolveFocused reads
	// this and nothing else; it never calls into a focus.Provider
	// itself.
	focused focus.AppInfo
	// proc backs resolveFocused's process-tree fallback rung (see
	// audio.ResolveFocused). Its zero value (Root "") reads the real
	// /proc; tests point it at a fabricated tree via
	// engine.Deps.ProcRoot.
	proc proctree.Walker
	log  *slog.Logger

	focusMemo focusMemo
}

// focusMemo caches resolveFocused's last answer, keyed on the two
// things that can change it: the focused AppInfo and the stream
// generation. Without this, every dispatched gesture bound to
// TargetFocused would re-run audio.ResolveFocused's process-tree
// fallback rung (bounded /proc reads) on every single detent; with it,
// that work happens once per actual focus or stream-set change, not
// once per event on the hot path.
type focusMemo struct {
	valid     bool
	focused   focus.AppInfo
	streamGen uint64
	refs      []audio.Ref
}

// newResolver returns a resolver with an empty stream/device cache and
// no focused application; setConfig must be called before resolve for
// app/group targets to work, and setStreams/setSinks/setSources before
// it can resolve anything at all. The engine calls all of these as
// their respective sources arrive. log may be nil (defaults to
// slog.Default() where used) — it exists solely so resolveFocused can
// note, at Debug, which of its matching rungs produced an answer (see
// its doc comment); nothing else in this type logs.
func newResolver(proc proctree.Walker, log *slog.Logger) *resolver {
	if log == nil {
		log = slog.Default()
	}
	return &resolver{
		matchers: make(map[string]model.AppMatcher),
		groups:   make(map[string]model.AppGroup),
		streams:  make(map[string]audio.Stream),
		proc:     proc,
		log:      log,
	}
}

// setConfig rebuilds the matcher/group tables from cfg. The stream/
// device cache is untouched — a config change doesn't invalidate what's
// actually playing.
func (r *resolver) setConfig(cfg model.Config) {
	matchers := make(map[string]model.AppMatcher, len(cfg.AppMatchers))
	for _, m := range cfg.AppMatchers {
		matchers[m.ID] = m
	}
	r.matchers = matchers

	groups := make(map[string]model.AppGroup, len(cfg.AppGroups))
	for _, g := range cfg.AppGroups {
		groups[g.ID] = g
	}
	r.groups = groups
}

// setFocused updates the cached focused-application value that
// resolveFocused reads. Called from engine.go's run loop whenever
// focus.Provider.Watch delivers a new AppInfo (and once at startup from
// Current, to seed it before the first event arrives).
func (r *resolver) setFocused(info focus.AppInfo) {
	r.focused = info
}

func (r *resolver) upsertStream(s audio.Stream) { r.streams[s.ID] = s; r.streamGen++ }
func (r *resolver) removeStream(id string)      { delete(r.streams, id); r.streamGen++ }

func (r *resolver) setStreams(streams []audio.Stream) {
	m := make(map[string]audio.Stream, len(streams))
	for _, s := range streams {
		m[s.ID] = s
	}
	r.streams = m
	r.streamGen++
}

func (r *resolver) setSinks(sinks []audio.Device)     { r.sinks = sinks }
func (r *resolver) setSources(sources []audio.Device) { r.sources = sources }

// resolve returns the refs t currently names. A valid target that
// currently matches nothing returns (nil, nil) — an app that isn't
// running, for instance, is a no-op for the caller to skip quietly, not
// an error.
func (r *resolver) resolve(t model.Target) ([]audio.Ref, error) {
	switch t.Kind {
	case model.TargetDefaultSink:
		return deviceRef(r.sinks, audio.RefSink)
	case model.TargetDefaultSource:
		return deviceRef(r.sources, audio.RefSource)
	case model.TargetSink:
		// Unverified: the real backend resolves by node.name server-side
		// (see pulseBackend.SetVolume) and errors clearly if it's absent.
		// Verifying here would cost a Sinks() enumeration per detent.
		return []audio.Ref{{Kind: audio.RefSink, ID: t.Ref}}, nil
	case model.TargetSource:
		return []audio.Ref{{Kind: audio.RefSource, ID: t.Ref}}, nil
	case model.TargetApp:
		return r.resolveApp(t.Ref)
	case model.TargetAllStreams:
		return r.allPlaybackStreams(), nil
	case model.TargetFocused:
		return r.resolveFocused()
	case model.TargetGroup:
		return r.resolveGroup(t.Ref)
	default:
		return nil, fmt.Errorf("engine: resolve target: unknown kind %q", t.Kind)
	}
}

func deviceRef(devices []audio.Device, kind audio.RefKind) ([]audio.Ref, error) {
	for _, d := range devices {
		if d.IsDefault {
			return []audio.Ref{{Kind: kind, ID: d.ID}}, nil
		}
	}
	return nil, nil
}

func (r *resolver) resolveApp(matcherID string) ([]audio.Ref, error) {
	matcher, ok := r.matchers[matcherID]
	if !ok {
		return nil, fmt.Errorf("engine: resolve app: unknown app matcher %q", matcherID)
	}
	return r.matchStreams(matcher)
}

// resolveGroup unions resolveApp over every matcher groupID names,
// de-duplicating by audio.Ref in case two matchers in the same group
// somehow match the same stream (see model.AppGroup's doc comment).
// model.Config.Validate already guarantees every AppGroup.MatcherIDs
// entry names a real AppMatcher, but that check is config-wide, not
// re-verified here, so an unknown matcher ID still surfaces as an
// error rather than being silently skipped.
func (r *resolver) resolveGroup(groupID string) ([]audio.Ref, error) {
	group, ok := r.groups[groupID]
	if !ok {
		return nil, fmt.Errorf("engine: resolve group: unknown app group %q", groupID)
	}
	var refs []audio.Ref
	seen := make(map[audio.Ref]bool)
	for _, matcherID := range group.MatcherIDs {
		matched, err := r.resolveApp(matcherID)
		if err != nil {
			return nil, fmt.Errorf("engine: resolve group %q: %w", groupID, err)
		}
		for _, ref := range matched {
			if !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
	}
	return refs, nil
}

func (r *resolver) matchStreams(matcher model.AppMatcher) ([]audio.Ref, error) {
	streams := r.streamSlice()
	matched, err := audio.Resolve(matcher, streams)
	if err != nil {
		return nil, fmt.Errorf("engine: resolve app matcher %q: %w", matcher.ID, err)
	}
	return streamRefs(matched), nil
}

func (r *resolver) streamSlice() []audio.Stream {
	streams := make([]audio.Stream, 0, len(r.streams))
	for _, s := range r.streams {
		streams = append(streams, s)
	}
	return streams
}

func streamRefs(streams []audio.Stream) []audio.Ref {
	if len(streams) == 0 {
		return nil
	}
	refs := make([]audio.Ref, len(streams))
	for i, s := range streams {
		refs[i] = s.Ref()
	}
	return refs
}

func (r *resolver) allPlaybackStreams() []audio.Ref {
	var refs []audio.Ref
	for _, s := range r.streams {
		if s.Direction == audio.StreamPlayback {
			refs = append(refs, s.Ref())
		}
	}
	return refs
}

// resolveFocused matches the cached focused AppInfo (see setFocused)
// against the stream cache via audio.ResolveFocused's three-rung
// ladder (name-based matching, folding in the focused window's
// /proc/<pid>/exe-derived binary name; then, only if that found
// nothing, a process-tree ancestry check against each candidate
// stream's own pid — see that function's doc comment for why the
// second rung is a fallback and not unioned with the first).
//
// This used to call focus.Provider.Current inline here, which was
// safe only as long as every Provider answered synchronously with no
// I/O (true of focus.Unavailable() and focus.FakeProvider, false of a
// real D-Bus-backed one) — M04 left an explicit TODO about this. M06
// removes the call entirely: resolveFocused now only ever reads
// r.focused, which engine.go's run loop keeps current via
// focus.Provider.Watch, and Provider.Current's contract (see
// focus/provider.go) requires it never block on I/O either, for the
// one remaining inline call (the startup seed, and Snapshot's use of
// this same cache).
//
// The memo means this whole function is a map lookup in the common
// case; the process-tree rung's /proc reads (when reached) happen at
// most once per actual focus-or-stream-set change, not once per
// dispatched gesture.
func (r *resolver) resolveFocused() ([]audio.Ref, error) {
	if r.focusMemo.valid && r.focusMemo.focused == r.focused && r.focusMemo.streamGen == r.streamGen {
		return r.focusMemo.refs, nil
	}

	req := audio.FocusRequest{
		PID:   r.focused.PID,
		Names: r.focused.NameCandidates(),
	}
	matched, viaProcTree, err := audio.ResolveFocused(req, r.streamSlice(), r.proc)
	if err != nil {
		return nil, fmt.Errorf("engine: resolve focused: %w", err)
	}
	if viaProcTree {
		r.log.Debug("engine: resolved focused app via the process-tree fallback rung",
			"pid", r.focused.PID, "resourceClass", r.focused.ResourceClass)
	}

	refs := streamRefs(matched)
	r.focusMemo = focusMemo{valid: true, focused: r.focused, streamGen: r.streamGen, refs: refs}
	return refs, nil
}
