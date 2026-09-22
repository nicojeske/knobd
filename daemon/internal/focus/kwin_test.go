package focus

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/njeske/knobd/internal/proctree"
)

// newTestKWinProvider builds a kwinProvider with no D-Bus connection at
// all -- FocusChanged, Watch, Current, and Close are all plain Go code
// with no bus access (see the type's doc comment in kwin.go), so this
// is enough to exercise the entire cache/fan-out path. procRoot lets a
// test fabricate /proc/<pid>/exe for the Binary-annotation case.
func newTestKWinProvider(procRoot string) *kwinProvider {
	return &kwinProvider{
		log:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1})), // discard
		proc: proctree.Walker{Root: procRoot},
		done: make(chan struct{}),
	}
}

func focusChangedPayload(t *testing.T, ev scriptEvent) string {
	t.Helper()
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal scriptEvent: %v", err)
	}
	return string(data)
}

func TestFocusChangedUpdatesCurrent(t *testing.T) {
	p := newTestKWinProvider("")
	payload := focusChangedPayload(t, scriptEvent{Normal: true, ResourceClass: "konsole", PID: 100})

	if dbErr := p.FocusChanged(payload); dbErr != nil {
		t.Fatalf("FocusChanged: %v", dbErr)
	}

	got, err := p.Current(context.Background())
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if got.ResourceClass != "konsole" || got.PID != 100 {
		t.Fatalf("Current() = %+v, want ResourceClass=konsole PID=100", got)
	}
}

func TestFocusChangedRejectedEventDoesNotUpdateCurrent(t *testing.T) {
	p := newTestKWinProvider("")
	p.publish(AppInfo{ResourceClass: "konsole"})

	// A non-normal window (a panel) must not overwrite the last real
	// focused app.
	payload := focusChangedPayload(t, scriptEvent{Normal: false, ResourceClass: "plasmashell"})
	if dbErr := p.FocusChanged(payload); dbErr != nil {
		t.Fatalf("FocusChanged: %v", dbErr)
	}

	got, _ := p.Current(context.Background())
	if got.ResourceClass != "konsole" {
		t.Fatalf("Current() = %+v, want the previous konsole value to be preserved", got)
	}
}

func TestFocusChangedMalformedPayloadReturnsError(t *testing.T) {
	p := newTestKWinProvider("")
	if dbErr := p.FocusChanged("{not json"); dbErr == nil {
		t.Fatal("FocusChanged with malformed JSON should have returned a *dbus.Error")
	}
}

func TestFocusChangedAnnotatesBinaryFromProcRoot(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "3172")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/opt/brave-bin/brave", filepath.Join(dir, "exe")); err != nil {
		t.Fatal(err)
	}

	p := newTestKWinProvider(root)
	payload := focusChangedPayload(t, scriptEvent{Normal: true, ResourceClass: "brave-browser", PID: 3172})
	if dbErr := p.FocusChanged(payload); dbErr != nil {
		t.Fatalf("FocusChanged: %v", dbErr)
	}

	got, _ := p.Current(context.Background())
	if got.Binary != "brave" {
		t.Fatalf("Current().Binary = %q, want %q", got.Binary, "brave")
	}
}

func TestWatchReceivesEveryUpdate(t *testing.T) {
	p := newTestKWinProvider("")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := p.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	p.publish(AppInfo{ResourceClass: "konsole"})
	select {
	case info := <-ch:
		if info.ResourceClass != "konsole" {
			t.Fatalf("Watch delivered %+v, want ResourceClass=konsole", info)
		}
	case <-time.After(time.Second):
		t.Fatal("Watch did not deliver the published update")
	}
}

func TestWatchMultipleSubscribersAllReceive(t *testing.T) {
	p := newTestKWinProvider("")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch1, _ := p.Watch(ctx)
	ch2, _ := p.Watch(ctx)

	p.publish(AppInfo{ResourceClass: "dolphin"})

	for i, ch := range []<-chan AppInfo{ch1, ch2} {
		select {
		case info := <-ch:
			if info.ResourceClass != "dolphin" {
				t.Fatalf("subscriber %d received %+v, want ResourceClass=dolphin", i, info)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d did not receive the update", i)
		}
	}
}

func TestWatchDropsOldestRatherThanBlocking(t *testing.T) {
	p := newTestKWinProvider("")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, _ := p.Watch(ctx)

	// Fill the channel's buffer (capacity 8) and then some, without
	// ever reading from it -- publish must never block on a stalled
	// subscriber.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 20; i++ {
			p.publish(AppInfo{ResourceClass: "app", Caption: string(rune('a' + i))})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked instead of dropping the oldest queued value")
	}

	// The channel should hold the most recent values, ending with the
	// very last one published.
	var last AppInfo
	for {
		select {
		case info := <-ch:
			last = info
			continue
		default:
		}
		break
	}
	if last.Caption != string(rune('a'+19)) {
		t.Fatalf("last value drained = %+v, want the final published caption", last)
	}
}

func TestWatchDeregistersOnContextCancel(t *testing.T) {
	p := newTestKWinProvider("")
	ctx, cancel := context.WithCancel(context.Background())

	ch, _ := p.Watch(ctx)
	cancel()

	select {
	case _, open := <-ch:
		if open {
			t.Fatal("expected the Watch channel to be closed after ctx cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("Watch channel was not closed after ctx cancel")
	}

	// The provider must no longer hold a reference to it either.
	p.subsMu.Lock()
	n := len(p.subs)
	p.subsMu.Unlock()
	if n != 0 {
		t.Fatalf("provider still holds %d subscriber(s) after cancel", n)
	}
}

func TestCloseIsIdempotentWithNoConnAndNoSupervise(t *testing.T) {
	p := newTestKWinProvider("")
	if err := p.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestCloseStopsSupervise(t *testing.T) {
	p := newTestKWinProvider("")
	runCtx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	go func() {
		<-runCtx.Done()
		close(p.done)
	}()

	done := make(chan struct{})
	go func() {
		p.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not return after canceling the supervise goroutine")
	}
}
