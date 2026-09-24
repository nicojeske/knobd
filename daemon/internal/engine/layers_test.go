package engine

import (
	"testing"

	"github.com/njeske/knobd/internal/model"
)

func TestLayerStateActiveDefaultsToZero(t *testing.T) {
	s := &layerState{}
	if got := s.active(); got != 0 {
		t.Errorf("active() = %d, want 0", got)
	}
}

func TestLayerStateMomentaryStacking(t *testing.T) {
	s := &layerState{}
	side1 := model.Control{Kind: model.ControlSideButton, Index: 1}
	side2 := model.Control{Kind: model.ControlSideButton, Index: 2}

	s.pushMomentary(side1, 1)
	if got := s.active(); got != 1 {
		t.Fatalf("active() after pushing side1 = %d, want 1", got)
	}

	// A second momentary held on top wins.
	s.pushMomentary(side2, 2)
	if got := s.active(); got != 2 {
		t.Fatalf("active() after pushing side2 = %d, want 2", got)
	}

	// Releasing the most-recently-pushed one falls back to whatever's
	// still held underneath, regardless of release order.
	s.popMomentary(side2)
	if got := s.active(); got != 1 {
		t.Fatalf("active() after popping side2 = %d, want 1", got)
	}

	s.popMomentary(side1)
	if got := s.active(); got != 0 {
		t.Fatalf("active() after popping side1 = %d, want 0", got)
	}
}

func TestLayerStateMomentaryReleaseOutOfOrder(t *testing.T) {
	s := &layerState{}
	side1 := model.Control{Kind: model.ControlSideButton, Index: 1}
	side2 := model.Control{Kind: model.ControlSideButton, Index: 2}

	s.pushMomentary(side1, 1)
	s.pushMomentary(side2, 2)

	// Release the *first* one held while the second is still down: the
	// second (most-recently-pushed) one must remain active.
	s.popMomentary(side1)
	if got := s.active(); got != 2 {
		t.Fatalf("active() = %d, want 2 (side2 still held)", got)
	}
	s.popMomentary(side2)
	if got := s.active(); got != 0 {
		t.Fatalf("active() = %d, want 0", got)
	}
}

func TestLayerStatePushMomentaryUnbalancedDownReplaces(t *testing.T) {
	s := &layerState{}
	c := model.Control{Kind: model.ControlSideButton, Index: 1}
	s.pushMomentary(c, 1)
	s.pushMomentary(c, 2) // a second down with no intervening up
	if got := len(s.held); got != 1 {
		t.Fatalf("len(held) = %d, want 1 (no duplicate entries for c)", got)
	}
	if got := s.active(); got != 2 {
		t.Fatalf("active() = %d, want 2", got)
	}
}

func TestLayerStatePopMomentaryUnknownControlIsNoop(t *testing.T) {
	s := &layerState{latched: 3}
	s.popMomentary(model.Control{Kind: model.ControlSideButton, Index: 1})
	if got := s.active(); got != 3 {
		t.Errorf("active() = %d, want 3 (unaffected)", got)
	}
}

func TestLayerStateLatchTogglesBackToZero(t *testing.T) {
	s := &layerState{}
	s.latch(1)
	if got := s.active(); got != 1 {
		t.Fatalf("active() after latch(1) = %d, want 1", got)
	}
	s.latch(1)
	if got := s.active(); got != 0 {
		t.Fatalf("active() after second latch(1) = %d, want 0 (toggled off)", got)
	}
}

func TestLayerStateLatchSwitchesBetweenLayers(t *testing.T) {
	s := &layerState{}
	s.latch(1)
	s.latch(2)
	if got := s.active(); got != 2 {
		t.Fatalf("active() = %d, want 2", got)
	}
}

func TestLayerStateLatchUnderMomentaryDoesNotChangeActive(t *testing.T) {
	// A latch fired while a momentary hold is on top (e.g. a
	// layer.latch bound on layer 0's press, triggered while a different
	// control's momentary hold is active) changes the base layer
	// underneath, but active() still reports the held layer.
	s := &layerState{}
	side := model.Control{Kind: model.ControlSideButton, Index: 1}
	s.pushMomentary(side, 5)
	s.latch(1)
	if got := s.active(); got != 5 {
		t.Fatalf("active() = %d, want 5 (momentary still on top)", got)
	}
	s.popMomentary(side)
	if got := s.active(); got != 1 {
		t.Fatalf("active() after releasing momentary = %d, want 1 (latched underneath)", got)
	}
}

func TestLayerStateCycleWithExplicitOrder(t *testing.T) {
	s := &layerState{}
	order := []int{0, 2, 3}
	s.cycle(order, 3)
	if got := s.active(); got != 2 {
		t.Fatalf("active() = %d, want 2", got)
	}
	s.cycle(order, 3)
	if got := s.active(); got != 3 {
		t.Fatalf("active() = %d, want 3", got)
	}
	s.cycle(order, 3) // wraps
	if got := s.active(); got != 0 {
		t.Fatalf("active() = %d, want 0 (wrapped)", got)
	}
}

func TestLayerStateCycleWithNoOrderStepsThroughMaxLayer(t *testing.T) {
	s := &layerState{}
	s.cycle(nil, 2)
	if got := s.active(); got != 1 {
		t.Fatalf("active() = %d, want 1", got)
	}
	s.cycle(nil, 2)
	if got := s.active(); got != 2 {
		t.Fatalf("active() = %d, want 2", got)
	}
	s.cycle(nil, 2) // wraps
	if got := s.active(); got != 0 {
		t.Fatalf("active() = %d, want 0 (wrapped)", got)
	}
}

func TestLayerStateCycleWithNoOrderAndNoOtherLayersIsNoop(t *testing.T) {
	s := &layerState{}
	s.cycle(nil, 0)
	if got := s.active(); got != 0 {
		t.Fatalf("active() = %d, want 0", got)
	}
}

func TestLayerStateCycleCurrentNotInOrderJumpsToFirst(t *testing.T) {
	s := &layerState{latched: 9}
	s.cycle([]int{1, 2}, 2)
	if got := s.active(); got != 1 {
		t.Fatalf("active() = %d, want 1 (order's first entry)", got)
	}
}
