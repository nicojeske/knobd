package model

import (
	"encoding/json"
	"fmt"
	"sort"
)

// ActionType identifies which action family a Binding fires. This is a
// deliberately open-ended but centrally registered set: adding a new
// action means adding a constant here, a params struct, an entry in
// actionRegistry, and — the part that actually does something — a
// handler in daemon/internal/actions once its milestone starts.
//
// The full brainstormed catalog (including actions with no Go type yet)
// lives in specs/reference/action-catalog.md.
type ActionType string

const (
	// -- Volume (specs/milestones/M03-audio-control.md) --

	ActionVolumeAdjust     ActionType = "volume.adjust"
	ActionVolumeSet        ActionType = "volume.set"
	ActionVolumeMuteToggle ActionType = "volume.mute_toggle"
	// ActionVolumeBalance has no handler and is not currently planned:
	// audio.Backend has no per-channel volume write (VolumeState.Channels
	// is read-only), and stereo balance is not a feature this project
	// wants. The type is kept rather than removed so a hand-written or
	// previously-generated config referencing it still decodes; see
	// specs/reference/action-catalog.md.
	ActionVolumeBalance ActionType = "volume.balance"
	// ActionVolumeFollow maps a continuous control's absolute position
	// directly onto a target's volume (see VolumeFollowAction). Fired on
	// GestureMove — currently only the fader produces that gesture.
	ActionVolumeFollow ActionType = "volume.follow"

	// -- Layers, groups, scenes (specs/milestones/M08-layers-groups-scenes.md) --

	ActionAudioSoloToggle ActionType = "audio.solo_toggle"
	ActionAudioDuckHold   ActionType = "audio.duck_hold"
	ActionSceneApply      ActionType = "scene.apply"
	ActionSceneSave       ActionType = "scene.save"
	ActionLayerMomentary  ActionType = "layer.momentary"
	ActionLayerLatch      ActionType = "layer.latch"
	ActionLayerCycle      ActionType = "layer.cycle"

	// -- Binding management (specs/milestones/M04, M06) --

	ActionKnobAssignFocusedApp ActionType = "knob.assign_focused_app"
	ActionKnobClear            ActionType = "knob.clear"
	ActionKnobLockToggle       ActionType = "knob.lock_toggle"

	// -- Media transport (specs/milestones/M09-media-transport-mpris.md) --

	ActionMediaTransport   ActionType = "media.transport"
	ActionMediaSeek        ActionType = "media.seek"
	ActionMediaTargetCycle ActionType = "media.target_cycle"
	ActionMediaNowPlaying  ActionType = "media.now_playing"

	// -- Spotify Web API (specs/milestones/M10-spotify-web-api.md) --

	ActionSpotifyLikeToggle         ActionType = "spotify.like_toggle"
	ActionSpotifyAddToPlaylist      ActionType = "spotify.add_to_playlist"
	ActionSpotifyRemoveFromPlaylist ActionType = "spotify.remove_from_playlist"
	ActionSpotifyStartPlaylist      ActionType = "spotify.start_playlist"
	ActionSpotifyQueueTrack         ActionType = "spotify.queue_track"
	ActionSpotifyTransferPlayback   ActionType = "spotify.transfer_playback"

	// -- Extended / system (specs/milestones/M11-extended-actions.md) --

	ActionSinkCycleDefault ActionType = "sink.cycle_default"
	ActionMicPushToTalk    ActionType = "mic.push_to_talk"
	ActionMicPushToMute    ActionType = "mic.push_to_mute"
	ActionShellRun         ActionType = "shell.run"
)

// Action is implemented by every concrete action-parameters type below.
// A Binding holds one Action; which concrete type it is determines both
// the JSON shape (via the "type" discriminator handled in binding.go)
// and, later, which handler in daemon/internal/actions runs it.
type Action interface {
	// ActionType returns the constant identifying this action's family.
	// Implementations must return a literal constant, not a computed
	// value, since it is also the map key in actionRegistry.
	ActionType() ActionType
}

// VolumeAdjustAction changes a target's volume by StepPercent per
// detent; fired on GestureTurn. StepPercent may be negative to invert
// the encoder's direction. CurveExponent, if non-zero, overrides the
// engine's default response curve (see specs/milestones/M03) for just
// this binding — useful for a fader-like control you want more
// precision at the low end of.
type VolumeAdjustAction struct {
	Target        Target  `json:"target"`
	StepPercent   float64 `json:"stepPercent"`
	CurveExponent float64 `json:"curveExponent,omitempty"`
}

