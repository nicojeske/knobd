package api

import (
	"time"

	"github.com/njeske/knobd/internal/model"
)

// State is the GET /state snapshot: everything a UI (or a human with
// curl) needs to answer "is knobd working, and what is each knob doing
// right now?". It is a point-in-time copy, not a live view -- M07 adds
// a WebSocket that pushes deltas of the same shape. It is deliberately
// not part of daemon/internal/model: model is the config schema that
// feeds docs/config.schema.json, and specs/milestones/M04-mapping-engine-daemon.md
// says GET /state's response shape must not affect that.
type State struct {
	Now time.Time `json:"now"`

	Device  DeviceState  `json:"device"`
	Audio   AudioState   `json:"audio"`
	Focus   FocusState   `json:"focus"`
	Profile ProfileState `json:"profile"`

	// Controls lists only controls that currently do something -- one
	// bound on the active layer (falling back to layer 0, same as
	// engine's binding lookup). A control bound to nothing is omitted
	// rather than listed as "unbound": the UI already knows the full
	// hardware layout from specs/reference/xtouch-mini-midi-map.md.
	Controls []ControlState `json:"controls"`
}

// DeviceState reports the MIDI controller's connection status.
type DeviceState struct {
	Connected bool   `json:"connected"`
	Name      string `json:"name,omitempty"`
	Path      string `json:"path,omitempty"`
	LastError string `json:"lastError,omitempty"`
}

// AudioState reports the PipeWire connection's status.
type AudioState struct {
	Connected bool   `json:"connected"`
	LastError string `json:"lastError,omitempty"`
}

// FocusState.Available is false until M06 lands a real focus.Provider --
// TargetFocused and knob.assign_focused_app do not resolve while it is
// false, and this field is how the UI explains that rather than the
// control silently doing nothing.
type FocusState struct {
	Available     bool   `json:"available"`
	ResourceClass string `json:"resourceClass,omitempty"`
}

// ProfileState names which profile/layer is active.
type ProfileState struct {
	ActiveProfileID string `json:"activeProfileId"`
	ActiveLayer     int    `json:"activeLayer"` // always 0 until M08
}

// ControlState is one bound (control, gesture)'s current behavior and
// what it resolves to right now.
type ControlState struct {
	Control    model.Control    `json:"control"`
	Gesture    model.Gesture    `json:"gesture"`
	ActionType model.ActionType `json:"actionType"`
	// Target is the action's configured target; nil for an action that
	// carries none (e.g. a layer action).
	Target *model.Target `json:"target,omitempty"`
	// Resolved is what Target currently points at in the live audio
	// graph; nil if Target is nil or currently resolves to nothing (the
	// app it names isn't running).
	Resolved *ResolvedTarget `json:"resolved,omitempty"`
}

// ResolvedTarget is what a ControlState's Target resolves to right now.
type ResolvedTarget struct {
	// Refs are the live audio entities, formatted "<kind>:<id>" (e.g.
	// "stream:118") -- plural because one target routinely resolves to
	// several simultaneous streams (an app publishing more than one
	// stream at once; see daemon/internal/audio's package doc comment).
	// Kept as strings rather than audio.Ref so this package needn't
	// import daemon/internal/audio.
	Refs          []string `json:"refs"`
	VolumePercent float64  `json:"volumePercent,omitempty"`
	Muted         bool     `json:"muted,omitempty"`
}
