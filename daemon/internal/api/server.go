// Package api exposes knobd's local control surface to the UI: an
// HTTP+WebSocket server over a unix domain socket
// ($XDG_RUNTIME_DIR/knobd.sock), documented as ADR 0004
// (specs/adr/0004-ipc-over-unix-socket.md). A unix socket rather than a
// TCP port means filesystem permissions do the authorization, so no
// bearer token is needed and no web page anywhere else can reach it.
//
// api depends on daemon/internal/model only, never on
// daemon/internal/engine or daemon/internal/config: the daemon
// (cmd/knobd) supplies a ConfigStore, a StateProvider and (as of M07) an
// AudioProvider and a LearnController -- small interfaces defined at
// their point of use here -- and does the translation to/from
// engine.Snapshot and config.Save itself.
//
// The route table (routes.go) is the single declaration of this
// package's surface: registerRoutes builds the mux from it, and
// daemon/internal/schema emits docs/openapi.json from the same table,
// so the two cannot drift apart.
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// SocketName is the socket's filename within $XDG_RUNTIME_DIR (or its
// fallback directory).
const SocketName = "knobd.sock"

// ShutdownGrace bounds how long Serve waits for in-flight requests after
// ctx is canceled before forcing the listener closed.
const ShutdownGrace = 5 * time.Second

// SocketPath returns the unix socket path the daemon listens on and the
// UI connects to: $XDG_RUNTIME_DIR/knobd.sock, falling back to
// $TMPDIR/knobd-<uid>.sock ($TMPDIR defaults to /tmp) if
// $XDG_RUNTIME_DIR is unset -- mirrors config.Path's XDG-fallback
// pattern (see daemon/internal/config.Path). The error return is
// currently always nil; it exists so a future fallback that can fail
// (the way config.Path's os.UserHomeDir call can) doesn't need a
// breaking signature change.
func SocketPath() (string, error) {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, SocketName), nil
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("knobd-%d.sock", os.Getuid())), nil
}

// Options configures New. Config and State are the daemon's adapters
// (see ConfigStore/StateProvider in handlers.go); either may be nil, in
// which case the routes that need it answer 503 rather than panicking,
// which lets a test construct a Server for just the half it cares about.
type Options struct {
	Config       ConfigStore
	State        StateProvider
	Audio        AudioProvider
	Capabilities CapabilitiesProvider
	Learn        LearnController
	// Events serves GET /events. Nil means that route answers 503,
	// exactly like a nil Config/State/Audio/Learn/Capabilities.
	Events *Hub
	// Logger receives request lifecycle logging. Nil means slog.Default().
	Logger *slog.Logger
}

// Server is knobd's local API surface. It implements http.Handler, so
// handler tests need no socket at all.
type Server struct {
	mux          *http.ServeMux
	config       ConfigStore
	state        StateProvider
	audio        AudioProvider
	capabilities CapabilitiesProvider
	learn        LearnController
	events       *Hub
	log          *slog.Logger
}

// New constructs a Server with its routes registered.
func New(opts Options) *Server {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		mux:          http.NewServeMux(),
		config:       opts.Config,
		state:        opts.State,
		audio:        opts.Audio,
		capabilities: opts.Capabilities,
		learn:        opts.Learn,
		events:       opts.Events,
		log:          log,
	}
	s.registerRoutes()
	return s
}

// handlers maps every Route's OperationID to the handler that serves it.
// registerRoutes panics if a Route has no entry here, or if an entry has
// no matching Route -- routes_test.go asserts that never happens, so the
// panic is a should-never-fire guard against the two falling out of
// sync, not expected user-facing behavior.
func (s *Server) handlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"getConfig":       s.handleGetConfig,
		"putConfig":       s.handlePutConfig,
		"getState":        s.handleGetState,
		"getAudio":        s.handleGetAudio,
		"getCapabilities": s.handleGetCapabilities,
		"startLearn":      s.handleStartLearn,
		"stopLearn":       s.handleStopLearn,
		"events":          s.handleEvents,
	}
}

