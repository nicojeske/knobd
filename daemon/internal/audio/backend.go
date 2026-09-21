// Package audio defines knobd's interface to the system's audio graph
// (PipeWire, spoken to via its PulseAudio-compatible native protocol on
// this system) and the logic for resolving a model.Target to the live
// Devices/Streams it currently refers to.
//
// The real Backend (pulse.go) talks to the pipewire-pulse compatibility
// server at $XDG_RUNTIME_DIR/pulse/native using the PulseAudio native
// protocol via github.com/jfreymuth/pulse's proto subpackage. Per ADR
// 0002, that library's API surface was confirmed sufficient for
// everything this package needs (SetSinkInputVolume/Mute,
// SetSourceOutputVolume/Mute, Subscribe with real change-event delivery)
// against this system's PipeWire 1.6.8 — no pactl-shelling fallback was
// required. audio.Supervisor (supervisor.go) wraps it with the same
// discover/reconnect-with-backoff pattern midi.Supervisor uses, since a
// pipewire-pulse restart is routine and must not require restarting
// knobd.
//
// A Stream's identifying properties are inconsistent in practice: some
// streams have no application.name/application.process.binary/
// application.process.id at all, only node.name (see
// testdata/pipewire/pw-dump-sample.json, id 112, a "java" node with
// nothing else set). Resolve (matcher.go) keys on node.name alone in
// that case, and Streams falls back to /proc/<pid>/exe
// (AnnotateProcessBinaries) when a pid is available but
// application.process.binary is not. One application can also own more
// than one simultaneous stream (id 118/128 "vesktop", id 132/146 "Pal"
// in the same fixture) — Resolve returns every matching stream, not just
// the first.
package audio

import (
	"context"
	"log/slog"

	"github.com/njeske/knobd/internal/model"
)

// RefKind identifies what kind of entity a Ref addresses.
type RefKind string

const (
	// RefSink addresses a sink (output device); ID is its node.name.
	RefSink RefKind = "sink"
	// RefSource addresses a source (input device); ID is its node.name.
	RefSource RefKind = "source"
	// RefStream addresses an application's playback stream (a PipeWire
	// sink-input); ID is its decimal sink-input index.
	RefStream RefKind = "stream"
	// RefRecord addresses an application's recording stream (a PipeWire
	// source-output); ID is its decimal source-output index.
	RefRecord RefKind = "record"
)

// Ref identifies one addressable entity in the audio graph for
// GetVolume/SetVolume/SetMute. It is deliberately typed rather than a
// bare string: a bare ID would force the backend to guess whether
// "alsa_output.x" names a sink or a source, or whether "118" is a
// stream index or a device literally named "118" — information the
// caller (a resolved model.Target) already has and should not discard.
type Ref struct {
	Kind RefKind
	ID   string
}

// Device is a sink (output) or source (input) — a hardware or virtual
// audio endpoint, as opposed to an application's Stream.
type Device struct {
	// ID is the backend-native node name (PipeWire node.name), stable
	// across the session but not guaranteed stable across reboots for
	// some virtual devices.
	ID          string
	Description string
	IsDefault   bool
}

// StreamDirection distinguishes an application's playback stream from
// its recording stream — the two are different PipeWire object types
// (sink-input vs. source-output) with different Ref kinds.
type StreamDirection string

const (
	StreamPlayback StreamDirection = "playback" // a sink-input
	StreamRecord   StreamDirection = "record"   // a source-output
)

// Stream is one application's audio stream (a PipeWire sink-input or
// source-output). Props holds every property the backend exposed for
// it, keyed the same way PipeWire itself does (e.g. "application.name",
// "node.name", "application.process.id") — kept as a generic bag rather
// than a fixed struct because, per the package doc comment, which
// properties are actually present varies per application. A property
// synthesized by knobd itself rather than published by PipeWire (see
// AnnotateProcessBinaries) is namespaced "knobd.*" so it's never
// confused with something the application actually published.
type Stream struct {
	ID        string
	Direction StreamDirection
	Props     map[string]string
}

// Ref returns the Ref that addresses this stream via
// GetVolume/SetVolume/SetMute.
func (s Stream) Ref() Ref {
	if s.Direction == StreamRecord {
		return Ref{Kind: RefRecord, ID: s.ID}
	}
	return Ref{Kind: RefStream, ID: s.ID}
}

