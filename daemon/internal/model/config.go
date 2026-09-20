package model

import "fmt"

// CurrentSchemaVersion is the schema version this build of knobd writes.
// Bumping it requires adding a step to the migration chain in
// daemon/internal/config (see specs/milestones/M01-foundations.md).
const CurrentSchemaVersion = 1

// Config is the full on-disk shape of ~/.config/knobd/config.json. It is
// loaded/saved/migrated by daemon/internal/config; model only defines
// the shape and cross-referential validation.
type Config struct {
	SchemaVersion   int          `json:"schemaVersion"`
	ActiveProfileID string       `json:"activeProfileId"`
	Profiles        []Profile    `json:"profiles"`
	AppMatchers     []AppMatcher `json:"appMatchers"`
	AppGroups       []AppGroup   `json:"appGroups"`
	Scenes          []Scene      `json:"scenes"`
}

// Profile is a named, independently selectable set of bindings, e.g.
// "Default" vs. "Streaming". Only one Profile is active at a time
// (Config.ActiveProfileID); switching profiles is distinct from
// switching layers within a profile (see LayerLatchAction).
type Profile struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	Bindings    []Binding `json:"bindings"`
}

// SceneEntry is one target's saved level/mute state within a Scene.
type SceneEntry struct {
	Target        Target  `json:"target"`
	VolumePercent float64 `json:"volumePercent"`
	Muted         bool    `json:"muted"`
}

// Scene is a named, recallable snapshot of several targets' volume and
// mute state, e.g. "Meeting" = {music: 10%, discord: 100%, mic: unmuted}.
// See ActionSceneApply / ActionSceneSave.
type Scene struct {
	ID          string       `json:"id"`
	DisplayName string       `json:"displayName"`
	Entries     []SceneEntry `json:"entries"`
}

// Validate checks the whole config for internal consistency: unique IDs
// within each collection, every cross-reference (ActiveProfileID, a
// Binding's TargetApp/TargetGroup ref, an AppGroup's matcher IDs, a
// scene entry's TargetApp/TargetGroup ref) pointing at something that
// actually exists, and every Profile/Binding/AppMatcher individually
// valid. It does not — and cannot — check that a TargetSink/TargetSource
// ref names a device that currently exists, since devices come and go;
// that is a runtime concern for daemon/internal/audio.
func (c Config) Validate() error {
	matcherIDs := make(map[string]bool, len(c.AppMatchers))
	for _, m := range c.AppMatchers {
		if matcherIDs[m.ID] {
			return fmt.Errorf("model: duplicate app matcher id %q", m.ID)
		}
		matcherIDs[m.ID] = true
		if err := m.Validate(); err != nil {
			return err
		}
	}

	groupIDs := make(map[string]bool, len(c.AppGroups))
	for _, g := range c.AppGroups {
		if groupIDs[g.ID] {
			return fmt.Errorf("model: duplicate app group id %q", g.ID)
		}
		groupIDs[g.ID] = true
		if len(g.MatcherIDs) == 0 {
			return fmt.Errorf("model: app group %q has no matchers", g.ID)
		}
		for _, ref := range g.MatcherIDs {
			if !matcherIDs[ref] {
				return fmt.Errorf("model: app group %q references unknown matcher %q", g.ID, ref)
			}
		}
	}

	sceneIDs := make(map[string]bool, len(c.Scenes))
	for _, s := range c.Scenes {
		if sceneIDs[s.ID] {
			return fmt.Errorf("model: duplicate scene id %q", s.ID)
		}
		sceneIDs[s.ID] = true
		for _, e := range s.Entries {
			if err := c.validateTargetRef(e.Target, matcherIDs, groupIDs); err != nil {
				return fmt.Errorf("model: scene %q: %w", s.ID, err)
			}
		}
	}

	profileIDs := make(map[string]bool, len(c.Profiles))
	for _, p := range c.Profiles {
		if profileIDs[p.ID] {
			return fmt.Errorf("model: duplicate profile id %q", p.ID)
		}
		profileIDs[p.ID] = true
		for i, b := range p.Bindings {
			if err := b.Validate(); err != nil {
				return fmt.Errorf("model: profile %q binding %d: %w", p.ID, i, err)
			}
			if target, ok := targetOf(b.Action); ok {
				if err := c.validateTargetRef(target, matcherIDs, groupIDs); err != nil {
					return fmt.Errorf("model: profile %q binding %d: %w", p.ID, i, err)
				}
			}
			if sceneID, ok := sceneRefOf(b.Action); ok && !sceneIDs[sceneID] {
				return fmt.Errorf("model: profile %q binding %d references unknown scene %q", p.ID, i, sceneID)
			}
		}
	}

	if c.ActiveProfileID != "" && !profileIDs[c.ActiveProfileID] {
		return fmt.Errorf("model: active profile %q does not exist", c.ActiveProfileID)
	}

	return nil
}

func (c Config) validateTargetRef(t Target, matcherIDs, groupIDs map[string]bool) error {
	if err := t.Validate(); err != nil {
		return err
	}
	switch t.Kind {
	case TargetApp:
		if !matcherIDs[t.Ref] {
			return fmt.Errorf("references unknown app matcher %q", t.Ref)
		}
	case TargetGroup:
		if !groupIDs[t.Ref] {
			return fmt.Errorf("references unknown app group %q", t.Ref)
		}
	}
	return nil
}

// targetOf extracts the Target an Action carries, for actions that carry
// exactly one. Actions with no target (e.g. LayerMomentaryAction) return
// ok=false.
func targetOf(a Action) (Target, bool) {
	switch v := a.(type) {
	case VolumeAdjustAction:
		return v.Target, true
	case VolumeSetAction:
		return v.Target, true
	case VolumeMuteToggleAction:
		return v.Target, true
	case VolumeBalanceAction:
		return v.Target, true
	case AudioSoloToggleAction:
		return v.Target, true
	case AudioDuckHoldAction:
		return v.Target, true
	default:
		return Target{}, false
	}
}

// sceneRefOf extracts the scene ID an Action carries, for the two
// actions that reference one.
func sceneRefOf(a Action) (string, bool) {
	switch v := a.(type) {
	case SceneApplyAction:
		return v.SceneID, true
	case SceneSaveAction:
		return v.SceneID, true
	default:
		return "", false
	}
}

// Default returns a minimal, valid, empty configuration: one profile
// named "Default" with no bindings, active. This is what
// daemon/internal/config writes on first run, before any binding has
// been made in the UI.
func Default() Config {
	return Config{
		SchemaVersion:   CurrentSchemaVersion,
		ActiveProfileID: "default",
		Profiles: []Profile{
			{ID: "default", DisplayName: "Default", Bindings: []Binding{}},
		},
		AppMatchers: []AppMatcher{},
		AppGroups:   []AppGroup{},
		Scenes:      []Scene{},
	}
}