func (VolumeAdjustAction) ActionType() ActionType { return ActionVolumeAdjust }

// VolumeSetAction jumps a target directly to Percent, e.g. a button
// bound to "always 50%".
type VolumeSetAction struct {
	Target  Target  `json:"target"`
	Percent float64 `json:"percent"`
}

func (VolumeSetAction) ActionType() ActionType { return ActionVolumeSet }

// VolumeMuteToggleAction toggles mute on a target. This is the default
// action for an encoder-push's short-press gesture (see
// specs/milestones/M04-mapping-engine-daemon.md).
type VolumeMuteToggleAction struct {
	Target Target `json:"target"`
}

func (VolumeMuteToggleAction) ActionType() ActionType { return ActionVolumeMuteToggle }

// VolumeBalanceAction adjusts left/right balance rather than overall
// level; Step is in the same [-1,1] balance units PipeWire uses, per
// detent.
type VolumeBalanceAction struct {
	Target Target  `json:"target"`
	Step   float64 `json:"step"`
}

func (VolumeBalanceAction) ActionType() ActionType { return ActionVolumeBalance }

// VolumeFollowAction maps a continuous control's absolute position onto
// a target's volume: the control at the bottom of its travel (Value 0)
// sets the target to MinPercent, at the top (Value 127) MaxPercent, and
// linearly in between. Fired on GestureMove — the fader is currently the
// only control that produces it. Unlike VolumeSetAction, it carries no
// fixed level of its own: the level comes from the triggering event's
// absolute position (see engine.Invocation.Value), not from a field
// here.
type VolumeFollowAction struct {
	Target Target `json:"target"`
	// MinPercent/MaxPercent bound the output range; both zero means the
	// default [0, 100].
	MinPercent float64 `json:"minPercent,omitempty"`
	MaxPercent float64 `json:"maxPercent,omitempty"`
}

func (VolumeFollowAction) ActionType() ActionType { return ActionVolumeFollow }

// AudioSoloToggleAction mutes every other known stream while leaving
// Target audible, and restores prior mute state on a second press.
type AudioSoloToggleAction struct {
	Target Target `json:"target"`
}

func (AudioSoloToggleAction) ActionType() ActionType { return ActionAudioSoloToggle }

// AudioDuckHoldAction drops every stream except Target to DuckPercent
// for as long as the control is held (GestureHold ... GestureRelease),
// restoring original levels on release.
type AudioDuckHoldAction struct {
	Target      Target  `json:"target"`
	DuckPercent float64 `json:"duckPercent"`
}

func (AudioDuckHoldAction) ActionType() ActionType { return ActionAudioDuckHold }

// SceneApplyAction restores the named Scene's saved levels and mute
// states.
type SceneApplyAction struct {
	SceneID string `json:"sceneId"`
}

func (SceneApplyAction) ActionType() ActionType { return ActionSceneApply }

// SceneSaveAction overwrites the named Scene with the current live mix
// (of whatever targets that scene's Entries already reference).
type SceneSaveAction struct {
	SceneID string `json:"sceneId"`
}

func (SceneSaveAction) ActionType() ActionType { return ActionSceneSave }

// LayerMomentaryAction switches the active layer to Layer only while
// the control is held, reverting to the previous layer on release. This
// is the default binding for the side buttons.
type LayerMomentaryAction struct {
	Layer int `json:"layer"`
}

func (LayerMomentaryAction) ActionType() ActionType { return ActionLayerMomentary }

// LayerLatchAction switches the active layer to Layer until something
// else changes it.
type LayerLatchAction struct {
	Layer int `json:"layer"`
}

func (LayerLatchAction) ActionType() ActionType { return ActionLayerLatch }

// LayerCycleAction advances the active layer to the next one in
// LayerOrder (wrapping), or 0->1->2->...->0 if LayerOrder is empty.
type LayerCycleAction struct {
	LayerOrder []int `json:"layerOrder,omitempty"`
}

func (LayerCycleAction) ActionType() ActionType { return ActionLayerCycle }

