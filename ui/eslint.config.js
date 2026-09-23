// Flat config (ESLint 9+/10+ have no other format). strictTypeChecked
// is what actually enforces CLAUDE.md's "no any" -- tsc alone compiles
// `any` without complaint; only @typescript-eslint's no-explicit-any and
// no-unsafe-* rules (which strictTypeChecked turns on) catch it.
// switch-exhaustiveness-check is what guards the 21-arm action-type
// switch and every field-kind switch the binding editor builds -- a
// missing case fails lint, not just at runtime.
import { defineConfig } from "eslint/config";
import js from "@eslint/js";
import tseslint from "typescript-eslint";
import reactHooks from "eslint-plugin-react-hooks";

export default defineConfig([
  {
    ignores: ["dist/**", "src-tauri/**", "src/types/**"],
  },
  js.configs.recommended,
  tseslint.configs.strictTypeChecked,
  tseslint.configs.stylisticTypeChecked,
  {
    languageOptions: {
      parserOptions: {
        // projectService (not a fixed tsconfig `project` path) lets
        // typed linting cover files outside tsconfig.json's own
        // `include: ["src"]` -- vite.config.ts and this file itself --
        // without adding them to the app's own compile.
        // allowDefaultProject: our two root-level config files, which
        // deliberately stay out of tsconfig.json's `include` (that file
        // governs what actually ships) but still need typed linting.
        projectService: { allowDefaultProject: ["eslint.config.js", "vite.config.ts", "vitest.config.ts"] },
        tsconfigRootDir: import.meta.dirname,
      },
    },
    plugins: {
      "react-hooks": reactHooks,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "@typescript-eslint/switch-exhaustiveness-check": "error",
      // Numbers in template literals are unambiguous (`${index}` reads
      // exactly like String(index)) and this codebase uses them
      // constantly for control indices/labels -- strictTypeChecked's
      // default disallows them, which would mean spelling out
      // String(...) at every one of those call sites for no real gain.
      "@typescript-eslint/restrict-template-expressions": ["error", { allowNumber: true }],
    },
  },
]);
