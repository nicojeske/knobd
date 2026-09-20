package midi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchTicksOnCreate(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := watch(ctx, dir)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "usb-Behringer_X-TOUCH_MINI_1.0.1-00"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not tick after a file was created in the watched directory")
	}
}

func TestWatchTicksOnDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usb-Behringer_X-TOUCH_MINI_1.0.1-00")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := watch(ctx, dir)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not tick after a file was removed from the watched directory")
	}
}

func TestWatchStopsOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := watch(ctx, dir)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected the watch channel to close, not emit a value")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch channel did not close after context cancellation")
	}
}

func TestWatchErrorsOnMissingDir(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := watch(ctx, filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected an error watching a nonexistent directory")
	}
}
