package actions

import (
	"context"
	"errors"
	"testing"

	"github.com/njeske/knobd/internal/focus"
	"github.com/njeske/knobd/internal/model"
)

// fakeConfigStore is a ConfigMutator test double: Config/SetConfig
// mirror *configStore's semantics (SetConfig commits cfg and records it)
// without the engine round trip or disk I/O.
type fakeConfigStore struct {
	cfg   model.Config
	saved []model.Config
	err   error
}

func (f *fakeConfigStore) Config() model.Config { return f.cfg }

func (f *fakeConfigStore) SetConfig(ctx context.Context, cfg model.Config) error {
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, cfg)
	f.cfg = cfg
	return nil
}

type errFocusReporter struct{ err error }

func (e errFocusReporter) Current(context.Context) (focus.AppInfo, error) {
	return focus.AppInfo{}, e.err
}

func push(index int) model.Control {
	return model.Control{Kind: model.ControlEncoderPush, Index: index}
}
func encoder(index int) model.Control { return model.Control{Kind: model.ControlEncoder, Index: index} }

// baseConfig is a minimal valid config with one profile, no bindings on
// encoder 3, and no app matchers -- the starting point most tests build
// from.
func baseConfig() model.Config {
	return model.Config{
		SchemaVersion:   model.CurrentSchemaVersion,
		ActiveProfileID: "default",
		Profiles: []model.Profile{
			{ID: "default", DisplayName: "Default", Bindings: []model.Binding{
				{Control: push(3), Gesture: model.GestureHold, Action: model.KnobAssignFocusedAppAction{}},
			}},
		},
	}
}

func newAssignHandlersFor(cfg model.Config, info focus.AppInfo, opts AssignOptions) (*AssignHandlers, *fakeConfigStore) {
	store := &fakeConfigStore{cfg: cfg}
	fp := focus.NewFakeProvider()
	fp.SetFocused(info)
	return NewAssignHandlers(store, fp, opts), store
}

func TestAssignFreshOntoUnboundEncoder(t *testing.T) {
	h, store := newAssignHandlersFor(baseConfig(), focus.AppInfo{ResourceClass: "brave-browser"}, AssignOptions{})

	inv := Invocation{Action: model.KnobAssignFocusedAppAction{}, Control: push(3), Gesture: model.GestureHold, Layer: 0}
	if err := h.execute(context.Background(), inv); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(store.saved) != 1 {
		t.Fatalf("SetConfig called %d times, want 1", len(store.saved))
	}
	cfg := store.cfg
	if len(cfg.AppMatchers) != 1 {
		t.Fatalf("AppMatchers = %+v, want exactly 1 new matcher", cfg.AppMatchers)
	}
	if cfg.AppMatchers[0].ID != "brave" {
		t.Errorf("matcher ID = %q, want %q", cfg.AppMatchers[0].ID, "brave")
	}

	bindings := cfg.Profiles[0].Bindings
	var found *model.Binding
	for i := range bindings {
		if bindings[i].Control == encoder(3) && bindings[i].Gesture == model.GestureTurn {
			found = &bindings[i]
		}
	}
	if found == nil {
		t.Fatal("no turn binding was written for encoder 3")
	}
	adj, ok := found.Action.(model.VolumeAdjustAction)
	if !ok {
		t.Fatalf("encoder 3's turn action = %T, want VolumeAdjustAction", found.Action)
	}
	if adj.Target != (model.Target{Kind: model.TargetApp, Ref: "brave"}) {
		t.Errorf("Target = %+v, want app:brave", adj.Target)
	}
	if adj.StepPercent != defaultAssignStepPercent {
		t.Errorf("StepPercent = %v, want the default %v", adj.StepPercent, defaultAssignStepPercent)
	}

	// The original hold binding on the push must be left alone, so
	// holding it again reassigns.
	var pushStillBound bool
	for _, b := range bindings {
		if b.Control == push(3) && b.Gesture == model.GestureHold {
			pushStillBound = true
		}
	}
	if !pushStillBound {
		t.Error("the encoder_push's hold binding was removed; it must stay bound to allow reassignment")
	}
}

