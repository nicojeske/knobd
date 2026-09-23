//! The #[tauri::command] surface: one thin wrapper per daemon route,
//! each just a method/path pair into socket::request. See socket.rs's
//! doc comment for why these return raw serde_json::Value rather than a
//! typed Rust struct.
use hyper::Method;
use serde::Serialize;
use serde_json::Value;

use crate::error::BridgeError;
use crate::socket;

/// DaemonStatus is the one Rust-side-typed response in this file: it
/// describes the bridge's own connectivity, not anything the daemon
/// itself returns, so there is no Go/OpenAPI shape to mirror. camelCase
/// to match every JSON shape elsewhere in this project (the daemon's
/// own JSON tags, and every generated TypeScript type).
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DaemonStatus {
    pub socket_path: String,
    pub reachable: bool,
    pub error: Option<String>,
}

/// daemon_status reports whether knobd is actually reachable right now,
/// via a real round trip (GET /capabilities: cheap, and -- unlike
/// GET /state or GET /audio -- never depends on a MIDI device or
/// PipeWire being present, only on the daemon process being up).
#[tauri::command]
pub async fn daemon_status() -> DaemonStatus {
    let socket_path = socket::socket_path().display().to_string();
    match socket::request(Method::GET, "/capabilities", None).await {
        Ok(_) => DaemonStatus {
            socket_path,
            reachable: true,
            error: None,
        },
        Err(BridgeError::Unreachable { message, .. }) => DaemonStatus {
            socket_path,
            reachable: false,
            error: Some(message),
        },
        // A Daemon or Protocol error still means something answered on
        // the socket -- reachable, just unhappy about this particular
        // request.
        Err(e) => DaemonStatus {
            socket_path,
            reachable: true,
            error: Some(e.to_string()),
        },
    }
}

#[tauri::command]
pub async fn get_config() -> Result<Value, BridgeError> {
    socket::request(Method::GET, "/config", None).await
}

#[tauri::command]
pub async fn save_config(config: Value) -> Result<(), BridgeError> {
    socket::request(Method::PUT, "/config", Some(config))
        .await
        .map(|_| ())
}

#[tauri::command]
pub async fn get_state() -> Result<Value, BridgeError> {
    socket::request(Method::GET, "/state", None).await
}

#[tauri::command]
pub async fn get_audio() -> Result<Value, BridgeError> {
    socket::request(Method::GET, "/audio", None).await
}

#[tauri::command]
pub async fn get_capabilities() -> Result<Value, BridgeError> {
    socket::request(Method::GET, "/capabilities", None).await
}

#[tauri::command]
pub async fn start_learn(timeout_ms: Option<u64>) -> Result<Value, BridgeError> {
    let body = timeout_ms.map(|ms| serde_json::json!({ "timeoutMs": ms }));
    socket::request(Method::POST, "/learn", body).await
}

#[tauri::command]
pub async fn cancel_learn() -> Result<(), BridgeError> {
    socket::request(Method::DELETE, "/learn", None)
        .await
        .map(|_| ())
}