// VolumeState is a target's current level and mute state, as reported by
// the backend or read back out of a Scene.
type VolumeState struct {
	// Percent is the channel average, in the same percent scale
	// pavucontrol/KDE/pactl display (100 == PulseAudio's VolumeNorm).
	Percent float64
	Muted   bool
	// Channels is the per-channel percent, in PipeWire's ChannelMap
	// order. It is what makes exact balance preservation (SetVolume
	// scaling every channel rather than flattening to Percent) and a
	// future VolumeBalanceAction possible; nil when the backend has no
	// finer-grained information than Percent (e.g. the FakeBackend).
	Channels []float64
}

// Event is a change notification from Subscribe: a device or stream
// appeared, disappeared, or had its volume/mute state change.
type Event struct {
	Kind   EventKind
	Device *Device // set for Kind == EventDeviceChanged/EventDeviceRemoved
	Stream *Stream // set for Kind == EventStreamChanged/EventStreamRemoved

	// State is the entity's volume/mute state at the time of the event,
	// already fetched by the backend's dispatcher so subscribers never
	// need a round-trip (and the read-after-change race that would
	// otherwise cause) per event. Nil when unknown, e.g. on a removal.
	State *VolumeState
}

// EventKind identifies what changed in an Event.
type EventKind string

const (
	EventDeviceChanged EventKind = "device_changed"
	EventDeviceRemoved EventKind = "device_removed"
	EventStreamChanged EventKind = "stream_changed"
	EventStreamRemoved EventKind = "stream_removed"
	// EventDefaultChanged fires when the default sink or source changes
	// (a PipeWire "server" subscription event). Device is unset; callers
	// interested in the new default should re-call Sinks/Sources.
	EventDefaultChanged EventKind = "default_changed"
	// EventResync fires when the backend cannot guarantee every change
	// was delivered — a slow subscriber caused the fan-out to drop
	// events, or the connection to PipeWire was lost and reconnected.
	// Device and Stream are unset; callers should treat their cached
	// state as stale and re-call Sinks/Sources/Streams.
	EventResync EventKind = "resync"
)

// Backend is knobd's interface to the live audio graph. All methods
// operate on IDs/Refs as returned by Sinks/Sources/Streams, resolved
// fresh each time — nothing in this package assumes a Device or Stream
// ID remains valid once obtained. Backend is explicitly NOT atomic
// across a Get followed by a Set: a caller needing read-modify-write
// atomicity (e.g. engine's volume-adjust handler, M04) must serialize
// its own access to a given Ref.
type Backend interface {
	Sinks(ctx context.Context) ([]Device, error)
	Sources(ctx context.Context) ([]Device, error)
	Streams(ctx context.Context) ([]Stream, error)

	GetVolume(ctx context.Context, ref Ref) (VolumeState, error)
	SetVolume(ctx context.Context, ref Ref, percent float64) error
	SetMute(ctx context.Context, ref Ref, muted bool) error

	// Subscribe streams change events until ctx is canceled. The
	// returned channel is closed once Subscribe stops delivering
	// events, whether due to cancellation or a backend failure — a
	// failure is not otherwise reported, so callers needing to
	// distinguish the two should watch ctx.Err() after the channel
	// closes. A slow receiver may miss events (see EventResync) rather
	// than stall the backend for every other caller.
	Subscribe(ctx context.Context) (<-chan Event, error)

	Close() error
}

// Options configures New. The zero value is sane defaults.
type Options struct {
	// ProcRoot overrides "/proc" for the AnnotateProcessBinaries
	// fallback; tests use a t.TempDir() tree of symlinks here.
	ProcRoot string
	// MaxPercent caps SetVolume and Curve.Adjust; 0 means
	// DefaultMaxPercent (roughly PulseAudio's own UI ceiling and KDE's
	// opt-in maximum).
	MaxPercent float64
	// Logger receives connection lifecycle and dropped-event logging.
	// Nil means slog.Default().
	Logger *slog.Logger
}

func (o Options) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

// New connects to the system's audio backend (the pipewire-pulse
// compatibility socket — see the package doc comment).
func New(ctx context.Context, opts Options) (Backend, error) {
	return newPulseBackend(ctx, opts)
}

// Resolve returns every Stream matching matcher (see model.AppMatcher's
// doc comment for the matching rules: a stream matches if it satisfies
// any one of the matcher's populated fields, matched case-insensitively
// except MediaNameRx). Resolve is a pure function — it does no /proc
// lookups of its own; see AnnotateProcessBinaries for the pid fallback,
// applied to streams before they reach Resolve.
func Resolve(matcher model.AppMatcher, streams []Stream) ([]Stream, error) {
	return resolve(matcher, streams)
}
