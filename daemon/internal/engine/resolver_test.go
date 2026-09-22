package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/focus"
	"github.com/njeske/knobd/internal/model"
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
	r := newResolver(focus.Unavailable())
	seedResolver(t, r)

	refs, err := r.resolve(context.Background(), model.Target{Kind: model.TargetDefaultSink})
	if err != nil {
		t.Fatalf("resolve default_sink: %v", err)
	}
	if len(refs) != 1 || refs[0] != (audio.Ref{Kind: audio.RefSink, ID: "alsa_output.default"}) {
		t.Errorf("default_sink refs = %+v", refs)
	}

	refs, err = r.resolve(context.Background(), model.Target{Kind: model.TargetDefaultSource})
	if err != nil {
		t.Fatalf("resolve default_source: %v", err)
	}
	if len(refs) != 1 || refs[0] != (audio.Ref{Kind: audio.RefSource, ID: "alsa_input.default"}) {
		t.Errorf("default_source refs = %+v", refs)
	}
}

func TestResolverSinkAndSourceByName(t *testing.T) {
	r := newResolver(focus.Unavailable())
	seedResolver(t, r)

	// Unverified: resolves to a Ref even though "nonexistent" isn't in
	// the cache — the real backend rejects it server-side on SetVolume.
	refs, err := r.resolve(context.Background(), model.Target{Kind: model.TargetSink, Ref: "nonexistent"})
	if err != nil {
		t.Fatalf("resolve sink: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "nonexistent" {
		t.Errorf("sink refs = %+v", refs)
	}
}

func TestResolverAppMultiStream(t *testing.T) {
	r := newResolver(focus.Unavailable())
	seedResolver(t, r)
	r.setConfig(model.Config{AppMatchers: []model.AppMatcher{
		{ID: "vesktop", AppNames: []string{"vesktop"}},
	}})

	refs, err := r.resolve(context.Background(), model.Target{Kind: model.TargetApp, Ref: "vesktop"})
	if err != nil {
		t.Fatalf("resolve app: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected both of vesktop's streams, got %+v", refs)
	}
}

func TestResolverAppByNodeNameOnly(t *testing.T) {
	r := newResolver(focus.Unavailable())
	seedResolver(t, r)
	r.setConfig(model.Config{AppMatchers: []model.AppMatcher{
		{ID: "java", NodeNames: []string{"java"}},
	}})

	refs, err := r.resolve(context.Background(), model.Target{Kind: model.TargetApp, Ref: "java"})
	if err != nil {
		t.Fatalf("resolve app: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "112" {
		t.Errorf("refs = %+v", refs)
	}
}

func TestResolverAppNotPlayingIsNotAnError(t *testing.T) {
	r := newResolver(focus.Unavailable())
	seedResolver(t, r)
	r.setConfig(model.Config{AppMatchers: []model.AppMatcher{
		{ID: "spotify", AppNames: []string{"spotify"}},
	}})

	refs, err := r.resolve(context.Background(), model.Target{Kind: model.TargetApp, Ref: "spotify"})
	if err != nil {
		t.Fatalf("resolve app not currently playing should not error: %v", err)
	}
	if refs != nil {
		t.Errorf("expected nil refs, got %+v", refs)
	}
}

func TestResolverAppUnknownMatcher(t *testing.T) {
	r := newResolver(focus.Unavailable())
	seedResolver(t, r)
	if _, err := r.resolve(context.Background(), model.Target{Kind: model.TargetApp, Ref: "nonexistent"}); err == nil {
		t.Fatal("expected an error resolving an unknown app matcher")
	}
}

func TestResolverAllStreams(t *testing.T) {
	r := newResolver(focus.Unavailable())
	seedResolver(t, r)
	refs, err := r.resolve(context.Background(), model.Target{Kind: model.TargetAllStreams})
	if err != nil {
		t.Fatalf("resolve all_streams: %v", err)
	}
	if len(refs) != 3 {
		t.Errorf("expected all 3 playback streams, got %+v", refs)
	}
}

func TestResolverGroupUnsupported(t *testing.T) {
	r := newResolver(focus.Unavailable())
	seedResolver(t, r)
	_, err := r.resolve(context.Background(), model.Target{Kind: model.TargetGroup, Ref: "voice"})
	if !errors.Is(err, errTargetUnsupported) {
		t.Fatalf("resolve group: err = %v, want errTargetUnsupported", err)
	}
}

func TestResolverFocusedUnavailable(t *testing.T) {
	r := newResolver(focus.Unavailable())
	seedResolver(t, r)
	_, err := r.resolve(context.Background(), model.Target{Kind: model.TargetFocused})
	if !errors.Is(err, focus.ErrUnavailable) {
		t.Fatalf("resolve focused: err = %v, want focus.ErrUnavailable", err)
	}
}

func TestResolverFocusedMatchesResourceClass(t *testing.T) {
	fp := focus.NewFakeProvider()
	fp.SetFocused(focus.AppInfo{ResourceClass: "vesktop"})
	r := newResolver(fp)
	seedResolver(t, r)

	refs, err := r.resolve(context.Background(), model.Target{Kind: model.TargetFocused})
	if err != nil {
		t.Fatalf("resolve focused: %v", err)
	}
	if len(refs) != 2 {
		t.Errorf("expected the focused app's two streams, got %+v", refs)
	}
}
