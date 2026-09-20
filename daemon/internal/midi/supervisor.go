package midi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Sentinel errors for Supervisor.
var (
	// ErrNoDevice means Discover found nothing to connect to.
	ErrNoDevice = errors.New("midi: no X-Touch Mini found")
	// ErrNotConnected means Write was called while Supervisor has no
	// live underlying Port. Unlike Read (which blocks across a
	// disconnect — see Supervisor's doc comment), Write fails
	// immediately: an LED update is a state push the caller can safely
	// drop and re-send once Connected fires again.
	ErrNotConnected = errors.New("midi: not currently connected")
)

// Supervisor is a self-healing Port: it discovers, opens, and reads from
// the X-Touch Mini, and transparently reconnects — rediscovering, since
// a card's number is not stable across a replug — when the device
// disappears. A caller that only needs to read/write MIDI can treat it
// as an ordinary Port: Read blocks across a reconnect rather than
// surfacing the disconnect as an error, and Write fails fast
// (ErrNotConnected) rather than blocking while disconnected.
//
// A reconnect is not otherwise visible to a plain Port caller, which is
// deliberately wrong for one consumer: M05's LED state has to be
// re-pushed after a reconnect, since the encoder rings don't remember
// anything across a power cycle. Connected and Disconnected exist for
// that.
type Supervisor struct {
	opts SupervisorOptions

	msgs           chan Message
	connectedCh    chan DeviceInfo
	disconnectedCh chan error

	portMu sync.Mutex
	port   Port

	closedFlag atomic.Bool
	closeOnce  sync.Once
	cancel     context.CancelFunc
	done       chan struct{}
}

// SupervisorOptions configures a Supervisor. Discover, Open, and Watch
// default to the package's real implementations; tests override them
// (and the timings) to exercise reconnect/backoff logic with no
// hardware, via SupervisorOptions.
type SupervisorOptions struct {
	Discover func() ([]DeviceInfo, error)
	Open     func(string) (Port, error)
	Watch    func(ctx context.Context, dir string) (<-chan struct{}, error)
	// WatchDir is what Watch observes for hotplug hints. Defaults to
	// /dev/snd/by-id. If Watch fails on it (e.g. it doesn't exist yet),
	// Supervisor logs a warning and falls back to PollInterval alone.
	WatchDir string

	// PollInterval is a backstop rescan interval, independent of Watch,
	// covering a missed or unavailable inotify event.
	PollInterval time.Duration
	// MinBackoff/MaxBackoff bound the retry delay after a failed connect
	// attempt (e.g. the device node exists but udev hasn't chmod'd it
	// yet after a replug) — starts at MinBackoff, doubles, caps at
	// MaxBackoff, and resets to MinBackoff after any successful connect.
	MinBackoff, MaxBackoff time.Duration

	Logger *slog.Logger
}

