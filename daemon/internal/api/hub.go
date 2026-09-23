package api

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// defaultFlushInterval throttles the hub's state pushes, leading edge
// plus trailing flush, citing daemon/internal/engine's ledFlushInterval
// verbatim: the fader's ~404 pitch-bend messages per free-play session
// is exactly the burst this exists to survive, and it's the same burst
// -- a knob turn changes api.State only once PipeWire (or the LED
// recompute) echoes it, at the same cadence LEDs already repaint at.
const defaultFlushInterval = 30 * time.Millisecond

// subscriberEventQueueDepth bounds each subscriber's queue of
// non-droppable events (hello, learn_input, error). State snapshots are
// not queued here at all -- see subscriber's stateCh.
const subscriberEventQueueDepth = 32

// Hub is the transport-agnostic core of GET /events: it tracks
// subscribers, throttles and fans out State snapshots, and broadcasts
// learn_input events. stream_sse.go is the only thing that knows this is
// SSE -- Hub itself produces typed Event values, so a later transport
// swap touches one file (see specs/adr/0004-ipc-over-unix-socket.md's
// Update (M07)).
//
// Full snapshots, not deltas: model.Default()'s starter config produces
// on the order of a dozen ControlState entries, a few KB as compact
// JSON -- cheap even at the 30ms worst case, and only during an actual
// fader sweep. A delta would need its own diff step, its own merge step
// on the UI side, a second OpenAPI shape, and -- the actual
// disqualifier -- a resync protocol, because a dropped delta is
// unrecoverable. With full snapshots, "drop the stale one, keep the
// newest" is the entire backpressure policy, and it is correct by
// construction (see subscriber.sendState).
type Hub struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}

	state          StateProvider
	flushInterval  time.Duration
	allowedOrigins map[string]struct{}
	log            *slog.Logger

	dirtyCh chan struct{}
}

// HubOptions configures NewHub.
type HubOptions struct {
	State StateProvider
	// FlushInterval overrides defaultFlushInterval; tests set this to
	// e.g. one millisecond so they don't need a real clock abstraction.
	FlushInterval time.Duration
	// AllowedOrigins is the Origin allowlist stream_sse.go enforces.
	// Requests with no Origin header (everything not a browser -- in
	// particular the Rust bridge, which is this API's only real client;
	// see ADR 0004) are always allowed regardless of this list.
	AllowedOrigins []string
	Logger         *slog.Logger
}

