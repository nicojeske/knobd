//! BridgeError is every command in commands.rs's error type. It has
//! three variants because the frontend needs to react to each
//! differently, and the daemon's own {code,message} error shape
//! (Unreachable, in particular) structurally cannot express one of
//! them: the daemon not running at all.
use serde::Serialize;

/// Serialized as `{"kind": "...", ...}` (via serde's tag) so
/// ui/src/api/client.ts's generated TypeScript type discriminates on
/// `kind` first, then -- inside `daemon` -- on `code`, typed against
/// the OpenAPI-generated `ErrorResponse["code"]` union. A new daemon
/// error code then becomes a TypeScript compile error at any switch
/// over it, rather than a silently-ignored string.
#[derive(Debug, thiserror::Error, Serialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum BridgeError {
    /// The daemon answered with a non-2xx status and its ordinary
    /// {code,message} body (see daemon/internal/api/errors.go).
    #[error("{code}: {message}")]
    Daemon {
        status: u16,
        code: String,
        message: String,
    },
    /// The socket wasn't there, or a connection to it failed outright --
    /// the daemon isn't running, or hasn't finished starting yet. This
    /// is the single most common real failure this bridge sees, and the
    /// one case api::ErrorResponse's own error codes cannot represent
    /// (there is no HTTP response to carry one).
    #[error("cannot reach knobd at {path}: {message}")]
    Unreachable { path: String, message: String },
    /// Connected and got an answer, but it wasn't the shape expected
    /// (malformed JSON, a body that failed to (de)serialize). Distinct
    /// from Daemon because it means a bug in this bridge or a daemon
    /// version mismatch, not something the daemon itself reported.
    #[error("protocol error: {message}")]
    Protocol { message: String },
}
