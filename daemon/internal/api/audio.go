package api

import (
	"context"
	"time"
)

// AudioGraph is the GET /audio response: the live sinks, sources and
// streams the UI's target/app pickers need. One route rather than three
// (a /sinks, /sources, /streams split) so the picker sees one
// consistent moment of the audio graph in a single request.
type AudioGraph struct {
	Now     time.Time     `json:"now"`
	Sinks   []AudioDevice `json:"sinks"`
	Sources []AudioDevice `json:"sources"`
	Streams []AudioStream `json:"streams"`
}

// AudioDevice is one sink or source.
type AudioDevice struct {
	// Ref is "<kind>:<id>", the same "<kind>:<id>" form
	// ResolvedTarget.Refs already uses.
	Ref string `json:"ref"`
	// ID is the backend-native node name (PipeWire node.name) -- what a
	// model.Target{Kind: sink|source, Ref: ...} actually wants.
	ID            string  `json:"id"`
	Description   string  `json:"description"`
	IsDefault     bool    `json:"isDefault"`
	VolumePercent float64 `json:"volumePercent,omitempty"`
	Muted         bool    `json:"muted,omitempty"`
}

// AudioStream is one live application stream (playback or record),
// shaped so the UI can offer "make a matcher from this" without ever
// knowing a PipeWire property key itself. Binary/BinaryGuess/AppName/
// NodeName/DesktopID/MediaName are each the candidate value for exactly
// one of model.AppMatcher's criteria fields, omitted when this stream
// doesn't publish that property. Props carries the full raw bag too,
// deliberately undigested: what a given application actually publishes
// varies (some streams have only node.name -- see
// testdata/pipewire/pw-dump-sample.json id 112), and pre-digesting it
// would hide exactly the information the matcher editor needs to show.
type AudioStream struct {
	Ref         string `json:"ref"` // "stream:118" or "record:7"
	ID          string `json:"id"`
	Direction   string `json:"direction"` // "playback" | "record"
	DisplayName string `json:"displayName"`

	Binary      string `json:"binary,omitempty"`      // application.process.binary -> AppMatcher.Binaries
	BinaryGuess string `json:"binaryGuess,omitempty"` // knobd.process.binary (synthesized) -> AppMatcher.Binaries
	AppName     string `json:"appName,omitempty"`     // application.name -> AppMatcher.AppNames
	NodeName    string `json:"nodeName,omitempty"`    // node.name -> AppMatcher.NodeNames
	DesktopID   string `json:"desktopId,omitempty"`   // application.id -> AppMatcher.DesktopIDs
	MediaName   string `json:"mediaName,omitempty"`   // media.name -> AppMatcher.MediaNameRx (regexp-quote it first)
	Corked      bool   `json:"corked"`

	Props map[string]string `json:"props"`

	// MatcherIDs lists every model.AppMatcher in the running config that
	// currently matches this stream, so the UI can say "already covered
	// by Discord" instead of offering a duplicate -- and so it never has
	// to reimplement audio.Resolve's case-insensitive-OR-plus-Go-RE2
	// matching in TypeScript.
	MatcherIDs []string `json:"matcherIds,omitempty"`

	VolumePercent float64 `json:"volumePercent,omitempty"`
	Muted         bool    `json:"muted,omitempty"`
}

// AudioProvider supplies GET /audio. Like StateProvider, an
// implementation must bound its own wait on an unreachable backend --
// audio.Supervisor's enumeration methods block across a reconnect by
// design (see audio.Supervisor.currentBackend), so this must not hang
// the handler indefinitely.
type AudioProvider interface {
	AudioGraph(ctx context.Context) (AudioGraph, error)
}
