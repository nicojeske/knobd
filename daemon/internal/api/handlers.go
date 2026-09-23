package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/njeske/knobd/internal/model"
)

// maxConfigBodyBytes caps a PUT /config request body: a runaway client
// gets a clean 400 instead of the daemon reading an unbounded body into
// memory.
const maxConfigBodyBytes = 1 << 20 // 1 MiB

// ConfigStore is how the API reads and replaces the daemon's live
// configuration. The daemon's implementation (cmd/knobd) decides the
// ordering of "persist to disk" and "push into the running engine" and
// how to unwind a partial failure; a handler here only ever sees
// all-or-nothing.
type ConfigStore interface {
	// Config returns the configuration the engine is running right now,
	// served from memory -- it never fails and never blocks on disk I/O.
	Config() model.Config
	// SetConfig atomically persists cfg and makes it the running
	// configuration. cfg has already passed model.Config.Validate by the
	// time this is called, so any error SetConfig returns is an
	// infrastructure failure (disk, engine), reported to the client as
	// 500.
	SetConfig(ctx context.Context, cfg model.Config) error
}

// StateProvider supplies the GET /state snapshot. Implementations must
// serve from state they already hold, not by round-tripping to
// PipeWire: GET /state is meant to be pollable and must never hang on an
// unreachable audio backend.
type StateProvider interface {
	State(ctx context.Context) (State, error)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if s.config == nil {
		writeError(w, s.log, http.StatusServiceUnavailable, CodeUnavailable, errNoConfigStore())
		return
	}
	// Marshal model.Config directly -- this is what makes Binding's
	// discriminated-union action envelope ({"type":...,"params":...})
	// come out right; a handler-local wire struct would bypass
	// Binding.MarshalJSON entirely.
	writeJSON(w, s.log, http.StatusOK, s.config.Config())
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	if s.config == nil {
		writeError(w, s.log, http.StatusServiceUnavailable, CodeUnavailable, errNoConfigStore())
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxConfigBodyBytes)
	dec := json.NewDecoder(r.Body)

	var cfg model.Config
	if err := dec.Decode(&cfg); err != nil {
		writeError(w, s.log, http.StatusBadRequest, CodeInvalidJSON, fmt.Errorf("decode request body: %w", err))
		return
	}
	// Reject trailing content after the first JSON value: catches a
	// doubled or corrupted body a bare Decode call wouldn't notice on
	// its own.
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		writeError(w, s.log, http.StatusBadRequest, CodeInvalidJSON, fmt.Errorf("unexpected content after the config document"))
		return
	}

	// No config.Migrate here: migration exists for on-disk files written
	// by an older daemon build. A PUT body comes from a UI built against
	// this daemon's own schema, so an explicit, named rejection of a
	// mismatched version beats silently mis-parsing a shape from the
	// future -- and it keeps this package from importing
	// daemon/internal/config.
	if cfg.SchemaVersion != model.CurrentSchemaVersion {
		writeError(w, s.log, http.StatusBadRequest, CodeUnsupportedSchemaVersion,
			fmt.Errorf("config schemaVersion %d does not match this daemon's %d", cfg.SchemaVersion, model.CurrentSchemaVersion))
		return
	}
	if err := cfg.Validate(); err != nil {
		writeError(w, s.log, http.StatusBadRequest, CodeInvalidConfig, err)
		return
	}
	if err := s.config.SetConfig(r.Context(), cfg); err != nil {
		writeError(w, s.log, http.StatusInternalServerError, CodeInternal, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetState(w http.ResponseWriter, r *http.Request) {
	if s.state == nil {
		writeError(w, s.log, http.StatusServiceUnavailable, CodeUnavailable, errNoStateProvider())
		return
	}
	state, err := s.state.State(r.Context())
	if err != nil {
		writeError(w, s.log, http.StatusInternalServerError, CodeInternal, err)
		return
	}
	writeJSON(w, s.log, http.StatusOK, state)
}

func (s *Server) handleGetAudio(w http.ResponseWriter, r *http.Request) {
	if s.audio == nil {
		writeError(w, s.log, http.StatusServiceUnavailable, CodeUnavailable, errNoAudioProvider())
		return
	}
	graph, err := s.audio.AudioGraph(r.Context())
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			writeError(w, s.log, http.StatusServiceUnavailable, CodeUnavailable, fmt.Errorf("audio backend did not answer in time: %w", err))
			return
		}
		writeError(w, s.log, http.StatusInternalServerError, CodeInternal, err)
		return
	}
	writeJSON(w, s.log, http.StatusOK, graph)
}

func (s *Server) handleStartLearn(w http.ResponseWriter, r *http.Request) {
	if s.learn == nil {
		writeError(w, s.log, http.StatusServiceUnavailable, CodeUnavailable, errNoLearnController())
		return
	}

	var req LearnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, s.log, http.StatusBadRequest, CodeInvalidJSON, fmt.Errorf("decode request body: %w", err))
		return
	}

	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	state, err := s.learn.StartLearn(r.Context(), timeout)
	if err != nil {
		writeError(w, s.log, http.StatusInternalServerError, CodeInternal, err)
		return
	}
	writeJSON(w, s.log, http.StatusOK, state)
}

func (s *Server) handleStopLearn(w http.ResponseWriter, r *http.Request) {
	if s.learn == nil {
		writeError(w, s.log, http.StatusServiceUnavailable, CodeUnavailable, errNoLearnController())
		return
	}
	if err := s.learn.StopLearn(r.Context()); err != nil {
		writeError(w, s.log, http.StatusInternalServerError, CodeInternal, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.capabilities == nil {
		writeError(w, s.log, http.StatusServiceUnavailable, CodeUnavailable, errNoCapabilitiesProvider())
		return
	}
	writeJSON(w, s.log, http.StatusOK, s.capabilities.Capabilities())
}