// NewHub constructs a Hub. Call Run to start its throttling loop.
func NewHub(opts HubOptions) *Hub {
	flush := opts.FlushInterval
	if flush <= 0 {
		flush = defaultFlushInterval
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	origins := make(map[string]struct{}, len(opts.AllowedOrigins))
	for _, o := range opts.AllowedOrigins {
		origins[o] = struct{}{}
	}
	return &Hub{
		subs:           make(map[*subscriber]struct{}),
		state:          opts.State,
		flushInterval:  flush,
		allowedOrigins: origins,
		log:            log,
		dirtyCh:        make(chan struct{}, 1),
	}
}

// FlushIntervalMs is Hello.FlushIntervalMs's value.
func (h *Hub) FlushIntervalMs() int {
	return int(h.flushInterval.Milliseconds())
}

// originAllowed reports whether origin may open GET /events. An empty
// origin (no Origin header at all) is always allowed -- see
// AllowedOrigins' doc comment.
func (h *Hub) originAllowed(origin string) bool {
	if origin == "" || len(h.allowedOrigins) == 0 {
		return true
	}
	_, ok := h.allowedOrigins[origin]
	return ok
}

// subscriber is one open GET /events connection's mailbox.
type subscriber struct {
	// stateCh is single-slot, latest-wins: a State snapshot is complete
	// on its own, so a subscriber that hasn't picked up the previous one
	// yet should see only the newest, never queue a backlog of stale
	// snapshots.
	stateCh chan State
	// eventsCh is a small append-only queue for events that must never
	// be silently dropped (hello, learn_input, error/close).
	eventsCh chan Event

	closeOnce   sync.Once
	closedCh    chan struct{}
	closeReason ErrorCode
}

func newSubscriber() *subscriber {
	return &subscriber{
		stateCh:  make(chan State, 1),
		eventsCh: make(chan Event, subscriberEventQueueDepth),
		closedCh: make(chan struct{}),
	}
}

// sendState replaces whatever snapshot (if any) is already queued,
// never blocking: the hub must not stall over one slow subscriber.
func (s *subscriber) sendState(st State) {
	for {
		select {
		case s.stateCh <- st:
			return
		default:
		}
		select {
		case <-s.stateCh:
		default:
		}
	}
}

// sendEvent enqueues a non-droppable event. On overflow the subscriber
// is closed with CodeSlowConsumer instead: better than blocking the hub
// for every other subscriber, and better than silently dropping a
// learn_input or a hello the client cannot afford to miss.
func (s *subscriber) sendEvent(ev Event) {
	select {
	case s.eventsCh <- ev:
	default:
		s.close(CodeSlowConsumer)
	}
}

func (s *subscriber) close(reason ErrorCode) {
	s.closeOnce.Do(func() {
		s.closeReason = reason
		close(s.closedCh)
	})
}

// Subscribe registers a new subscriber. Callers must call Unsubscribe
// when the connection ends (stream_sse.go does this via defer).
func (h *Hub) Subscribe() *subscriber {
	sub := newSubscriber()
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

// Unsubscribe removes sub. Safe to call more than once or after Run has
// already closed every subscriber at shutdown.
func (h *Hub) Unsubscribe(sub *subscriber) {
	h.mu.Lock()
	delete(h.subs, sub)
	h.mu.Unlock()
}

func (h *Hub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// NotifyStateDirty wakes the hub to rebuild and push a fresh State
// snapshot, throttled by flushInterval. Safe to call concurrently with
// Run, including from the engine's run goroutine (it is
// engine.Deps.OnStateChanged's non-blocking implementation); a no-op if
// Run isn't consuming, and cheap with zero subscribers (State is never
// even called -- see Run).
func (h *Hub) NotifyStateDirty() {
	select {
	case h.dirtyCh <- struct{}{}:
	default:
	}
}

// NotifyLearnInput broadcasts input to every current subscriber as a
// learn_input event.
func (h *Hub) NotifyLearnInput(input LearnInput) {
	h.broadcast(Event{Type: EventLearnInput, Input: &input})
}

// NotifyConfigChanged broadcasts a config_changed event carrying
// revision. Wired up in cmd/knobd/configstore.go (M07's next slice).
func (h *Hub) NotifyConfigChanged(revision uint64) {
	h.broadcast(Event{Type: EventConfigChanged, Config: &ConfigChanged{Revision: revision}})
}

func (h *Hub) broadcast(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		sub.sendEvent(ev)
	}
}

func (h *Hub) publishState(ctx context.Context) {
	if h.state == nil {
		return
	}
	st, err := h.state.State(ctx)
	if err != nil {
		h.log.Warn("api: hub: build state snapshot for push failed", "err", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		sub.sendState(st)
	}
}

func (h *Hub) closeAllSubscribers() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		sub.close(CodeInternal)
	}
}

// Run drives the hub's throttling loop until ctx is canceled: on each
// NotifyStateDirty wakeup, it flushes immediately if flushInterval has
// already elapsed since the last flush (the leading edge -- a single
// deliberate knob turn is reflected with no added latency), or arms a
// timer for the remainder otherwise (the trailing edge -- what collapses
// a burst of wakeups to one push per interval). Mirrors
// engine's markLEDsDirty/flushLEDs pair exactly, for the same reason.
func (h *Hub) Run(ctx context.Context) error {
	var lastFlush time.Time
	var timer *time.Timer
	var timerC <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			h.closeAllSubscribers()
			return nil

		case <-h.dirtyCh:
			if h.subscriberCount() == 0 {
				continue // nothing to build a snapshot for
			}
			if time.Since(lastFlush) >= h.flushInterval {
				h.publishState(ctx)
				lastFlush = time.Now()
				continue
			}
			if timerC == nil {
				d := h.flushInterval - time.Since(lastFlush)
				if d < 0 {
					d = 0
				}
				timer = time.NewTimer(d)
				timerC = timer.C
			}

		case <-timerC:
			timerC = nil
			h.publishState(ctx)
			lastFlush = time.Now()
		}
	}
}
