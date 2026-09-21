package audio

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestFakeBackendSeedAndQuery(t *testing.T) {
	f := NewFakeBackend()
	f.Seed(
		[]Device{{ID: "sink1", Description: "Speakers", IsDefault: true}},
		[]Device{{ID: "mic1", Description: "Mic"}},
		[]Stream{{ID: "118", Direction: StreamPlayback, Props: map[string]string{"application.name": "vesktop"}}},
	)

	ctx := context.Background()

	sinks, err := f.Sinks(ctx)
	if err != nil || len(sinks) != 1 || sinks[0].ID != "sink1" {
		t.Fatalf("Sinks() = %+v, %v", sinks, err)
	}
	sources, err := f.Sources(ctx)
	if err != nil || len(sources) != 1 || sources[0].ID != "mic1" {
		t.Fatalf("Sources() = %+v, %v", sources, err)
	}
	streams, err := f.Streams(ctx)
	if err != nil || len(streams) != 1 || streams[0].ID != "118" {
		t.Fatalf("Streams() = %+v, %v", streams, err)
	}

	v, err := f.GetVolume(ctx, Ref{Kind: RefSink, ID: "sink1"})
	if err != nil {
		t.Fatalf("GetVolume: %v", err)
	}
	if v.Percent != 100 || v.Muted {
		t.Errorf("GetVolume(sink1) = %+v, want 100%%/unmuted default", v)
	}
}

func TestFakeBackendSetVolumeAndMute(t *testing.T) {
	f := NewFakeBackend()
	f.Seed([]Device{{ID: "sink1"}}, nil, nil)
	ctx := context.Background()
	ref := Ref{Kind: RefSink, ID: "sink1"}

	if err := f.SetVolume(ctx, ref, 42); err != nil {
		t.Fatalf("SetVolume: %v", err)
	}
	if err := f.SetMute(ctx, ref, true); err != nil {
		t.Fatalf("SetMute: %v", err)
	}
	v, err := f.GetVolume(ctx, ref)
	if err != nil {
		t.Fatalf("GetVolume: %v", err)
	}
	if v.Percent != 42 || !v.Muted {
		t.Errorf("GetVolume(sink1) = %+v, want {42 true}", v)
	}
}

func TestFakeBackendUnknownIDErrors(t *testing.T) {
	f := NewFakeBackend()
	ctx := context.Background()
	ref := Ref{Kind: RefSink, ID: "nope"}
	if _, err := f.GetVolume(ctx, ref); err == nil {
		t.Error("expected error for unknown ref")
	}
	if err := f.SetVolume(ctx, ref, 1); err == nil {
		t.Error("expected error for unknown ref")
	}
	if err := f.SetMute(ctx, ref, true); err == nil {
		t.Error("expected error for unknown ref")
	}
}

func TestFakeBackendSubscribeReceivesEmit(t *testing.T) {
	f := NewFakeBackend()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := f.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	want := Event{Kind: EventStreamRemoved, Stream: &Stream{ID: "118"}}
	f.Emit(want)

	select {
	case got := <-ch:
		if got.Kind != want.Kind || got.Stream.ID != want.Stream.ID {
			t.Errorf("got %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for emitted event")
	}
}

func TestFakeBackendSubscribeClosesOnCancel(t *testing.T) {
	f := NewFakeBackend()
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := f.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed, got a value instead")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel to close after cancel")
	}
}

func TestFakeBackendSubscribeConcurrentEmitAndCancel(t *testing.T) {
	// Regression test for the send-races-close bug fixed in Emit/Subscribe:
	// hammer Emit and cancel concurrently across many subscribers and
	// confirm the race detector (run via `go test -race`) finds nothing
	// and nothing panics.
	f := NewFakeBackend()
	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := f.Subscribe(ctx)
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range ch {
			}
		}()
		go func() {
			defer wg.Done()
			cancel()
		}()
	}
	for i := 0; i < 200; i++ {
		f.Emit(Event{Kind: EventStreamRemoved, Stream: &Stream{ID: "x"}})
	}
	wg.Wait()
}
