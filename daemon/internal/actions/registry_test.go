package actions

import (
	"context"
	"testing"

	"github.com/njeske/knobd/internal/model"
)

type fakeHandler struct {
	calls []Invocation
	err   error
}

func (h *fakeHandler) Execute(_ context.Context, inv Invocation) error {
	h.calls = append(h.calls, inv)
	return h.err
}

func TestRegistryDispatchesToRegisteredHandler(t *testing.T) {
	r := NewRegistry()
	h := &fakeHandler{}
	r.Register(model.ActionVolumeMuteToggle, h)

	action := model.VolumeMuteToggleAction{Target: model.Target{Kind: model.TargetFocused}}
	inv := Invocation{Action: action, Control: model.Control{Kind: model.ControlEncoderPush, Index: 1}, Gesture: model.GesturePress}
	if err := r.Execute(context.Background(), inv); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(h.calls) != 1 || h.calls[0].Action != action {
		t.Errorf("handler calls = %+v, want [%+v]", h.calls, inv)
	}
}

func TestRegistryDispatchesByActionType(t *testing.T) {
	// Execute keys off inv.Action.ActionType(), not any field the caller
	// sets explicitly — this guards against a future refactor accidentally
	// routing on something else.
	r := NewRegistry()
	h := &fakeHandler{}
	r.Register(model.ActionVolumeAdjust, h)

	inv := Invocation{Action: model.VolumeAdjustAction{StepPercent: 2}, Delta: 3}
	if err := r.Execute(context.Background(), inv); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(h.calls) != 1 || h.calls[0].Delta != 3 {
		t.Errorf("handler did not receive the invocation's Delta: %+v", h.calls)
	}
}

func TestRegistryUnregisteredActionErrors(t *testing.T) {
	r := NewRegistry()
	err := r.Execute(context.Background(), Invocation{Action: model.KnobClearAction{}})
	if err == nil {
		t.Fatal("expected error for unregistered action type")
	}
}

func TestRegistryNilActionErrors(t *testing.T) {
	r := NewRegistry()
	if err := r.Execute(context.Background(), Invocation{}); err == nil {
		t.Fatal("expected error for nil action")
	}
}

func TestRegistryPropagatesHandlerError(t *testing.T) {
	r := NewRegistry()
	wantErr := context.DeadlineExceeded
	r.Register(model.ActionShellRun, &fakeHandler{err: wantErr})

	err := r.Execute(context.Background(), Invocation{Action: model.ShellRunAction{Command: "true"}})
	if err != wantErr {
		t.Fatalf("Execute error = %v, want %v", err, wantErr)
	}
}
