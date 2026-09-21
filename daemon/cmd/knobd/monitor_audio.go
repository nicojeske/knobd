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

	"github.com/njeske/knobd/internal/audio"
)

// runMonitorAudio implements `knobd monitor-audio`: it connects to
// pipewire-pulse via an audio.Supervisor (so it survives PipeWire
// restarting mid-session — see specs/milestones/M03-audio-control.md's
// acceptance criteria), and either dumps the current sinks/sources/
// streams once (-once) or streams live change events (the default). It
// does not act on events at all, same as `knobd monitor` for MIDI — that
// remains M04's engine.
func runMonitorAudio(args []string) error {
	fs := flag.NewFlagSet("monitor-audio", flag.ContinueOnError)
	once := fs.Bool("once", false, "dump current sinks/sources/streams and exit, instead of watching for changes")
	logLevel := fs.String("log-level", "warn", "log level for the underlying connection: debug, info, warn, error")
	if err := fs.Parse(args); err != nil {
		return err
	}

	level := slog.LevelWarn
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		return fmt.Errorf("invalid -log-level %q: %w", *logLevel, err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sup := audio.NewSupervisor(audio.SupervisorOptions{Logger: logger})
	defer sup.Close()

	if *once {
		return dumpOnce(ctx, sup)
	}
	return watchAudio(ctx, sup)
}

func dumpOnce(ctx context.Context, sup *audio.Supervisor) error {
	sinks, err := sup.Sinks(ctx)
	if err != nil {
		return fmt.Errorf("Sinks: %w", err)
	}
	sources, err := sup.Sources(ctx)
	if err != nil {
		return fmt.Errorf("Sources: %w", err)
	}
	streams, err := sup.Streams(ctx)
	if err != nil {
		return fmt.Errorf("Streams: %w", err)
	}

	fmt.Printf("%d sinks:\n", len(sinks))
	for _, d := range sinks {
		fmt.Println("  " + formatDevice(d))
	}
	fmt.Printf("%d sources:\n", len(sources))
	for _, d := range sources {
		fmt.Println("  " + formatDevice(d))
	}
	fmt.Printf("%d streams:\n", len(streams))
	for _, s := range streams {
		fmt.Println("  " + formatStream(s))
	}
	return nil
}

func watchAudio(ctx context.Context, sup *audio.Supervisor) error {
	fmt.Println("waiting for pipewire-pulse... (Ctrl+C to quit)")

	events, err := sup.Subscribe(ctx)
	if err != nil {
		return fmt.Errorf("Subscribe: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-sup.Connected():
			fmt.Println("-- connected")

		case err := <-sup.Disconnected():
			fmt.Printf("-- disconnected: %v\n", err)

		case ev, ok := <-events:
			if !ok {
				if errors.Is(ctx.Err(), context.Canceled) {
					return nil
				}
				return fmt.Errorf("audio event stream closed unexpectedly")
			}
			fmt.Println(formatAudioEvent(ev))
		}
	}
}

// formatAudioEvent is a pure function so it can be table-tested without
// a live connection — see monitor_audio_test.go.
func formatAudioEvent(ev audio.Event) string {
	switch ev.Kind {
	case audio.EventStreamChanged:
		line := fmt.Sprintf("stream %s changed  %s", refString(ev.Stream), formatStreamProps(ev.Stream))
		if ev.State != nil {
			line += fmt.Sprintf("  vol=%.0f%% muted=%v", ev.State.Percent, ev.State.Muted)
		}
		return line
	case audio.EventStreamRemoved:
		return fmt.Sprintf("stream %s removed  %s", refString(ev.Stream), formatStreamProps(ev.Stream))
	case audio.EventDeviceChanged:
		return fmt.Sprintf("device changed  %s", formatDevicePtr(ev.Device))
	case audio.EventDeviceRemoved:
		return fmt.Sprintf("device removed  %s", formatDevicePtr(ev.Device))
	case audio.EventDefaultChanged:
		return "default sink/source changed"
	case audio.EventResync:
		return "-- resync: some events may have been missed, re-enumerating recommended"
	default:
		return fmt.Sprintf("unknown event kind %q", ev.Kind)
	}
}

func refString(s *audio.Stream) string {
	if s == nil {
		return "<nil>"
	}
	return s.ID
}

func formatDevicePtr(d *audio.Device) string {
	if d == nil {
		return "<nil>"
	}
	return formatDevice(*d)
}

func formatDevice(d audio.Device) string {
	def := ""
	if d.IsDefault {
		def = " (default)"
	}
	return fmt.Sprintf("%-40s %s%s", d.ID, d.Description, def)
}

func formatStream(s audio.Stream) string {
	return fmt.Sprintf("%-6s %-9s %s", s.ID, s.Direction, formatStreamProps(&s))
}

// formatStreamProps prints a fixed subset of Props, in a fixed order,
// rather than ranging over the map directly — map iteration order is
// randomized, and this output is table-tested (monitor_audio_test.go).
func formatStreamProps(s *audio.Stream) string {
	if s == nil {
		return ""
	}
	keys := []string{"application.name", "node.name", "media.name", "application.process.binary", "knobd.process.binary"}
	var parts []string
	for _, k := range keys {
		if v, ok := s.Props[k]; ok && v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, " ")
}
