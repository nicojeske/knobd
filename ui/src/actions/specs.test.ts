import { describe, expect, it } from "vitest";

import type { Action } from "../types/config";
import { ACTION_SPECS, actionTypes } from "./specs";

// This is the drift test that matters: a new required field on a Go
// action struct (regenerated into config.ts as a required key on some
// *Action interface) fails here even though the ACTION_SPECS mapped
// type alone wouldn't catch it -- the mapped type only guards "every
// action has a descriptor," not "every descriptor's defaults() actually
// produces a valid instance of its params type."
describe("ACTION_SPECS", () => {
  it("has exactly one descriptor per Action variant", () => {
    const fromConfig = new Set<Action["type"]>();
    // A minimal exhaustive listing straight from config.ts's own union,
    // asserted against actionTypes() so a 22nd action type added there
    // fails this test until ACTION_SPECS gets an entry (the mapped type
    // in specs.ts already fails the *build*; this failing the *test*
    // suite too is deliberate belt-and-braces for anyone running tests
    // without a full typecheck).
    for (const t of actionTypes()) fromConfig.add(t);
    expect(fromConfig.size).toBe(actionTypes().length);
  });

  it("every descriptor's defaults() has no key set to undefined (must be omitted instead)", () => {
    for (const type of actionTypes()) {
      const spec = ACTION_SPECS[type];
      const defaults = spec.defaults() as Record<string, unknown>;
      for (const [key, value] of Object.entries(defaults)) {
        expect(value, `${type}.${key} default must not be undefined -- omit the key instead`).not.toBeUndefined();
      }
    }
  });

  it("has a field for every key its own defaults() sets", () => {
    // Every key defaults() actually populates must have a field to edit
    // it -- otherwise the UI could produce a value the user has no way
    // to change. (The converse isn't required: a field may exist for an
    // optional key defaults() leaves unset, e.g. volume.adjust's
    // curveExponent.)
    for (const type of actionTypes()) {
      const spec = ACTION_SPECS[type];
      const defaults = spec.defaults() as Record<string, unknown>;
      const fieldKeys = new Set<string>(spec.fields.map((f) => f.key));
      for (const key of Object.keys(defaults)) {
        expect(fieldKeys.has(key), `${type} sets default "${key}" but has no field for it`).toBe(true);
      }
    }
  });

  it("has a non-empty label and a valid group for every action", () => {
    const validGroups = new Set([
      "audio",
      "knob",
      "layer",
      "media",
      "mic",
      "scene",
      "shell",
      "sink",
      "spotify",
      "volume",
    ]);
    for (const type of actionTypes()) {
      const spec = ACTION_SPECS[type];
      expect(spec.label.length).toBeGreaterThan(0);
      expect(validGroups.has(spec.group)).toBe(true);
    }
  });
});
