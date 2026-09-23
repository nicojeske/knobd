package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sseFrame is one parsed "event: X\ndata: Y\n\n" frame.
type sseFrame struct {
	event string
	data  []byte
}

// readSSEFrames reads n frames from body (an httptest server response),
// skipping keep-alive comment lines (": ping").
func readSSEFrames(t *testing.T, body *bufio.Reader, n int) []sseFrame {
	t.Helper()
	var frames []sseFrame
	for len(frames) < n {
		line, err := body.ReadString('\n')
		if err != nil {
			t.Fatalf("reading SSE stream: %v (got %d of %d frames)", err, len(frames), n)
		}
		line = strings.TrimRight(line, "\n")
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "event: ") {
			t.Fatalf("expected an \"event: \" line, got %q", line)
		}
		event := strings.TrimPrefix(line, "event: ")
		dataLine, err := body.ReadString('\n')
		if err != nil {
			t.Fatalf("reading data line: %v", err)
		}
		dataLine = strings.TrimRight(dataLine, "\n")
		if !strings.HasPrefix(dataLine, "data: ") {
			t.Fatalf("expected a \"data: \" line, got %q", dataLine)
		}
		frames = append(frames, sseFrame{event: event, data: []byte(strings.TrimPrefix(dataLine, "data: "))})
		// consume the blank line terminating the frame
		if _, err := body.ReadString('\n'); err != nil {
			t.Fatalf("reading frame terminator: %v", err)
		}
	}
	return frames
}

func TestEventsStreamSendsHelloThenState(t *testing.T) {
	hub := NewHub(HubOptions{State: &fakeState{state: State{Profile: ProfileState{ActiveProfileID: "default"}}}, FlushInterval: time.Millisecond})
	cancel := runHub(t, hub)
	defer cancel()

	s := New(Options{Events: hub, State: &fakeState{state: State{Profile: ProfileState{ActiveProfileID: "default"}}}})
	srv := httptest.NewServer(s)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	frames := readSSEFrames(t, bufio.NewReader(resp.Body), 2)

	if frames[0].event != "hello" {
		t.Errorf("frame[0].event = %q, want %q", frames[0].event, "hello")
	}
	var hello Event
	if err := json.Unmarshal(frames[0].data, &hello); err != nil {
		t.Fatalf("unmarshal hello frame: %v", err)
	}
	if hello.Hello == nil || hello.Hello.ProtocolVersion != EventProtocolVersion {
		t.Errorf("hello.Hello = %+v, want ProtocolVersion %d", hello.Hello, EventProtocolVersion)
	}
	if hello.Seq != 1 {
		t.Errorf("hello.Seq = %d, want 1", hello.Seq)
	}

	if frames[1].event != "state" {
		t.Errorf("frame[1].event = %q, want %q", frames[1].event, "state")
	}
	var state Event
	if err := json.Unmarshal(frames[1].data, &state); err != nil {
		t.Fatalf("unmarshal state frame: %v", err)
	}
	if state.State == nil || state.State.Profile.ActiveProfileID != "default" {
		t.Errorf("state.State = %+v, want ActiveProfileID \"default\"", state.State)
	}
	if state.Seq != 2 {
		t.Errorf("state.Seq = %d, want 2", state.Seq)
	}
}

func TestEventsStreamPushesConfigChanged(t *testing.T) {
	hub := NewHub(HubOptions{State: &fakeState{}, FlushInterval: time.Millisecond})
	cancel := runHub(t, hub)
	defer cancel()

	s := New(Options{Events: hub, State: &fakeState{}})
	srv := httptest.NewServer(s)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)

	// hello, then the initial state snapshot.
	readSSEFrames(t, reader, 2)

	hub.NotifyConfigChanged(42)

	frame := readSSEFrames(t, reader, 1)[0]
	if frame.event != "config_changed" {
		t.Fatalf("event = %q, want %q", frame.event, "config_changed")
	}
	var ev Event
	if err := json.Unmarshal(frame.data, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ev.Config == nil || ev.Config.Revision != 42 {
		t.Errorf("Config = %+v, want Revision 42", ev.Config)
	}
}

func TestEventsStreamOriginAllowlist(t *testing.T) {
	hub := NewHub(HubOptions{AllowedOrigins: []string{"tauri://localhost"}, FlushInterval: time.Millisecond})
	cancel := runHub(t, hub)
	defer cancel()

	s := New(Options{Events: hub, State: &fakeState{}})
	srv := httptest.NewServer(s)
	defer srv.Close()

	t.Run("allowed origin connects", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/events", nil)
		req.Header.Set("Origin", "tauri://localhost")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /events: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("disallowed origin is rejected", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/events", nil)
		req.Header.Set("Origin", "https://evil.example")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /events: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", resp.StatusCode)
		}
		var errResp ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			t.Fatalf("decode error response: %v", err)
		}
		if errResp.Code != CodeForbiddenOrigin {
			t.Errorf("code = %q, want %q", errResp.Code, CodeForbiddenOrigin)
		}
	})
}

func TestEventsStreamNoHubIs503(t *testing.T) {
	s := New(Options{})
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/events", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rr.Code)
	}
}

// TestEventsStreamRealSocketSmoke exercises the handler through
// httptest's real HTTP transport (not just ServeHTTP), matching the
// spirit of server_test.go's TestSocketTransport -- a streaming
// response is exactly the kind of thing that behaves differently over
// an httptest.NewRecorder (which never flushes) than over a real
// connection.
func TestEventsStreamRealSocketSmoke(t *testing.T) {
	hub := NewHub(HubOptions{State: &fakeState{}, FlushInterval: time.Millisecond})
	cancel := runHub(t, hub)
	defer cancel()

	s := New(Options{Events: hub, State: &fakeState{}})
	srv := httptest.NewServer(s)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 512)
	n, err := resp.Body.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Contains(buf[:n], []byte("event: hello")) {
		t.Errorf("expected a hello frame in the first read, got: %s", buf[:n])
	}
}
