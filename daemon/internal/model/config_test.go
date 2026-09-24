package model

import "testing"

func TestDefaultConfigIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("Default() config is not valid: %v", err)
	}
}

// TestDefaultConfigStarterBindings pins M05's addition: encoder/button 1
// on default_sink, encoder/button 2 on default_source, and the fader
// following default_sink -- so there's something to see on a
// freshly-installed system's LEDs (see Default's doc comment).
func TestDefaultConfigStarterBindings(t *testing.T) {
	c := Default()
	if len(c.Profiles) != 1 {
		t.Fatalf("Default() has %d profiles, want 1", len(c.Profiles))
	}
	bindings := c.Profiles[0].Bindings

	want := []struct {
		control Control
		gesture Gesture
		target  Target
	}{
		{Control{Kind: ControlEncoder, Index: 1}, GestureTurn, Target{Kind: TargetDefaultSink}},
		{Control{Kind: ControlButton, Index: 1}, GesturePress, Target{Kind: TargetDefaultSink}},
		{Control{Kind: ControlEncoder, Index: 2}, GestureTurn, Target{Kind: TargetDefaultSource}},
		{Control{Kind: ControlButton, Index: 2}, GesturePress, Target{Kind: TargetDefaultSource}},
		{Control{Kind: ControlFader, Index: 1}, GestureMove, Target{Kind: TargetDefaultSink}},
	}
	const wantExtra = 6 + 4 // 6 encoder-push 3-8 assign bindings, 4 side-button layer bindings, both below
	if len(bindings) != len(want)+wantExtra {
		t.Fatalf("Default() has %d bindings, want %d", len(bindings), len(want)+wantExtra)
	}
	for i, w := range want {
		b := bindings[i]
		if b.Control != w.control || b.Gesture != w.gesture {
			t.Errorf("binding %d = (%+v, %v), want (%+v, %v)", i, b.Control, b.Gesture, w.control, w.gesture)
		}
		target, ok := TargetOf(b.Action)
		if !ok || target != w.target {
			t.Errorf("binding %d target = %+v (ok=%v), want %+v", i, target, ok, w.target)
		}
	}

	bound := make(map[Control]Binding)
	for _, b := range bindings {
		bound[b.Control] = b
	}

	// Encoders/buttons 3-8 themselves are still left unbound.
	for i := 3; i <= 8; i++ {
		if _, ok := bound[Control{Kind: ControlEncoder, Index: i}]; ok {
			t.Errorf("encoder %d unexpectedly bound in Default()", i)
		}
		if _, ok := bound[Control{Kind: ControlButton, Index: i}]; ok {
			t.Errorf("button %d unexpectedly bound in Default()", i)
		}
	}

	// M06: encoder-push 3-8's hold is bound to knob.assign_focused_app,
	// so a freshly-installed system can demonstrate the feature with no
	// config editing.
	for i := 3; i <= 8; i++ {
		b, ok := bound[Control{Kind: ControlEncoderPush, Index: i}]
		if !ok {
			t.Errorf("encoder_push %d not bound in Default()", i)
			continue
		}
		if b.Gesture != GestureHold {
			t.Errorf("encoder_push %d bound on gesture %v, want %v", i, b.Gesture, GestureHold)
		}
		if _, ok := b.Action.(KnobAssignFocusedAppAction); !ok {
			t.Errorf("encoder_push %d action = %T, want KnobAssignFocusedAppAction", i, b.Action)
		}
	}

	// M08: side button 1 switches to layer 1 (hold=momentary,
	// press=latch), side button 2 to layer 2 -- see Default's doc
	// comment.
	byControlGesture := make(map[Control]map[Gesture]Action)
	for _, b := range bindings {
		if byControlGesture[b.Control] == nil {
			byControlGesture[b.Control] = make(map[Gesture]Action)
		}
		byControlGesture[b.Control][b.Gesture] = b.Action
	}
	for i, layer := range []int{1, 2} {
		side := Control{Kind: ControlSideButton, Index: i + 1}
		hold, ok := byControlGesture[side][GestureHold].(LayerMomentaryAction)
		if !ok || hold.Layer != layer {
			t.Errorf("side button %d hold = %+v (ok=%v), want LayerMomentaryAction{Layer: %d}", side.Index, hold, ok, layer)
		}
		press, ok := byControlGesture[side][GesturePress].(LayerLatchAction)
		if !ok || press.Layer != layer {
			t.Errorf("side button %d press = %+v (ok=%v), want LayerLatchAction{Layer: %d}", side.Index, press, ok, layer)
		}
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
