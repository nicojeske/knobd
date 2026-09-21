package audio

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func quietSupervisorOptions() SupervisorOptions {
	return SupervisorOptions{
		MinBackoff: 5 * time.Millisecond,
		MaxBackoff: 20 * time.Millisecond,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestSupervisorConnectsAndPassesThroughCalls(t *testing.T) {
	fb := NewFakeBackend()
	fb.Seed(nil, nil, []Stream{{ID: "1", Props: map[string]string{"node.name": "java"}}})

	opts := quietSupervisorOptions()
	opts.Connect = func(context.Context) (Backend, error) { return fb, nil }

	s := NewSupervisor(opts)
	defer s.Close()

	select {
	case <-s.Connected():
	case <-time.After(2 * time.Second):
		t.Fatal("Supervisor never reported Connected")
	}

	streams, err := s.Streams(context.Background())
	if err != nil {
		t.Fatalf("Streams: %v", err)
	}
	if len(streams) != 1 || streams[0].ID != "1" {
		t.Errorf("Streams() = %+v, want the single fake stream", streams)
	}
}

func TestSupervisorSubscribeDeliversEmittedEvents(t *testing.T) {
	fb := NewFakeBackend()
	opts := quietSupervisorOptions()
	opts.Connect = func(context.Context) (Backend, error) { return fb, nil }

	s := NewSupervisor(opts)
	defer s.Close()

	select {
	case <-s.Connected():
	case <-time.After(2 * time.Second):
		t.Fatal("never connected")
	}

	ch, err := s.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	want := Event{Kind: EventStreamRemoved, Stream: &Stream{ID: "7"}}
	// fb.Emit only reaches subscribers currently registered on fb itself;
	// Supervisor.Subscribe registers exactly one such subscriber (via its
	// own run loop) the moment the connection succeeds, so this may need
	// a moment to land.
	waitForCond(t, 2*time.Second, func() bool {
		fb.mu.Lock()
		n := len(fb.subs)
		fb.mu.Unlock()
		return n >= 1
	})
	fb.Emit(want)

	select {
	case got := <-ch:
		if got.Kind != want.Kind || got.Stream.ID != want.Stream.ID {
			t.Errorf("got %+v, want %+v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an emitted event through Supervisor")
	}
}

func TestSupervisorRetriesUntilConnectSucceeds(t *testing.T) {
	var attempts atomic.Int32
	fb := NewFakeBackend()

	opts := quietSupervisorOptions()
	opts.Connect = func(context.Context) (Backend, error) {
		if attempts.Add(1) <= 2 {
			return nil, errors.New("connection refused")
		}
		return fb, nil
	}

	s := NewSupervisor(opts)
	defer s.Close()

	select {
	case <-s.Connected():
	case <-time.After(2 * time.Second):
		t.Fatal("Supervisor never connected after transient failures")
	}
	if n := attempts.Load(); n < 3 {
		t.Errorf("Connect called %d times, want at least 3", n)
	}
}

func TestSupervisorReconnectsAfterBackendFailureAndResyncs(t *testing.T) {
	fb1 := NewFakeBackend()
	fb2 := NewFakeBackend()
	fb2.Seed(nil, nil, []Stream{{ID: "42"}})

	backends := make(chan Backend, 1)
	backends <- fb1

	opts := quietSupervisorOptions()
	opts.Connect = func(context.Context) (Backend, error) {
		select {
		case b := <-backends:
			return b, nil
		default:
			return fb2, nil
		}
	}

	s := NewSupervisor(opts)
	defer s.Close()

	select {
	case <-s.Connected():
	case <-time.After(2 * time.Second):
		t.Fatal("never got first Connected")
	}

	ch, err := s.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	waitForCond(t, 2*time.Second, func() bool {
		fb1.mu.Lock()
		n := len(fb1.subs)
		fb1.mu.Unlock()
		return n >= 1
	})
	fb1.Fail() // simulate the connection dying

	select {
	case err := <-s.Disconnected():
		if err == nil {
			t.Error("Disconnected() delivered a nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Supervisor never reported Disconnected after the backend failed")
	}

	select {
	case <-s.Connected():
	case <-time.After(2 * time.Second):
		t.Fatal("Supervisor never reconnected")
	}

	// The subscriber channel obtained before the failure must survive
	// the reconnect (not be closed and force a re-Subscribe), and must
	// see a resync notice once reconnected.
	select {
	case ev := <-ch:
		if ev.Kind != EventResync {
			t.Errorf("first event after reconnect = %+v, want EventResync", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for EventResync after reconnect")
	}

	// The new connection must actually work.
	streams, err := s.Streams(context.Background())
	if err != nil {
		t.Fatalf("Streams after reconnect: %v", err)
	}
	found := false
	for _, str := range streams {
		if str.ID == "42" {
			found = true
		}
	}
	if !found {
		t.Errorf("Streams() after reconnect = %+v, want it to include the second backend's stream", streams)
	}
}

func TestSupervisorSetVolumeFailsFastWhenDisconnected(t *testing.T) {
	block := make(chan struct{})
	opts := quietSupervisorOptions()
	opts.Connect = func(context.Context) (Backend, error) {
		<-block // never actually connects for the life of this test
		return nil, errors.New("unreachable")
	}

	s := NewSupervisor(opts)
	defer func() {
		close(block)
		s.Close()
	}()

	start := time.Now()
	err := s.SetVolume(context.Background(), Ref{Kind: RefSink, ID: "x"}, 50)
	if !errors.Is(err, ErrDisconnected) {
		t.Fatalf("SetVolume() error = %v, want ErrDisconnected", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("SetVolume took %v while disconnected, want to fail immediately", elapsed)
	}

	err = s.SetMute(context.Background(), Ref{Kind: RefSink, ID: "x"}, true)
	if !errors.Is(err, ErrDisconnected) {
		t.Fatalf("SetMute() error = %v, want ErrDisconnected", err)
	}
}

func TestSupervisorCloseStopsReconnectingAndClosesSubscribers(t *testing.T) {
	fb := NewFakeBackend()
	var connectCount atomic.Int32

	opts := quietSupervisorOptions()
	opts.Connect = func(context.Context) (Backend, error) {
		connectCount.Add(1)
		return fb, nil
	}

	s := NewSupervisor(opts)
	select {
	case <-s.Connected():
	case <-time.After(2 * time.Second):
		t.Fatal("never connected")
	}

	ch, err := s.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	countAtClose := connectCount.Load()
	time.Sleep(100 * time.Millisecond)
	if got := connectCount.Load(); got != countAtClose {
		t.Errorf("Connect was called %d more time(s) after Close; Supervisor kept reconnecting", got-countAtClose)
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected the subscriber channel to be closed after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the subscriber channel to close after Close")
	}

	if _, err := s.Sinks(context.Background()); err == nil {
		t.Error("expected Sinks() to fail after Close")
	}
}

func waitForCond(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
