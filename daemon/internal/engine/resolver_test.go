package engine

import (
	"errors"
	"testing"

	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/focus"
	"github.com/njeske/knobd/internal/model"
	"github.com/njeske/knobd/internal/proctree"
)

// The fixture below mirrors the two edge cases already pinned by
// audio/matcher_test.go against testdata/pipewire/pw-dump-sample.json:
// an app that publishes more than one simultaneous stream (vesktop), and
// a stream with no application.* properties at all, matchable only by
// node.name (a "java" node).
func seedResolver(t *testing.T, r *resolver) {
	t.Helper()
	r.setSinks([]audio.Device{
		{ID: "alsa_output.default", Description: "Default speakers", IsDefault: true},
		{ID: "alsa_output.hdmi", Description: "HDMI"},
	})
	r.setSources([]audio.Device{
		{ID: "alsa_input.default", Description: "Default mic", IsDefault: true},
	})
	r.setStreams([]audio.Stream{
		{ID: "118", Direction: audio.StreamPlayback, Props: map[string]string{"application.name": "vesktop", "application.id": "vesktop"}},
		{ID: "128", Direction: audio.StreamPlayback, Props: map[string]string{"application.name": "vesktop", "application.id": "vesktop"}},
		{ID: "112", Direction: audio.StreamPlayback, Props: map[string]string{"node.name": "java"}},
	})
}

func TestResolverDefaultSinkAndSource(t *testing.T) {
	r := newResolver(proctree.Walker{}, nil)
	seedResolver(t, r)

	refs, err := r.resolve(model.Target{Kind: model.TargetDefaultSink})
	if err != nil {
		t.Fatalf("resolve default_sink: %v", err)
	}
	if len(refs) != 1 || refs[0] != (audio.Ref{Kind: audio.RefSink, ID: "alsa_output.default"}) {
		t.Errorf("default_sink refs = %+v", refs)
	}

	refs, err = r.resolve(model.Target{Kind: model.TargetDefaultSource})
	if err != nil {
		t.Fatalf("resolve default_source: %v", err)
	}
	if len(refs) != 1 || refs[0] != (audio.Ref{Kind: audio.RefSource, ID: "alsa_input.default"}) {
		t.Errorf("default_source refs = %+v", refs)
	}
}

