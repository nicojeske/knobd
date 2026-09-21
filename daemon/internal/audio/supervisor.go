package audio

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// errBackendConnectionLost is Supervisor's internal terminal error for
// "the active backend's Subscribe channel closed on its own" — the
// contract Backend.Subscribe documents for exactly this situation (a
// backend failure, as opposed to our own ctx being canceled).
var errBackendConnectionLost = errors.New("audio: backend connection lost")

// Supervisor is a self-healing Backend: it connects, and transparently
// reconnects with backoff when the connection to pipewire-pulse is lost
// (a PipeWire crash or `systemctl --user restart pipewire` are both
// routine, and knobd should not need restarting for either), mirroring
// midi.Supervisor's discover/reconnect pattern for the MIDI side.
//
// Reads and enumerations (Sinks/Sources/Streams/GetVolume) wait across a
// reconnect, like midi.Supervisor.Read. Writes (SetVolume/SetMute) fail
// fast with ErrDisconnected instead, like midi.Supervisor.Write: a
// dropped volume change is a state push the caller can safely re-send
// once reconnected, but a stalled one is not something the caller should
// have to wait out.
//
// Subscribe is the one part of Backend with no MIDI analogue: it is
// multi-caller and long-lived, so a Subscribe registration must survive
// a reconnect rather than have its channel closed and force the caller
// to re-subscribe. Supervisor re-issues the underlying Subscribe after
// every reconnect and emits EventResync to every still-live caller, so
// subscriber channels are only ever closed by Supervisor.Close or by the
// caller's own ctx — never by a reconnect.
//
// One simplification, acceptable for M03: registering/unregistering a
// Subscribe caller is served by the same goroutine that dials a new
// connection, so a Subscribe/ctx-cancel during the brief window of an
// active connect attempt is served slightly late (bounded by however
// long that attempt takes) rather than instantly. This isn't on any
// latency-sensitive path — a knob turn goes through SetVolume, which
// never blocks on this goroutine — so a fully decoupled connector was
// judged not worth the extra complexity for this milestone.
type Supervisor struct {
	opts SupervisorOptions

	backendMu sync.Mutex
	backend   Backend
	readyCh   chan struct{} // closed exactly once backend != nil; swapped fresh on disconnect

	registerCh   chan *subRequest
	unregisterCh chan *audioSub2

	connectedCh    chan struct{}
	disconnectedCh chan error

	closedFlag bool
	closeOnce  sync.Once
	cancel     context.CancelFunc
	done       chan struct{}
}

// audioSub2 is Supervisor's own subscriber bookkeeping — named
// distinctly from dispatcher's audioSub since the two are unrelated
// (Supervisor fans out across reconnects; dispatcher fans out within
// one connection) despite the similar shape.
type audioSub2 struct{ ch chan Event }

type subRequest struct {
	ctx  context.Context
	resp chan chan Event
}

// SupervisorOptions configures a Supervisor. Connect defaults to
// connecting a real pulseBackend with BackendOptions; tests override it
// to script a sequence of successes/failures against FakeBackend with no
// PipeWire, the same way midi.SupervisorOptions overrides Discover/Open.
type SupervisorOptions struct {
	Connect        func(ctx context.Context) (Backend, error)
	BackendOptions Options

	// MinBackoff/MaxBackoff bound the retry delay after a failed connect
	// attempt, starting at MinBackoff, doubling, capping at MaxBackoff,
	// and resetting to MinBackoff after any successful connect — same
	// shape as midi.SupervisorOptions.
	MinBackoff, MaxBackoff time.Duration

	Logger *slog.Logger
}

