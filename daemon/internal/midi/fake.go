package midi

import (
	"context"
	"errors"
	"sync"
)

// FakePort is an in-memory Port for tests: nothing else in the daemon
// should need a real device to be tested, since every hardware-facing
// package is built behind an interface (see CLAUDE.md). Tests push
// inbound messages with Inject and inspect outbound ones via Written.
type FakePort struct {
	mu      sync.Mutex
	inbox   chan Message
	written []Message
	closed  bool
}

// NewFakePort returns a ready-to-use FakePort. inboxSize bounds how many
// injected messages can be buffered before Inject blocks; 0 means
// unbuffered (Inject blocks until a Read consumes the message).
func NewFakePort(inboxSize int) *FakePort {
	return &FakePort{inbox: make(chan Message, inboxSize)}
}

// Inject makes msg available to the next Read call. It blocks if the
// inbox is full (see NewFakePort) or returns immediately with an error
// if the port has been closed.
func (p *FakePort) Inject(ctx context.Context, msg Message) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errors.New("midi: fake port is closed")
	}
	p.mu.Unlock()

	select {
	case p.inbox <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Read implements Port.
func (p *FakePort) Read(ctx context.Context) (Message, error) {
	select {
	case msg, ok := <-p.inbox:
		if !ok {
			return Message{}, errors.New("midi: fake port is closed")
		}
		return msg, nil
	case <-ctx.Done():
		return Message{}, ctx.Err()
	}
}

// Write implements Port, recording msg for later inspection via
// Written.
func (p *FakePort) Write(ctx context.Context, msg Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("midi: fake port is closed")
	}
	p.written = append(p.written, msg)
	return nil
}

// Written returns every message passed to Write so far, in order.
func (p *FakePort) Written() []Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Message, len(p.written))
	copy(out, p.written)
	return out
}

// Close implements Port. Close is idempotent.
func (p *FakePort) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.inbox)
	}
	return nil
}

var _ Port = (*FakePort)(nil)