func TestResolverSinkAndSourceByName(t *testing.T) {
	r := newResolver(proctree.Walker{}, nil)
	seedResolver(t, r)

	// Unverified: resolves to a Ref even though "nonexistent" isn't in
	// the cache — the real backend rejects it server-side on SetVolume.
	refs, err := r.resolve(model.Target{Kind: model.TargetSink, Ref: "nonexistent"})
	if err != nil {
		t.Fatalf("resolve sink: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "nonexistent" {
		t.Errorf("sink refs = %+v", refs)
	}
}

func TestResolverAppMultiStream(t *testing.T) {
	r := newResolver(proctree.Walker{}, nil)
	seedResolver(t, r)
	r.setConfig(model.Config{AppMatchers: []model.AppMatcher{
		{ID: "vesktop", AppNames: []string{"vesktop"}},
	}})

	refs, err := r.resolve(model.Target{Kind: model.TargetApp, Ref: "vesktop"})
	if err != nil {
		t.Fatalf("resolve app: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected both of vesktop's streams, got %+v", refs)
	}
}

func TestResolverAppByNodeNameOnly(t *testing.T) {
	r := newResolver(proctree.Walker{}, nil)
	seedResolver(t, r)
	r.setConfig(model.Config{AppMatchers: []model.AppMatcher{
		{ID: "java", NodeNames: []string{"java"}},
	}})

	refs, err := r.resolve(model.Target{Kind: model.TargetApp, Ref: "java"})
	if err != nil {
		t.Fatalf("resolve app: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "112" {
		t.Errorf("refs = %+v", refs)
	}
}

func TestResolverAppNotPlayingIsNotAnError(t *testing.T) {
	r := newResolver(proctree.Walker{}, nil)
	seedResolver(t, r)
	r.setConfig(model.Config{AppMatchers: []model.AppMatcher{
		{ID: "spotify", AppNames: []string{"spotify"}},
	}})

	refs, err := r.resolve(model.Target{Kind: model.TargetApp, Ref: "spotify"})
	if err != nil {
		t.Fatalf("resolve app not currently playing should not error: %v", err)
	}
	if refs != nil {
		t.Errorf("expected nil refs, got %+v", refs)
	}
}

func TestResolverAppUnknownMatcher(t *testing.T) {
	r := newResolver(proctree.Walker{}, nil)
	seedResolver(t, r)
	if _, err := r.resolve(model.Target{Kind: model.TargetApp, Ref: "nonexistent"}); err == nil {
		t.Fatal("expected an error resolving an unknown app matcher")
	}
}

func TestResolverAllStreams(t *testing.T) {
	r := newResolver(proctree.Walker{}, nil)
	seedResolver(t, r)
	refs, err := r.resolve(model.Target{Kind: model.TargetAllStreams})
	if err != nil {
		t.Fatalf("resolve all_streams: %v", err)
	}
	if len(refs) != 3 {
		t.Errorf("expected all 3 playback streams, got %+v", refs)
	}
}

func TestResolverGroupUnsupported(t *testing.T) {
	r := newResolver(proctree.Walker{}, nil)
	seedResolver(t, r)
	_, err := r.resolve(model.Target{Kind: model.TargetGroup, Ref: "voice"})
	if !errors.Is(err, errTargetUnsupported) {
		t.Fatalf("resolve group: err = %v, want errTargetUnsupported", err)
	}
}

// TestResolverFocusedWithNothingFocusedResolvesToNothing pins the M06
// behavior change from resolveFocused no longer calling into a
// focus.Provider inline: with no focused app ever set (the zero-value
// focus.AppInfo, matching a freshly constructed resolver, or an engine
// wired against focus.Unavailable()), TargetFocused resolves to
// (nil, nil) -- "resolves to nothing right now" -- rather than an
// error. dispatchGesture already treats that as a quiet no-op (logged
// at Debug, not Warn); see resolveFocused's doc comment.
func TestResolverFocusedWithNothingFocusedResolvesToNothing(t *testing.T) {
	r := newResolver(proctree.Walker{}, nil)
	seedResolver(t, r)
	refs, err := r.resolve(model.Target{Kind: model.TargetFocused})
	if err != nil {
		t.Fatalf("resolve focused with nothing focused: %v", err)
	}
	if refs != nil {
		t.Errorf("expected nil refs, got %+v", refs)
	}
}

func TestResolverFocusedMatchesResourceClass(t *testing.T) {
	r := newResolver(proctree.Walker{}, nil)
	seedResolver(t, r)
	r.setFocused(focus.AppInfo{ResourceClass: "vesktop"})

	refs, err := r.resolve(model.Target{Kind: model.TargetFocused})
	if err != nil {
		t.Fatalf("resolve focused: %v", err)
	}
	if len(refs) != 2 {
		t.Errorf("expected the focused app's two streams, got %+v", refs)
	}
}

// TestResolverFocusedMemoInvalidatedByStreamChanges pins that
// resolveFocused's memo (keyed on (focused, streamGen), see
// resolver.go) is actually invalidated by every stream-cache mutation
// -- not just recomputed once and then stuck.
func TestResolverFocusedMemoInvalidatedByStreamChanges(t *testing.T) {
	r := newResolver(proctree.Walker{}, nil)
	r.setFocused(focus.AppInfo{ResourceClass: "vesktop"})
	r.setStreams([]audio.Stream{
		{ID: "118", Direction: audio.StreamPlayback, Props: map[string]string{"application.name": "vesktop"}},
	})

	refs, err := r.resolve(model.Target{Kind: model.TargetFocused})
	if err != nil {
		t.Fatalf("resolve focused: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref before the stream change, got %+v", refs)
	}

	// A second stream for the same app appears; if upsertStream failed
	// to bump streamGen, this would wrongly keep returning the first
	// call's memoized single ref.
	r.upsertStream(audio.Stream{ID: "128", Direction: audio.StreamPlayback, Props: map[string]string{"application.name": "vesktop"}})
	refs, err = r.resolve(model.Target{Kind: model.TargetFocused})
	if err != nil {
		t.Fatalf("resolve focused after upsertStream: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs after upsertStream, got %+v (memo not invalidated)", refs)
	}

	r.removeStream("128")
	refs, err = r.resolve(model.Target{Kind: model.TargetFocused})
	if err != nil {
		t.Fatalf("resolve focused after removeStream: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref after removeStream, got %+v (memo not invalidated)", refs)
	}
}

// TestResolverFocusedMemoInvalidatedBySetFocusedChange pins the other
// half of the memo key: a focus change alone (no stream mutation) must
// also invalidate it.
func TestResolverFocusedMemoInvalidatedBySetFocusedChange(t *testing.T) {
	r := newResolver(proctree.Walker{}, nil)
	seedResolver(t, r)

	r.setFocused(focus.AppInfo{ResourceClass: "vesktop"})
	refs, err := r.resolve(model.Target{Kind: model.TargetFocused})
	if err != nil {
		t.Fatalf("resolve focused: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected vesktop's 2 streams, got %+v", refs)
	}

	r.setFocused(focus.AppInfo{ResourceClass: "java"})
	refs, err = r.resolve(model.Target{Kind: model.TargetFocused})
	if err != nil {
		t.Fatalf("resolve focused after setFocused changed: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "112" {
		t.Fatalf("expected java's single stream after setFocused changed the target, got %+v (memo not invalidated)", refs)
	}
}
