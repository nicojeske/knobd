Everything here except `source/*.svg` is generated; regenerate with
`make ui-icons` (from the repo root) after changing a source SVG.

- `source/knobd.svg` — the app icon (an encoder knob with its LED ring).
  `npm run tauri icon src-tauri/icons/source/knobd.svg` (run from `ui/`)
  produces `32x32.png`, `128x128.png`, `128x128@2x.png`, `icon.icns`,
  `icon.ico`, and `icon.png` from it — the desktop set `tauri.conf.json`'s
  `bundle.icon` and default window icon reference. `tauri icon` also
  emits iOS/Android/Windows-Store assets by default; this project is
  Linux-desktop-only (see `specs/reference/environment.md`), so those are
  deleted after each regeneration rather than committed.
- `source/tray.svg` — a flat, single-color simplification of the same
  shape, sized for a 22-32px panel icon rather than a home-screen tile.
  `tauri icon` doesn't produce a tray asset, so `tray.png` is rendered
  from this file directly: `rsvg-convert -w 32 -h 32 src-tauri/icons/source/tray.svg -o src-tauri/icons/tray.png`.
