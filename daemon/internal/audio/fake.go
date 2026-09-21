package audio

import (
	"context"
	"fmt"
	"sync"
)

// FakeBackend is an in-memory Backend for tests: seed it with Sinks,
// Sources, and Streams, then drive it through the same interface real
// callers use. It has no PipeWire logic of its own (in particular, it
// does not implement model.AppMatcher resolution — see Resolve in
// matcher.go) — it is deliberately just a map, so tests can assert on
// exactly the state they put in.
type FakeBackend struct {
	mu sync.Mutex

	sinks   []Device
	sources []Device
	streams []Stream
	volume  map[Ref]VolumeState

	subs map[*fakeSub]struct{}
}

type fakeSub struct{ ch chan Event }

// NewFakeBackend returns an empty FakeBackend. Use Seed to populate it.
func NewFakeBackend() *FakeBackend {
	return &FakeBackend{volume: make(map[Ref]VolumeState), subs: make(map[*fakeSub]struct{})}
}

// Seed replaces the backend's sinks, sources, and streams wholesale, and
// initializes any newly-seen ID's volume state to 100%/unmuted if it did
// not already have one.
func (f *FakeBackend) Seed(sinks, sources []Device, streams []Stream) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sinks, f.sources, f.streams = sinks, sources, streams
	for kind, all := range map[RefKind][]Device{RefSink: sinks, RefSource: sources} {
		for _, d := range all {
			ref := Ref{Kind: kind, ID: d.ID}
			if _, ok := f.volume[ref]; !ok {
				f.volume[ref] = VolumeState{Percent: 100}
			}
		}
	}
	for _, s := range streams {
		ref := s.Ref()
		if _, ok := f.volume[ref]; !ok {
			f.volume[ref] = VolumeState{Percent: 100}
		}
	}
}

// Emit pushes ev to every current Subscribe channel, under the same lock
// Subscribe's cleanup goroutine uses to remove and close a subscriber's
// channel. That shared lock is what makes this safe: Emit only ever sees
// (and sends to) subscribers cleanup hasn't already removed, and cleanup
// can never close a channel Emit is concurrently sending to — the two
// are strictly ordered by f.mu, never interleaved. The cost is that a
// receiver that has fallen more than 16 events behind blocks Emit (and,
// for its duration, every other FakeBackend call) until it drains or its
// ctx is canceled — deliberate for tests that want to exercise
// back-pressure, and acceptable because this is test-only code with no
// real PipeWire read loop underneath to stall.
func (f *FakeBackend) Emit(ev Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for sub := range f.subs {
		sub.ch <- ev
	}
}

func (f *FakeBackend) Sinks(context.Context) ([]Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Device(nil), f.sinks...), nil
}

func (f *FakeBackend) Sources(context.Context) ([]Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Device(nil), f.sources...), nil
}

func (f *FakeBackend) Streams(context.Context) ([]Stream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Stream(nil), f.streams...), nil
}

func (f *FakeBackend) GetVolume(_ context.Context, ref Ref) (VolumeState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.volume[ref]
	if !ok {
		return VolumeState{}, fmt.Errorf("audio: fake backend has no such ref %+v", ref)
	}
	return v, nil
}

func (f *FakeBackend) SetVolume(_ context.Context, ref Ref, percent float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.volume[ref]
	if !ok {
		return fmt.Errorf("audio: fake backend has no such ref %+v", ref)
	}
	v.Percent = percent
	f.volume[ref] = v
	return nil
}

func (f *FakeBackend) SetMute(_ context.Context, ref Ref, muted bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.volume[ref]
	if !ok {
		return fmt.Errorf("audio: fake backend has no such ref %+v", ref)
	}
	v.Muted = muted
	f.volume[ref] = v
	return nil
}

// Subscribe registers a new subscriber and spawns the one goroutine that
// is ever allowed to close its channel: it waits for ctx to finish, then
// removes the subscriber and closes its channel under f.mu — the same
// lock Emit holds for its entire send loop, which is what rules out a
// send racing a close (see Emit's doc comment).
func (f *FakeBackend) Subscribe(ctx context.Context) (<-chan Event, error) {
	sub := &fakeSub{ch: make(chan Event, 16)}
	f.mu.Lock()
	f.subs[sub] = struct{}{}
	f.mu.Unlock()

	go func() {
		<-ctx.Done()
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, ok := f.subs[sub]; ok {
			delete(f.subs, sub)
			close(sub.ch)
		}
	}()

	return sub.ch, nil
}

// Fail simulates the backend dying out from under a caller: it closes
// every current Subscribe channel, as Backend's contract allows for ("a
// backend failure" — see Subscribe's doc comment) without requiring the
// test to cancel each subscriber's ctx individually. It's how
// audio.Supervisor's tests exercise reconnect-after-backend-failure with
// no real PipeWire.
func (f *FakeBackend) Fail() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for sub := range f.subs {
		delete(f.subs, sub)
		close(sub.ch)
	}
}

func (f *FakeBackend) Close() error { return nil }

var _ Backend = (*FakeBackend)(nil)
