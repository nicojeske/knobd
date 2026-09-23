# knobd-ui

Tauri + React + TypeScript configuration UI for knobd. Talks to the
daemon over its unix socket (`daemon/internal/api`), never directly to
MIDI or PipeWire — see `specs/adr/0004-ipc-over-unix-socket.md`.

**Status: feature-complete for M07.** `npm run tauri dev` opens a window,
`npm run tauri build` produces a `.deb`, and every editor talks to a real
daemon over `daemon/internal/api`'s unix socket — no stubs remain in
`src/api/client.ts`. See
[`specs/milestones/M07-config-ui.md`](../specs/milestones/M07-config-ui.md)
for the full design and the hardware checklist that still needs running
against a physical X-Touch Mini before this milestone closes.

## What's here

```
src/
  main.tsx           React entry point
  App.tsx            view switch (Panel / Apps & Groups / Profiles /
                      Diagnostics), StatusBar, the config_changed banner
  api/client.ts       real client — free functions over invoke(), typed
                      by src/types/{config,api}.ts
  state/              ConnectionContext (SSE-pushed live state, seeded on
                      mount), ConfigContext (config draft + reload +
                      config_changed banner), CapabilitiesContext,
                      useAudioGraph
  device/             layout.ts (generated device-layout.json wrapper:
                      index ranges, gesture validity) + geometry.ts
                      (hand-written SVG positions, vitest-checked against
                      layout.ts's ranges)
  components/panel/   the visual X-Touch Mini (SVG), MIDI learn overlay
  components/binding/ the binding editor: gesture + action pickers,
                      per-action param forms driven by actions/specs.ts
  components/apps/    app matcher / group CRUD + live GET /audio pickers
  components/profiles/ profile create/rename/duplicate/delete/activate
  types/config.ts     generated from docs/config.schema.json by
                      `npm run codegen:config` — do not edit by hand
  types/api.ts        generated from docs/openapi.json by
                      `npm run codegen:api` — do not edit by hand
  types/device-layout.json  copied from docs/device-layout.json by
                      `npm run codegen:device-layout`
src-tauri/
  src/lib.rs      Tauri app assembly; src/main.rs just calls lib::run()
  src/socket.rs   hand-rolled unix-socket HTTP client (hyper, no pooling)
  src/events.rs   SSE reader, re-emitted as the `knobd:event` Tauri event
  src/tray.rs     tray icon + menu; window close hides, doesn't quit
  capabilities/   default.json grants core:default (needed for
                  @tauri-apps/api/event's listen(), not for our own
                  #[tauri::command]s)
  icons/          generated from icons/source/*.svg by `make ui-icons`
                  (from the repo root) — see icons/README.md
  tauri.conf.json window + bundle config; bundle.targets is narrowed to
                  ["deb"] for now (AppImage needs patchelf, which isn't
                  installed, plus a network fetch of linuxdeploy) — the
                  Arch package (packaging/arch) doesn't use this bundler
                  at all, building with `tauri build --no-bundle` and
                  installing the resulting binary + a hand-written
                  desktop file/icons itself; see packaging/README.md
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
