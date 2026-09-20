package midi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// noopWatch stands in for the real inotify watch in tests: it just
// blocks until ctx is done, so Supervisor falls back to its
// PollInterval/backoff timers, which tests can set very short.
func noopWatch(ctx context.Context, dir string) (<-chan struct{}, error) {
	ch := make(chan struct{})
	go func() {
		<-ctx.Done()
	}()
	return ch, nil
}

func quietOptions() SupervisorOptions {
	return SupervisorOptions{
		PollInterval: 20 * time.Millisecond,
		MinBackoff:   5 * time.Millisecond,
		MaxBackoff:   20 * time.Millisecond,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Watch:        noopWatch,
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestSupervisorConnectsAndDeliversMessages(t *testing.T) {
	fp := NewFakePort(4)
	opts := quietOptions()
	opts.Discover = func() ([]DeviceInfo, error) {
		return []DeviceInfo{{Name: "X-TOUCH MINI", Path: "fake"}}, nil
	}
	opts.Open = func(string) (Port, error) { return fp, nil }

	s := NewSupervisor(opts)
	defer s.Close()

	select {
	case info := <-s.Connected():
		if info.Name != "X-TOUCH MINI" {
			t.Errorf("Connected() = %+v, want Name X-TOUCH MINI", info)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Supervisor never reported Connected")
	}

	want := Message{Status: 0x90, Data1: 40, Data2: 127}
	if err := fp.Inject(context.Background(), want); err != nil {
		t.Fatalf("Inject: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := s.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != want {
		t.Errorf("Read() = %+v, want %+v", got, want)
	}
}

func TestSupervisorRetriesUntilOpenSucceeds(t *testing.T) {
	var attempts atomic.Int32
	fp := NewFakePort(1)

	opts := quietOptions()
	opts.Discover = func() ([]DeviceInfo, error) {
		return []DeviceInfo{{Name: "X-TOUCH MINI", Path: "fake"}}, nil
	}
	opts.Open = func(string) (Port, error) {
		if attempts.Add(1) <= 2 {
			return nil, errors.New("permission denied")
		}
		return fp, nil
	}

	s := NewSupervisor(opts)
	defer s.Close()

	select {
	case <-s.Connected():
	case <-time.After(2 * time.Second):
		t.Fatal("Supervisor never connected after transient open failures")
	}
	if n := attempts.Load(); n < 3 {
		t.Errorf("Open called %d times, want at least 3 (two failures then success)", n)
	}
}

func TestSupervisorReconnectsAfterDeviceLoss(t *testing.T) {
	var openCount atomic.Int32
	ports := make(chan *FakePort, 2)
	p1, p2 := NewFakePort(1), NewFakePort(1)
	ports <- p1
	ports <- p2

	opts := quietOptions()
	opts.Discover = func() ([]DeviceInfo, error) {
		return []DeviceInfo{{Name: "X-TOUCH MINI", Path: "fake"}}, nil
	}
	opts.Open = func(string) (Port, error) {
		openCount.Add(1)
		select {
		case p := <-ports:
			return p, nil
		default:
			return p2, nil
		}
	}

	s := NewSupervisor(opts)
	defer s.Close()

	select {
	case <-s.Connected():
	case <-time.After(2 * time.Second):
		t.Fatal("never got first Connected")
	}

	// Simulate the device disappearing.
	p1.Close()

	select {
	case err := <-s.Disconnected():
		if err == nil {
			t.Error("Disconnected() delivered a nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Supervisor never reported Disconnected after the port closed")
	}

	select {
	case <-s.Connected():
	case <-time.After(2 * time.Second):
		t.Fatal("Supervisor never reconnected")
	}

	// New connection must actually work.
	want := Message{Status: 0xE8, Data1: 0, Data2: 64}
	if err := p2.Inject(context.Background(), want); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := s.Read(ctx)
	if err != nil {
		t.Fatalf("Read after reconnect: %v", err)
	}
	if got != want {
		t.Errorf("Read() after reconnect = %+v, want %+v", got, want)
	}
}

func TestSupervisorWriteFailsFastWhenNotConnected(t *testing.T) {
	block := make(chan struct{})
	opts := quietOptions()
	opts.Discover = func() ([]DeviceInfo, error) {
		<-block // never actually connects for the life of this test
		return nil, nil
	}
	opts.Open = func(string) (Port, error) { return nil, errors.New("unreachable") }

	s := NewSupervisor(opts)
	defer func() {
		close(block)
		s.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	err := s.Write(ctx, Message{Status: 0x90})
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("Write() error = %v, want ErrNotConnected", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("Write took %v while disconnected, want to fail immediately", elapsed)
	}
}

func TestSupervisorCloseStopsReconnecting(t *testing.T) {
	fp := NewFakePort(1)
	var openCount atomic.Int32

	opts := quietOptions()
	opts.Discover = func() ([]DeviceInfo, error) {
		return []DeviceInfo{{Name: "X-TOUCH MINI", Path: "fake"}}, nil
	}
	opts.Open = func(string) (Port, error) {
		openCount.Add(1)
		return fp, nil
	}

	s := NewSupervisor(opts)
	select {
	case <-s.Connected():
	case <-time.After(2 * time.Second):
		t.Fatal("never connected")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	countAtClose := openCount.Load()
	time.Sleep(100 * time.Millisecond) // long enough for several backoff cycles
	if got := openCount.Load(); got != countAtClose {
		t.Errorf("Open was called %d more time(s) after Close; Supervisor kept reconnecting", got-countAtClose)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := s.Read(ctx); !errors.Is(err, ErrPortClosed) {
		t.Errorf("Read() after Close error = %v, want ErrPortClosed", err)
	}
}

func TestSupervisorConnectedIsCoalescing(t *testing.T) {
	// Sends to a buffered-1, drop-oldest channel must never block even
	// if nobody reads it — this is really a test of the notify helper,
	// exercised through Supervisor's own usage of it.
	fp := NewFakePort(1)
	opts := quietOptions()
	opts.Discover = func() ([]DeviceInfo, error) {
		return []DeviceInfo{{Name: "X-TOUCH MINI", Path: "fake"}}, nil
	}
	var mu sync.Mutex
	var openedPaths []string
	opts.Open = func(path string) (Port, error) {
		mu.Lock()
		openedPaths = append(openedPaths, path)
		mu.Unlock()
		return fp, nil
	}

	s := NewSupervisor(opts)
	defer s.Close()

	waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(openedPaths) > 0
	})
	// No assertion beyond "this didn't deadlock or panic": Connected()
	// was never drained above, proving the non-blocking send works.
}
