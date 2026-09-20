import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Config for the knobd-ui Tauri frontend. See ui/README.md and
// specs/milestones/M07-config-ui.md for what is and isn't built yet.
export default defineConfig({
  plugins: [react()],

  // Tauri expects a fixed dev server port; fail instead of silently
  // picking another one if it's taken.
  server: {
    port: 1420,
    strictPort: true,
  },

  // Tauri supports es2021+ on all its targets.
  build: {
    target: "esnext",
    minify: "esbuild",
    sourcemap: true,
  },
});