func (o *SupervisorOptions) setDefaults() {
	if o.Connect == nil {
		bo := o.BackendOptions
		o.Connect = func(ctx context.Context) (Backend, error) { return newPulseBackend(ctx, bo) }
	}
	if o.MinBackoff <= 0 {
		o.MinBackoff = 100 * time.Millisecond
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = 5 * time.Second
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
}

// NewSupervisor starts connecting in the background immediately and
// returns without waiting for a first connection. Call Close to stop it.
func NewSupervisor(opts SupervisorOptions) *Supervisor {
	opts.setDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Supervisor{
		opts:           opts,
		readyCh:        make(chan struct{}),
		registerCh:     make(chan *subRequest, 8),
		unregisterCh:   make(chan *audioSub2, 8),
		connectedCh:    make(chan struct{}, 1),
		disconnectedCh: make(chan error, 1),
		cancel:         cancel,
		done:           make(chan struct{}),
	}
	go s.run(ctx)
	return s
}

func (s *Supervisor) run(ctx context.Context) {
	defer close(s.done)

	subs := make(map[*audioSub2]struct{})
	defer func() {
		for sub := range subs {
			close(sub.ch)
		}
	}()

	backoff := s.opts.MinBackoff
	everConnected := false

	for {
		backend, events, err := s.connectAndSubscribe(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.opts.Logger.Debug("audio: connect attempt failed", "err", err, "retry_in", backoff)
			select {
			case <-ctx.Done():
				return
			case req := <-s.registerCh:
				s.handleRegister(subs, req)
			case sub := <-s.unregisterCh:
				s.handleUnregister(subs, sub)
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > s.opts.MaxBackoff {
				backoff = s.opts.MaxBackoff
			}
			continue
		}

		backoff = s.opts.MinBackoff
		s.setBackend(backend)
		notify1(s.connectedCh)
		s.opts.Logger.Info("audio: connected")
		if everConnected {
			s.fanOut(subs, Event{Kind: EventResync})
		}
		everConnected = true

		disconnectErr := s.pump(ctx, subs, events)
		s.clearBackend()
		backend.Close()
		if ctx.Err() != nil {
			return
		}
		s.opts.Logger.Warn("audio: disconnected", "err", disconnectErr)
		notify(s.disconnectedCh, disconnectErr)
	}
}

func (s *Supervisor) connectAndSubscribe(ctx context.Context) (Backend, <-chan Event, error) {
	b, err := s.opts.Connect(ctx)
	if err != nil {
		return nil, nil, err
	}
	events, err := b.Subscribe(ctx)
	if err != nil {
		b.Close()
		return nil, nil, err
	}
	return b, events, nil
}

// pump services subscriber (un)registration and forwards translated
// events until the backend's events channel closes (a backend failure)
// or ctx is done.
func (s *Supervisor) pump(ctx context.Context, subs map[*audioSub2]struct{}, events <-chan Event) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case req := <-s.registerCh:
			s.handleRegister(subs, req)
		case sub := <-s.unregisterCh:
			s.handleUnregister(subs, sub)
		case ev, ok := <-events:
			if !ok {
				return errBackendConnectionLost
			}
			s.fanOut(subs, ev)
		}
	}
}

func (s *Supervisor) handleRegister(subs map[*audioSub2]struct{}, req *subRequest) {
	sub := &audioSub2{ch: make(chan Event, subBufferSize)}
	subs[sub] = struct{}{}
	go func() {
		select {
		case <-req.ctx.Done():
		case <-s.done:
			return // run's own shutdown defer will close every sub.ch
		}
		select {
		case s.unregisterCh <- sub:
		case <-s.done:
		}
	}()
	req.resp <- sub.ch
}

func (s *Supervisor) handleUnregister(subs map[*audioSub2]struct{}, sub *audioSub2) {
	if _, ok := subs[sub]; ok {
		delete(subs, sub)
		close(sub.ch)
	}
}

func (s *Supervisor) fanOut(subs map[*audioSub2]struct{}, ev Event) {
	for sub := range subs {
		select {
		case sub.ch <- ev:
		default:
			s.opts.Logger.Debug("audio: dropped event for a slow Supervisor subscriber", "kind", ev.Kind)
		}
	}
}

// --- Backend implementation ---

func (s *Supervisor) currentBackend(ctx context.Context) (Backend, error) {
	for {
		s.backendMu.Lock()
		b, ready := s.backend, s.readyCh
		s.backendMu.Unlock()
		if b != nil {
			return b, nil
		}
		select {
		case <-ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.done:
			return nil, ErrDisconnected
		}
	}
}

