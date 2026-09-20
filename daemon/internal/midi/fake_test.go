package midi

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestFakePortReadWriteRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	p := NewFakePort(1)

	want := Message{Status: 0x90, Data1: 32, Data2: 127, Time: time.Now()}
	if err := p.Inject(ctx, want); err != nil {
		t.Fatalf("Inject: %v", err)
	}

	got, err := p.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != want {
		t.Errorf("Read() = %+v, want %+v", got, want)
	}

	reply := Message{Status: 0x90, Data1: 32, Data2: 1}
	if err := p.Write(ctx, reply); err != nil {
		t.Fatalf("Write: %v", err)
	}
	written := p.Written()
	if len(written) != 1 || written[0] != reply {
		t.Errorf("Written() = %+v, want [%+v]", written, reply)
	}
}

func TestFakePortReadRespectsContextCancellation(t *testing.T) {
	p := NewFakePort(0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := p.Read(ctx); err == nil {
		t.Fatal("expected Read to return an error for an already-canceled context")
	}
}

func TestFakePortCloseIsIdempotentAndUnblocksRead(t *testing.T) {
	p := NewFakePort(0)
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := p.Read(ctx); err == nil {
		t.Fatal("expected Read on a closed port to return an error")
	}
}

func TestFakePortRejectsAfterClose(t *testing.T) {
	ctx := context.Background()
	p := NewFakePort(1)
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := p.Inject(ctx, Message{}); err == nil {
		t.Error("expected Inject after Close to error")
	}
	if err := p.Write(ctx, Message{}); err == nil {
		t.Error("expected Write after Close to error")
	}
}

// TestFakePortConcurrentInjectAndClose is a regression test for a race
// where Inject checked p.closed, released the lock, and only then sent
// on p.inbox — a Close landing in that window used to close p.inbox out
// from under the send, panicking with "send on closed channel". Run
// with -race; the fix (closedCh is a separate, never-closed-twice signal
// and p.inbox itself is never closed) makes every interleaving safe.
func TestFakePortConcurrentInjectAndClose(t *testing.T) {
	for i := 0; i < 200; i++ {
		p := NewFakePort(0)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			p.Inject(ctx, Message{Status: 0x90})
		}()
		go func() {
			defer wg.Done()
			p.Close()
		}()
		wg.Wait()
	}
}
