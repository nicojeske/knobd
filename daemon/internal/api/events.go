package api

import "time"

// EventProtocolVersion is bumped whenever Event's shape changes in a way
// a client needs to know about (a field removed or reinterpreted, not a
// purely additive one) -- Hello.ProtocolVersion lets a UI refuse to talk
// to a daemon whose stream it can't parse rather than misinterpreting it
// silently.
const EventProtocolVersion = 1

// EventType identifies one GET /events frame's payload.
type EventType string

const (
	// EventHello is sent once, immediately on connect.
	EventHello EventType = "hello"
	// EventState is a full State snapshot -- see Hub's doc comment for
	// why full snapshots rather than deltas.
	EventState EventType = "state"
	// EventConfigChanged notifies that the running configuration changed
	// (a PUT /config, a SIGHUP reload, or a device-triggered mutation
	// like knob.assign_focused_app); see configstore.go (cmd/knobd).
	EventConfigChanged EventType = "config_changed"
	// EventLearnInput is pushed for each control captured while MIDI
	// learn was armed. Learn's own armed/disarmed/expired status is
	// carried in every EventState frame's State.Learn field instead of
	// a separate event type, since markLEDsDirty (and therefore a state
	// push) already fires on every learn transition.
	EventLearnInput EventType = "learn_input"
	// EventError is terminal: the server is about to close this stream.
	EventError EventType = "error"
)

// Event is one frame on GET /events, framed as Server-Sent Events
// ("event: <type>\ndata: <compact json>\n\n" -- see
// specs/adr/0004-ipc-over-unix-socket.md's Update (M07)). Payload
// fields are optional pointers keyed off Type, rather than a raw JSON
// blob plus a oneOf: it reflects cleanly into docs/openapi.json and
// generates a single, usable TypeScript union.
type Event struct {
	Type EventType `json:"type"`
	// Seq is monotonic per connection, assigned by the stream handler as
	// each frame is actually written to the wire. A gap in Seq (as
	// opposed to a gap in wall-clock time) means an intermediate State
	// snapshot was coalesced away by the hub's single-slot backpressure
	// -- never a sign anything was lost, since each State frame is
	// complete on its own.
	Seq uint64    `json:"seq"`
	Now time.Time `json:"now"`

	Hello  *Hello         `json:"hello,omitempty"`
	State  *State         `json:"state,omitempty"`
	Config *ConfigChanged `json:"config,omitempty"`
	Input  *LearnInput    `json:"input,omitempty"`
	Error  *ErrorResponse `json:"error,omitempty"`
}

// Hello is EventHello's payload: everything a client needs to decide
// whether it can talk to this daemon, and how the connection is paced.
type Hello struct {
	ProtocolVersion int `json:"protocolVersion"`
	// SchemaVersion is model.CurrentSchemaVersion, so a UI generated
	// against a different config schema version can refuse to proceed
	// rather than send a PUT /config the daemon will reject anyway.
	SchemaVersion int `json:"schemaVersion"`
	// FlushIntervalMs is the hub's state-push throttle -- see Hub's doc
	// comment. Informational only; nothing about the protocol depends on
	// a client knowing this.
	FlushIntervalMs int `json:"flushIntervalMs"`
}

// ConfigChanged is EventConfigChanged's payload: a revision number, not
// the configuration itself. A UI re-GETs /config on this signal --
// keeping GET /config the single parse path, keeping this event small
// and notification-shaped, and sidesteps PUT /config's own 1 MiB body
// cap for no benefit here.
type ConfigChanged struct {
	Revision uint64 `json:"revision"`
}
