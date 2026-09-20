package focus

import (
	"context"
	"testing"
	"time"
)

func TestFakeProviderCurrentAndWatch(t *testing.T) {
	f := NewFakeProvider()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := f.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	want := AppInfo{PID: 4417, ResourceClass: "brave-browser", Caption: "Example"}
	f.SetFocused(want)

	got, err := f.Current(ctx)
	if err != nil || got != want {
		t.Fatalf("Current() = %+v, %v; want %+v", got, err, want)
	}

	select {
	case ev := <-ch:
		if ev != want {
			t.Errorf("Watch event = %+v, want %+v", ev, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for focus event")
	}
}

func TestFakeProviderWatchClosesOnCancel(t *testing.T) {
	f := NewFakeProvider()
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := f.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to close on cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel to close")
	}
}
