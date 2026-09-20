package midi

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"
)

// watchDebounce coalesces a burst of filesystem events into one tick.
// This machine has 6 sound cards, so plugging in any USB audio device —
// not just the X-Touch Mini — touches /dev/snd/by-id; debouncing avoids
// hammering Discover on every individual inotify event from that churn.
const watchDebounce = 150 * time.Millisecond

// watch emits on the returned channel whenever dir changes in a way that
// might mean the X-Touch Mini appeared or disappeared. The channel is
// closed when ctx is canceled or the watch otherwise ends. This is a
// hint to rescan, not a reliable delivery guarantee for any specific
// event — Supervisor also polls periodically as a backstop.
func watch(ctx context.Context, dir string) (<-chan struct{}, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("midi: inotify_init1: %w", err)
	}
	// os.NewFile returns a pollable *os.File because the fd was created
	// with IN_NONBLOCK (see os.NewFile's doc comment) — so Close below
	// cleanly unblocks the reader goroutine's blocked Read, the same
	// Close-unblocks-Read property rawmidi.go relies on for the device
	// itself.
	f := os.NewFile(uintptr(fd), "inotify")

	const mask = syscall.IN_CREATE | syscall.IN_DELETE | syscall.IN_MOVED_TO | syscall.IN_MOVED_FROM
	if _, err := syscall.InotifyAddWatch(fd, dir, mask); err != nil {
		f.Close()
		return nil, fmt.Errorf("midi: inotify_add_watch %s: %w", dir, err)
	}

	// raw fires once per underlying read (i.e. per burst of kernel
	// events, not per individual inotify_event in that burst — we don't
	// care which file changed, only that something in dir did).
	raw := make(chan struct{})
	go func() {
		defer close(raw)
		buf := make([]byte, syscall.SizeofInotifyEvent+64)
		for {
			if _, err := f.Read(buf); err != nil {
				return
			}
			select {
			case raw <- struct{}{}:
			case <-ctx.Done():
				return
			}
		}
	}()

	out := make(chan struct{}, 1)
	go func() {
		defer f.Close() // unblocks the reader goroutine above on exit
		defer close(out)
		var debounce <-chan time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-raw:
				if !ok {
					return
				}
				debounce = time.After(watchDebounce)
			case <-debounce:
				debounce = nil
				select {
				case out <- struct{}{}:
				default:
				}
			}
		}
	}()

	return out, nil
}
