package main

import (
	"testing"

	"github.com/njeske/knobd/internal/engine"
	"github.com/njeske/knobd/internal/model"
)

type fakeRegistry struct{ types []model.ActionType }

func (f *fakeRegistry) ActionTypes() []model.ActionType { return f.types }

// TestCapabilitiesIncludesGroupTargetKind pins M08's group resolution
// landing: SupportedTargetKinds no longer excludes "group" (contrast
// the pre-M08 version of this test, which asserted the opposite).
func TestCapabilitiesIncludesGroupTargetKind(t *testing.T) {
	c := newCapabilitiesProvider(&fakeRegistry{types: []model.ActionType{model.ActionVolumeAdjust}})
	got := c.Capabilities()

	found := false
	for _, k := range got.SupportedTargetKinds {
		if k == model.TargetGroup {
			found = true
		}
	}
	if !found {
		t.Error("SupportedTargetKinds does not include \"group\"; group resolution landed in M08")
	}
	if len(got.SupportedTargetKinds) != len(model.TargetKinds()) {
		t.Errorf("SupportedTargetKinds has %d entries, want %d (every TargetKind)", len(got.SupportedTargetKinds), len(model.TargetKinds()))
	}
}

// TestCapabilitiesReflectsRegistry: ImplementedActions is the
// registry's own set unioned with engine.InlineActionTypes() (the
// layer.* actions engine executes itself -- see capabilitiesProvider.
// Capabilities' doc comment), not just a passthrough of the registry.
func TestCapabilitiesReflectsRegistry(t *testing.T) {
	types := []model.ActionType{model.ActionVolumeAdjust, model.ActionVolumeSet, model.ActionVolumeMuteToggle}
	c := newCapabilitiesProvider(&fakeRegistry{types: types})
	got := c.Capabilities()

	want := len(types) + len(engine.InlineActionTypes())
	if len(got.ImplementedActions) != want {
		t.Errorf("ImplementedActions = %v, want %d entries (registry + inline)", got.ImplementedActions, want)
	}
	for _, t2 := range engine.InlineActionTypes() {
		found := false
		for _, a := range got.ImplementedActions {
			if a == t2 {
				found = true
			}
		}
		if !found {
			t.Errorf("ImplementedActions missing inline action %q", t2)
		}
	}
}
