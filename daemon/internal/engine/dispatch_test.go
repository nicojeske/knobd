package engine

import (
	"testing"

	"github.com/njeske/knobd/internal/actions"
	"github.com/njeske/knobd/internal/model"
)

func seekWork(control model.Control, delta int) work {
	return work{inv: &actions.Invocation{
		Action:  model.MediaSeekAction{SeekMs: 2000},
		Control: control,
		Delta:   delta,
	}}
}

func TestCoalesceMergesMediaSeekDeltasOnSameControl(t *testing.T) {
	ctrl := model.Control{Kind: model.ControlEncoder, Index: 1}
	batch := []work{seekWork(ctrl, 1), seekWork(ctrl, 1), seekWork(ctrl, 1)}

	got := coalesce(batch)
	if len(got) != 1 {
		t.Fatalf("coalesce produced %d items, want 1", len(got))
	}
	if got[0].inv.Delta != 3 {
		t.Errorf("merged Delta = %d, want 3", got[0].inv.Delta)
	}
}

func TestCoalesceDoesNotMergeMediaSeekAcrossDifferentControls(t *testing.T) {
	a := model.Control{Kind: model.ControlEncoder, Index: 1}
	b := model.Control{Kind: model.ControlEncoder, Index: 2}
	batch := []work{seekWork(a, 1), seekWork(b, 1)}

	got := coalesce(batch)
	if len(got) != 2 {
		t.Fatalf("coalesce produced %d items, want 2 (different controls must not merge)", len(got))
	}
}

func TestCoalesceDoesNotMergeMediaSeekWithOtherActionTypes(t *testing.T) {
	ctrl := model.Control{Kind: model.ControlEncoder, Index: 1}
	seek := seekWork(ctrl, 1)
	volume := work{inv: &actions.Invocation{
		Action:  model.VolumeAdjustAction{StepPercent: 2},
		Control: ctrl,
		Delta:   1,
	}}

	got := coalesce([]work{seek, volume})
	if len(got) != 2 {
		t.Fatalf("coalesce produced %d items, want 2 (different action types must not merge)", len(got))
	}
}

func TestCoalesceMergesVolumeAdjustDeltas(t *testing.T) {
	ctrl := model.Control{Kind: model.ControlEncoder, Index: 1}
	batch := []work{
		{inv: &actions.Invocation{Action: model.VolumeAdjustAction{StepPercent: 2}, Control: ctrl, Delta: 1}},
		{inv: &actions.Invocation{Action: model.VolumeAdjustAction{StepPercent: 2}, Control: ctrl, Delta: 2}},
	}

	got := coalesce(batch)
	if len(got) != 1 || got[0].inv.Delta != 3 {
		t.Fatalf("coalesce = %+v, want one merged item with Delta 3", got)
	}
}

func TestCoalesceLeavesResyncAndNilInvUntouched(t *testing.T) {
	ctrl := model.Control{Kind: model.ControlEncoder, Index: 1}
	batch := []work{
		{resync: true},
		seekWork(ctrl, 1),
		{resync: true},
	}

	got := coalesce(batch)
	if len(got) != 3 {
		t.Fatalf("coalesce produced %d items, want 3 (resync entries are never merged)", len(got))
	}
}
