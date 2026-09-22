package focus

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUnavailableCurrentReturnsErrUnavailable(t *testing.T) {
	p := Unavailable()
	_, err := p.Current(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Current() error = %v, want ErrUnavailable", err)
	}
}

func TestUnavailableWatchClosesOnContextDone(t *testing.T) {
	p := Unavailable()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("Watch channel delivered a value; want it closed with no events")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch channel was not closed within the timeout")
	}
}

func TestUnavailableCloseIsNoop(t *testing.T) {
	if err := Unavailable().Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
}
