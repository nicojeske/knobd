//! The unix-socket HTTP client this bridge speaks to knobd's local API
//! over (see specs/adr/0004-ipc-over-unix-socket.md). One connection per
//! request: the UI issues a handful of requests per user interaction,
//! not a hot path, and the one long-lived path (the live event stream)
//! is events.rs's own separate connection.
//!
//! Hand-rolled over hyper's connection API rather than `hyperlocal`:
//! hyperlocal exists to make hyper's pooling Client and its URI-based
//! connector work with a filesystem path (it hex-encodes the path into
//! a fake hostname so a `Client<UnixConnector>` can route by URI). This
//! bridge needs neither pooling nor routing -- there is exactly one
//! socket at one path, dialed fresh each call -- so hyperlocal would be
//! a dependency whose entire value proposition goes unused.
//!
//! Every payload from the daemon passes through as a raw
//! `serde_json::Value`, never deserialized into a Rust struct that
//! mirrors api.State/api.Config/etc. That would be a third
//! hand-maintained copy of shapes the daemon's Go model already owns and
//! docs/openapi.json already describes -- CLAUDE.md's "API types are
//! generated from the daemon's Go structs, not hand duplicated" applies
//! here exactly as it does to ui/src/types/. The type authority stays
//! Go model -> docs/openapi.json -> TypeScript; this crate is transport.
//!
//! Note tauri::async_runtime::spawn is used to drive each connection's
//! background IO task, not tokio::spawn or a second #[tokio::main]
//! runtime: Tauri already sets up a tokio runtime internally (that is
//! what tauri::async_runtime wraps), and every #[tauri::command] async
//! fn already runs on it.

use std::env;
use std::path::PathBuf;

use http_body_util::{BodyExt, Full};
use hyper::body::Bytes;
use hyper::Method;
use hyper_util::rt::TokioIo;
use serde::Deserialize;
use serde_json::Value;
use tokio::net::UnixStream;

use crate::error::BridgeError;

/// SOCKET_NAME mirrors daemon/internal/api's SocketName constant.
const SOCKET_NAME: &str = "knobd.sock";

/// socket_path mirrors daemon/internal/api.SocketPath's fallback logic
/// exactly: $XDG_RUNTIME_DIR/knobd.sock, or $TMPDIR/knobd-<uid>.sock
/// ($TMPDIR defaulting to /tmp) if $XDG_RUNTIME_DIR is unset. Covered by
/// socket_path_matches_expected_fallback below since this is the one
/// place in this crate that duplicates daemon-side logic rather than
/// treating the daemon as the sole source of truth.
pub fn socket_path() -> PathBuf {
    if let Ok(dir) = env::var("XDG_RUNTIME_DIR") {
        if !dir.is_empty() {
            return PathBuf::from(dir).join(SOCKET_NAME);
        }
    }
    let tmp = env::var("TMPDIR").unwrap_or_else(|_| "/tmp".to_string());
    let uid = libc_getuid();
    PathBuf::from(tmp).join(format!("knobd-{uid}.sock"))
}

// A tiny hand-rolled getuid rather than pulling in the `libc` or `nix`
// crate for one syscall this crate needs exactly once.
#[cfg(unix)]
fn libc_getuid() -> u32 {
    extern "C" {
        fn getuid() -> u32;
    }
    unsafe { getuid() }
}

#[derive(Deserialize)]
struct ErrorBody {
    code: String,
    message: String,
}