func TestAssignReassignReplacesExistingBindingInPlace(t *testing.T) {
	cfg := baseConfig()
	cfg.AppMatchers = []model.AppMatcher{{ID: "vesktop", AppNames: []string{"vesktop"}}}
	cfg.Profiles[0].Bindings = append(cfg.Profiles[0].Bindings, model.Binding{
		Control: encoder(3), Gesture: model.GestureTurn,
		Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetApp, Ref: "vesktop"}, StepPercent: 5},
	})

	h, store := newAssignHandlersFor(cfg, focus.AppInfo{ResourceClass: "brave-browser"}, AssignOptions{})
	inv := Invocation{Action: model.KnobAssignFocusedAppAction{}, Control: push(3), Gesture: model.GestureHold, Layer: 0}
	if err := h.execute(context.Background(), inv); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	bindings := store.cfg.Profiles[0].Bindings
	var turnCount int
	var target model.Target
	for _, b := range bindings {
		if b.Control == encoder(3) && b.Gesture == model.GestureTurn {
			turnCount++
			target = b.Action.(model.VolumeAdjustAction).Target
		}
	}
	if turnCount != 1 {
		t.Fatalf("found %d turn bindings for encoder 3, want exactly 1 (replaced in place, not duplicated)", turnCount)
	}
	if target.Ref != "brave" {
		t.Errorf("Target.Ref = %q, want %q", target.Ref, "brave")
	}
}

func TestAssignMatcherReuseDoesNotDuplicate(t *testing.T) {
	cfg := baseConfig()
	cfg.AppMatchers = []model.AppMatcher{{ID: "brave", Binaries: []string{"brave"}, DisplayName: "Brave"}}

	h, store := newAssignHandlersFor(cfg, focus.AppInfo{ResourceClass: "brave-browser", Binary: "brave"}, AssignOptions{})
	inv := Invocation{Action: model.KnobAssignFocusedAppAction{}, Control: push(3), Gesture: model.GestureHold, Layer: 0}
	if err := h.execute(context.Background(), inv); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(store.cfg.AppMatchers) != 1 {
		t.Fatalf("AppMatchers = %+v, want the existing matcher reused, not duplicated", store.cfg.AppMatchers)
	}
	if store.cfg.AppMatchers[0].DisplayName != "Brave" {
		t.Errorf("existing matcher was modified: DisplayName = %q, want the original %q preserved", store.cfg.AppMatchers[0].DisplayName, "Brave")
	}
}

func TestAssignIDCollisionWithoutOverlapDisambiguates(t *testing.T) {
	cfg := baseConfig()
	// An unrelated matcher that happens to already use the "brave" id,
	// with criteria that share nothing with the focused app's tokens.
	cfg.AppMatchers = []model.AppMatcher{{ID: "brave", NodeNames: []string{"some-other-node"}}}

	h, store := newAssignHandlersFor(cfg, focus.AppInfo{ResourceClass: "brave-browser"}, AssignOptions{})
	inv := Invocation{Action: model.KnobAssignFocusedAppAction{}, Control: push(3), Gesture: model.GestureHold, Layer: 0}
	if err := h.execute(context.Background(), inv); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(store.cfg.AppMatchers) != 2 {
		t.Fatalf("AppMatchers = %+v, want 2 (the original plus a disambiguated new one)", store.cfg.AppMatchers)
	}
	if store.cfg.AppMatchers[1].ID != "brave-2" {
		t.Errorf("new matcher ID = %q, want %q", store.cfg.AppMatchers[1].ID, "brave-2")
	}
}

func TestAssignStepPercentPrecedence(t *testing.T) {
	cases := []struct {
		name     string
		action   model.KnobAssignFocusedAppAction
		opts     AssignOptions
		wantStep float64
	}{
		{"explicit action step wins", model.KnobAssignFocusedAppAction{StepPercent: 10}, AssignOptions{DefaultStepPercent: 7}, 10},
		{"falls back to option default", model.KnobAssignFocusedAppAction{}, AssignOptions{DefaultStepPercent: 7}, 7},
		{"falls back to the hardcoded default", model.KnobAssignFocusedAppAction{}, AssignOptions{}, defaultAssignStepPercent},
		{"clamped to the max", model.KnobAssignFocusedAppAction{StepPercent: 999}, AssignOptions{}, maxAssignStepPercent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, store := newAssignHandlersFor(baseConfig(), focus.AppInfo{ResourceClass: "brave-browser"}, tc.opts)
			inv := Invocation{Action: tc.action, Control: push(3), Gesture: model.GestureHold, Layer: 0}
			if err := h.execute(context.Background(), inv); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			for _, b := range store.cfg.Profiles[0].Bindings {
				if b.Control == encoder(3) && b.Gesture == model.GestureTurn {
					if got := b.Action.(model.VolumeAdjustAction).StepPercent; got != tc.wantStep {
						t.Errorf("StepPercent = %v, want %v", got, tc.wantStep)
					}
					return
				}
			}
			t.Fatal("no turn binding written")
		})
	}
}