// KnobAssignFocusedAppAction binds the triggering encoder to whichever
// application currently owns the focused window, replacing whatever
// VolumeAdjustAction it had. This is the "press a knob to grab the
// active app" feature requested for the project.
type KnobAssignFocusedAppAction struct {
	// StepPercent carries over to the newly-created VolumeAdjustAction;
	// if zero, the engine's default is used.
	StepPercent float64 `json:"stepPercent,omitempty"`
}

func (KnobAssignFocusedAppAction) ActionType() ActionType { return ActionKnobAssignFocusedApp }

// KnobClearAction removes whatever binding the triggering control
// currently has (on its own gesture, not a target's), leaving it
// unbound.
type KnobClearAction struct{}

func (KnobClearAction) ActionType() ActionType { return ActionKnobClear }

// KnobLockToggleAction freezes/unfreezes the triggering encoder so it
// stops responding to turns, to avoid accidental nudges.
type KnobLockToggleAction struct{}

func (KnobLockToggleAction) ActionType() ActionType { return ActionKnobLockToggle }

// MediaCommand enumerates the transport operations MediaTransportAction
// can send. See specs/milestones/M09-media-transport-mpris.md.
type MediaCommand string

const (
	MediaPlayPause     MediaCommand = "play_pause"
	MediaNext          MediaCommand = "next"
	MediaPrevious      MediaCommand = "previous"
	MediaShuffleToggle MediaCommand = "shuffle_toggle"
	MediaRepeatCycle   MediaCommand = "repeat_cycle"
)

// MediaCommands returns every valid MediaCommand, for validation and for
// daemon/internal/schema's enum generation.
func MediaCommands() []MediaCommand {
	return []MediaCommand{
		MediaPlayPause, MediaNext, MediaPrevious, MediaShuffleToggle, MediaRepeatCycle,
	}
}

// MediaTransportAction sends one MPRIS transport command to PlayerRef
// (an MPRIS bus name suffix, e.g. "spotify"), or to whichever player is
// currently selected if PlayerRef is empty.
type MediaTransportAction struct {
	Command   MediaCommand `json:"command"`
	PlayerRef string       `json:"playerRef,omitempty"`
}

func (MediaTransportAction) ActionType() ActionType { return ActionMediaTransport }

// MediaSeekAction seeks the current track by SeekMs milliseconds
// (negative to seek backward) per detent; fired on GestureTurn.
type MediaSeekAction struct {
	SeekMs    int64  `json:"seekMs"`
	PlayerRef string `json:"playerRef,omitempty"`
}

func (MediaSeekAction) ActionType() ActionType { return ActionMediaSeek }

// MediaTargetCycleAction advances the "currently selected" MPRIS player
// (the one PlayerRef-less MediaTransportAction/MediaSeekAction/
// MediaNowPlayingAction bindings act on) to the next running, non-
// ignored player.
type MediaTargetCycleAction struct{}

func (MediaTargetCycleAction) ActionType() ActionType { return ActionMediaTargetCycle }

// MediaNowPlayingAction shows a desktop notification naming PlayerRef's
// (or, if empty, the currently selected player's) current track.
type MediaNowPlayingAction struct {
	PlayerRef string `json:"playerRef,omitempty"`
}

func (MediaNowPlayingAction) ActionType() ActionType { return ActionMediaNowPlaying }

// SpotifyLikeToggleAction saves the currently-playing track to the
// user's Liked Songs library if it isn't already there, or removes it
// if it is. Resolved against Spotify's own "currently playing" endpoint
// at dispatch time -- there is no Target/PlayerRef, unlike the M09
// media actions, since this only ever means "the Spotify account this
// daemon is authorized as," not an MPRIS bus name.
type SpotifyLikeToggleAction struct{}

func (SpotifyLikeToggleAction) ActionType() ActionType { return ActionSpotifyLikeToggle }

// SpotifyAddToPlaylistAction adds the currently-playing track to a
// specific, pre-configured playlist. PlaylistID accepts a bare base62
// playlist ID, a spotify:playlist:... URI, or an open.spotify.com
// playlist URL -- see daemon/internal/spotify.ParseID, which the
// handler normalizes it through.
type SpotifyAddToPlaylistAction struct {
	PlaylistID string `json:"playlistId"`
}

func (SpotifyAddToPlaylistAction) ActionType() ActionType { return ActionSpotifyAddToPlaylist }

