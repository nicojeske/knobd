import { defineConfig } from "vitest/config";

// environment: "node", not jsdom: this project tests logic (config
// helpers, the action-spec table, device layout geometry), not rendered
// components -- see specs/milestones/M07-config-ui.md's plan for why
// jsdom + @testing-library/react are deliberately not added. Real UI
// verification is manual and hardware-driven, per the milestone's own
// Verification section.
export default defineConfig({
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
});
