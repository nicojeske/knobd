//! knobd-ui's Tauri application: the library form (rather than
//! everything living in main.rs) is Tauri v2's own convention, and it's
//! what lets `cargo test` exercise the bridge (socket.rs, commands.rs)
//! without ever spawning a webview. main.rs just calls `run()`.
//!
//! The webview never touches knobd's unix socket directly (see
//! specs/adr/0004-ipc-over-unix-socket.md): commands.rs's
//! #[tauri::command]s proxy individual requests, and events.rs holds
//! the GET /events connection and re-emits frames as Tauri events.

mod commands;
mod error;
mod events;
mod socket;
mod tray;

use tauri::Manager;

pub fn run() {
    tauri::Builder::default()
        // Without this, launching the app a second time (e.g. clicking
        // its .desktop entry while it's already hidden to the tray)
        // spawns a second process and a second tray icon rather than
        // showing the existing window.
        .plugin(tauri_plugin_single_instance::init(|app, _argv, _cwd| {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.show();
                let _ = window.unminimize();
                let _ = window.set_focus();
            }
        }))
        .invoke_handler(tauri::generate_handler![
            commands::daemon_status,
            commands::get_config,
            commands::save_config,
            commands::get_state,
            commands::get_audio,
            commands::get_capabilities,
            commands::start_learn,
            commands::cancel_learn,
            commands::spotify_login,
            commands::spotify_logout,
            commands::spotify_playlists,
            commands::spotify_devices,
        ])
        .setup(|app| {
            tray::build(app)?;
            events::spawn_pump(app.handle().clone());
            Ok(())
        })
        .on_window_event(tray::window_event)
        .run(tauri::generate_context!())
        .expect("error while running knobd-ui");
}
