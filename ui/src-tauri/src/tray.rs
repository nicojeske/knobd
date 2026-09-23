//! The tray icon and window-close-hides-to-tray lifecycle: knobd-ui's
//! own process is separate from the knobd daemon (ADR 0004), so closing
//! this window must never touch the daemon, and the tray icon must stay
//! alive as the way back in.
//!
//! KDE/Wayland specifics that shape this file: with the Ayatana
//! AppIndicator backend (what `libayatana-appindicator` -- installed on
//! this system, see specs/reference/environment.md -- backs on Linux),
//! a left click on the tray icon is NOT delivered to the app at all;
//! the desktop shell owns it and only ever opens the menu. So
//! "Configure…" must be a menu item, never a left-click handler, and
//! `show_menu_on_left_click(true)` is what makes a single click do
//! anything at all. Getting this wrong produces a tray icon that
//! silently does nothing on click, which reads as a Wayland bug rather
//! than what it actually is.
use tauri::menu::{MenuBuilder, MenuItemBuilder};
use tauri::tray::TrayIconBuilder;
use tauri::{App, AppHandle, Manager, WindowEvent};

const MAIN_WINDOW: &str = "main";

/// build installs the tray icon and its menu. Called from lib.rs's
/// .setup() hook.
pub fn build(app: &App) -> tauri::Result<()> {
    let configure = MenuItemBuilder::with_id("configure", "Configure…").build(app)?;
    let quit = MenuItemBuilder::with_id("quit", "Quit knobd-ui").build(app)?;
    let menu = MenuBuilder::new(app)
        .item(&configure)
        .separator()
        .item(&quit)
        .build()?;

    // include_bytes! rather than a runtime Image::from_path: a bundled
    // resource path differs between `tauri dev` and an installed .deb,
    // and this is one PNG that never changes at runtime, so there is no
    // reason to resolve it as a resource at all.
    let icon_bytes = include_bytes!("../icons/tray.png");
    let icon = tauri::image::Image::from_bytes(icon_bytes)?;

    TrayIconBuilder::with_id("knobd")
        .icon(icon)
        .menu(&menu)
        .show_menu_on_left_click(true)
        .tooltip("knobd")
        .on_menu_event(|app, event| match event.id().as_ref() {
            "configure" => show_main_window(app),
            "quit" => app.exit(0),
            _ => {}
        })
        .build(app)?;

    Ok(())
}

/// window_event is lib.rs's .on_window_event handler: closing the
/// window hides it to the tray instead of destroying it. A tray icon
/// whose owning window (and, on some platforms, whose process) is gone
/// the moment you close it is a tray icon that no longer does anything
/// -- which would break this milestone's own acceptance criterion that
/// "Configure…" reopens the window.
pub fn window_event(window: &tauri::Window, event: &WindowEvent) {
    if let WindowEvent::CloseRequested { api, .. } = event {
        api.prevent_close();
        let _ = window.hide();
    }
}

/// show_main_window un-hides the main window and brings it to front.
/// show() then unminimize() then set_focus(), in that order: a hidden
/// Wayland window's surface is destroyed and recreated rather than
/// merely unmapped, and it routinely comes back both minimized and
/// unfocused if these are skipped or reordered.
fn show_main_window(app: &AppHandle) {
    let Some(window) = app.get_webview_window(MAIN_WINDOW) else {
        return;
    };
    let _ = window.show();
    let _ = window.unminimize();
    let _ = window.set_focus();
}
