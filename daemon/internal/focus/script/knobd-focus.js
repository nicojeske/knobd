// knobd-focus.js -- KWin script: reports focused-window changes to
// knobd over D-Bus.
//
// This file is never installed anywhere by packaging. knobd embeds it
// (see daemon/internal/focus/script.go's go:embed), substitutes the
// @KNOBD_*@ placeholders below for the actual bus name/path/interface
// it registered at startup, writes the result to
// $XDG_RUNTIME_DIR/knobd/knobd-focus.js, and loads THAT path via:
//   org.kde.KWin /Scripting org.kde.kwin.Scripting.loadScript(path, "knobd")
//   org.kde.KWin /Scripting org.kde.kwin.Scripting.start()
// -- see specs/adr/0003-focus-tracking-via-kwin-script.md for why this
// is embedded rather than a file installed by packaging.
//
// Confirmed live during M06 planning against KWin 6.7.5 on this
// machine's Plasma/Wayland session: callDBus CAN call out to an
// arbitrary service (not just back into org.kde.KWin itself), and
// workspace.activeWindow is already populated by the time a script's
// top-level code runs -- see ADR 0003's "Resolved" section for the
// evidence. Debug a running instance with:
//   journalctl --user -f | grep knobd-focus

const SERVICE = "@KNOBD_SERVICE@";
const OBJECT = "@KNOBD_OBJECT@";
const INTERFACE = "@KNOBD_INTERFACE@";
const METHOD = "FocusChanged";
const PAYLOAD_VERSION = 1;

let lastPayload = "";

function str(v) {
    return typeof v === "string" ? v : "";
}

// describe() never throws and never omits a field: the daemon decodes
// a fixed JSON shape, and a window property KWin doesn't expose in
// this version must degrade to a zero value, not to a malformed
// payload the daemon can't parse at all.
function describe(w) {
    if (!w) {
        return {
            v: PAYLOAD_VERSION, normal: false,
            resourceClass: "", desktopFileId: "", caption: "", pid: 0,
        };
    }
    return {
        v: PAYLOAD_VERSION,
        normal: w.normalWindow === true,
        resourceClass: str(w.resourceClass),
        desktopFileId: str(w.desktopFileName),
        caption: str(w.caption),
        pid: (typeof w.pid === "number" && w.pid > 0) ? w.pid : 0,
    };
}

// report() sends exactly one string argument, always -- never a typed
// number/bool. callDBus infers each argument's D-Bus type from the JS
// value, and a mismatch against the daemon's exported method signature
// fails with no error visible anywhere (not in this journal, not on
// the daemon side); a single JSON string sidesteps that entirely.
function report(w) {
    const payload = JSON.stringify(describe(w));
    if (payload === lastPayload) {
        return; // re-activating the same window is common; don't spam
    }
    lastPayload = payload;
    callDBus(SERVICE, OBJECT, INTERFACE, METHOD, payload);
}

workspace.windowActivated.connect(report);

// There is no "give me the current window" call separate from the
// signal, so report once, unconditionally, right away. This doubles as
// the handshake the daemon waits for to confirm the whole path (KWin
// script -> callDBus -> daemon's D-Bus service) is actually working --
// see kwin.go's handshake-timeout comment for why that matters more
// than it might seem: callDBus failures are otherwise silent.
report(workspace.activeWindow);
console.info("knobd-focus: watching workspace.windowActivated, reporting to " + SERVICE);
