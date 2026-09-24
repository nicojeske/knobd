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
	// Learn is MIDI learn's current status -- see LearnController and
	// engine.Engine.SetLearnUntil.
	Learn LearnState `json:"learn"`
	// Media is MPRIS player discovery/selection status -- see
	// media.Tracker (M09).
	Media MediaState `json:"media"`
	// Spotify is the Spotify Web API OAuth connection's status -- see
	// spotify.Service (M10).
	Spotify SpotifyState `json:"spotify"`

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

// MediaState.Available is false until M09's media.New succeeds --
// mirrors FocusState.Available's role: media.transport/media.seek/
// media.target_cycle/media.now_playing don't resolve while it's false,
// and this is how the UI explains that.
type MediaState struct {
	Available bool `json:"available"`
	// Selected is the currently selected player's ref (see
	// media.PlayerInfo.Ref), or "" if none is selected -- the same
	// short form MediaTransportAction.PlayerRef/Config.Media.
	// IgnorePlayers use, not the full MPRIS bus name.
	Selected string        `json:"selected,omitempty"`
	Players  []MediaPlayer `json:"players"`
}

// MediaPlayer is one currently-known MPRIS player, for the UI's Media
// tab / player picker (ui/src/components/binding's PlayerField).
type MediaPlayer struct {
	// Ref is media.PlayerInfo.Ref -- what PlayerRef/IgnorePlayers name.
	Ref      string `json:"ref"`
	BusName  string `json:"busName"`
	Identity string `json:"identity,omitempty"`
	Status   string `json:"status,omitempty"`
	Title    string `json:"title,omitempty"`
	Artist   string `json:"artist,omitempty"`
	CanSeek  bool   `json:"canSeek"`
	// Ignored is true when Ref is in Config.Media.IgnorePlayers -- shown
	// even though such a player never appears as Selected, so the Media
	// tab can offer to un-ignore it.
	Ignored bool `json:"ignored"`
}

// SpotifyState reports the Spotify Web API OAuth connection's status --
// mirrors MediaState/FocusState's Available-gated shape: the
// spotify.* actions don't resolve while Authorized is false, and this
// is how the UI's Spotify tab explains why and offers a Connect button.
type SpotifyState struct {
	// Configured is true once Config.Spotify.ClientID is non-empty.
	Configured bool `json:"configured"`
	// Authorized is true once a refresh token is stored and was last
	// confirmed good.
	Authorized bool `json:"authorized"`
	// LoginInProgress is true between POST /spotify/login returning and
	// its loopback callback completing.
	LoginInProgress bool `json:"loginInProgress"`
	// LoginURL is the in-progress login's authorize URL, for the UI's
	// copyable fallback if the daemon's best-effort browser-open didn't
	// work. Empty unless LoginInProgress.
	LoginURL string `json:"loginUrl,omitempty"`
	// User is the authorized account's display name, empty until
	// confirmed.
	User string `json:"user,omitempty"`
	// LastError is the most recent login/refresh/validation failure's
	// message, empty if there is none to report.
	LastError string `json:"lastError,omitempty"`
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
