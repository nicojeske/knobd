package focus

import (
	"context"
	"sync"
)

// FakeProvider is an in-memory Provider for tests: call SetFocused to
// simulate a focus change, which both updates what Current returns and
// pushes an event to every active Watch channel.
type FakeProvider struct {
	mu      sync.Mutex
	current AppInfo
	watches []chan AppInfo
}

// NewFakeProvider returns a FakeProvider with no application focused.
func NewFakeProvider() *FakeProvider {
	return &FakeProvider{}
}

// SetFocused simulates the window manager reporting a new focused
// window.
func (f *FakeProvider) SetFocused(info AppInfo) {
	f.mu.Lock()
	f.current = info
	watches := append([]chan AppInfo(nil), f.watches...)
	f.mu.Unlock()

	for _, ch := range watches {
		ch <- info
	}
}

func (f *FakeProvider) Watch(ctx context.Context) (<-chan AppInfo, error) {
	ch := make(chan AppInfo, 16)
	f.mu.Lock()
	f.watches = append(f.watches, ch)
	f.mu.Unlock()

	go func() {
		<-ctx.Done()
		f.mu.Lock()
		defer f.mu.Unlock()
		for i, c := range f.watches {
			if c == ch {
				f.watches = append(f.watches[:i], f.watches[i+1:]...)
				break
			}
		}
		close(ch)
	}()

	return ch, nil
}

func (f *FakeProvider) Current(context.Context) (AppInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current, nil
}

func (f *FakeProvider) Close() error { return nil }

var _ Provider = (*FakeProvider)(nil)