// SpotifyRemoveFromPlaylistAction removes the currently-playing track
// from PlaylistID (same ID forms as SpotifyAddToPlaylistAction).
type SpotifyRemoveFromPlaylistAction struct {
	PlaylistID string `json:"playlistId"`
}

func (SpotifyRemoveFromPlaylistAction) ActionType() ActionType {
	return ActionSpotifyRemoveFromPlaylist
}

// SpotifyStartPlaylistAction starts playback of PlaylistID on the
// currently active Spotify Connect device.
type SpotifyStartPlaylistAction struct {
	PlaylistID string `json:"playlistId"`
}

func (SpotifyStartPlaylistAction) ActionType() ActionType { return ActionSpotifyStartPlaylist }

// SpotifyQueueTrackAction adds TrackID (a bare ID, spotify:track:... URI,
// or open.spotify.com track URL) to the playback queue on the currently
// active device.
type SpotifyQueueTrackAction struct {
	TrackID string `json:"trackId"`
}

func (SpotifyQueueTrackAction) ActionType() ActionType { return ActionSpotifyQueueTrack }

// SpotifyTransferPlaybackAction moves playback to the Spotify Connect
// device named DeviceName (matched case-insensitively against
// GET /spotify/devices at dispatch time -- device IDs are not stable
// across client restarts, so binding by name is the only usable
// option). Play controls whether playback resumes on the new device
// (Spotify's own transfer endpoint's "play" flag) or stays paused.
type SpotifyTransferPlaybackAction struct {
	DeviceName string `json:"deviceName"`
	Play       bool   `json:"play,omitempty"`
}

func (SpotifyTransferPlaybackAction) ActionType() ActionType { return ActionSpotifyTransferPlayback }

// SinkCycleDefaultAction advances the system default sink to the next
// entry in SinkNames (wrapping), e.g. cycling headphones -> speakers ->
// HDMI on one button.
type SinkCycleDefaultAction struct {
	SinkNames []string `json:"sinkNames"`
}

func (SinkCycleDefaultAction) ActionType() ActionType { return ActionSinkCycleDefault }

// MicPushToTalkAction unmutes the default source while held, muting it
// again on release.
type MicPushToTalkAction struct{}

func (MicPushToTalkAction) ActionType() ActionType { return ActionMicPushToTalk }

// MicPushToMuteAction is the inverse of push-to-talk: mutes the default
// source while held.
type MicPushToMuteAction struct{}

func (MicPushToMuteAction) ActionType() ActionType { return ActionMicPushToMute }

// ShellRunAction runs Command (via "sh -c") as the escape hatch for any
// action not otherwise modeled. Command runs with the daemon's own
// environment and user privileges — never with elevated privileges, and
// the UI must show it verbatim (not silently) wherever a binding is
// displayed, since it can do anything the user's shell can do.
type ShellRunAction struct {
	Command string `json:"command"`
}

func (ShellRunAction) ActionType() ActionType { return ActionShellRun }

// actionEntry pairs an ActionType's JSON decoder with its zero value.
// The zero value carries no behavior on its own — decodeInto's T is what
// actually gets decoded into — but exposing it (via ZeroAction) gives
// daemon/internal/schema a concrete reflect.Type to generate a JSON
// Schema branch from, which a bare decoder closure (T erased) cannot.
type actionEntry struct {
	decode func(json.RawMessage) (Action, error)
	zero   Action
}

// newActionEntry builds an actionEntry for T. T must be one of the
// concrete action-params types below (i.e. implement Action on its
// value receiver).
func newActionEntry[T Action]() actionEntry {
	var zero T
	return actionEntry{decode: decodeInto[T], zero: zero}
}

