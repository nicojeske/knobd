package main

import (
	"github.com/njeske/knobd/internal/api"
	"github.com/njeske/knobd/internal/model"
)

// registryActionTypes is the slice of actions.Registry
// capabilitiesProvider needs -- a point-of-use interface so it's
// testable without constructing a real Registry.
type registryActionTypes interface {
	ActionTypes() []model.ActionType
}

// capabilitiesProvider is cmd/knobd's api.CapabilitiesProvider adapter.
type capabilitiesProvider struct {
	registry registryActionTypes
}

func newCapabilitiesProvider(registry registryActionTypes) *capabilitiesProvider {
	return &capabilitiesProvider{registry: registry}
}

// Capabilities implements api.CapabilitiesProvider.
//
// SupportedTargetKinds excludes model.TargetGroup: engine/resolver.go's
// TargetGroup case is a stub that always errors until M08 does group
// resolution (see daemon/internal/engine/errors.go). Every other
// model.TargetKinds() entry resolves today. Features.Layers and
// Features.Scenes are also M08's; Features.Learn is true now that
// POST/DELETE /learn are wired up (see cmd/knobd/learn.go).
func (c *capabilitiesProvider) Capabilities() api.Capabilities {
	var targetKinds []model.TargetKind
	for _, k := range model.TargetKinds() {
		if k == model.TargetGroup {
			continue
		}
		targetKinds = append(targetKinds, k)
	}

	return api.Capabilities{
		ImplementedActions:   c.registry.ActionTypes(),
		SupportedTargetKinds: targetKinds,
		Features: api.Features{
			Layers: false,
			Scenes: false,
			Learn:  true,
		},
	}
}
