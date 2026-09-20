package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/njeske/knobd/internal/device"
	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
)

// runMonitor implements `knobd monitor`: it discovers and connects to
// the X-Touch Mini via a midi.Supervisor (so it survives an unplug/
// replug — see specs/milestones/M02-midi-transport.md's acceptance
// criteria), decodes every message with device.NewXTouchMiniCodec, and
// prints one line per event. It does not act on events at all — that's
// M04's engine.
func runMonitor(args []string) error {
	fs := flag.NewFlagSet("monitor", flag.ContinueOnError)
	raw := fs.Bool("raw", false, "also print the raw MIDI bytes for each event")
	devicePath := fs.String("device", "", "open this rawmidi path directly instead of discovering one")
	logLevel := fs.String("log-level", "warn", "log level for the underlying connection: debug, info, warn, error")
	if err := fs.Parse(args); err != nil {
		return err
	}

	level := slog.LevelWarn
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		return fmt.Errorf("invalid -log-level %q: %w", *logLevel, err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	opts := midi.SupervisorOptions{Logger: logger}
	if *devicePath != "" {
		path := *devicePath
		opts.Discover = func() ([]midi.DeviceInfo, error) {
			return []midi.DeviceInfo{{Name: path, Path: path}}, nil
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sup := midi.NewSupervisor(opts)
	defer sup.Close()

	fmt.Println("waiting for the X-Touch Mini... (Ctrl+C to quit)")

	type readResult struct {
		msg midi.Message
		err error
	}
	msgs := make(chan readResult)
	go func() {
		for {
			msg, err := sup.Read(ctx)
			select {
			case msgs <- readResult{msg, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	codec := device.NewXTouchMiniCodec()
	// downAt tracks when each control last went down, purely for
	// monitor's own "(held Xms)" display annotation — this is NOT
	// gesture detection (press vs. hold, double-press): that state
	// machine belongs to engine (M04), which is explicitly out of scope
	// here (see M02's Scope section). This map only ever informs a
	// string printed after the fact, once a release has already
	// happened.
	downAt := make(map[model.Control]time.Time)

	for {
		select {
		case <-ctx.Done():
			return nil

		case info := <-sup.Connected():
			fmt.Printf("-- connected: %s (%s)\n", info.Name, info.Path)

		case err := <-sup.Disconnected():
			fmt.Printf("-- disconnected: %v\n", err)

		case res := <-msgs:
			if res.err != nil {
				if errors.Is(res.err, context.Canceled) || errors.Is(res.err, midi.ErrPortClosed) {
					return nil
				}
				return res.err
			}
			printEvent(codec, res.msg, downAt, *raw)
		}
	}
}

func printEvent(codec device.Codec, msg midi.Message, downAt map[model.Control]time.Time, raw bool) {
	if raw {
		fmt.Printf("   raw: %02X %02X %02X\n", msg.Status, msg.Data1, msg.Data2)
	}

	ev, ok, err := codec.Decode(msg)
	if err != nil {
		// Neither ErrStandardMode nor any other Decode error is fatal to
		// this loop — see Codec.Decode's doc comment — but it's worth
		// surfacing since it usually means the physical mode switch is
		// wrong.
		fmt.Printf("-- decode error: %v\n", err)
		return
	}
	if !ok {
		return
	}

	line := formatEvent(ev)
	switch ev.Kind {
	case device.EventButtonDown:
		downAt[ev.Control] = ev.Time
	case device.EventButtonUp:
		if start, ok := downAt[ev.Control]; ok {
			delete(downAt, ev.Control)
			line += fmt.Sprintf("  (held %v)", ev.Time.Sub(start).Round(time.Millisecond))
		}
	}
	fmt.Println(line)
}

func formatEvent(ev device.Event) string {
	name := controlName(ev.Control)
	switch ev.Kind {
	case device.EventTurn:
		return fmt.Sprintf("%-16s turn    %+d", name, ev.Delta)
	case device.EventButtonDown:
		return fmt.Sprintf("%-16s down", name)
	case device.EventButtonUp:
		return fmt.Sprintf("%-16s up", name)
	case device.EventFaderMove:
		return fmt.Sprintf("%-16s move    %d", name, ev.Value)
	default:
		return fmt.Sprintf("%-16s %s", name, ev.Kind)
	}
}

func controlName(c model.Control) string {
	return fmt.Sprintf("%s %d", strings.ReplaceAll(string(c.Kind), "_", " "), c.Index)
}
