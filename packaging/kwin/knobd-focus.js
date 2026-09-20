// KWin script: reports focused-window changes to knobd over D-Bus.
//
// NOT IMPLEMENTED YET — see specs/milestones/M06-focus-tracking.md and
// specs/adr/0003-focus-tracking-via-kwin-script.md for why this exists
// and what it needs to do:
//
//   1. Listen for workspace.windowActivated (KWin Scripting API).
//   2. On each activation, read whatever the activated window object
//      exposes — resourceClass, desktopFileName/pid if available,
//      caption — matching the daemon/internal/focus.AppInfo shape.
//   3. Call back into knobd via callDBus(...) on a well-known bus
//      name/path/interface the daemon registers at startup (see
//      daemon/internal/focus's package doc comment for the open
//      question of whether callDBus is actually permitted for a KWin
//      script in this Plasma version — confirm this FIRST, before
//      writing the rest of this file, since the fallback if it isn't
//      permitted — writing to a FIFO or file knobd tails instead —
//      is a different implementation entirely).
//
// Installed by the daemon at startup via:
//   org.kde.KWin /Scripting org.kde.kwin.Scripting.loadScript(path, "knobd")
//   org.kde.KWin /Scripting org.kde.kwin.Scripting.start()
//
// Developed against KWin 6.7.5 (see specs/reference/environment.md);
// the Scripting API has changed across major Plasma versions before,
// so re-check the API surface if targeting anything else.
