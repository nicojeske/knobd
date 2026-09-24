package engine

import "github.com/njeske/knobd/internal/model"

// InlineActionTypes returns the model.ActionType values engine executes
// itself (see dispatchGesture) rather than dispatching to
// actions.Registry -- daemon/cmd/knobd's capabilities adapter folds
// these into api.Capabilities.ImplementedActions alongside whatever's
// actually registered, since a client asking "what can a binding do"
// has no reason to care which of the two mechanisms answers it.
func InlineActionTypes() []model.ActionType {
	return []model.ActionType{model.ActionLayerMomentary, model.ActionLayerLatch, model.ActionLayerCycle}
}

// heldLayer is one momentary layer switch currently active, in the
// order its control went down.
type heldLayer struct {
	control model.Control
	layer   int
}

// layerState tracks which layer bindings currently resolve against: a
// latched base layer, overlaid by zero or more momentary holds. See
// model.LayerMomentaryAction/LayerLatchAction/LayerCycleAction's doc
// comments and specs/milestones/M08-layers-groups-scenes.md's Design
// section.
//
// Unlike bindingIndex, ledState, and every other piece of Run's mutable
// state, layerState is not rebuilt on SetConfig: a layer switch belongs
// to the live session, not the config, so replacing bindings mid-session
// (e.g. the config UI editing an unrelated profile) must not silently
// snap the user back to layer 0. Run only resets it when the active
// profile itself changes (see the configCh case in engine.go).
type layerState struct {
	latched int
	held    []heldLayer
}

// active is the layer dispatch should resolve bindings against right
// now: the most recently pressed still-held momentary layer, or the
// latched layer if none is currently held.
func (s *layerState) active() int {
	if n := len(s.held); n > 0 {
		return s.held[n-1].layer
	}
	return s.latched
}

// pushMomentary records c's momentary switch to layer, called on the
// control's raw button-down (see Run's EventButtonDown handling) rather
// than waiting for GestureHold, so a knob bound on the new layer
// responds the instant the side button goes down. Replaces any existing
// entry for c first: Codec.Decode permits an unbalanced down/down
// sequence (see gestureMachine.Handle's own EventButtonDown case), and
// this must not leave two stale entries for the same control.
func (s *layerState) pushMomentary(c model.Control, layer int) {
	s.popMomentary(c)
	s.held = append(s.held, heldLayer{control: c, layer: layer})
}

// popMomentary removes c's momentary switch, if any -- called
// unconditionally on the control's raw button-up, since Run has no
// cheaper way to know whether c was actually a momentary-bound control
// without duplicating the lookup that pushMomentary already did.
func (s *layerState) popMomentary(c model.Control) {
	for i, h := range s.held {
		if h.control == c {
			s.held = append(s.held[:i], s.held[i+1:]...)
			return
		}
	}
}

// latch switches the latched layer to layer, or back to 0 if layer is
// already latched -- with only two side buttons on the unit, toggling
// is how a user gets back out of a latched layer without a dedicated
// "layer 0" control.
func (s *layerState) latch(layer int) {
	if s.latched == layer {
		s.latched = 0
		return
	}
	s.latched = layer
}

// cycle advances the latched layer to the next entry in order
// (wrapping), or steps 0->1->...->maxLayer->0 if order is empty. If the
// latched layer isn't in order at all, it jumps to order's first entry
// rather than erroring -- a config that removed a layer out from under
// an existing cycle binding shouldn't get stuck.
func (s *layerState) cycle(order []int, maxLayer int) {
	if len(order) == 0 {
		if maxLayer <= 0 {
			s.latched = 0
			return
		}
		s.latched = (s.latched + 1) % (maxLayer + 1)
		return
	}
	for i, l := range order {
		if l == s.latched {
			s.latched = order[(i+1)%len(order)]
			return
		}
	}
	s.latched = order[0]
}
