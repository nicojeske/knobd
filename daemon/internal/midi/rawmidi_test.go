package midi

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// pipePort builds a *realPort over an os.Pipe(), driving the exact same
// reader-goroutine/close/deadline machinery Open uses against the real
// character device — a pipe is pollable with the same
// Close-unblocks-Read semantics (see port.go's package doc comment) —
// so this exercises real code with no hardware. It returns the port and
// the write end the test uses to feed it raw bytes.
func pipePort(t *testing.T) (*realPort, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return newPort(r), w
}

func ctxTimeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestPortReadDelivers(t *testing.T) {
	p, w := pipePort(t)
	defer p.Close()

	if _, err := w.Write([]byte{0x90, 0x28, 0x7F}); err != nil {
		t.Fatalf("write to pipe: %v", err)
	}

	msg, err := p.Read(ctxTimeout(t))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if msg.Status != 0x90 || msg.Data1 != 0x28 || msg.Data2 != 0x7F {
		t.Errorf("Read() = %+v, want {Status:0x90 Data1:0x28 Data2:0x7F}", msg)
	}
}

func TestPortReadContextCancel(t *testing.T) {
	p, _ := pipePort(t)
	defer p.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Read(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Read() error = %v, want context.Canceled", err)
	}
}

func TestPortCloseUnblocksRead(t *testing.T) {
	p, _ := pipePort(t)

	done := make(chan error, 1)
	go func() {
		_, err := p.Read(context.Background())
		done <- err
	}()

	// Give Read a moment to actually be blocked before closing, so this
	// test would fail (by timing out) rather than pass trivially if
	// Close ever stopped unblocking a pending Read.
	time.Sleep(20 * time.Millisecond)
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, ErrPortClosed) {
			t.Errorf("Read() error = %v, want ErrPortClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not unblock a pending Read")
	}
}

func TestPortCloseIsIdempotent(t *testing.T) {
	p, _ := pipePort(t)
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestPortReadMapsEOFToDeviceGone(t *testing.T) {
	p, w := pipePort(t)
	defer p.Close()

	// Closing the write end (rather than calling p.Close()) simulates
	// the device disappearing out from under an open port, not a
	// deliberate Close — Supervisor treats these very differently.
	if err := w.Close(); err != nil {
		t.Fatalf("close write end: %v", err)
	}

	_, err := p.Read(ctxTimeout(t))
	if err == nil {
		t.Fatal("expected an error after the peer closed")
	}
	if !errors.Is(err, ErrDeviceGone) {
		t.Errorf("Read() error = %v, want ErrDeviceGone", err)
	}
	if errors.Is(err, ErrPortClosed) {
		t.Errorf("Read() error = %v, must NOT be ErrPortClosed (we didn't call Close)", err)
	}
}

func TestPortWriteSendsExactBytes(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	p := newPort(w)
	defer p.Close()
	defer r.Close()

	msg := Message{Status: 0xB0, Data1: 0x30, Data2: 0x05}
	if err := p.Write(ctxTimeout(t), msg); err != nil {
		t.Fatalf("Write: %v", err)
	}

	buf := make([]byte, 3)
	if _, err := r.Read(buf); err != nil {
		t.Fatalf("read back: %v", err)
	}
	want := []byte{0xB0, 0x30, 0x05}
	for i := range want {
		if buf[i] != want[i] {
			t.Errorf("byte %d = %#x, want %#x", i, buf[i], want[i])
		}
	}
}

func TestPortWriteConcurrentDoesNotInterleave(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	p := newPort(w)
	defer p.Close()
	defer r.Close()

	const n = 50
	done := make(chan struct{})
	for i := 0; i < n; i++ {
		go func(i byte) {
			p.Write(context.Background(), Message{Status: 0x90, Data1: i, Data2: 0x7F})
			done <- struct{}{}
		}(byte(i % 128))
	}
	for i := 0; i < n; i++ {
		<-done
	}
	w.Close()

	// Every 3-byte group read back must be an intact message (a valid
	// note-on with velocity 0x7F): if two concurrent Writes ever
	// interleaved at the byte level, some group would come out wrong.
	buf := make([]byte, n*3)
	total := 0
	for total < len(buf) {
		nRead, err := r.Read(buf[total:])
		if err != nil {
			break
		}
		total += nRead
	}
	if total != len(buf) {
		t.Fatalf("read %d bytes, want %d", total, len(buf))
	}
	for i := 0; i < n; i++ {
		status, _, vel := buf[i*3], buf[i*3+1], buf[i*3+2]
		if status != 0x90 || vel != 0x7F {
			t.Errorf("message %d = % x, want an intact 0x90 .. 0x7F note-on (writes interleaved)", i, buf[i*3:i*3+3])
		}
	}
}

func TestPortDropsOnOverflowWithoutBlocking(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	p := newPort(r)
	defer p.Close()
	defer w.Close()

	// Flood well past portInboxSize without ever calling Read, and
	// confirm the writer (standing in for the reader goroutine's kernel
	// source) is never stalled and the port ends up reporting drops
	// instead of wedging.
	total := (portInboxSize + 50) * 3
	buf := make([]byte, total)
	for i := 0; i < total; i += 3 {
		buf[i], buf[i+1], buf[i+2] = 0x90, 0x28, 0x7F
	}

	writeDone := make(chan error, 1)
	go func() {
		_, err := w.Write(buf)
		writeDone <- err
	}()

	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("write: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer blocked — reader goroutine must never block on a full inbox")
	}

	// Give the reader goroutine time to finish draining/parsing.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if p.Dropped() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if p.Dropped() == 0 {
		t.Error("Dropped() = 0, want > 0 after flooding well past the inbox capacity")
	}
}

func TestOpenRejectsNonCharDevice(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "not-a-device")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	_, err = Open(f.Name())
	if err == nil {
		t.Fatal("expected Open to reject a regular file")
	}
	if !errors.Is(err, ErrNotCharDevice) {
		t.Errorf("Open() error = %v, want ErrNotCharDevice", err)
	}
}

func TestOpenRejectsMissingPath(t *testing.T) {
	_, err := Open("/does/not/exist/midiXD0")
	if err == nil {
		t.Fatal("expected an error for a nonexistent path")
	}
}
