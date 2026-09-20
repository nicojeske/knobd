Icons referenced by `../tauri.conf.json` (`32x32.png`, `128x128.png`,
`128x128@2x.png`, `icon.icns`, `icon.ico`, `tray.png`) are not generated
yet — there is no source artwork for the project. Once there is a source
image, generate the full set with:

```
npm run tauri icon path/to/source-icon.png
```

This is a blocker for actually running `tauri build` (M07), not for
anything before it.
