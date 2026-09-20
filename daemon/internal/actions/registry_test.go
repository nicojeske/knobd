package actions

import (
	"context"
	"testing"

	"github.com/njeske/knobd/internal/model"
)

type fakeHandler struct {
	calls []model.Action
	err   error
}

func (h *fakeHandler) Execute(_ context.Context, action model.Action) error {
	h.calls = append(h.calls, action)
	return h.err
}

func TestRegistryDispatchesToRegisteredHandler(t *testing.T) {
	r := NewRegistry()
	h := &fakeHandler{}
	r.Register(model.ActionVolumeMuteToggle, h)

	action := model.VolumeMuteToggleAction{Target: model.Target{Kind: model.TargetFocused}}
	if err := r.Execute(context.Background(), action); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(h.calls) != 1 || h.calls[0] != action {
		t.Errorf("handler calls = %+v, want [%+v]", h.calls, action)
	}
}

func TestRegistryUnregisteredActionErrors(t *testing.T) {
	r := NewRegistry()
	err := r.Execute(context.Background(), model.KnobClearAction{})
	if err == nil {
		t.Fatal("expected error for unregistered action type")
	}
}

func TestRegistryNilActionErrors(t *testing.T) {
	r := NewRegistry()
	if err := r.Execute(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil action")
	}
}

func TestRegistryPropagatesHandlerError(t *testing.T) {
	r := NewRegistry()
	wantErr := context.DeadlineExceeded
	r.Register(model.ActionShellRun, &fakeHandler{err: wantErr})

	err := r.Execute(context.Background(), model.ShellRunAction{Command: "true"})
	if err != wantErr {
		t.Fatalf("Execute error = %v, want %v", err, wantErr)
	}
}
