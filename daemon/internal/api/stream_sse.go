package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/njeske/knobd/internal/model"
)

// eventKeepAlive is how often an idle connection gets an SSE comment
// line, so an intermediary (or a dead peer) is noticed rather than the
// connection idling forever with no data to react to.
const eventKeepAlive = 20 * time.Second

// handleEvents implements GET /events: a Server-Sent Events stream (see
// specs/adr/0004-ipc-over-unix-socket.md's Update (M07) for why SSE
// rather than WebSocket). On connect it sends a hello frame, then an
// immediate full state snapshot, then state/config_changed/learn_input
// frames as Hub delivers them, plus a periodic keep-alive comment.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if s.events == nil {
		writeError(w, s.log, http.StatusServiceUnavailable, CodeUnavailable, errNoHub())
		return
	}
	if origin := r.Header.Get("Origin"); !s.events.originAllowed(origin) {
		writeError(w, s.log, http.StatusForbidden, CodeForbiddenOrigin, fmt.Errorf("origin %q is not allowed", origin))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, s.log, http.StatusInternalServerError, CodeInternal, fmt.Errorf("response writer does not support streaming"))
		return
	}

	sub := s.events.Subscribe()
	defer s.events.Unsubscribe(sub)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	var seq uint64
	writeFrame := func(ev Event) error {
		seq++
		ev.Seq = seq
		ev.Now = time.Now()
		data, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("api: marshal event: %w", err)
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	hello := Hello{
		ProtocolVersion: EventProtocolVersion,
		SchemaVersion:   model.CurrentSchemaVersion,
		FlushIntervalMs: s.events.FlushIntervalMs(),
	}
	if err := writeFrame(Event{Type: EventHello, Hello: &hello}); err != nil {
		return
	}
	if s.state != nil {
		if st, err := s.state.State(ctx); err == nil {
			if err := writeFrame(Event{Type: EventState, State: &st}); err != nil {
				return
			}
		} else {
			s.log.Warn("api: events: initial state snapshot failed", "err", err)
		}
	}

	keepAlive := time.NewTicker(eventKeepAlive)
	defer keepAlive.Stop()

	for {
		select {
		case <-ctx.Done():
			// BaseContext (server.go's Serve) ties every request's
			// context to the daemon's own shutdown context, so this
			// fires promptly on shutdown instead of Shutdown's grace
			// period being spent waiting out this long-lived response.
			return

		case <-sub.closedCh:
			// Best-effort: the client may already be gone, in which case
			// this write simply fails and the deferred Unsubscribe still
			// runs.
			_ = writeFrame(Event{Type: EventError, Error: &ErrorResponse{
				Code:    sub.closeReason,
				Message: "server closed this stream",
			}})
			return

		case st := <-sub.stateCh:
			if err := writeFrame(Event{Type: EventState, State: &st}); err != nil {
				return
			}

		case ev := <-sub.eventsCh:
			if err := writeFrame(ev); err != nil {
				return
			}

		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
