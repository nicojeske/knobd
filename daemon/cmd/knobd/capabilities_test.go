package main

import (
	"testing"

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

func TestCapabilitiesReflectsRegistry(t *testing.T) {
	types := []model.ActionType{model.ActionVolumeAdjust, model.ActionVolumeSet, model.ActionVolumeMuteToggle}
	c := newCapabilitiesProvider(&fakeRegistry{types: types})
	got := c.Capabilities()

	if len(got.ImplementedActions) != len(types) {
		t.Errorf("ImplementedActions = %v, want %v", got.ImplementedActions, types)
	}
}
