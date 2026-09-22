package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/njeske/knobd/internal/model"
)

type fakeCapabilitiesProvider struct {
	caps Capabilities
}

func (f *fakeCapabilitiesProvider) Capabilities() Capabilities { return f.caps }

func TestGetCapabilities(t *testing.T) {
	t.Run("no provider is 503", func(t *testing.T) {
		s := New(Options{})
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/capabilities", nil))
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", rr.Code)
		}
	})

	t.Run("success", func(t *testing.T) {
		want := Capabilities{
			ImplementedActions:   []model.ActionType{model.ActionVolumeAdjust, model.ActionVolumeSet},
			SupportedTargetKinds: []model.TargetKind{model.TargetDefaultSink},
			Features:             Features{Layers: false, Scenes: false, Learn: true},
		}
		s := New(Options{Capabilities: &fakeCapabilitiesProvider{caps: want}})
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/capabilities", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
		}

		var wire map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &wire); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		implemented, ok := wire["implementedActions"].([]any)
		if !ok || len(implemented) != 2 {
			t.Fatalf(`expected "implementedActions" to be a two-element array, got %#v`, wire["implementedActions"])
		}
		features, ok := wire["features"].(map[string]any)
		if !ok {
			t.Fatal(`expected "features" to be an object`)
		}
		if learn, _ := features["learn"].(bool); !learn {
			t.Error(`expected "features.learn" to be true`)
		}
	})
}
