# knobd-ui

Tauri + React + TypeScript configuration UI for knobd. Talks to the
daemon over its unix socket (`daemon/internal/api`), never directly to
MIDI or PipeWire — see `specs/adr/0004-ipc-over-unix-socket.md`.

**Status: buildable.** `npm run tauri dev` opens a window and
`npm run tauri build` produces a `.deb`; see
[`specs/milestones/M07-config-ui.md`](../specs/milestones/M07-config-ui.md)
for what's implemented so far versus still in progress (the Rust bridge
to the daemon, the visual panel, the binding editor, and everything else
in `src/` beyond the placeholder root component).

## What's here

```
src/
  main.tsx        React entry point
  App.tsx         placeholder root component
  api/client.ts   typed client for the daemon API — every method throws
                  "not implemented" until the Rust bridge lands
  types/config.ts generated from docs/config.schema.json by
                  `npm run codegen` — do not edit by hand, see M01
src-tauri/
  src/lib.rs      Tauri app assembly; src/main.rs just calls lib::run()
  capabilities/   default.json grants core:default (needed for
                  @tauri-apps/api/event's listen(), not for our own
                  #[tauri::command]s)
  icons/          generated from icons/source/*.svg by `make ui-icons`
                  (from the repo root) — see icons/README.md
  tauri.conf.json window + bundle config; bundle.targets is narrowed to
                  ["deb"] for now (AppImage needs patchelf, which isn't
                  installed, plus a network fetch of linuxdeploy) —
                  M12 owns the final packaging decision
```

## Building

1. Install a Rust toolchain (already done on this development machine —
   see `specs/reference/environment.md`) and the Tauri Linux
   prerequisites: `webkit2gtk-4.1`, `libayatana-appindicator`,
   `librsvg`, `patchelf` (package names vary by distro).
2. `npm install` (or `make ui-install` from the repo root).
3. `npm run tauri dev` (or `make ui-dev`).

`npm run tauri build` produces `src-tauri/target/release/bundle/deb/*.deb`.

### A note on this development machine's Wayland session

`npm run tauri dev`'s native Wayland path fails here with `Gdk-Message:
Error 71 (Protocol error) dispatching to Wayland display` — this
sandboxed session's compositor doesn't support something WebKitGTK's
Wayland backend needs (most likely GPU/DRM access for its DMA-BUF
renderer; `Failed to create GBM buffer` shows up under X11 for the same
reason). Confirmed via the X11 window list (`xprop -root
_NET_CLIENT_LIST`), running with `GDK_BACKEND=x11
WEBKIT_DISABLE_DMABUF_RENDERER=1` opens the window cleanly through
XWayland instead. This has not been reproduced on a full, unsandboxed
KDE Plasma session with normal GPU access — if it recurs there, the same
two environment variables are the first thing to try.