func (s *Server) registerRoutes() {
	handlers := s.handlers()
	for _, route := range Routes() {
		h, ok := handlers[route.OperationID]
		if !ok {
			panic(fmt.Sprintf("api: no handler registered for route %s %s (operationId %q)", route.Method, route.Path, route.OperationID))
		}
		s.mux.HandleFunc(route.Method+" "+route.Path, h)
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// listen binds the unix socket at path, handling the two things a plain
// net.Listen("unix", path) gets wrong for a long-running daemon:
//
//   - A regular file (or anything else) already at path is never
//     unlinked -- only a genuine, provably-dead socket is. Silently
//     deleting whatever a user-supplied --socket path happens to point
//     at is not acceptable.
//   - A socket file left behind by a crashed daemon is distinguished
//     from one a live daemon is listening on by actually dialing it
//     (250ms timeout): a successful dial means another knobd already
//     owns it, which is reported as the single most useful error this
//     package can produce ("another knobd is already listening"); a
//     failed dial (ECONNREFUSED or similar) means it's a stale leftover,
//     safe to remove and rebind.
//
// This dial-then-unlink approach has a narrow race (two daemons starting
// within the same few hundred microseconds could both see "refused" and
// both rebind, last writer winning) that a flock on a sibling lock file
// would close. It is accepted for M04 because the failure it actually
// guards against -- a crashed daemon's leftover socket -- has no
// concurrency at all, a single non-templated `systemctl --user` unit
// already serializes ordinary starts, and the real long-term fix is
// systemd socket activation (a `knobd.socket` unit), which eliminates
// the problem entirely. M12 (packaging) deliberately left this for a
// future follow-up rather than taking it on alongside the PKGBUILD --
// see that milestone's spec.
func listen(path string) (net.Listener, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("api: create socket directory %s: %w", dir, err)
	}

	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		// Nothing there; proceed.
	case err != nil:
		return nil, fmt.Errorf("api: stat %s: %w", path, err)
	case info.Mode()&os.ModeSocket == 0:
		return nil, fmt.Errorf("api: %s exists and is not a socket; refusing to remove it", path)
	default:
		conn, dialErr := net.DialTimeout("unix", path, 250*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			return nil, fmt.Errorf("api: another knobd is already listening on %s", path)
		}
		if rmErr := os.Remove(path); rmErr != nil {
			return nil, fmt.Errorf("api: remove stale socket %s: %w", path, rmErr)
		}
	}

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("api: listen on %s: %w", path, err)
	}
	// Belt-and-braces under $XDG_RUNTIME_DIR (systemd creates it 0700,
	// so directory traversal already blocks every other user) but
	// load-bearing under the /tmp fallback (world-traversable 1777):
	// there, this chmod is the ONLY access control standing between
	// another local user and full read/write control of this daemon's
	// config and audio graph. There is a sub-millisecond TOCTOU window
	// between net.Listen creating the node (mode 0777&^umask, i.e. 0755
	// under the usual umask 022) and this Chmod; it is not closed here
	// (syscall.Umask is process-global and racy against any other
	// goroutine) -- under $XDG_RUNTIME_DIR the 0700 directory covers the
	// window regardless, and closing it for the /tmp fallback too would
	// need a 0700 parent directory, a path change out of scope for M04.
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("api: chmod %s: %w", path, err)
	}
	return ln, nil
}

// Serve serves s on ln until ctx is canceled, then shuts down gracefully
// (bounded by ShutdownGrace) and returns. It does not remove the socket
// file itself: net.Listen("unix", ...) marks the returned
// *net.UnixListener to unlink its file on Close because it created that
// file, so ln.Close() (which http.Server.Shutdown/Close call) already
// does it. A redundant os.Remove here could delete a successor daemon's
// socket if one bound the same path in between.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 5 * time.Second,
		// Deliberately no WriteTimeout: GET /events is a long-lived
		// response, and a WriteTimeout would sever it on a timer.
		//
		// BaseContext ties every request's context to ctx, so a
		// long-lived GET /events handler's ctx.Done() fires the moment
		// shutdown starts. http.Server.Shutdown does NOT cancel
		// in-flight request contexts on its own -- without this, a
		// single open stream would consume the entire ShutdownGrace
		// waiting for its handler to notice Shutdown was called, since
		// nothing else would ever tell it to return.
		BaseContext: func(net.Listener) context.Context { return ctx },
		ErrorLog:    slog.NewLogLogger(s.log.Handler(), slog.LevelDebug),
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("api: serve: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ShutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		srv.Close()
	}
	<-errCh
	return nil
}

// ListenAndServe listens on the unix socket at path and serves until ctx
// is canceled.
func (s *Server) ListenAndServe(ctx context.Context, path string) error {
	ln, err := listen(path)
	if err != nil {
		return err
	}
	return s.Serve(ctx, ln)
}
