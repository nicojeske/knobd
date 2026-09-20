package midi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Sentinel errors a caller can match with errors.Is. midi.Open and
// realPort's methods wrap the underlying os error with the appropriate
// one of these so callers (chiefly Supervisor) can tell "we closed it on
// purpose" apart from "the device went away" without parsing strings.
var (
	// ErrPortClosed means Close was called; a Read/Write failing with
	// this should not trigger a reconnect.
	ErrPortClosed = errors.New("midi: port closed")
	// ErrDeviceGone means the device disappeared out from under an open
	// Port (unplugged, or dropped by the kernel around suspend/resume).
	ErrDeviceGone = errors.New("midi: device disconnected")
	// ErrNotCharDevice means Open's path exists but is not a character
	// device — most commonly because it's /dev/snd/controlC<N> (the ALSA
	// *control* device that /dev/snd/by-id's symlink actually points at)
	// rather than /dev/snd/midiC<N>D0. Opening a control device as if it
	// were rawmidi doesn't fail — read(2) on it just blocks forever — so
	// this check exists to fail loudly instead of hanging.
	ErrNotCharDevice = errors.New("midi: not a character device")
)

// readBufferSize is sized well above the largest single read(2) this
// device produces in practice; it just needs to be "big enough that a
// burst doesn't take many syscalls", not exact.
const readBufferSize = 4096

// portInboxSize bounds how many decoded messages can queue between the
// reader goroutine and a Read call. Sized against the highest rate
// actually observed (404 pitch-bend messages in a 25s free-play capture,
// ~16/s; a fast manual sweep is bursty but brief) with real headroom —
// see Dropped's doc comment for what happens if it's not enough.
const portInboxSize = 256

// realPort is the real Port backend: an open rawmidi character device
// plus one goroutine parsing its byte stream.
type realPort struct {
	f *os.File

	inbox   chan Message
	dropped atomic.Uint64

	writeMu sync.Mutex

	closeOnce sync.Once
	closeErr  error

	done      chan struct{}
	readErrMu sync.Mutex
	readErr   error
}

// Open opens the rawmidi character device at path as a Port.
func Open(path string) (Port, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("midi: stat %s: %w", path, err)
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		return nil, fmt.Errorf("midi: %s is not a rawmidi character device: %w", path, ErrNotCharDevice)
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("midi: open %s: %w", path, err)
	}
	return newPort(f), nil
}

// newPort wraps an already-open file as a Port. Split out from Open so
// tests can drive the exact same reader/close/write machinery over an
// os.Pipe() instead of the real character device — a pipe is pollable
// with the same Close-unblocks-Read semantics (see port.go's package doc
// comment), so this exercises the real code path with no hardware.
func newPort(f *os.File) *realPort {
	p := &realPort{
		f:     f,
		inbox: make(chan Message, portInboxSize),
		done:  make(chan struct{}),
	}
	go p.readLoop()
	return p
}

func (p *realPort) readLoop() {
	defer close(p.done)
	var ps parser
	buf := make([]byte, readBufferSize)
	for {
		n, err := p.f.Read(buf)
		now := time.Now() // closest available to the hardware event: the
		// rawmidi character device carries no hardware timestamp itself
		// (that's a sequencer-API feature), so the instant read(2)
		// returns is what Message.Time promises.
		if n > 0 {
			for _, msg := range ps.push(buf[:n], now) {
				p.deliver(msg)
			}
		}
		if err != nil {
			p.setReadErr(err)
			return
		}
	}
}

// deliver enqueues msg without ever blocking: the reader goroutine must
// keep calling read(2) promptly, because a reader that blocks risks a
// kernel-side rawmidi FIFO overrun, which drops *bytes* rather than
// whole messages and corrupts running-status framing for everything
// after it. If the inbox is full, the oldest queued message is dropped
// to make room. See Dropped.
func (p *realPort) deliver(msg Message) {
	select {
	case p.inbox <- msg:
		return
	default:
	}
	// Inbox full: evicting the oldest message to make room for msg is
	// itself the drop — count it here, not only when a send outright
	// fails, since the eviction+retry below almost always succeeds.
	select {
	case <-p.inbox:
		p.dropped.Add(1)
	default:
	}
	select {
	case p.inbox <- msg:
	default:
		p.dropped.Add(1)
	}
}

func (p *realPort) setReadErr(err error) {
	p.readErrMu.Lock()
	defer p.readErrMu.Unlock()
	switch {
	case errors.Is(err, os.ErrClosed):
		p.readErr = ErrPortClosed
	case errors.Is(err, io.EOF):
		p.readErr = fmt.Errorf("midi: device closed the stream: %w", ErrDeviceGone)
	default:
		p.readErr = fmt.Errorf("midi: read: %v: %w", err, ErrDeviceGone)
	}
}

// Dropped returns how many messages have been discarded so far because
// the inbox was full. Dropping the newest pitch-bend in a fast fader
// sweep is harmless (the next one supersedes it); dropping a note-off
// is not (it can wedge a press as "held forever"), but at the measured
// message rates the buffer is never expected to fill — this exists so
// monitor can report it if that assumption is ever wrong.
func (p *realPort) Dropped() uint64 {
	return p.dropped.Load()
}

// Read implements Port.
func (p *realPort) Read(ctx context.Context) (Message, error) {
	select {
	case msg := <-p.inbox:
		return msg, nil
	case <-ctx.Done():
		return Message{}, ctx.Err()
	case <-p.done:
		// Drain anything already queued before reporting the terminal
		// error, so a Close racing the last few in-flight reads doesn't
		// lose them.
		select {
		case msg := <-p.inbox:
			return msg, nil
		default:
		}
		p.readErrMu.Lock()
		err := p.readErr
		p.readErrMu.Unlock()
		return Message{}, err
	}
}

// Write implements Port. It holds its own mutex (rather than relying on
// os.File.Write's single-call atomicity) because a caller may need to
// send several Messages as one logical update — an encoder ring update
// is more than one MIDI message — and Write may be called concurrently
// per Port's doc comment.
func (p *realPort) Write(ctx context.Context, msg Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	if dl, ok := ctx.Deadline(); ok {
		if err := p.f.SetWriteDeadline(dl); err == nil {
			defer p.f.SetWriteDeadline(time.Time{})
		}
	}
	if _, err := p.f.Write([]byte{msg.Status, msg.Data1, msg.Data2}); err != nil {
		if errors.Is(err, os.ErrClosed) {
			return ErrPortClosed
		}
		return fmt.Errorf("midi: write: %w", err)
	}
	return nil
}

// Close implements Port. Close is idempotent. Closing the file unblocks
// a Read currently blocked in the kernel (see port.go's package doc
// comment on Go's netpoller covering character devices on Linux);
// SetReadDeadline first is redundant insurance in case that ever isn't
// true for some future kernel/fd combination — if it fires,
// os.ErrNoDeadline distinguishes "not pollable" from "already closed"
// and the reader would otherwise hang instead of exiting promptly.
func (p *realPort) Close() error {
	p.closeOnce.Do(func() {
		if err := p.f.SetReadDeadline(time.Now()); err != nil && !errors.Is(err, os.ErrNoDeadline) {
			// Non-fatal: proceed to Close regardless.
			_ = err
		}
		p.closeErr = p.f.Close()
		<-p.done
	})
	return p.closeErr
}

var _ Port = (*realPort)(nil)
