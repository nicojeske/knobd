package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/njeske/knobd/internal/model"
)

// fakeStore is a ConfigStore test double. It records whether SetConfig
// was ever called with a config that made it all the way through
// (saved == nil means "nothing was written," the signal every 4xx test
// case below checks).
type fakeStore struct {
	cfg   model.Config
	saved *model.Config
	err   error
}

func (f *fakeStore) Config() model.Config { return f.cfg }

func (f *fakeStore) SetConfig(_ context.Context, cfg model.Config) error {
	if f.err != nil {
		return f.err
	}
	c := cfg
	f.saved = &c
	f.cfg = cfg
	return nil
}

type fakeState struct {
	state State
	err   error
}

func (f *fakeState) State(context.Context) (State, error) { return f.state, f.err }

// sampleConfig carries a binding whose action is a discriminated-union
// type -- the same shape config_test.go's own round-trip fixtures use --
// so GET /config's test can assert the envelope actually made it onto
// the wire, not just that the Go struct round-trips.
func sampleConfig() model.Config {
	return model.Config{
		SchemaVersion:   model.CurrentSchemaVersion,
		ActiveProfileID: "default",
		AppMatchers: []model.AppMatcher{
			{ID: "vesktop", AppNames: []string{"vesktop"}},
		},
		Profiles: []model.Profile{{
			ID:          "default",
			DisplayName: "Default",
			Bindings: []model.Binding{
				{
					Layer:   0,
					Control: model.Control{Kind: model.ControlEncoder, Index: 1},
					Gesture: model.GestureTurn,
					Action: model.VolumeAdjustAction{
						Target:      model.Target{Kind: model.TargetApp, Ref: "vesktop"},
						StepPercent: 2,
					},
				},
			},
		}},
	}
}

func TestGetConfigRoundTripsBindingAction(t *testing.T) {
	want := sampleConfig()
	s := New(Options{Config: &fakeStore{cfg: want}})

	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/config", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}

	// (a) the raw bytes must actually carry the discriminated-union
	// envelope -- this is the part a Go-only round trip can't catch: the
	// envelope could silently degenerate into something only this Go
	// build can read, which would break M07's generated TypeScript
	// client.
	if !bytes.Contains(rr.Body.Bytes(), []byte(`"volume.adjust"`)) {
		t.Errorf("response body does not contain the volume.adjust discriminator:\n%s", rr.Body.String())
	}

	// (b) and it must decode back to an identical model.Config.
	var got model.Config
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip mismatch:\ngot:  %+v\nwant: %+v", got, want)
	}
}

func TestPutConfig(t *testing.T) {
	valid := sampleConfig()
	validBody, err := json.Marshal(valid)
	if err != nil {
		t.Fatalf("marshal sampleConfig: %v", err)
	}

	invalidConfig := sampleConfig()
	invalidConfig.Profiles[0].Bindings[0].Action = model.VolumeAdjustAction{
		Target: model.Target{Kind: model.TargetApp, Ref: "nonexistent-matcher"},
	}
	invalidConfigBody, err := json.Marshal(invalidConfig)
	if err != nil {
		t.Fatalf("marshal invalidConfig: %v", err)
	}

	wrongVersion := sampleConfig()
	wrongVersion.SchemaVersion = model.CurrentSchemaVersion + 1
	wrongVersionBody, err := json.Marshal(wrongVersion)
	if err != nil {
		t.Fatalf("marshal wrongVersion: %v", err)
	}

	cases := []struct {
		name       string
		body       []byte
		storeErr   error
		wantStatus int
		wantCode   ErrorCode // empty for a 2xx case
		wantSaved  bool
	}{
		{"valid config", validBody, nil, http.StatusNoContent, "", true},
		{"malformed json", []byte(`{"schemaVersion": `), nil, http.StatusBadRequest, CodeInvalidJSON, false},
		{"trailing garbage", append(append([]byte{}, validBody...), '{', '}'), nil, http.StatusBadRequest, CodeInvalidJSON, false},
		{"invalid config (unknown matcher ref)", invalidConfigBody, nil, http.StatusBadRequest, CodeInvalidConfig, false},
		{"unsupported schema version", wrongVersionBody, nil, http.StatusBadRequest, CodeUnsupportedSchemaVersion, false},
		{"store failure", validBody, fmt.Errorf("disk full"), http.StatusInternalServerError, CodeInternal, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{cfg: model.Default(), err: tc.storeErr}
			s := New(Options{Config: store})

			req := httptest.NewRequest(http.MethodPut, "/config", bytes.NewReader(tc.body))
			rr := httptest.NewRecorder()
			s.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", rr.Code, tc.wantStatus, rr.Body.String())
			}
			if (store.saved != nil) != tc.wantSaved {
				t.Errorf("saved = %v (nil=%v), want saved != nil: %v", store.saved, store.saved == nil, tc.wantSaved)
			}
			if tc.wantCode != "" {
				var errResp ErrorResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
					t.Fatalf("unmarshal error response: %v", err)
				}
				if errResp.Code != tc.wantCode {
					t.Errorf("error code = %q, want %q", errResp.Code, tc.wantCode)
				}
			}
		})
	}
}

func TestMethodAndPathRouting(t *testing.T) {
	s := New(Options{})

	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/config", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /config status = %d, want 405", rr.Code)
	}

	rr = httptest.NewRecorder()
	s.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("GET /nope status = %d, want 404", rr.Code)
	}

	rr = httptest.NewRecorder()
	s.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/config", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /config with no ConfigStore: status = %d, want 503", rr.Code)
	}
}

func TestGetState(t *testing.T) {
	want := State{
		Profile: ProfileState{ActiveProfileID: "default", ActiveLayer: 0},
		Device:  DeviceState{Connected: true, Name: "X-TOUCH MINI"},
		Audio:   AudioState{Connected: true},
		Focus:   FocusState{Available: false},
		Controls: []ControlState{{
			Control:    model.Control{Kind: model.ControlEncoder, Index: 1},
			Gesture:    model.GestureTurn,
			ActionType: model.ActionVolumeAdjust,
			Resolved:   &ResolvedTarget{Refs: []string{"stream:118"}, VolumePercent: 42},
		}},
	}
	s := New(Options{State: &fakeState{state: want}})

	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/state", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}

	// Assert wire key names, not just a Go round trip: these are the
	// contract M07's generated TypeScript client is built from, and a
	// struct-to-struct comparison alone wouldn't catch a renamed tag.
	var wire map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := wire["profile"].(map[string]any)["activeProfileId"]; !ok {
		t.Error(`expected "profile.activeProfileId" in the response`)
	}
	controls, ok := wire["controls"].([]any)
	if !ok || len(controls) != 1 {
		t.Fatalf(`expected "controls" to be a one-element array, got %#v`, wire["controls"])
	}
	control0 := controls[0].(map[string]any)
	if _, ok := control0["control"].(map[string]any)["kind"]; !ok {
		t.Error(`expected "controls[0].control.kind" in the response`)
	}
	resolved, ok := control0["resolved"].(map[string]any)
	if !ok {
		t.Fatal(`expected "controls[0].resolved" in the response`)
	}
	if _, ok := resolved["volumePercent"]; !ok {
		t.Error(`expected "controls[0].resolved.volumePercent" in the response`)
	}
}
