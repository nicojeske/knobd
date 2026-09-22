package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

func errNotImplemented(fn string) error {
	return fmt.Errorf("api: %s is not implemented yet (see specs/milestones/M04-mapping-engine-daemon.md)", fn)
}

// errNoConfigStore/errNoStateProvider are returned when a route is hit
// on a Server constructed without the dependency it needs (see
// Options's doc comment) -- a daemon-wiring bug, not a client error,
// hence 503 rather than 404/501.
func errNoConfigStore() error {
	return fmt.Errorf("api: this server has no ConfigStore configured")
}

func errNoStateProvider() error {
	return fmt.Errorf("api: this server has no StateProvider configured")
}

// ErrorCode is the machine-readable half of ErrorResponse, so a client
// can distinguish "you sent something invalid" from "my disk is full"
// without string-matching Message.
type ErrorCode string

const (
	CodeInvalidJSON              ErrorCode = "invalid_json"
	CodeUnsupportedSchemaVersion ErrorCode = "unsupported_schema_version"
	CodeInvalidConfig            ErrorCode = "invalid_config"
	CodeUnavailable              ErrorCode = "unavailable"
	CodeInternal                 ErrorCode = "internal"
)

// ErrorResponse is the JSON body of every non-2xx response. Message
// carries the full %w error chain verbatim -- including internal detail
// that would be an information leak on a public API, but this API is
// served on an owner-only unix socket (ADR 0004: filesystem permissions
// are the entire access control mechanism), so the only reader is the
// person running the daemon, and CLAUDE.md's "enough context to debug
// without a debugger attached" argues directly for including it. Do not
// "sanitize" this without revisiting that reasoning.
type ErrorResponse struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func writeError(w http.ResponseWriter, log *slog.Logger, status int, code ErrorCode, err error) {
	if status >= 500 {
		log.Error("api: request failed", "status", status, "code", code, "err", err)
	} else {
		log.Debug("api: request rejected", "status", status, "code", code, "err", err)
	}
	writeJSON(w, log, status, ErrorResponse{Code: code, Message: err.Error()})
}

func writeJSON(w http.ResponseWriter, log *slog.Logger, status int, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Error("api: marshal response failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(data)
}
