package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeAudioProvider struct {
	graph AudioGraph
	err   error
}

func (f *fakeAudioProvider) AudioGraph(context.Context) (AudioGraph, error) { return f.graph, f.err }

func TestGetAudio(t *testing.T) {
	t.Run("no provider is 503", func(t *testing.T) {
		s := New(Options{})
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/audio", nil))
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", rr.Code)
		}
	})

	t.Run("provider error is 500", func(t *testing.T) {
		s := New(Options{Audio: &fakeAudioProvider{err: errors.New("backend exploded")}})
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/audio", nil))
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rr.Code)
		}
	})

	t.Run("deadline exceeded is 503 unavailable", func(t *testing.T) {
		s := New(Options{Audio: &fakeAudioProvider{err: context.DeadlineExceeded}})
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/audio", nil))
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", rr.Code)
		}
		var errResp ErrorResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("unmarshal error response: %v", err)
		}
		if errResp.Code != CodeUnavailable {
			t.Errorf("code = %q, want %q", errResp.Code, CodeUnavailable)
		}
	})

	t.Run("success round trips the raw props bag", func(t *testing.T) {
		want := AudioGraph{
			Now: time.Now().UTC().Truncate(time.Second),
			Sinks: []AudioDevice{
				{Ref: "sink:alsa_output.x", ID: "alsa_output.x", Description: "Speakers", IsDefault: true},
			},
			Streams: []AudioStream{
				{
					Ref: "stream:118", ID: "118", Direction: "playback", DisplayName: "vesktop",
					AppName: "vesktop", NodeName: "vesktop",
					Props:      map[string]string{"node.name": "vesktop", "application.name": "vesktop"},
					MatcherIDs: []string{"vesktop"},
				},
			},
		}
		s := New(Options{Audio: &fakeAudioProvider{graph: want}})
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/audio", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
		}

		var wire map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &wire); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		streams, ok := wire["streams"].([]any)
		if !ok || len(streams) != 1 {
			t.Fatalf(`expected "streams" to be a one-element array, got %#v`, wire["streams"])
		}
		stream0 := streams[0].(map[string]any)
		props, ok := stream0["props"].(map[string]any)
		if !ok {
			t.Fatal(`expected "streams[0].props" to be an object`)
		}
		if props["node.name"] != "vesktop" {
			t.Errorf(`streams[0].props["node.name"] = %v, want "vesktop"`, props["node.name"])
		}
		matcherIDs, ok := stream0["matcherIds"].([]any)
		if !ok || len(matcherIDs) != 1 || matcherIDs[0] != "vesktop" {
			t.Errorf(`streams[0].matcherIds = %#v, want ["vesktop"]`, stream0["matcherIds"])
		}
	})
}
