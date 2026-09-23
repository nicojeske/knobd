//! knobd-ui's Tauri application: the library form (rather than
//! everything living in main.rs) is Tauri v2's own convention, and it's
//! what lets `cargo test` exercise the bridge (socket.rs, commands.rs)
//! without ever spawning a webview. main.rs just calls `run()`.
//!
//! The webview never touches knobd's unix socket directly (see
//! specs/adr/0004-ipc-over-unix-socket.md): commands.rs's
//! #[tauri::command]s proxy individual requests, and a later commit
//! adds an event pump that holds the GET /events connection and
//! re-emits frames as Tauri events.

mod commands;
mod error;
mod socket;

pub fn run() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![
            commands::daemon_status,
            commands::get_config,
            commands::save_config,
            commands::get_state,
            commands::get_audio,
            commands::get_capabilities,
            commands::start_learn,
            commands::cancel_learn,
        ])
        .run(tauri::generate_context!())
        .expect("error while running knobd-ui");
}