func (s *Supervisor) setBackend(b Backend) {
	s.backendMu.Lock()
	s.backend = b
	close(s.readyCh)
	s.backendMu.Unlock()
}

func (s *Supervisor) clearBackend() {
	s.backendMu.Lock()
	s.backend = nil
	s.readyCh = make(chan struct{})
	s.backendMu.Unlock()
}

func (s *Supervisor) liveBackend() Backend {
	s.backendMu.Lock()
	defer s.backendMu.Unlock()
	return s.backend
}

func (s *Supervisor) Sinks(ctx context.Context) ([]Device, error) {
	b, err := s.currentBackend(ctx)
	if err != nil {
		return nil, err
	}
	return b.Sinks(ctx)
}

func (s *Supervisor) Sources(ctx context.Context) ([]Device, error) {
	b, err := s.currentBackend(ctx)
	if err != nil {
		return nil, err
	}
	return b.Sources(ctx)
}

func (s *Supervisor) Streams(ctx context.Context) ([]Stream, error) {
	b, err := s.currentBackend(ctx)
	if err != nil {
		return nil, err
	}
	return b.Streams(ctx)
}

func (s *Supervisor) GetVolume(ctx context.Context, ref Ref) (VolumeState, error) {
	b, err := s.currentBackend(ctx)
	if err != nil {
		return VolumeState{}, err
	}
	return b.GetVolume(ctx, ref)
}

// SetVolume fails fast with ErrDisconnected rather than waiting for a
// reconnect — see the type doc comment for why this is asymmetric with
// the read methods above.
func (s *Supervisor) SetVolume(ctx context.Context, ref Ref, percent float64) error {
	b := s.liveBackend()
	if b == nil {
		return ErrDisconnected
	}
	return b.SetVolume(ctx, ref, percent)
}

// SetMute fails fast; see SetVolume.
func (s *Supervisor) SetMute(ctx context.Context, ref Ref, muted bool) error {
	b := s.liveBackend()
	if b == nil {
		return ErrDisconnected
	}
	return b.SetMute(ctx, ref, muted)
}

// Subscribe registers ctx's caller with the run loop and returns a
// channel that survives every reconnect until ctx is done or the
// Supervisor is closed — see the type doc comment.
func (s *Supervisor) Subscribe(ctx context.Context) (<-chan Event, error) {
	resp := make(chan chan Event, 1)
	req := &subRequest{ctx: ctx, resp: resp}
	select {
	case s.registerCh <- req:
	case <-s.done:
		ch := make(chan Event)
		close(ch)
		return ch, nil
	}
	select {
	case ch := <-resp:
		return ch, nil
	case <-s.done:
		ch := make(chan Event)
		close(ch)
		return ch, nil
	}
}

// Close stops Supervisor and closes every live Subscribe channel. Idempotent.
func (s *Supervisor) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
	})
	<-s.done
	return nil
}

// Connected reports each time a connection is (re)established. Buffered
// 1 and coalescing, matching midi.Supervisor.Connected.
func (s *Supervisor) Connected() <-chan struct{} { return s.connectedCh }

// Disconnected reports each time the connection is lost, with the error
// that caused it. Buffered 1 and coalescing, matching
// midi.Supervisor.Disconnected.
func (s *Supervisor) Disconnected() <-chan error { return s.disconnectedCh }

// notify sends v on ch without blocking, coalescing into "there's
// something new to look at" if the previous value hasn't been consumed
// yet. Identical in spirit to midi.Supervisor's package-private notify
// helper (unexported there, so duplicated here rather than shared across
// packages for a four-line function).
func notify[T any](ch chan T, v T) {
	select {
	case ch <- v:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- v:
	default:
	}
}

// notify1 is notify specialized for chan struct{}, used where there's no
// payload worth coalescing, just a "connected" pulse.
func notify1(ch chan struct{}) { notify(ch, struct{}{}) }

var _ Backend = (*Supervisor)(nil)
