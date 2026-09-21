// Command knobd is the background daemon: it owns the MIDI connection,
// the audio and focus backends, and the mapping engine, and serves the
// local API the configuration UI talks to. See ../../../specs/README.md
// for the milestone plan. Bare `knobd` runs the daemon; `knobd monitor`
// (M02) prints decoded MIDI events, and `knobd monitor-audio` (M03)
// prints the live PipeWire sink/source/stream graph and its change
// events — both without the rest of the daemon.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/njeske/knobd/internal/config"
)

func main() {
	cmd, args := parseArgs(os.Args[1:])

	var err error
	switch cmd {
	case "":
		err = runDaemon(args)
	case "monitor":
		err = runMonitor(args)
	case "monitor-audio":
		err = runMonitorAudio(args)
	default:
		err = fmt.Errorf("unknown subcommand %q (known subcommands: monitor, monitor-audio)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "knobd:", err)
		os.Exit(1)
	}
}

// parseArgs splits argv (excluding the program name) into a subcommand
// and its remaining arguments. The daemon itself takes no positional
// arguments, only flags, so anything starting with "-" is never a
// subcommand — this lets `knobd --log-level debug` keep working exactly
// as before subcommands existed, while `knobd monitor --raw` dispatches.
func parseArgs(argv []string) (cmd string, rest []string) {
	if len(argv) == 0 || strings.HasPrefix(argv[0], "-") {
		return "", argv
	}
	return argv[0], argv[1:]
}

func runDaemon(args []string) error {
	fs := flag.NewFlagSet("knobd", flag.ContinueOnError)
	logLevel := fs.String("log-level", "info", "log level: debug, info, warn, error")
	configPath := fs.String("config", "", "path to config.json (default: $XDG_CONFIG_HOME/knobd/config.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}

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

	// TODO(M03-M04): construct audio.New, focus.New, engine.New, api.New
	// and run them until ctx is canceled, using midi.NewSupervisor and
	// device.NewXTouchMiniCodec (M02, see monitor.go) as the input side.
	// Until then, knobd starts, loads its config, and exits cleanly on
	// SIGINT/SIGTERM — enough to prove the foundations (config
	// load/save/migrate, logging, module layout) actually work
	// end to end.
	logger.Info("knobd scaffold running; press Ctrl+C to exit (no audio/focus/engine backends wired up yet, see specs/milestones)")
	<-ctx.Done()
	logger.Info("shutting down")
	return nil
}
