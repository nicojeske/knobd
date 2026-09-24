package engine

import (
	"io"
	"log/slog"
	"testing"

	"github.com/njeske/knobd/internal/model"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

var (
	enc1  = model.Control{Kind: model.ControlEncoder, Index: 1}
	side1 = model.Control{Kind: model.ControlSideButton, Index: 1}
)

func TestBindingIndexLookupFallsBackToLayer0(t *testing.T) {
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn, Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetDefaultSink}, StepPercent: 1}},
				{Layer: 1, Control: enc1, Gesture: model.GestureTurn, Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetDefaultSource}, StepPercent: 2}},
			},
		}},
	}
	ix := newBindingIndex(cfg, discardLogger())

	// Layer 1 has its own binding: no fallback.
	a, ok := ix.lookup(1, enc1, model.GestureTurn)
	if !ok {
		t.Fatal("layer 1 lookup: not found")
	}
	if a.(model.VolumeAdjustAction).Target.Kind != model.TargetDefaultSource {
		t.Errorf("layer 1 lookup returned the wrong action: %+v", a)
	}

	// Layer 2 has no binding of its own: falls back to layer 0.
	a, ok = ix.lookup(2, enc1, model.GestureTurn)
	if !ok {
		t.Fatal("layer 2 lookup: not found (should fall back to layer 0)")
	}
	if a.(model.VolumeAdjustAction).Target.Kind != model.TargetDefaultSink {
		t.Errorf("layer 2 fallback returned the wrong action: %+v", a)
	}

	// A control with no binding on any layer.
	if _, ok := ix.lookup(0, side1, model.GesturePress); ok {
		t.Error("expected no binding for an unbound control")
	}
}

func TestBindingIndexDuplicateKeyLastWins(t *testing.T) {
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn, Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetDefaultSink}, StepPercent: 1}},
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn, Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetDefaultSource}, StepPercent: 5}},
			},
		}},
	}
	ix := newBindingIndex(cfg, discardLogger())
	a, ok := ix.lookup(0, enc1, model.GestureTurn)
	if !ok {
		t.Fatal("not found")
	}
	if got := a.(model.VolumeAdjustAction); got.Target.Kind != model.TargetDefaultSource || got.StepPercent != 5 {
		t.Errorf("expected the second (last) binding to win, got %+v", got)
	}
}

func TestBindingIndexDoubleBoundDerivation(t *testing.T) {
	push2 := model.Control{Kind: model.ControlEncoderPush, Index: 2}
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: push1, Gesture: model.GesturePress, Action: model.VolumeMuteToggleAction{Target: model.Target{Kind: model.TargetFocused}}},
				{Layer: 1, Control: push2, Gesture: model.GestureDoublePress, Action: model.KnobClearAction{}},
			},
		}},
	}
	ix := newBindingIndex(cfg, discardLogger())
	if ix.deferPress(push1) {
		t.Error("push1 has no double_press binding; should not defer")
	}
	if !ix.deferPress(push2) {
		t.Error("push2 has a double_press binding on layer 1; should defer regardless of active layer")
	}
}

func TestBindingIndexEmptyActiveProfile(t *testing.T) {
	cfg := model.Config{ActiveProfileID: "", Profiles: []model.Profile{{ID: "default"}}}
	ix := newBindingIndex(cfg, discardLogger())
	if _, ok := ix.lookup(0, enc1, model.GestureTurn); ok {
		t.Error("expected no bindings when ActiveProfileID is empty")
	}
}

func TestBindingIndexInvalidBindingSkipped(t *testing.T) {
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				// A gesture the control kind doesn't support: invalid,
				// must be skipped rather than crash newBindingIndex.
				{Layer: 0, Control: enc1, Gesture: model.GesturePress, Action: model.VolumeMuteToggleAction{Target: model.Target{Kind: model.TargetFocused}}},
			},
		}},
	}
	ix := newBindingIndex(cfg, discardLogger())
	if _, ok := ix.lookup(0, enc1, model.GesturePress); ok {
		t.Error("expected the invalid binding to be skipped, not indexed")
	}
}

func TestBindingIndexMaxLayer(t *testing.T) {
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn, Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetDefaultSink}, StepPercent: 1}},
				{Layer: 3, Control: enc1, Gesture: model.GestureTurn, Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetDefaultSource}, StepPercent: 1}},
			},
		}},
	}
	ix := newBindingIndex(cfg, discardLogger())
	if ix.maxLayer != 3 {
		t.Errorf("maxLayer = %d, want 3", ix.maxLayer)
	}
}

func TestBindingIndexMaxLayerDefaultsToZero(t *testing.T) {
	cfg := model.Config{
		ActiveProfileID: "default",
		Profiles: []model.Profile{{
			ID: "default",
			Bindings: []model.Binding{
				{Layer: 0, Control: enc1, Gesture: model.GestureTurn, Action: model.VolumeAdjustAction{Target: model.Target{Kind: model.TargetDefaultSink}, StepPercent: 1}},
			},
		}},
	}
	ix := newBindingIndex(cfg, discardLogger())
	if ix.maxLayer != 0 {
		t.Errorf("maxLayer = %d, want 0", ix.maxLayer)
	}
}
