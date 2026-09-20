package audio

import (
	"context"
	"fmt"
	"sync"
)

// FakeBackend is an in-memory Backend for tests: seed it with Sinks,
// Sources, and Streams, then drive it through the same interface real
// callers use. It has no PipeWire logic of its own (in particular, it
// does not implement model.AppMatcher resolution — see M03) — it is
// deliberately just a map, so tests can assert on exactly the state they
// put in.
type FakeBackend struct {
	mu sync.Mutex

	sinks   []Device
	sources []Device
	streams []Stream
	volume  map[string]VolumeState

	subs []chan Event
}

// NewFakeBackend returns an empty FakeBackend. Use Seed to populate it.
func NewFakeBackend() *FakeBackend {
	return &FakeBackend{volume: make(map[string]VolumeState)}
}

// Seed replaces the backend's sinks, sources, and streams wholesale, and
// initializes any newly-seen ID's volume state to 100%/unmuted if it did
// not already have one.
func (f *FakeBackend) Seed(sinks, sources []Device, streams []Stream) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sinks, f.sources, f.streams = sinks, sources, streams
	for _, all := range [][]Device{sinks, sources} {
		for _, d := range all {
			if _, ok := f.volume[d.ID]; !ok {
				f.volume[d.ID] = VolumeState{Percent: 100}
			}
		}
	}
	for _, s := range streams {
		if _, ok := f.volume[s.ID]; !ok {
			f.volume[s.ID] = VolumeState{Percent: 100}
		}
	}
}

// Emit pushes ev to every current Subscribe channel. It is how a test
// simulates something changing out from under the daemon (e.g. an app
// closing mid-scenario). Emit does not hold the backend lock while
// sending, so a slow/absent receiver cannot deadlock Subscribe's cleanup
// goroutine; a receiver that falls more than 16 events behind will block
// Emit itself, which is deliberate for tests that want back-pressure.
func (f *FakeBackend) Emit(ev Event) {
	f.mu.Lock()
	subs := append([]chan Event(nil), f.subs...)
	f.mu.Unlock()
	for _, ch := range subs {
		ch <- ev
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

func (f *FakeBackend) GetVolume(_ context.Context, id string) (VolumeState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.volume[id]
	if !ok {
		return VolumeState{}, fmt.Errorf("audio: fake backend has no such id %q", id)
	}
	return v, nil
}

func (f *FakeBackend) SetVolume(_ context.Context, id string, percent float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.volume[id]
	if !ok {
		return fmt.Errorf("audio: fake backend has no such id %q", id)
	}
	v.Percent = percent
	f.volume[id] = v
	return nil
}

func (f *FakeBackend) SetMute(_ context.Context, id string, muted bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.volume[id]
	if !ok {
		return fmt.Errorf("audio: fake backend has no such id %q", id)
	}
	v.Muted = muted
	f.volume[id] = v
	return nil
}

func (f *FakeBackend) Subscribe(ctx context.Context) (<-chan Event, error) {
	ch := make(chan Event, 16)
	f.mu.Lock()
	f.subs = append(f.subs, ch)
	f.mu.Unlock()

	go func() {
		<-ctx.Done()
		f.mu.Lock()
		defer f.mu.Unlock()
		for i, c := range f.subs {
			if c == ch {
				f.subs = append(f.subs[:i], f.subs[i+1:]...)
				break
			}
		}
		close(ch)
	}()

	return ch, nil
}

func (f *FakeBackend) Close() error { return nil }

var _ Backend = (*FakeBackend)(nil)
