// Entry point for the knobd-ui Tauri shell. This currently just starts
// a window with the placeholder React app (see ../src/App.tsx) — no
// Tauri commands are registered yet.
//
// TODO(M07): register commands that proxy to knobd's unix socket API
// (see ../src/api/client.ts's TODOs and specs/adr/0004-ipc-over-unix-socket.md),
// and wire up the tray icon's "Configure..." menu item / launch-hidden
// behavior described in specs/milestones/M07-config-ui.md.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

fn main() {
    tauri::Builder::default()
        .run(tauri::generate_context!())
        .expect("error while running knobd-ui");
}
