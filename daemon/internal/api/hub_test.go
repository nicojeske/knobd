package api

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// countingState is a StateProvider that counts how many times State was
// actually called, and can return a distinguishable value each time.
type countingState struct {
	calls atomic.Int64
}

func (c *countingState) State(context.Context) (State, error) {
	n := c.calls.Add(1)
	return State{Profile: ProfileState{ActiveProfileID: "call", ActiveLayer: int(n)}}, nil
}

func runHub(t *testing.T, h *Hub) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Hub.Run did not return after cancel")
		}
	})
	return cancel
}

func TestHubZeroSubscribersNeverCallsState(t *testing.T) {
	st := &countingState{}
	h := NewHub(HubOptions{State: st, FlushInterval: time.Millisecond})
	runHub(t, h)

	for i := 0; i < 10; i++ {
		h.NotifyStateDirty()
	}
	time.Sleep(20 * time.Millisecond)

	if n := st.calls.Load(); n != 0 {
		t.Errorf("State was called %d times with zero subscribers, want 0", n)
	}
}

func TestHubLeadingEdgeFiresImmediately(t *testing.T) {
	st := &countingState{}
	h := NewHub(HubOptions{State: st, FlushInterval: 50 * time.Millisecond})
	runHub(t, h)

	sub := h.Subscribe()
	defer h.Unsubscribe(sub)

	h.NotifyStateDirty()
	select {
	case <-sub.stateCh:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not receive a state snapshot on the leading edge")
	}
}

func TestHubBurstCollapsesToFewFlushes(t *testing.T) {
	st := &countingState{}
	h := NewHub(HubOptions{State: st, FlushInterval: 30 * time.Millisecond})
	runHub(t, h)

	sub := h.Subscribe()
	defer h.Unsubscribe(sub)

	// A burst of 50 notifies within one flush interval must collapse to
	// the leading edge plus at most one trailing flush.
	for i := 0; i < 50; i++ {
		h.NotifyStateDirty()
	}
	time.Sleep(100 * time.Millisecond)

	if n := st.calls.Load(); n > 2 {
		t.Errorf("State was called %d times for one burst, want <= 2", n)
	}
	if n := st.calls.Load(); n < 1 {
		t.Errorf("State was never called despite a burst of notifies")
	}
}

func TestHubSlowSubscriberGetsLatestState(t *testing.T) {
	st := &countingState{}
	h := NewHub(HubOptions{State: st, FlushInterval: time.Millisecond})
	runHub(t, h)

	sub := h.Subscribe()
	defer h.Unsubscribe(sub)

	h.NotifyStateDirty()
	time.Sleep(10 * time.Millisecond)
	h.NotifyStateDirty()
	time.Sleep(10 * time.Millisecond)
	h.NotifyStateDirty()
	time.Sleep(10 * time.Millisecond)

	// The subscriber never drained stateCh -- it must hold exactly the
	// most recent snapshot, not the first or a queue of all three.
	select {
	case got := <-sub.stateCh:
		if got.Profile.ActiveLayer != int(st.calls.Load()) {
			t.Errorf("got snapshot from call %d, want the latest (%d)", got.Profile.ActiveLayer, st.calls.Load())
		}
	default:
		t.Fatal("expected a queued state snapshot")
	}
	select {
	case <-sub.stateCh:
		t.Fatal("expected only one queued snapshot (latest-wins), found a second")
	default:
	}
}

func TestHubEventOverflowClosesSubscriberAsSlowConsumer(t *testing.T) {
	h := NewHub(HubOptions{FlushInterval: time.Millisecond})
	runHub(t, h)

	sub := h.Subscribe()
	defer h.Unsubscribe(sub)

	// Overflow the non-droppable events queue without ever draining it.
	for i := 0; i < subscriberEventQueueDepth+1; i++ {
		h.NotifyLearnInput(LearnInput{})
	}

	select {
	case <-sub.closedCh:
		if sub.closeReason != CodeSlowConsumer {
			t.Errorf("closeReason = %q, want %q", sub.closeReason, CodeSlowConsumer)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber was not closed after its event queue overflowed")
	}
}

func TestHubOriginAllowlist(t *testing.T) {
	h := NewHub(HubOptions{AllowedOrigins: []string{"tauri://localhost", "http://localhost:1420"}})

	cases := []struct {
		origin string
		want   bool
	}{
		{"", true}, // no Origin header at all -- the Rust bridge's case
		{"tauri://localhost", true},
		{"http://localhost:1420", true},
		{"https://evil.example", false},
	}
	for _, tc := range cases {
		if got := h.originAllowed(tc.origin); got != tc.want {
			t.Errorf("originAllowed(%q) = %v, want %v", tc.origin, got, tc.want)
		}
	}
}

func TestHubOriginAllowlistEmptyMeansAllowAll(t *testing.T) {
	h := NewHub(HubOptions{})
	if !h.originAllowed("https://anything.example") {
		t.Error("an empty allowlist should allow every origin")
	}
}
