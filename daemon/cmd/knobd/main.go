// Command knobd is the background daemon: it owns the MIDI connection,
// the audio and focus backends, and the mapping engine, and serves the
// local API the configuration UI talks to. See ../../../specs/README.md
// for the milestone plan; this entry point currently only wires up
// configuration loading and logging, since everything it would
// otherwise start (internal/midi, internal/audio, internal/focus,
// internal/engine, internal/api) is still scaffolding.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/njeske/knobd/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "knobd:", err)
		os.Exit(1)
	}
}

func run() error {
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, error")
	configPath := flag.String("config", "", "path to config.json (default: $XDG_CONFIG_HOME/knobd/config.json)")
	flag.Parse()

	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		return fmt.Errorf("invalid -log-level %q: %w", *logLevel, err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	path := *configPath
	if path == "" {
		p, err := config.Path()
		if err != nil {
			return fmt.Errorf("resolve config path: %w", err)
		}
		path = p
	}

	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger.Info("loaded config",
		"path", path,
		"schemaVersion", cfg.SchemaVersion,
		"activeProfile", cfg.ActiveProfileID,
		"profiles", len(cfg.Profiles),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// TODO(M02-M04): construct midi.Open, device.NewXTouchMiniCodec,
	// audio.New, focus.New, engine.New, api.New and run them until ctx
	// is canceled. Until then, knobd starts, loads its config, and
	// exits cleanly on SIGINT/SIGTERM — enough to prove the foundations
	// (config load/save/migrate, logging, module layout) actually work
	// end to end.
	logger.Info("knobd scaffold running; press Ctrl+C to exit (no MIDI/audio/focus backends wired up yet, see specs/milestones)")
	<-ctx.Done()
	logger.Info("shutting down")
	return nil
}
