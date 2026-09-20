// Package audio defines knobd's interface to the system's audio graph
// (PipeWire, spoken to via its PulseAudio-compatible native protocol on
// this system) and the logic for resolving a model.Target to the live
// Devices/Streams it currently refers to.
//
// TODO(M03): implement the real Backend. Findings from live inspection
// this implementation should target, captured in
// specs/reference/environment.md and testdata/pipewire/:
//   - This system runs PipeWire 1.6.8 with the pipewire-pulse
//     compatibility server at $XDG_RUNTIME_DIR/pulse/native — talk to
//     that with the PulseAudio native protocol
//     (github.com/jfreymuth/pulse's proto subpackage), which gives
//     change-event subscription instead of having to poll. Fall back to
//     shelling out to pactl behind the same Backend interface if a
//     needed operation (e.g. SetVolume on a sink-input) turns out to be
//     missing from that library — confirm this on the first day of M03,
//     not after building against it.
//   - A Stream's identifying properties are inconsistent in practice:
//     some streams have no application.name/application.process.binary/
//     application.process.id at all, only node.name (see
//     testdata/pipewire/pw-dump-sample.json, id 112, a "java" node with
//     nothing else set). The resolver must be able to key on node.name
//     alone, and fall back to /proc/<pid>/exe when a pid is available
//     but application.process.binary is not.
//   - One application can own more than one simultaneous stream (id.
//     118/128 "vesktop", id 132/146 "Pal" in the same fixture) — an
//     action targeting an app must apply to every matching stream, not
//     just the first one found.
package audio

import (
	"context"

	"github.com/njeske/knobd/internal/model"
)

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

// Stream is one application's audio stream (a PipeWire sink-input or
// source-output). Props holds every property the backend exposed for
// it, keyed the same way PipeWire itself does (e.g. "application.name",
// "node.name", "application.process.id") — kept as a generic bag rather
// than a fixed struct because, per the package doc comment, which
// properties are actually present varies per application.
type Stream struct {
	ID    string
	Props map[string]string
}

// VolumeState is a target's current level and mute state, as reported by
// the backend or read back out of a Scene.
type VolumeState struct {
	Percent float64
	Muted   bool
}

// Event is a change notification from Subscribe: a device or stream
// appeared, disappeared, or had its volume/mute state change.
type Event struct {
	Kind   EventKind
	Device *Device // set for Kind == EventDeviceChanged
	Stream *Stream // set for Kind == EventStreamChanged
}

// EventKind identifies what changed in an Event.
type EventKind string

const (
	EventDeviceChanged EventKind = "device_changed"
	EventStreamChanged EventKind = "stream_changed"
	EventStreamRemoved EventKind = "stream_removed"
)

// Backend is knobd's interface to the live audio graph. All methods
// operate on IDs as returned by Sinks/Sources/Streams, resolved fresh
// each time — nothing in this package assumes a Device or Stream ID
// remains valid once obtained.
type Backend interface {
	Sinks(ctx context.Context) ([]Device, error)
	Sources(ctx context.Context) ([]Device, error)
	Streams(ctx context.Context) ([]Stream, error)

	GetVolume(ctx context.Context, id string) (VolumeState, error)
	SetVolume(ctx context.Context, id string, percent float64) error
	SetMute(ctx context.Context, id string, muted bool) error

	// Subscribe streams change events until ctx is canceled. The
	// returned channel is closed once Subscribe stops delivering
	// events, whether due to cancellation or a backend failure — a
	// failure is not otherwise reported, so callers needing to
	// distinguish the two should watch ctx.Err() after the channel
	// closes.
	Subscribe(ctx context.Context) (<-chan Event, error)

	Close() error
}

// New connects to the system's audio backend. TODO(M03): implement; see
// the package doc comment for the target protocol and fallback.
func New(ctx context.Context) (Backend, error) {
	return nil, errNotImplemented("audio.New")
}

// Resolve returns every Stream matching matcher (see model.AppMatcher's
// doc comment for the matching rules: a stream matches if it satisfies
// any one of the matcher's populated fields). TODO(M03): implement,
// against the fixtures in testdata/pipewire/pw-dump-sample.json — in
// particular the "java" stream that has only node.name set, and the two
// applications that each publish more than one simultaneous stream.
func Resolve(matcher model.AppMatcher, streams []Stream) ([]Stream, error) {
	return nil, errNotImplemented("audio.Resolve")
}
