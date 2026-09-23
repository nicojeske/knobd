//! The live event pump: holds knobd's GET /events Server-Sent-Events
//! connection (see specs/adr/0004-ipc-over-unix-socket.md's Update
//! (M07)) and re-emits each frame as a Tauri event ("knobd:event"), plus
//! a "knobd:connection" event when the connection drops or is
//! (re-)established. This is the webview's only path to live state --
//! it never touches the socket itself.
use std::time::Duration;

use http_body_util::{BodyExt, Empty};
use hyper::body::Bytes;
use hyper::Method;
use hyper_util::rt::TokioIo;
use serde_json::Value;
use tauri::{AppHandle, Emitter};
use tokio::net::UnixStream;

use crate::error::BridgeError;
use crate::socket;

const MIN_BACKOFF: Duration = Duration::from_millis(250);
const MAX_BACKOFF: Duration = Duration::from_secs(5);

const EVENT_FRAME: &str = "knobd:event";
const EVENT_CONNECTION: &str = "knobd:connection";

/// spawn_pump starts the reconnect-forever loop on Tauri's own async
/// runtime (see socket.rs's doc comment for why this is
/// tauri::async_runtime::spawn, not a second tokio runtime). Call once,
/// from lib.rs's .setup() hook.
pub fn spawn_pump(app: AppHandle) {
    tauri::async_runtime::spawn(async move {
        let mut backoff = MIN_BACKOFF;
        loop {
            match run_once(&app).await {
                // A clean stream close (e.g. the daemon shutting down
                // gracefully) resets backoff -- it isn't evidence the
                // daemon is having trouble, unlike a connect failure.
                Ok(()) => backoff = MIN_BACKOFF,
                Err(e) => {
                    let _ = app.emit(
                        EVENT_CONNECTION,
                        serde_json::json!({"connected": false, "error": e.to_string()}),
                    );
                    backoff = (backoff * 2).min(MAX_BACKOFF);
                }
            }
            tokio::time::sleep(backoff).await;
        }
    });
}

/// run_once connects, streams frames until the connection ends, and
/// returns. No jitter on the caller's backoff: this is a single
/// UI-per-user client, not a fleet of reconnecting clients that could
/// thunder against the daemon together.
async fn run_once(app: &AppHandle) -> Result<(), BridgeError> {
    let sock_path = socket::socket_path();
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
    tauri::async_runtime::spawn(async move {
        let _ = conn.await;
    });

    let req = hyper::Request::builder()
        .method(Method::GET)
        .uri("/events")
        .header("host", "localhost")
        .body(Empty::<Bytes>::new())
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
    if !res.status().is_success() {
        return Err(BridgeError::Daemon {
            status: res.status().as_u16(),
            code: "internal".to_string(),
            message: format!("GET /events returned {}", res.status()),
        });
    }

    let _ = app.emit(EVENT_CONNECTION, serde_json::json!({"connected": true}));

    let mut body = res.into_body();
    let mut buf: Vec<u8> = Vec::new();

    loop {
        // Drain every complete frame already buffered before waiting for
        // more bytes, coalescing consecutive "state" frames into the
        // latest one. This is opportunistic, not a scheduled interval:
        // it only coalesces frames the daemon's own writes happened to
        // land in the same read (which a burst of its 30ms-throttled
        // pushes commonly does over a unix socket) -- the daemon's own
        // Hub throttle (see daemon/internal/api/hub.go) is what actually
        // guarantees the interval; this is a cheap second pass on top of
        // it, not a replacement for it.
        let mut pending_state: Option<Value> = None;
        while let Some(pos) = find_frame_end(&buf) {
            let raw: Vec<u8> = buf.drain(..pos + 2).collect();
            if let Some((event_type, data)) = parse_sse_frame(&raw) {
                let Ok(value) = serde_json::from_str::<Value>(&data) else {
                    continue;
                };
                if event_type == "state" {
                    pending_state = Some(value);
                } else {
                    // Flush any coalesced state first so events stay in
                    // the order the daemon actually sent them.
                    if let Some(v) = pending_state.take() {
                        let _ = app.emit(EVENT_FRAME, v);
                    }
                    let _ = app.emit(EVENT_FRAME, value);
                }
            }
        }
        if let Some(v) = pending_state {
            let _ = app.emit(EVENT_FRAME, v);
        }

        let frame = body
            .frame()
            .await
            .ok_or_else(|| BridgeError::Unreachable {
                path: sock_path_str.clone(),
                message: "event stream closed".to_string(),
            })?
            .map_err(|e| BridgeError::Protocol {
                message: e.to_string(),
            })?;
        if let Ok(chunk) = frame.into_data() {
            buf.extend_from_slice(&chunk);
        }
    }
}

/// find_frame_end returns the index of the first byte of the "\n\n" that
/// terminates one SSE frame, if a complete frame is present in buf. The
/// daemon writes plain "\n\n" (Go's fmt.Fprintf with \n, not \r\n), so
/// that's the only form looked for.
fn find_frame_end(buf: &[u8]) -> Option<usize> {
    buf.windows(2).position(|w| w == b"\n\n")
}

/// parse_sse_frame extracts (event type, data) from one raw frame's
/// bytes. Returns None for a keep-alive comment (": ping\n\n") or any
/// frame missing either field.
fn parse_sse_frame(raw: &[u8]) -> Option<(String, String)> {
    let text = std::str::from_utf8(raw).ok()?;
    let mut event_type: Option<&str> = None;
    let mut data: Option<&str> = None;
    for line in text.lines() {
        if let Some(rest) = line.strip_prefix("event: ") {
            event_type = Some(rest);
        } else if let Some(rest) = line.strip_prefix("data: ") {
            data = Some(rest);
        }
    }
    Some((event_type?.to_string(), data?.to_string()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_a_well_formed_frame() {
        let raw = b"event: hello\ndata: {\"a\":1}\n\n";
        let (event_type, data) = parse_sse_frame(raw).expect("frame should parse");
        assert_eq!(event_type, "hello");
        assert_eq!(data, "{\"a\":1}");
    }

    #[test]
    fn ignores_a_keepalive_comment() {
        assert_eq!(parse_sse_frame(b": ping\n\n"), None);
    }

    #[test]
    fn find_frame_end_locates_the_terminator() {
        let buf = b"event: state\ndata: {}\n\nevent: state\ndata: {}\n\n";
        let pos = find_frame_end(buf).expect("a terminator should be found");
        assert_eq!(&buf[..pos + 2], b"event: state\ndata: {}\n\n");
    }

    #[test]
    fn find_frame_end_none_for_a_partial_frame() {
        assert_eq!(find_frame_end(b"event: state\ndata: {"), None);
    }
}
