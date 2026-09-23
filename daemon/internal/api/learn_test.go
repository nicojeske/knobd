package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeLearnController struct {
	startState      LearnState
	startErr        error
	stopErr         error
	gotStartTimeout time.Duration
	stopCalled      bool
}

func (f *fakeLearnController) StartLearn(_ context.Context, timeout time.Duration) (LearnState, error) {
	f.gotStartTimeout = timeout
	return f.startState, f.startErr
}

func (f *fakeLearnController) StopLearn(context.Context) error {
	f.stopCalled = true
	return f.stopErr
}

func TestStartLearn(t *testing.T) {
	t.Run("no controller is 503", func(t *testing.T) {
		s := New(Options{})
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/learn", nil))
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", rr.Code)
		}
	})

	t.Run("no body means a zero timeout is passed through", func(t *testing.T) {
		expiresAt := time.Now().Add(15 * time.Second)
		ctl := &fakeLearnController{startState: LearnState{Active: true, ExpiresAt: &expiresAt}}
		s := New(Options{Learn: ctl})
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/learn", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
		}
		if ctl.gotStartTimeout != 0 {
			t.Errorf("timeout passed to StartLearn = %v, want 0 (no body -> engine default)", ctl.gotStartTimeout)
		}
		var got LearnState
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !got.Active {
			t.Error(`expected "active": true in the response`)
		}
	})

	t.Run("timeoutMs is converted to a time.Duration", func(t *testing.T) {
		ctl := &fakeLearnController{startState: LearnState{Active: true}}
		s := New(Options{Learn: ctl})
		body := bytes.NewBufferString(`{"timeoutMs": 5000}`)
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/learn", body))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
		}
		if ctl.gotStartTimeout != 5*time.Second {
			t.Errorf("timeout passed to StartLearn = %v, want 5s", ctl.gotStartTimeout)
		}
	})

	t.Run("malformed json is 400", func(t *testing.T) {
		ctl := &fakeLearnController{}
		s := New(Options{Learn: ctl})
		body := bytes.NewBufferString(`{"timeoutMs": `)
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/learn", body))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rr.Code)
		}
		var errResp ErrorResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("unmarshal error response: %v", err)
		}
		if errResp.Code != CodeInvalidJSON {
			t.Errorf("code = %q, want %q", errResp.Code, CodeInvalidJSON)
		}
	})

	t.Run("controller error is 500", func(t *testing.T) {
		ctl := &fakeLearnController{startErr: errors.New("engine not consuming")}
		s := New(Options{Learn: ctl})
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/learn", nil))
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rr.Code)
		}
	})
}

func TestStopLearn(t *testing.T) {
	t.Run("no controller is 503", func(t *testing.T) {
		s := New(Options{})
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/learn", nil))
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", rr.Code)
		}
	})

	t.Run("success is 204 and calls StopLearn", func(t *testing.T) {
		ctl := &fakeLearnController{}
		s := New(Options{Learn: ctl})
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/learn", nil))
		if rr.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204", rr.Code)
		}
		if !ctl.stopCalled {
			t.Error("StopLearn was not called")
		}
	})

	t.Run("controller error is 500", func(t *testing.T) {
		ctl := &fakeLearnController{stopErr: errors.New("engine not consuming")}
		s := New(Options{Learn: ctl})
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/learn", nil))
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rr.Code)
		}
	})
}