/// request sends one HTTP request to knobd's local API over a fresh
/// unix-socket connection and returns the decoded JSON response body
/// (Value::Null for an empty body, e.g. PUT /config's 204).
pub async fn request(
    method: Method,
    path: &str,
    body: Option<Value>,
) -> Result<Value, BridgeError> {
    let sock_path = socket_path();
    let sock_path_str = sock_path.display().to_string();

    let stream = UnixStream::connect(&sock_path)
        .await
        .map_err(|e| BridgeError::Unreachable {
            path: sock_path_str.clone(),
            message: e.to_string(),
        })?;
    let io = TokioIo::new(stream);

    let (mut sender, conn) = hyper::client::conn::http1::handshake(io)
        .await
        .map_err(|e| BridgeError::Unreachable {
            path: sock_path_str.clone(),
            message: e.to_string(),
        })?;
    // Drives the connection's IO; the handshake above only sets it up.
    // Its own error (a mid-request disconnect) surfaces through
    // send_request's Err below instead, so it's fine to drop this
    // JoinHandle rather than await it.
    tauri::async_runtime::spawn(async move {
        let _ = conn.await;
    });

    let body_bytes = match &body {
        Some(v) => serde_json::to_vec(v).map_err(|e| BridgeError::Protocol {
            message: e.to_string(),
        })?,
        None => Vec::new(),
    };

    let mut builder = hyper::Request::builder()
        .method(method)
        .uri(path)
        .header("host", "localhost");
    if !body_bytes.is_empty() {
        builder = builder.header("content-type", "application/json");
    }
    let req = builder
        .body(Full::new(Bytes::from(body_bytes)))
        .map_err(|e| BridgeError::Protocol {
            message: e.to_string(),
        })?;

    let res = sender
        .send_request(req)
        .await
        .map_err(|e| BridgeError::Unreachable {
            path: sock_path_str.clone(),
            message: e.to_string(),
        })?;

    let status = res.status();
    let collected = res.collect().await.map_err(|e| BridgeError::Protocol {
        message: e.to_string(),
    })?;
    let bytes = collected.to_bytes();

    if !status.is_success() {
        return Err(match serde_json::from_slice::<ErrorBody>(&bytes) {
            Ok(body) => BridgeError::Daemon {
                status: status.as_u16(),
                code: body.code,
                message: body.message,
            },
            Err(_) => BridgeError::Protocol {
                message: format!("daemon returned status {status} with an unparseable error body"),
            },
        });
    }

    if bytes.is_empty() {
        return Ok(Value::Null);
    }
    serde_json::from_slice(&bytes).map_err(|e| BridgeError::Protocol {
        message: e.to_string(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    // A single test function rather than several #[test] fns: cargo
    // test runs tests in the same binary concurrently by default, and
    // std::env mutation is process-global, so exercising both branches
    // sequentially in one test (saving and restoring every var it
    // touches) is what keeps this deterministic without pulling in a
    // serial-test crate for one test.
    #[test]
    fn socket_path_matches_expected_fallback() {
        let saved_xdg = env::var("XDG_RUNTIME_DIR").ok();
        let saved_tmpdir = env::var("TMPDIR").ok();

        env::set_var("XDG_RUNTIME_DIR", "/run/user/1000");
        assert_eq!(socket_path(), PathBuf::from("/run/user/1000/knobd.sock"));

        env::remove_var("XDG_RUNTIME_DIR");
        env::set_var("TMPDIR", "/tmp/knobd-test");
        let uid = libc_getuid();
        assert_eq!(
            socket_path(),
            PathBuf::from(format!("/tmp/knobd-test/knobd-{uid}.sock"))
        );

        match saved_xdg {
            Some(v) => env::set_var("XDG_RUNTIME_DIR", v),
            None => env::remove_var("XDG_RUNTIME_DIR"),
        }
        match saved_tmpdir {
            Some(v) => env::set_var("TMPDIR", v),
            None => env::remove_var("TMPDIR"),
        }
    }
}

#[cfg(test)]
mod smoke {
    use super::*;

    // Ignored by default (no daemon running in CI): a manual real-socket
    // check against an actual running knobd, mirroring the daemon's own
    // pattern of "unit-testable without hardware, but still verify by
    // hand against the real thing." Run with a knobd listening (real or
    // XDG_RUNTIME_DIR pointed at one) via:
    //   cargo test smoke_real_daemon -- --ignored --nocapture
    #[tokio::test]
    #[ignore]
    async fn smoke_real_daemon() {
        let caps = request(Method::GET, "/capabilities", None).await.unwrap();
        println!("capabilities: {caps}");
        let cfg = request(Method::GET, "/config", None).await.unwrap();
        println!("config schemaVersion: {}", cfg["schemaVersion"]);
    }
}
