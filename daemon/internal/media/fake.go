package media

import (
	"context"
	"sync"
	"time"
)

// unavailableBackend is a Backend that reports no players, ever --
// mirrors focus.Unavailable(), for a system with no D-Bus session bus
// (or before media.New has been tried).
type unavailableBackend struct{}

// Unavailable returns a Backend that never reports a player, matching
// focus.Unavailable()'s role: a documented production stand-in, not a
// test double.
func Unavailable() Backend {
	return unavailableBackend{}
}

func (unavailableBackend) Watch(ctx context.Context) (<-chan Event, error) {
	ch := make(chan Event)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func (unavailableBackend) PlayPause(string) error             { return ErrUnavailable }
func (unavailableBackend) Next(string) error                  { return ErrUnavailable }
func (unavailableBackend) Previous(string) error              { return ErrUnavailable }
func (unavailableBackend) Seek(string, time.Duration) error   { return ErrUnavailable }
func (unavailableBackend) SetShuffle(string, bool) error      { return ErrUnavailable }
func (unavailableBackend) SetLoopStatus(string, string) error { return ErrUnavailable }
func (unavailableBackend) Close() error                       { return nil }

var _ Backend = unavailableBackend{}

// unavailableNotifier is a Notifier that always fails, matching
// unavailableBackend's role for when the session bus connection
// media.now_playing's Notify would use isn't available.
type unavailableNotifier struct{}

// UnavailableNotifier returns a Notifier that always fails with
// ErrUnavailable.
func UnavailableNotifier() Notifier { return unavailableNotifier{} }

func (unavailableNotifier) Notify(string, string) error { return ErrUnavailable }

var _ Notifier = unavailableNotifier{}

// FakeBackend is a Backend test double: Watch delivers whatever Events
// are pushed to it via Emit, and every command call is recorded rather
// than sent anywhere -- the same shape as audio.FakeBackend/
// focus.FakeProvider.
type FakeBackend struct {
	mu     sync.Mutex
	events chan Event

	// Calls records every command method invoked, in order, as
	// "Method(busName[, arg])".
	Calls []string
	// Err, if set, is returned by every command call instead of
	// recording it.
	Err error
}

// NewFakeBackend returns a ready-to-use FakeBackend.
func NewFakeBackend() *FakeBackend {
	return &FakeBackend{events: make(chan Event, 32)}
}

// Emit pushes ev to whatever's currently reading Watch's channel. It is
// safe to call before Watch, but the event is dropped if the channel's
// buffer (32) is full -- tests should Emit synchronously with reads.
func (f *FakeBackend) Emit(ev Event) {
	select {
	case f.events <- ev:
	default:
	}
}

func (f *FakeBackend) Watch(ctx context.Context) (<-chan Event, error) {
	return f.events, nil
}

func (f *FakeBackend) record(call string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.Calls = append(f.Calls, call)
	return nil
}

func (f *FakeBackend) PlayPause(busName string) error { return f.record("PlayPause(" + busName + ")") }
func (f *FakeBackend) Next(busName string) error      { return f.record("Next(" + busName + ")") }
func (f *FakeBackend) Previous(busName string) error  { return f.record("Previous(" + busName + ")") }

func (f *FakeBackend) Seek(busName string, offset time.Duration) error {
	return f.record("Seek(" + busName + "," + offset.String() + ")")
}

func (f *FakeBackend) SetShuffle(busName string, shuffle bool) error {
	v := "false"
	if shuffle {
		v = "true"
	}
	return f.record("SetShuffle(" + busName + "," + v + ")")
}

func (f *FakeBackend) SetLoopStatus(busName string, status string) error {
	return f.record("SetLoopStatus(" + busName + "," + status + ")")
}

func (f *FakeBackend) Close() error { return nil }

var _ Backend = (*FakeBackend)(nil)

// FakeNotifier is a Notifier test double: every call is recorded rather
// than sent anywhere.
type FakeNotifier struct {
	mu   sync.Mutex
	Err  error
	Sent []struct{ Summary, Body string }
}

func (f *FakeNotifier) Notify(summary, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.Sent = append(f.Sent, struct{ Summary, Body string }{summary, body})
	return nil
}

var _ Notifier = (*FakeNotifier)(nil)
