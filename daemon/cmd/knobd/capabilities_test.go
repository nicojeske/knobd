package main

import (
	"testing"

	"github.com/njeske/knobd/internal/engine"
	"github.com/njeske/knobd/internal/model"
)

type fakeRegistry struct{ types []model.ActionType }

func (f *fakeRegistry) ActionTypes() []model.ActionType { return f.types }

func TestCapabilitiesExcludesGroupTargetKind(t *testing.T) {
	c := newCapabilitiesProvider(&fakeRegistry{types: []model.ActionType{model.ActionVolumeAdjust}})
	got := c.Capabilities()

	for _, k := range got.SupportedTargetKinds {
		if k == model.TargetGroup {
			t.Error("SupportedTargetKinds includes \"group\", which does not resolve until M08")
		}
	}
	// Every other TargetKind should still be listed.
	if len(got.SupportedTargetKinds) != len(model.TargetKinds())-1 {
		t.Errorf("SupportedTargetKinds has %d entries, want %d (all but group)", len(got.SupportedTargetKinds), len(model.TargetKinds())-1)
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
