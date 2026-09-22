package engine

import (
	"context"
	"fmt"

	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/focus"
	"github.com/njeske/knobd/internal/model"
)

// resolver turns a model.Target into the live audio.Refs it currently
// names. It lives in engine, not audio: resolving app/group/focused
// targets needs the config's AppMatchers and the focus.Provider, neither
// of which belongs in audio (which deliberately keeps audio.Resolve a
// pure function over a matcher and a stream slice, testable with no I/O
// — see specs/milestones/M03-audio-control.md).
//
// resolve reads a cache the engine's run goroutine maintains from
// audio.Backend.Subscribe events (see engine.go), never calling
// Backend.Streams/Sinks/Sources itself: that keeps a blocking
// enumeration off the hot dispatch path, and audio.Supervisor's reads
// wait across a reconnect, which would otherwise stall gesture timing
// for the whole reconnect window.
type resolver struct {
	matchers map[string]model.AppMatcher

	streams map[string]audio.Stream
	sinks   []audio.Device
	sources []audio.Device

	focus focus.Provider
}

// newResolver returns a resolver with an empty stream/device cache;
// setConfig must be called before resolve for app/group targets to work,
// and setStreams/setSinks/setSources before it can resolve anything at
// all. The engine calls all of these as their respective sources arrive.
func newResolver(f focus.Provider) *resolver {
	return &resolver{
		matchers: make(map[string]model.AppMatcher),
		streams:  make(map[string]audio.Stream),
		focus:    f,
	}
}

// setConfig rebuilds the matcher table from cfg. The stream/device cache
// is untouched — a config change doesn't invalidate what's actually
// playing.
func (r *resolver) setConfig(cfg model.Config) {
	matchers := make(map[string]model.AppMatcher, len(cfg.AppMatchers))
	for _, m := range cfg.AppMatchers {
		matchers[m.ID] = m
	}
	r.matchers = matchers
}

func (r *resolver) upsertStream(s audio.Stream) { r.streams[s.ID] = s }
func (r *resolver) removeStream(id string)      { delete(r.streams, id) }

func (r *resolver) setStreams(streams []audio.Stream) {
	m := make(map[string]audio.Stream, len(streams))
	for _, s := range streams {
		m[s.ID] = s
	}
	r.streams = m
}

func (r *resolver) setSinks(sinks []audio.Device)     { r.sinks = sinks }
func (r *resolver) setSources(sources []audio.Device) { r.sources = sources }

// resolve returns the refs t currently names. A valid target that
// currently matches nothing returns (nil, nil) — an app that isn't
// running, for instance, is a no-op for the caller to skip quietly, not
// an error.
func (r *resolver) resolve(ctx context.Context, t model.Target) ([]audio.Ref, error) {
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
		return r.resolveFocused(ctx)
	case model.TargetGroup:
		return nil, fmt.Errorf("engine: resolve group %q: %w (see specs/milestones/M08-layers-groups-scenes.md)", t.Ref, errTargetUnsupported)
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

func (r *resolver) matchStreams(matcher model.AppMatcher) ([]audio.Ref, error) {
	streams := make([]audio.Stream, 0, len(r.streams))
	for _, s := range r.streams {
		streams = append(streams, s)
	}
	matched, err := audio.Resolve(matcher, streams)
	if err != nil {
		return nil, fmt.Errorf("engine: resolve app matcher %q: %w", matcher.ID, err)
	}
	if len(matched) == 0 {
		return nil, nil
	}
	refs := make([]audio.Ref, len(matched))
	for i, s := range matched {
		refs[i] = s.Ref()
	}
	return refs, nil
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

// resolveFocused is called inline on the engine's run goroutine (both
// for dispatch and for Snapshot), which is safe in M04 only because
// focus.Unavailable() and focus.FakeProvider both answer Current
// synchronously with no I/O. M06's real kwinProvider talks to D-Bus, so
// once it lands, this needs to move behind a cached-latest-Watch-value
// pattern (the same shape the stream cache already uses for audio) so a
// slow focus lookup can never stall gesture timing.
func (r *resolver) resolveFocused(ctx context.Context) ([]audio.Ref, error) {
	info, err := r.focus.Current(ctx)
	if err != nil {
		return nil, fmt.Errorf("engine: resolve focused: %w", err)
	}
	// Match the focused window's identity against every property
	// application.id (PipeWire) or a window's resourceClass could equal,
	// the same fields AppMatcher.DesktopIDs is documented to match.
	var ids []string
	if info.ResourceClass != "" {
		ids = append(ids, info.ResourceClass)
	}
	if info.DesktopFileID != "" {
		ids = append(ids, info.DesktopFileID)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return r.matchStreams(model.AppMatcher{ID: "focused", DesktopIDs: ids})
}