// TestAssignStepPercentDoesNotInheritFromReplacedBinding pins that the
// action's own StepPercent (or the default) is what's used -- never the
// step of whatever binding is being overwritten.
func TestAssignStepPercentDoesNotInheritFromReplacedBinding(t *testing.T) {
	cfg := baseConfig()
	cfg.AppMatchers = []model.AppMatcher{{ID: "vesktop", AppNames: []string{"vesktop"}}}
	cfg.Profiles[0].Bindings = append(cfg.Profiles[0].Bindings, model.Binding{
		Control: encoder(3), Gesture: model.GestureTurn,
		Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetApp, Ref: "vesktop"}, StepPercent: 33},
	})

	h, store := newAssignHandlersFor(cfg, focus.AppInfo{ResourceClass: "brave-browser"}, AssignOptions{})
	inv := Invocation{Action: model.KnobAssignFocusedAppAction{}, Control: push(3), Gesture: model.GestureHold, Layer: 0}
	if err := h.execute(context.Background(), inv); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, b := range store.cfg.Profiles[0].Bindings {
		if b.Control == encoder(3) && b.Gesture == model.GestureTurn {
			if got := b.Action.(model.VolumeAdjustAction).StepPercent; got != defaultAssignStepPercent {
				t.Errorf("StepPercent = %v, want the default %v, not the replaced binding's 33", got, defaultAssignStepPercent)
			}
			return
		}
	}
	t.Fatal("no turn binding written")
}

func TestAssignWrongControlKindErrorsWithNoSetConfig(t *testing.T) {
	h, store := newAssignHandlersFor(baseConfig(), focus.AppInfo{ResourceClass: "brave-browser"}, AssignOptions{})
	inv := Invocation{Action: model.KnobAssignFocusedAppAction{}, Control: model.Control{Kind: model.ControlButton, Index: 1}, Gesture: model.GestureHold}
	if err := h.execute(context.Background(), inv); err == nil {
		t.Fatal("expected an error for a non-encoder_push control")
	}
	if len(store.saved) != 0 {
		t.Errorf("SetConfig was called %d times, want 0", len(store.saved))
	}
}

func TestAssignFocusCurrentErrorPropagatesWithNoSetConfig(t *testing.T) {
	store := &fakeConfigStore{cfg: baseConfig()}
	h := NewAssignHandlers(store, errFocusReporter{err: errors.New("no session bus")}, AssignOptions{})
	inv := Invocation{Action: model.KnobAssignFocusedAppAction{}, Control: push(3), Gesture: model.GestureHold}
	if err := h.execute(context.Background(), inv); err == nil {
		t.Fatal("expected an error when focus.Current fails")
	}
	if len(store.saved) != 0 {
		t.Errorf("SetConfig was called %d times, want 0", len(store.saved))
	}
}

func TestAssignUnnameableAppInfoErrorsWithNoSetConfig(t *testing.T) {
	// Only Caption set -- NameCandidates() deliberately never uses it,
	// so this focused app has no usable identity at all.
	h, store := newAssignHandlersFor(baseConfig(), focus.AppInfo{Caption: "some window"}, AssignOptions{})
	inv := Invocation{Action: model.KnobAssignFocusedAppAction{}, Control: push(3), Gesture: model.GestureHold}
	if err := h.execute(context.Background(), inv); err == nil {
		t.Fatal("expected an error for an unnameable focused app")
	}
	if len(store.saved) != 0 {
		t.Errorf("SetConfig was called %d times, want 0", len(store.saved))
	}
}

