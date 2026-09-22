package engine

import (
	"sync"
	"time"
)

// testClock is a fully deterministic Clock for engine-level tests: Now()
// only changes when Advance is called, and a Timer this clock creates
// fires exactly when Advance moves the clock to or past its deadline --
// zero real time elapses for the 600ms hold boundary or the 350ms
// double-press window under test.
//
// Callers driving a real Engine.Run loop through this clock must stamp
// every injected midi.Message.Time with testClock.Now() at the moment
// of injection (or a deliberate offset from it), so gesture timestamps
// (Handle) and the loop's own clock (Tick) share one timeline.
type testClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*testTimer
	// resets is signaled, best-effort and non-blocking, every time any
	// Timer this clock created is Reset. Engine.Run calls Reset exactly
	// once per loop iteration where a gesture deadline is pending, so a
	// test can wait on this to know the loop has processed an event and
	// rearmed its timer -- deterministic synchronization with no sleep.
	resets chan struct{}
}

func newTestClock(start time.Time) *testClock {
	return &testClock{now: start, resets: make(chan struct{}, 16)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) NewTimer(d time.Duration) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &testTimer{clock: c, ch: make(chan time.Time, 1), deadline: c.now.Add(d), armed: true}
	c.timers = append(c.timers, t)
	return t
}

// Advance moves the clock forward by d, firing every armed timer whose
// deadline is now due (deadline <= the new now).
func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	var due []*testTimer
	for _, t := range c.timers {
		if t.armed && !t.deadline.After(now) {
			t.armed = false
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	for _, t := range due {
		select {
		case t.ch <- now:
		default:
		}
	}
}

// waitForReset blocks until a Timer belonging to this clock has been
// Reset since the last call, or timeout elapses.
func (c *testClock) waitForReset(timeout time.Duration) bool {
	select {
	case <-c.resets:
		return true
	case <-time.After(timeout):
		return false
	}
}

type testTimer struct {
	clock    *testClock
	ch       chan time.Time
	deadline time.Time
	armed    bool
}

func (t *testTimer) C() <-chan time.Time { return t.ch }

func (t *testTimer) Reset(d time.Duration) bool {
	t.clock.mu.Lock()
	was := t.armed
	t.deadline = t.clock.now.Add(d)
	t.armed = true
	t.clock.mu.Unlock()

	select {
	case t.clock.resets <- struct{}{}:
	default:
	}
	return was
}

func (t *testTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	was := t.armed
	t.armed = false
	return was
}

var _ Clock = (*testClock)(nil)
var _ Timer = (*testTimer)(nil)
