# knobd-ui

Tauri + React + TypeScript configuration UI for knobd. Talks to the
daemon over its unix socket (`daemon/internal/api`), never directly to
MIDI or PipeWire — see `specs/adr/0004-ipc-over-unix-socket.md`.

**Status: scaffolding, not buildable yet.** The daemon's API has no
handlers (`daemon/internal/api` is a stub until M04/M07), and this
machine has no Rust toolchain installed, which Tauri requires. Both are
expected — real UI work starts at
[`specs/milestones/M07-config-ui.md`](../specs/milestones/M07-config-ui.md).

## What's here

```
src/
  main.tsx        React entry point
  App.tsx         placeholder root component
  api/client.ts   typed client for the daemon API — every method throws
                  "not implemented" until M04/M07 land
  types/config.ts hand-written mirror of daemon/internal/model's JSON
                  shape; M07 replaces this with generated types
src-tauri/
  src/main.rs     Tauri entry point, no commands registered yet
  tauri.conf.json window + bundle config; icon paths point at files
                  that don't exist yet (see src-tauri/icons/README.md)
```

## Before this is buildable

1. Install a Rust toolchain: `rustup-init`, then the Tauri Linux
   prerequisites (`webkit2gtk-4.1`, `libappindicator-gtk3`, `librsvg`,
   `patchelf` — package names vary by distro).
2. `npm install`
3. `npm run tauri dev`

None of this blocks M01–M06 (daemon-only milestones), so it isn't set up
by this scaffolding session — see
`specs/reference/environment.md`'s Rust toolchain note.