func TestAssignSetConfigErrorPropagatesAndSkipsOnAssigned(t *testing.T) {
	store := &fakeConfigStore{cfg: baseConfig(), err: errors.New("disk full")}
	var onAssignedCalled bool
	h := newAssignHandlersWithStore(store, focus.AppInfo{ResourceClass: "brave-browser"}, AssignOptions{
		OnAssigned: func(model.Control, model.AppMatcher) { onAssignedCalled = true },
	})
	inv := Invocation{Action: model.KnobAssignFocusedAppAction{}, Control: push(3), Gesture: model.GestureHold}
	if err := h.execute(context.Background(), inv); err == nil {
		t.Fatal("expected the SetConfig error to propagate")
	}
	if onAssignedCalled {
		t.Error("OnAssigned was called despite SetConfig failing")
	}
}

func TestAssignOnAssignedCalledAfterSuccessfulPersist(t *testing.T) {
	var gotControl model.Control
	var gotMatcher model.AppMatcher
	h, _ := newAssignHandlersFor(baseConfig(), focus.AppInfo{ResourceClass: "brave-browser"}, AssignOptions{
		OnAssigned: func(c model.Control, m model.AppMatcher) { gotControl, gotMatcher = c, m },
	})
	inv := Invocation{Action: model.KnobAssignFocusedAppAction{}, Control: push(3), Gesture: model.GestureHold}
	if err := h.execute(context.Background(), inv); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotControl != encoder(3) {
		t.Errorf("OnAssigned control = %+v, want %+v", gotControl, encoder(3))
	}
	if gotMatcher.ID != "brave" {
		t.Errorf("OnAssigned matcher ID = %q, want %q", gotMatcher.ID, "brave")
	}
}

func TestAssignPersistedConfigAlwaysValidates(t *testing.T) {
	h, store := newAssignHandlersFor(baseConfig(), focus.AppInfo{ResourceClass: "brave-browser"}, AssignOptions{})
	inv := Invocation{Action: model.KnobAssignFocusedAppAction{}, Control: push(3), Gesture: model.GestureHold}
	if err := h.execute(context.Background(), inv); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if err := store.cfg.Validate(); err != nil {
		t.Fatalf("persisted config failed Validate: %v", err)
	}
}

// TestAssignDoesNotMutateCallersOriginalConfig pins the aliasing
// concern applyAssignment's doc comment describes: store.Config()
// returns a struct whose slices alias the store's live config, and
// mutating them in place would corrupt the running config before
// persistence even completes.
func TestAssignDoesNotMutateCallersOriginalConfig(t *testing.T) {
	cfg := baseConfig()
	cfg.AppMatchers = []model.AppMatcher{{ID: "vesktop", AppNames: []string{"vesktop"}}}
	originalBindingsLen := len(cfg.Profiles[0].Bindings)
	originalMatchersLen := len(cfg.AppMatchers)
	originalFirstBinding := cfg.Profiles[0].Bindings[0]

	h, _ := newAssignHandlersFor(cfg, focus.AppInfo{ResourceClass: "brave-browser"}, AssignOptions{})
	inv := Invocation{Action: model.KnobAssignFocusedAppAction{}, Control: push(3), Gesture: model.GestureHold}
	if err := h.execute(context.Background(), inv); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(cfg.Profiles[0].Bindings) != originalBindingsLen {
		t.Errorf("caller's original Bindings slice length changed: %d -> %d", originalBindingsLen, len(cfg.Profiles[0].Bindings))
	}
	if len(cfg.AppMatchers) != originalMatchersLen {
		t.Errorf("caller's original AppMatchers slice length changed: %d -> %d", originalMatchersLen, len(cfg.AppMatchers))
	}
	if cfg.Profiles[0].Bindings[0] != originalFirstBinding {
		t.Error("caller's original first binding was mutated in place")
	}
}

// newAssignHandlersWithStore is like newAssignHandlersFor but takes an
// already-constructed store, for tests that need to preset its err
// field.
func newAssignHandlersWithStore(store *fakeConfigStore, info focus.AppInfo, opts AssignOptions) *AssignHandlers {
	fp := focus.NewFakeProvider()
	fp.SetFocused(info)
	return NewAssignHandlers(store, fp, opts)
}
