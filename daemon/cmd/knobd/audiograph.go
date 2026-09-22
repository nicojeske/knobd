package main

import (
	"context"
	"fmt"
	"time"

	"github.com/njeske/knobd/internal/api"
	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/model"
)

// audioEnumerateTimeout bounds how long a GET /audio request waits on
// the audio backend. audio.Supervisor's enumeration methods (Sinks/
// Sources/Streams/GetVolume) block across a PipeWire reconnect by
// design -- without this bound, a request would hang until PipeWire
// comes back rather than answering 503 promptly.
const audioEnumerateTimeout = 2 * time.Second

// These mirror the unexported property-name constants in
// daemon/internal/audio/matcher.go; duplicated here (rather than
// exported from audio) because they're a fixed part of the PipeWire/
// PulseAudio property namespace, not something audiograph.go should be
// able to change independently of matcher.go's matching rules.
const (
	propAppName     = "application.name"
	propNodeName    = "node.name"
	propDesktopID   = "application.id"
	propMediaName   = "media.name"
	propBinary      = "application.process.binary"
	propBinaryKnobd = "knobd.process.binary" // see audio.AnnotateProcessBinaries
	propCorked      = "knobd.stream.corked"
)

// audioBackend is the slice of audio.Backend audioGraph needs -- a
// point-of-use interface so it's testable against audio.FakeBackend
// without a real audio.Supervisor or PipeWire.
type audioBackend interface {
	Sinks(ctx context.Context) ([]audio.Device, error)
	Sources(ctx context.Context) ([]audio.Device, error)
	Streams(ctx context.Context) ([]audio.Stream, error)
	GetVolume(ctx context.Context, ref audio.Ref) (audio.VolumeState, error)
}

// configProvider is the slice of configStore audioGraph needs, to know
// which model.AppMatchers to test each live stream against for
// AudioStream.MatcherIDs.
type configProvider interface {
	Config() model.Config
}

// audioGraph is cmd/knobd's api.AudioProvider adapter.
type audioGraph struct {
	backend audioBackend
	config  configProvider
}

func newAudioGraph(backend audioBackend, config configProvider) *audioGraph {
	return &audioGraph{backend: backend, config: config}
}

// AudioGraph implements api.AudioProvider. It enumerates fresh on every
// call rather than subscribing: GET /audio is opened on demand (a
// picker being opened), and a second long-lived audio.Supervisor
// subscriber would mean keeping a second cache coherent with the
// engine's own resolver for no benefit here.
func (a *audioGraph) AudioGraph(ctx context.Context) (api.AudioGraph, error) {
	ctx, cancel := context.WithTimeout(ctx, audioEnumerateTimeout)
	defer cancel()

	sinks, err := a.backend.Sinks(ctx)
	if err != nil {
		return api.AudioGraph{}, fmt.Errorf("audiograph: sinks: %w", err)
	}
	sources, err := a.backend.Sources(ctx)
	if err != nil {
		return api.AudioGraph{}, fmt.Errorf("audiograph: sources: %w", err)
	}
	streams, err := a.backend.Streams(ctx)
	if err != nil {
		return api.AudioGraph{}, fmt.Errorf("audiograph: streams: %w", err)
	}

	cfg := a.config.Config()
	out := api.AudioGraph{Now: time.Now()}
	for _, d := range sinks {
		out.Sinks = append(out.Sinks, a.deviceToAPI(ctx, audio.RefSink, d))
	}
	for _, d := range sources {
		out.Sources = append(out.Sources, a.deviceToAPI(ctx, audio.RefSource, d))
	}
	for _, s := range streams {
		out.Streams = append(out.Streams, a.streamToAPI(ctx, s, cfg))
	}
	return out, nil
}

func (a *audioGraph) deviceToAPI(ctx context.Context, kind audio.RefKind, d audio.Device) api.AudioDevice {
	percent, muted := a.volume(ctx, audio.Ref{Kind: kind, ID: d.ID})
	return api.AudioDevice{
		Ref:           string(kind) + ":" + d.ID,
		ID:            d.ID,
		Description:   d.Description,
		IsDefault:     d.IsDefault,
		VolumePercent: percent,
		Muted:         muted,
	}
}

func (a *audioGraph) streamToAPI(ctx context.Context, s audio.Stream, cfg model.Config) api.AudioStream {
	p := s.Props
	appName := p[propAppName]
	nodeName := p[propNodeName]
	mediaName := p[propMediaName]

	display := firstNonEmpty(appName, mediaName, nodeName)
	if display == "" {
		display = "stream " + s.ID
	}

	ref := s.Ref()
	percent, muted := a.volume(ctx, ref)

	var matcherIDs []string
	for _, m := range cfg.AppMatchers {
		matches, err := audio.Resolve(m, []audio.Stream{s})
		if err == nil && len(matches) > 0 {
			matcherIDs = append(matcherIDs, m.ID)
		}
	}

	return api.AudioStream{
		Ref:           string(ref.Kind) + ":" + ref.ID,
		ID:            s.ID,
		Direction:     string(s.Direction),
		DisplayName:   display,
		Binary:        p[propBinary],
		BinaryGuess:   p[propBinaryKnobd],
		AppName:       appName,
		NodeName:      nodeName,
		DesktopID:     p[propDesktopID],
		MediaName:     mediaName,
		Corked:        p[propCorked] == "true",
		Props:         p,
		MatcherIDs:    matcherIDs,
		VolumePercent: percent,
		Muted:         muted,
	}
}

// volume fetches a Ref's current volume, best-effort: a device or
// stream that disappeared between enumeration and this call (a real
// race on a live audio graph) just gets the zero value rather than
// failing the whole GET /audio request over one stale entry.
func (a *audioGraph) volume(ctx context.Context, ref audio.Ref) (percent float64, muted bool) {
	v, err := a.backend.GetVolume(ctx, ref)
	if err != nil {
		return 0, false
	}
	return v.Percent, v.Muted
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
