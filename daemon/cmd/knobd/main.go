// Command knobd is the background daemon: it owns the MIDI connection,
// the audio and focus backends, and the mapping engine, and serves the
// local API the configuration UI talks to. See ../../../specs/README.md
// for the milestone plan. Bare `knobd` runs the daemon; `knobd monitor`
// (M02) prints decoded MIDI events, `knobd monitor-audio` (M03) prints
// the live PipeWire sink/source/stream graph and its change events,
// `knobd monitor-focus` (M06) prints focus changes as KWin reports
// them, and `knobd calibrate-leds` (M05) sends one raw LED MIDI message
// and exits — all four without the rest of the daemon.
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

	"github.com/njeske/knobd/internal/actions"
	"github.com/njeske/knobd/internal/api"
	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/config"
	"github.com/njeske/knobd/internal/device"
	"github.com/njeske/knobd/internal/engine"
	"github.com/njeske/knobd/internal/focus"
	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
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
	case "monitor-focus":
		err = runMonitorFocus(args)
	case "calibrate-leds":
		err = runCalibrateLEDs(args)
	default:
		err = fmt.Errorf("unknown subcommand %q (known subcommands: monitor, monitor-audio, monitor-focus, calibrate-leds)", cmd)
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
	socketPath := fs.String("socket", "", "path to the local API unix socket (default: $XDG_RUNTIME_DIR/knobd.sock)")
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

	sock := *socketPath
	if sock == "" {
		p, err := api.SocketPath()
		if err != nil {
			return fmt.Errorf("resolve socket path: %w", err)
		}
		sock = p
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// midi.Supervisor and audio.Supervisor both start connecting in the
	// background immediately and never block waiting for a device or
	// PipeWire to actually be there -- that's deliberate. No physical
	// X-Touch Mini plugged in, or pipewire-pulse not running, is not a
	// startup failure: the supervisors' whole contract is
	// discover-wait-reconnect, PUT /config and GET /state must work with
	// nothing attached (so M07's UI is usable before you plug anything
	// in), and treating either as fatal would turn "controller unplugged"
	// into a Restart=on-failure crash loop under systemd.
	midiSup := midi.NewSupervisor(midi.SupervisorOptions{Logger: logger})
	defer midiSup.Close()

	audioSup := audio.NewSupervisor(audio.SupervisorOptions{Logger: logger})
	defer audioSup.Close()

	codec := device.NewXTouchMiniCodec()

	focusProv := focus.Unavailable()
	focusAvailable := false
	// KNOBD_KWIN_SCRIPT lets the embedded script be overridden with one
	// loaded from disk verbatim, for iterating on
	// daemon/internal/focus/script/knobd-focus.js without rebuilding.
	if p, ferr := focus.New(ctx, focus.Options{Logger: logger, ScriptPath: os.Getenv("KNOBD_KWIN_SCRIPT")}); ferr != nil {
		logger.Warn("focus tracking unavailable; 'focused' targets and knob.assign_focused_app will not resolve", "err", ferr)
	} else {
		focusProv = p
		focusAvailable = true
		defer focusProv.Close()
	}

	registry := actions.NewRegistry()
	// eng is assigned below, after Deps needs volumeHandlers -- OnApplied
	// closes over the variable itself (not its value at closure-creation
	// time), and is never actually called until well after eng is set,
	// once Engine.Run is consuming its own dispatcher's writes.
	var eng *engine.Engine
	volumeHandlers := actions.NewVolumeHandlers(audioSup, actions.VolumeOptions{
		Logger: logger,
		OnApplied: func(audio.Ref, audio.VolumeState) {
			if eng != nil {
				eng.NotifyLEDDirty()
			}
		},
	})
	volumeHandlers.Register(registry)

	eng = engine.New(engine.Deps{
		Port:     midiSup,
		Codec:    codec,
		Audio:    audioSup,
		Focus:    focusProv,
		Config:   cfg,
		Registry: registry,
		Logger:   logger,
		Observer: volumeHandlers,
	})

	store := newConfigStore(path, cfg, eng, logger)

	assignHandlers := actions.NewAssignHandlers(store, focusProv, actions.AssignOptions{
		Logger: logger,
		OnAssigned: func(control model.Control, matcher model.AppMatcher) {
			if eng != nil {
				eng.FlashControl(control, engine.DefaultFlashDuration)
			}
		},
	})
	// Must run before the eng.Run goroutine below starts: Registry's map
	// isn't safe to mutate concurrently with Execute, and every handler
	// in this codebase is registered once at startup for that reason
	// (see volumeHandlers.Register above and actions.Registry's doc
	// comment).
	assignHandlers.Register(registry)

	status := &connStatus{}
	state := &daemonState{eng: eng, status: status, focusAvailable: focusAvailable}
	srv := api.New(api.Options{Config: store, State: state, Logger: logger})

	logger.Info("serving the local API", "socket", sock)

	// SIGHUP reloads config.json from disk -- for a hand edit, as
	// opposed to PUT /config's live path into the running engine (both
	// end up going through configStore.SetConfig). See
	// specs/milestones/M04-mapping-engine-daemon.md's Design section for
	// why this is SIGHUP-and-not-inotify.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	type exit struct {
		name string
		err  error
	}
	// Three components race to exit first; whichever does cancels the
	// other two. Hand-rolled rather than golang.org/x/sync/errgroup: it's
	// ~20 lines, it would be this binary's third direct dependency in a
	// project whose ADR 0001 makes the single-static-binary property
	// explicit, and errgroup discards every error after the first, when
	// the second/third component's exit reason is worth logging too.
	exits := make(chan exit, 3)
	go func() { exits <- exit{"engine", eng.Run(runCtx)} }()
	go func() { exits <- exit{"api", srv.ListenAndServe(runCtx, sock)} }()
	go func() {
		watchConnections(runCtx, midiSup, audioSup, status, eng, logger)
		exits <- exit{"connections", nil}
	}()
	go func() {
		for {
			select {
			case <-runCtx.Done():
				return
			case <-hup:
				reloaded, lerr := config.Load(path)
				if lerr != nil {
					logger.Error("SIGHUP reload: load config failed", "path", path, "err", lerr)
					continue
				}
				if serr := store.SetConfig(runCtx, reloaded); serr != nil {
					logger.Error("SIGHUP reload: apply config failed", "err", serr)
					continue
				}
				logger.Info("reloaded config from disk", "path", path)
			}
		}
	}()

	first := <-exits
	cancelRun()
	var fatal error
	if first.err != nil {
		fatal = fmt.Errorf("%s: %w", first.name, first.err)
	}
	for i := 0; i < 2; i++ {
		e := <-exits
		if e.err != nil {
			logger.Error("component stopped", "component", e.name, "err", e.err)
			if fatal == nil {
				fatal = fmt.Errorf("%s: %w", e.name, e.err)
			}
		}
	}

	logger.Info("shutting down")
	return fatal
}