// actionRegistry maps each ActionType to its decoder and zero value.
// Every concrete Action type must be registered here for Binding's JSON
// (de)serialization (see binding.go) and for action_test.go's
// completeness check, which fails the build if a new ActionType constant
// is added above without a matching registry entry.
var actionRegistry = map[ActionType]actionEntry{
	ActionVolumeAdjust:              newActionEntry[VolumeAdjustAction](),
	ActionVolumeSet:                 newActionEntry[VolumeSetAction](),
	ActionVolumeMuteToggle:          newActionEntry[VolumeMuteToggleAction](),
	ActionVolumeBalance:             newActionEntry[VolumeBalanceAction](),
	ActionVolumeFollow:              newActionEntry[VolumeFollowAction](),
	ActionAudioSoloToggle:           newActionEntry[AudioSoloToggleAction](),
	ActionAudioDuckHold:             newActionEntry[AudioDuckHoldAction](),
	ActionSceneApply:                newActionEntry[SceneApplyAction](),
	ActionSceneSave:                 newActionEntry[SceneSaveAction](),
	ActionLayerMomentary:            newActionEntry[LayerMomentaryAction](),
	ActionLayerLatch:                newActionEntry[LayerLatchAction](),
	ActionLayerCycle:                newActionEntry[LayerCycleAction](),
	ActionKnobAssignFocusedApp:      newActionEntry[KnobAssignFocusedAppAction](),
	ActionKnobClear:                 newActionEntry[KnobClearAction](),
	ActionKnobLockToggle:            newActionEntry[KnobLockToggleAction](),
	ActionMediaTransport:            newActionEntry[MediaTransportAction](),
	ActionMediaSeek:                 newActionEntry[MediaSeekAction](),
	ActionMediaTargetCycle:          newActionEntry[MediaTargetCycleAction](),
	ActionMediaNowPlaying:           newActionEntry[MediaNowPlayingAction](),
	ActionSpotifyLikeToggle:         newActionEntry[SpotifyLikeToggleAction](),
	ActionSpotifyAddToPlaylist:      newActionEntry[SpotifyAddToPlaylistAction](),
	ActionSpotifyRemoveFromPlaylist: newActionEntry[SpotifyRemoveFromPlaylistAction](),
	ActionSpotifyStartPlaylist:      newActionEntry[SpotifyStartPlaylistAction](),
	ActionSpotifyQueueTrack:         newActionEntry[SpotifyQueueTrackAction](),
	ActionSpotifyTransferPlayback:   newActionEntry[SpotifyTransferPlaybackAction](),
	ActionSinkCycleDefault:          newActionEntry[SinkCycleDefaultAction](),
	ActionMicPushToTalk:             newActionEntry[MicPushToTalkAction](),
	ActionMicPushToMute:             newActionEntry[MicPushToMuteAction](),
	ActionShellRun:                  newActionEntry[ShellRunAction](),
}

// decodeInto unmarshals raw into a zero-value T and returns it as an
// Action. T must be one of the concrete action-params types above (i.e.
// implement Action on its value receiver).
func decodeInto[T Action](raw json.RawMessage) (Action, error) {
	var v T
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("model: decode %T: %w", v, err)
		}
	}
	return v, nil
}

// ActionTypes returns every registered ActionType, sorted, so callers
// (notably daemon/internal/schema) get a deterministic order regardless
// of map iteration.
func ActionTypes() []ActionType {
	types := make([]ActionType, 0, len(actionRegistry))
	for t := range actionRegistry {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	return types
}

// ZeroAction returns the zero value of the concrete Action type
// registered for t, for callers that need a reflect.Type to introspect
// (see daemon/internal/schema) rather than a decoded value. ok is false
// if t is not registered.
func ZeroAction(t ActionType) (action Action, ok bool) {
	entry, ok := actionRegistry[t]
	if !ok {
		return nil, false
	}
	return entry.zero, true
}

// DecodeAction decodes a {"type": ..., "params": {...}} document into
// the concrete Action it names.
func DecodeAction(data []byte) (Action, error) {
	var envelope struct {
		Type   ActionType      `json:"type"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("model: decode action envelope: %w", err)
	}
	entry, ok := actionRegistry[envelope.Type]
	if !ok {
		return nil, fmt.Errorf("model: unknown action type %q", envelope.Type)
	}
	action, err := entry.decode(envelope.Params)
	if err != nil {
		return nil, fmt.Errorf("model: decode action %q: %w", envelope.Type, err)
	}
	return action, nil
}

// EncodeAction encodes an Action as a {"type": ..., "params": {...}}
// document.
func EncodeAction(a Action) ([]byte, error) {
	if a == nil {
		return nil, fmt.Errorf("model: cannot encode nil action")
	}
	params, err := json.Marshal(a)
	if err != nil {
		return nil, fmt.Errorf("model: encode %T params: %w", a, err)
	}
	envelope := struct {
		Type   ActionType      `json:"type"`
		Params json.RawMessage `json:"params"`
	}{Type: a.ActionType(), Params: params}
	return json.Marshal(envelope)
}
