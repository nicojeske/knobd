//! knobd-ui's Tauri application: the library form (rather than
//! everything living in main.rs) is Tauri v2's own convention, and it's
//! what will let `cargo test` exercise the bridge (added in a later
//! commit: socket.rs, commands.rs) without ever spawning a webview.
//! main.rs just calls `run()`.
//!
//! The webview never touches knobd's unix socket directly (see
//! specs/adr/0004-ipc-over-unix-socket.md): a later commit adds
//! `#[tauri::command]`s that proxy individual requests, and an event
//! pump that holds the GET /events connection and re-emits frames as
//! Tauri events.

pub fn run() {
    tauri::Builder::default()
        .run(tauri::generate_context!())
        .expect("error while running knobd-ui");
}
