package main

import (
	"sort"

	"github.com/njeske/knobd/internal/api"
	"github.com/njeske/knobd/internal/engine"
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
// ImplementedActions merges the registry's own set with
// engine.InlineActionTypes(): the three layer.* actions never go
// through actions.Registry (engine.dispatchGesture executes them
// itself, since they mutate state only its own run goroutine owns), but
// a client asking "what can a binding do" has no reason to care which
// of the two mechanisms answers it. Features.Scenes is still M08's
// (scene.apply/scene.save aren't registered yet); Features.Learn is
// true now that POST/DELETE /learn are wired up (see
// cmd/knobd/learn.go).
func (c *capabilitiesProvider) Capabilities() api.Capabilities {
	actionSet := make(map[model.ActionType]bool)
	for _, t := range c.registry.ActionTypes() {
		actionSet[t] = true
	}
	for _, t := range engine.InlineActionTypes() {
		actionSet[t] = true
	}
	actionTypes := make([]model.ActionType, 0, len(actionSet))
	for t := range actionSet {
		actionTypes = append(actionTypes, t)
	}
	sort.Slice(actionTypes, func(i, j int) bool { return actionTypes[i] < actionTypes[j] })

	return api.Capabilities{
		ImplementedActions:   actionTypes,
		SupportedTargetKinds: model.TargetKinds(),
		Features: api.Features{
			Layers: true,
			Scenes: false,
			Learn:  true,
		},
	}
}
