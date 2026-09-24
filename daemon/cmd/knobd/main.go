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

	"github.com/godbus/dbus/v5"

	"github.com/njeske/knobd/internal/actions"
	"github.com/njeske/knobd/internal/api"
	"github.com/njeske/knobd/internal/audio"
	"github.com/njeske/knobd/internal/config"
	"github.com/njeske/knobd/internal/device"
	"github.com/njeske/knobd/internal/engine"
	"github.com/njeske/knobd/internal/focus"
	"github.com/njeske/knobd/internal/media"
	"github.com/njeske/knobd/internal/midi"
	"github.com/njeske/knobd/internal/model"
	"github.com/njeske/knobd/internal/spotify"
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

	// LoadAndUpgrade, not Load: a config found at an old schema version
	// on startup should be migrated and written back to disk so the
	// upgrade happens once, not on every start (see M12's Design
	// section). The SIGHUP reload path below stays on Load, since a
	// hand-edited file shouldn't be silently rewritten by a reload.
	cfg, err := config.LoadAndUpgrade(path)
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
	// eng and hub are assigned below, after Deps needs them -- OnApplied/
	// OnStateChanged/OnInput close over the variables themselves (not
	// their values at closure-creation time), and none is actually
	// called until well after both are set, once Engine.Run and Hub.Run
	// are consuming their own inputs.
	var eng *engine.Engine
	var hub *api.Hub

	// media.New is best-effort in the same way focus.New is: no D-Bus
	// session bus (or, later, a hung tracker) must never turn into a
	// knobd startup failure -- see the comment above focus.New's call on
	// why no backend at startup is fatal. mediaAvailable feeds
	// api.MediaState.
	//
	// The session bus connection is dialed here, once, rather than
	// inside media.New itself, so the same connection can also back
	// mediaNotifier (org.freedesktop.Notifications lives on the same
	// bus) without media.Backend needing to expose its *dbus.Conn --
	// and, as of M10, also back spotify.NewSecretService (the Secret
	// Service API lives on the session bus too), via the sessionConn
	// var hoisted out of this if/else so it survives past this block.
	var sessionConn *dbus.Conn
	mediaBackend := media.Unavailable()
	mediaNotifier := media.UnavailableNotifier()
	mediaAvailable := false
	if conn, cerr := dbus.ConnectSessionBus(); cerr != nil {
		logger.Warn("session bus unavailable; media.*/spotify.* actions will not resolve", "err", cerr)
	} else {
		sessionConn = conn
		defer conn.Close()
		if b, merr := media.New(ctx, media.Options{Logger: logger, Conn: conn}); merr != nil {
			logger.Warn("media transport unavailable; media.* actions will not resolve", "err", merr)
		} else {
			mediaBackend = b
			mediaNotifier = media.NewNotifier(conn)
			mediaAvailable = true
			defer b.Close()
		}
	}
	mediaTrk, terr := media.NewTracker(ctx, mediaBackend, media.TrackerOptions{
		Logger: logger,
		OnChange: func() {
			if hub != nil {
				hub.NotifyStateDirty()
			}
		},
	})
	if terr != nil {
		// media.NewTracker only fails if Watch itself errors, which
		// Unavailable()'s Backend never does -- kept as a hard error
		// rather than another Unavailable fallback since it would mean
		// a real bug in media.New's returned Backend.
		return fmt.Errorf("start media tracker: %w", terr)
	}

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
		OnStateChanged: func() {
			if hub != nil {
				hub.NotifyStateDirty()
			}
		},
		OnInput: func(ev device.Event) {
			if hub != nil {
				hub.NotifyLearnInput(learnInputFromEvent(ev))
			}
		},
	})

	store := newConfigStore(path, cfg, eng, func(rev uint64) {
		if hub != nil {
			hub.NotifyConfigChanged(rev)
		}
	}, logger)

	assignHandlers := actions.NewAssignHandlers(store, focusProv, actions.AssignOptions{
		Logger: logger,
		OnAssigned: func(control model.Control, matcher model.AppMatcher) {
			if eng != nil {
				eng.FlashControl(control, engine.DefaultFlashDuration)
			}
		},
	})
	sceneHandlers := actions.NewSceneHandlers(volumeHandlers, store, actions.SceneOptions{Logger: logger})
	mixHandlers := actions.NewMixHandlers(volumeHandlers, actions.MixOptions{Logger: logger})
	ignorePlayers := func() []string { return store.Config().Media.IgnorePlayers }
	mediaHandlers := actions.NewMediaHandlers(mediaTrk, mediaBackend, mediaNotifier, actions.MediaOptions{
		Logger:        logger,
		IgnorePlayers: ignorePlayers,
	})

	// spotify.Service is best-effort in the same way media.New is: no
	// session bus (or, later, a missing Client ID) must never turn into
	// a knobd startup failure -- see the comment above the media block.
	// It shares sessionConn with media/mediaNotifier rather than dialing
	// its own connection (org.freedesktop.secrets and
	// org.freedesktop.Notifications both live on the session bus too).
	spotifyClientID := func() string { return store.Config().Spotify.ClientID }
	spotifySecrets := spotify.UnavailableSecretStore()
	if sessionConn != nil {
		if ss, serr := spotify.NewSecretService(sessionConn); serr != nil {
			logger.Warn("spotify secret storage unavailable; spotify.* actions will not resolve", "err", serr)
		} else {
			spotifySecrets = ss
		}
	}
	// ctx (this function's own daemon-lifetime context, canceled on
	// SIGINT/SIGTERM) is passed here, not runCtx (constructed later) or
	// any HTTP request's r.Context() -- Login's OAuth flow must keep its
	// loopback listener up well after POST /spotify/login's own handler
	// returns, until the user completes the browser redirect. See
	// spotify.Service's ctx field's doc comment.
	spotifySvc := spotify.NewService(ctx, spotifyClientID, spotifySecrets, spotify.ServiceOptions{
		Logger: logger,
		OnChange: func() {
			if hub != nil {
				hub.NotifyStateDirty()
			}
		},
	})
	// Checks, off the startup path, whether a refresh token already
	// stored under the current Client ID is still good -- so a daemon
	// restart doesn't show "Connect" while a perfectly good token is
	// sitting in the Secret Service.
	spotifySvc.ValidateStoredToken()
	// Reuses mediaNotifier (org.freedesktop.Notifications, same session
	// bus) for like_toggle/add_to_playlist/remove_from_playlist's result
	// notifications -- no reason for a second Notifier implementation.
	spotifyHandlers := actions.NewSpotifyHandlers(spotifySvc.Client(), mediaNotifier, actions.SpotifyOptions{Logger: logger})

	// Must run before the eng.Run goroutine below starts: Registry's map
	// isn't safe to mutate concurrently with Execute, and every handler
	// in this codebase is registered once at startup for that reason
	// (see volumeHandlers.Register above and actions.Registry's doc
	// comment).
	assignHandlers.Register(registry)
	sceneHandlers.Register(registry)
	mixHandlers.Register(registry)
	mediaHandlers.Register(registry)
	spotifyHandlers.Register(registry)

	status := &connStatus{}
	state := &daemonState{
		eng:            eng,
		status:         status,
		focusAvailable: focusAvailable,
		media:          mediaTrk,
		mediaAvailable: mediaAvailable,
		mediaIgnore:    ignorePlayers,
		spotify:        spotifySvc,
	}
	// hub was forward-declared above so eng.Deps.OnStateChanged/OnInput
	// could close over it; assign it now that state (its StateProvider)
	// exists. See specs/adr/0004-ipc-over-unix-socket.md's Update (M07)
	// for why this is SSE rather than WebSocket.
	hub = api.NewHub(api.HubOptions{
		State: state,
		// tauri://localhost is the packaged UI's origin; the
		// http://localhost:1420 entries are Vite's dev server. Requests
		// with no Origin header at all (in particular the Rust bridge,
		// this API's only real client) are always allowed regardless --
		// see HubOptions.AllowedOrigins' doc comment.
		AllowedOrigins: []string{"tauri://localhost", "http://localhost:1420"},
		Logger:         logger,
	})
	audioGraph := newAudioGraph(audioSup, store)
	capabilities := newCapabilitiesProvider(registry)
	learnCtl := newLearnController(eng)
	spotifyProv := newSpotifyProvider(spotifySvc, logger)
	srv := api.New(api.Options{
		Config:       store,
		State:        state,
		Audio:        audioGraph,
		Capabilities: capabilities,
		Learn:        learnCtl,
		Spotify:      spotifyProv,
		Events:       hub,
		Logger:       logger,
	})

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
	// Four components race to exit first; whichever does cancels the
	// rest. Hand-rolled rather than golang.org/x/sync/errgroup: it's
	// ~20 lines, it would be this binary's third direct dependency in a
	// project whose ADR 0001 makes the single-static-binary property
	// explicit, and errgroup discards every error after the first, when
	// another component's exit reason is worth logging too.
	exits := make(chan exit, 4)
	go func() { exits <- exit{"engine", eng.Run(runCtx)} }()
	go func() { exits <- exit{"api", srv.ListenAndServe(runCtx, sock)} }()
	go func() { exits <- exit{"events", hub.Run(runCtx)} }()
	// spotifyHandlers.Run never returns an error worth racing the other
	// four goroutines over (see its own doc comment) -- it just stops
	// when runCtx is canceled, so it's started here but left out of the
	// exits race.
	go spotifyHandlers.Run(runCtx)
	go func() {
		watchConnections(runCtx, midiSup, audioSup, status, eng, hub.NotifyStateDirty, logger)
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
	for i := 0; i < 3; i++ {
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
