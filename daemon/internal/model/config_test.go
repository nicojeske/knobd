package model

import "testing"

func TestDefaultConfigIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("Default() config is not valid: %v", err)
	}
}

func validAppMatcherConfig() Config {
	c := Default()
	c.AppMatchers = []AppMatcher{
		{ID: "vesktop", DisplayName: "Vesktop", Binaries: []string{"vesktop"}},
		{ID: "java-app", DisplayName: "Java App", NodeNames: []string{"java"}},
	}
	c.AppGroups = []AppGroup{
		{ID: "voice", DisplayName: "Voice", MatcherIDs: []string{"vesktop"}},
	}
	return c
}

func TestConfigValidate_DuplicateMatcherID(t *testing.T) {
	c := validAppMatcherConfig()
	c.AppMatchers = append(c.AppMatchers, AppMatcher{ID: "vesktop", AppNames: []string{"x"}})
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for duplicate app matcher id")
	}
}

func TestConfigValidate_GroupReferencesUnknownMatcher(t *testing.T) {
	c := validAppMatcherConfig()
	c.AppGroups[0].MatcherIDs = append(c.AppGroups[0].MatcherIDs, "does-not-exist")
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for app group referencing unknown matcher")
	}
}

func TestConfigValidate_BindingReferencesUnknownApp(t *testing.T) {
	c := validAppMatcherConfig()
	c.Profiles[0].Bindings = []Binding{
		{
			Control: Control{Kind: ControlEncoder, Index: 1},
			Gesture: GestureTurn,
			Action:  VolumeAdjustAction{Target: Target{Kind: TargetApp, Ref: "not-registered"}, StepPercent: 2},
		},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for binding referencing unknown app matcher")
	}
}

func TestConfigValidate_BindingReferencesKnownApp(t *testing.T) {
	c := validAppMatcherConfig()
	c.Profiles[0].Bindings = []Binding{
		{
			Control: Control{Kind: ControlEncoder, Index: 1},
			Gesture: GestureTurn,
			Action:  VolumeAdjustAction{Target: Target{Kind: TargetApp, Ref: "vesktop"}, StepPercent: 2},
		},
		{
			Control: Control{Kind: ControlEncoderPush, Index: 1},
			Gesture: GesturePress,
			Action:  VolumeMuteToggleAction{Target: Target{Kind: TargetGroup, Ref: "voice"}},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestConfigValidate_SceneReferencesUnknownScene(t *testing.T) {
	c := validAppMatcherConfig()
	c.Profiles[0].Bindings = []Binding{
		{
			Control: Control{Kind: ControlButton, Index: 1},
			Gesture: GesturePress,
			Action:  SceneApplyAction{SceneID: "meeting"},
		},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for binding referencing unknown scene")
	}
}

func TestConfigValidate_ActiveProfileMustExist(t *testing.T) {
	c := Default()
	c.ActiveProfileID = "ghost"
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for nonexistent active profile")
	}
}

func TestConfigValidate_DuplicateProfileID(t *testing.T) {
	c := Default()
	c.Profiles = append(c.Profiles, Profile{ID: "default", DisplayName: "Dup"})
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for duplicate profile id")
	}
}

func TestConfigValidate_AppMatcherNeedsCriteria(t *testing.T) {
	c := Default()
	c.AppMatchers = []AppMatcher{{ID: "empty", DisplayName: "Nothing"}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for app matcher with no matching criteria")
	}
}
