package main

import (
	"context"
	"testing"

	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/model"
)

type fakeConfigProvider struct{ cfg model.Config }

func (f *fakeConfigProvider) Config() model.Config { return f.cfg }

func TestAudioGraphDisplayNameFallback(t *testing.T) {
	cases := []struct {
		name  string
		props map[string]string
		want  string
	}{
		{"prefers application.name", map[string]string{"application.name": "Vesktop", "node.name": "vesktop", "media.name": "Discord"}, "Vesktop"},
		{"falls back to media.name", map[string]string{"media.name": "Discord", "node.name": "vesktop"}, "Discord"},
		// Mirrors testdata/pipewire/pw-dump-sample.json id 112: a stream
		// with no application.* properties at all, only node.name.
		{"falls back to node.name when nothing else is set", map[string]string{"node.name": "java"}, "java"},
		{"falls back to \"stream <id>\" when nothing is set", map[string]string{}, "stream 42"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := audio.NewFakeBackend()
			stream := audio.Stream{ID: "42", Direction: audio.StreamPlayback, Props: tc.props}
			backend.Seed(nil, nil, []audio.Stream{stream})

			g := newAudioGraph(backend, &fakeConfigProvider{})
			got, err := g.AudioGraph(context.Background())
			if err != nil {
				t.Fatalf("AudioGraph: %v", err)
			}
			if len(got.Streams) != 1 {
				t.Fatalf("got %d streams, want 1", len(got.Streams))
			}
			if got.Streams[0].DisplayName != tc.want {
				t.Errorf("DisplayName = %q, want %q", got.Streams[0].DisplayName, tc.want)
			}
		})
	}
}

func TestAudioGraphRawPropsAndBinaryFallback(t *testing.T) {
	backend := audio.NewFakeBackend()
	props := map[string]string{
		"node.name":            "vesktop",
		"application.name":     "Vesktop",
		"knobd.process.binary": "vesktop", // AnnotateProcessBinaries synthesized this
		"knobd.stream.corked":  "true",
		"application.id":       "com.vencord.Vesktop",
		"media.name":           "Discord",
	}
	backend.Seed(nil, nil, []audio.Stream{{ID: "118", Direction: audio.StreamPlayback, Props: props}})

	g := newAudioGraph(backend, &fakeConfigProvider{})
	got, err := g.AudioGraph(context.Background())
	if err != nil {
		t.Fatalf("AudioGraph: %v", err)
	}
	s := got.Streams[0]

	// Real application.process.binary is absent; the synthesized
	// knobd.process.binary fallback must surface separately, not get
	// silently promoted into Binary.
	if s.Binary != "" {
		t.Errorf("Binary = %q, want empty (no real application.process.binary)", s.Binary)
	}
	if s.BinaryGuess != "vesktop" {
		t.Errorf("BinaryGuess = %q, want %q", s.BinaryGuess, "vesktop")
	}
	if !s.Corked {
		t.Error("Corked = false, want true")
	}
	if s.DesktopID != "com.vencord.Vesktop" {
		t.Errorf("DesktopID = %q, want %q", s.DesktopID, "com.vencord.Vesktop")
	}
	// Props must carry everything raw and undigested.
	for k, v := range props {
		if s.Props[k] != v {
			t.Errorf("Props[%q] = %q, want %q", k, s.Props[k], v)
		}
	}
}

func TestAudioGraphMatcherIDs(t *testing.T) {
	backend := audio.NewFakeBackend()
	backend.Seed(nil, nil, []audio.Stream{
		{ID: "118", Direction: audio.StreamPlayback, Props: map[string]string{"application.name": "vesktop"}},
		{ID: "132", Direction: audio.StreamPlayback, Props: map[string]string{"application.name": "unrelated-app"}},
	})
	cfg := model.Config{
		AppMatchers: []model.AppMatcher{
			{ID: "vesktop", AppNames: []string{"vesktop"}},
		},
	}

	g := newAudioGraph(backend, &fakeConfigProvider{cfg: cfg})
	got, err := g.AudioGraph(context.Background())
	if err != nil {
		t.Fatalf("AudioGraph: %v", err)
	}

	byID := make(map[string][]string, len(got.Streams))
	for _, s := range got.Streams {
		byID[s.ID] = s.MatcherIDs
	}
	if want := []string{"vesktop"}; len(byID["118"]) != 1 || byID["118"][0] != want[0] {
		t.Errorf("stream 118 MatcherIDs = %v, want %v", byID["118"], want)
	}
	if len(byID["132"]) != 0 {
		t.Errorf("stream 132 MatcherIDs = %v, want empty", byID["132"])
	}
}

func TestAudioGraphDevicesCarryRefAndDefault(t *testing.T) {
	backend := audio.NewFakeBackend()
	backend.Seed(
		[]audio.Device{{ID: "alsa_output.x", Description: "Speakers", IsDefault: true}},
		[]audio.Device{{ID: "alsa_input.x", Description: "Mic"}},
		nil,
	)

	g := newAudioGraph(backend, &fakeConfigProvider{})
	got, err := g.AudioGraph(context.Background())
	if err != nil {
		t.Fatalf("AudioGraph: %v", err)
	}
	if len(got.Sinks) != 1 || got.Sinks[0].Ref != "sink:alsa_output.x" || !got.Sinks[0].IsDefault {
		t.Errorf("Sinks = %+v, want one default sink with ref \"sink:alsa_output.x\"", got.Sinks)
	}
	if len(got.Sources) != 1 || got.Sources[0].Ref != "source:alsa_input.x" || got.Sources[0].IsDefault {
		t.Errorf("Sources = %+v, want one non-default source with ref \"source:alsa_input.x\"", got.Sources)
	}
	// Both were seeded at 100%/unmuted by FakeBackend.Seed.
	if got.Sinks[0].VolumePercent != 100 {
		t.Errorf("Sinks[0].VolumePercent = %v, want 100", got.Sinks[0].VolumePercent)
	}
}
