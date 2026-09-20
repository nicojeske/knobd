// Package api exposes knobd's local control surface to the UI: an
// HTTP+WebSocket server over a unix domain socket
// ($XDG_RUNTIME_DIR/knobd.sock), documented as ADR 0004
// (specs/adr/0004-ipc-over-unix-socket.md). A unix socket rather than a
// TCP port means filesystem permissions do the authorization, so no
// bearer token is needed and no web page anywhere else can reach it.
//
// TODO(M04): implement the HTTP handlers (profiles/bindings/app
// matchers CRUD) backed by daemon/internal/config.
// TODO(M07): implement the WebSocket push channel for live state (knob
// positions, resolved volumes, focus changes) the config UI subscribes
// to, and add OpenAPI spec emission (see the Makefile's `schema`
// target) so ui/'s TypeScript client can be generated rather than
// hand-written.
package api

import (
	"context"
	"net/http"
)

// SocketPath returns the unix socket path the daemon listens on and the
// UI connects to: $XDG_RUNTIME_DIR/knobd.sock, falling back to
// /tmp/knobd-<uid>.sock if $XDG_RUNTIME_DIR is unset.
//
// TODO(M04): implement (mirrors config.Path's XDG-fallback approach).
func SocketPath() (string, error) {
	return "", errNotImplemented("api.SocketPath")
}

// Server is knobd's local API surface.
type Server struct {
	mux *http.ServeMux
}

// New constructs a Server with no routes registered yet.
func New() *Server {
	return &Server{mux: http.NewServeMux()}
}

// ListenAndServe listens on the unix socket at path and serves until ctx
// is canceled. TODO(M04): implement — bind the socket with appropriate
// permissions (0600, owner-only) and wire up http.Serve with the
// context-aware shutdown pattern (http.Server.Shutdown on ctx.Done()).
func (s *Server) ListenAndServe(ctx context.Context, path string) error {
	return errNotImplemented("Server.ListenAndServe")
}