func (o *SupervisorOptions) setDefaults() {
	if o.Discover == nil {
		o.Discover = Discover
	}
	if o.Open == nil {
		o.Open = Open
	}
	if o.Watch == nil {
		o.Watch = watch
	}
	if o.WatchDir == "" {
		o.WatchDir = sndDir + "/by-id"
	}
	if o.PollInterval <= 0 {
		o.PollInterval = time.Second
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

// NewSupervisor starts discovering and connecting in the background
// immediately and returns without waiting for a first connection. Call
// Close to stop it.
func NewSupervisor(opts SupervisorOptions) *Supervisor {
	opts.setDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Supervisor{
		opts:           opts,
		msgs:           make(chan Message),
		connectedCh:    make(chan DeviceInfo, 1),
		disconnectedCh: make(chan error, 1),
		cancel:         cancel,
		done:           make(chan struct{}),
	}
	go s.run(ctx)
	return s
}

func (s *Supervisor) run(ctx context.Context) {
	defer close(s.done)

	watchCh, err := s.opts.Watch(ctx, s.opts.WatchDir)
	if err != nil {
		s.opts.Logger.Warn("midi: hotplug watch unavailable, falling back to polling only", "dir", s.opts.WatchDir, "err", err)
		watchCh = nil // nil channel: the select case below simply never fires
	}

	backstop := time.NewTicker(s.opts.PollInterval)
	defer backstop.Stop()

	backoff := s.opts.MinBackoff
	for {
		port, info, err := s.tryConnect()
		if err != nil {
			s.opts.Logger.Debug("midi: connect attempt failed", "err", err, "retry_in", backoff)
			select {
			case <-ctx.Done():
				return
			case <-watchCh:
			case <-backstop.C:
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > s.opts.MaxBackoff {
				backoff = s.opts.MaxBackoff
			}
			continue
		}

		backoff = s.opts.MinBackoff
		s.setPort(port)
		notify(s.connectedCh, info)
		s.opts.Logger.Info("midi: connected", "device", info.Name, "path", info.Path)

		readErr := s.pump(ctx, port)
		s.clearPort()
		if s.closedFlag.Load() {
			return
		}
		s.opts.Logger.Warn("midi: disconnected", "err", readErr)
		notify(s.disconnectedCh, readErr)
	}
}

func (s *Supervisor) tryConnect() (Port, DeviceInfo, error) {
	infos, err := s.opts.Discover()
	if err != nil {
		return nil, DeviceInfo{}, fmt.Errorf("midi: discover: %w", err)
	}
	if len(infos) == 0 {
		return nil, DeviceInfo{}, ErrNoDevice
	}
	info := infos[0]
	port, err := s.opts.Open(info.Path)
	if err != nil {
		return nil, DeviceInfo{}, fmt.Errorf("midi: open %s: %w", info.Path, err)
	}
	return port, info, nil
}

// pump forwards msg after msg from port into s.msgs until port.Read
// fails or ctx is canceled, and returns that terminal error.
func (s *Supervisor) pump(ctx context.Context, port Port) error {
	for {
		msg, err := port.Read(ctx)
		if err != nil {
			return err
		}
		select {
		case s.msgs <- msg:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// notify sends v on ch without blocking, coalescing into "there's
// something new to look at" if the previous value hasn't been consumed
// yet — callers like monitor only care about the latest status.
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

func (s *Supervisor) setPort(p Port) {
	s.portMu.Lock()
	s.port = p
	s.portMu.Unlock()
}

func (s *Supervisor) clearPort() {
	s.portMu.Lock()
	p := s.port
	s.port = nil
	s.portMu.Unlock()
	if p != nil {
		p.Close()
	}
}

// Connected reports each time a connection is (re)established. Buffered
// 1 and coalescing: a slow consumer sees the most recent device, not a
// backlog.
func (s *Supervisor) Connected() <-chan DeviceInfo {
	return s.connectedCh
}

// Disconnected reports each time the connection is lost, with the error
// that caused it. Buffered 1 and coalescing, like Connected.
func (s *Supervisor) Disconnected() <-chan error {
	return s.disconnectedCh
}

// Read implements Port. It blocks across a reconnect — see the type doc
// comment — returning only when ctx is done or Close is called.
func (s *Supervisor) Read(ctx context.Context) (Message, error) {
	select {
	case msg := <-s.msgs:
		return msg, nil
	case <-ctx.Done():
		return Message{}, ctx.Err()
	case <-s.done:
		return Message{}, ErrPortClosed
	}
}

// Write implements Port. Unlike Read, it does not block across a
// disconnect: it fails immediately with ErrNotConnected so a caller
// (M05's LED updates) isn't stalled by an unplugged controller.
func (s *Supervisor) Write(ctx context.Context, msg Message) error {
	s.portMu.Lock()
	port := s.port
	s.portMu.Unlock()
	if port == nil {
		return ErrNotConnected
	}
	return port.Write(ctx, msg)
}

// Close implements Port. Close is idempotent and stops all reconnect
// attempts; after Close, Read returns ErrPortClosed instead of blocking.
func (s *Supervisor) Close() error {
	s.closeOnce.Do(func() {
		s.closedFlag.Store(true)
		s.cancel()
		s.portMu.Lock()
		p := s.port
		s.portMu.Unlock()
		if p != nil {
			p.Close()
		}
	})
	<-s.done
	return nil
}

var _ Port = (*Supervisor)(nil)
