// Entry point for the knobd-ui Tauri shell. See lib.rs for the app
// itself -- this file only exists because Tauri (like any GUI app on
// Windows) needs a separate binary target to attach the
// windows_subsystem attribute to.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

fn main() {
    knobd_ui_lib::run()
}
