package audio

import (
	"context"
	"testing"
	"time"
)

func TestFakeBackendSeedAndQuery(t *testing.T) {
	f := NewFakeBackend()
	f.Seed(
		[]Device{{ID: "sink1", Description: "Speakers", IsDefault: true}},
		[]Device{{ID: "mic1", Description: "Mic"}},
		[]Stream{{ID: "vesktop", Props: map[string]string{"application.name": "vesktop"}}},
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
	if err != nil || len(streams) != 1 || streams[0].ID != "vesktop" {
		t.Fatalf("Streams() = %+v, %v", streams, err)
	}

	v, err := f.GetVolume(ctx, "sink1")
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

	if err := f.SetVolume(ctx, "sink1", 42); err != nil {
		t.Fatalf("SetVolume: %v", err)
	}
	if err := f.SetMute(ctx, "sink1", true); err != nil {
		t.Fatalf("SetMute: %v", err)
	}
	v, err := f.GetVolume(ctx, "sink1")
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
	if _, err := f.GetVolume(ctx, "nope"); err == nil {
		t.Error("expected error for unknown id")
	}
	if err := f.SetVolume(ctx, "nope", 1); err == nil {
		t.Error("expected error for unknown id")
	}
	if err := f.SetMute(ctx, "nope", true); err == nil {
		t.Error("expected error for unknown id")
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

	want := Event{Kind: EventStreamRemoved, Stream: &Stream{ID: "vesktop"}}
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
