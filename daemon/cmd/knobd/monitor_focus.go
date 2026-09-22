package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/njeske/knobd/internal/focus"
)

// runMonitorFocus implements `knobd monitor-focus`: it connects a real
// focus.Provider (loading and running the KWin script exactly as the
// daemon does) and prints every focus change as it arrives, until
// Ctrl+C. It does not act on events at all, same as `knobd monitor` for
// MIDI and `knobd monitor-audio` for PipeWire — those remain M04's
// engine's job.
//
// This is also the fastest way to confirm the whole KWin/D-Bus path is
// actually working on a given machine (see
// specs/milestones/M06-focus-tracking.md's Verification section):
// -log-level debug surfaces the "resolved via the process-tree fallback"
// note and the handshake-timeout warning that would otherwise only show
// up in the full daemon's log.
func runMonitorFocus(args []string) error {
	fs := flag.NewFlagSet("monitor-focus", flag.ContinueOnError)
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

	prov, err := focus.New(ctx, focus.Options{Logger: logger, ScriptPath: os.Getenv("KNOBD_KWIN_SCRIPT")})
	if err != nil {
		return fmt.Errorf("focus.New: %w", err)
	}
	defer prov.Close()

	if info, err := prov.Current(ctx); err == nil && (info.ResourceClass != "" || info.DesktopFileID != "") {
		fmt.Println("current:", formatFocusInfo(info))
	}

	events, err := prov.Watch(ctx)
	if err != nil {
		return fmt.Errorf("Watch: %w", err)
	}

	fmt.Println("watching for focus changes... (Ctrl+C to quit)")
	for {
		select {
		case <-ctx.Done():
			return nil
		case info, ok := <-events:
			if !ok {
				if errors.Is(ctx.Err(), context.Canceled) {
					return nil
				}
				return fmt.Errorf("focus event stream closed unexpectedly")
			}
			fmt.Printf("[%s] %s\n", time.Now().Format(time.RFC3339), formatFocusInfo(info))
		}
	}
}

// formatFocusInfo is a pure function so it can be table-tested without
// a live provider.
func formatFocusInfo(info focus.AppInfo) string {
	s := fmt.Sprintf("resourceClass=%q desktopFileId=%q pid=%d", info.ResourceClass, info.DesktopFileID, info.PID)
	if info.Binary != "" {
		s += fmt.Sprintf(" binary=%q", info.Binary)
	}
	if info.Caption != "" {
		s += fmt.Sprintf(" caption=%q", info.Caption)
	}
	return s
}
