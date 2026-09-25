import { describe, expect, it } from "vitest";

import { LAYOUT } from "./geometry";
import { ALL_KINDS, gestureHint, gestureLabel, hasLed, indexRange, supportsGesture } from "./layout";
import type { Gesture } from "./layout";

describe("LAYOUT completeness", () => {
  it("covers every (kind, index) in the generated ranges exactly once", () => {
    const seen = new Set<string>();
    for (const entry of LAYOUT) {
      const key = `${entry.kind}:${entry.index}`;
      expect(seen.has(key), `duplicate geometry entry for ${key}`).toBe(false);
      seen.add(key);
    }

    for (const kind of ALL_KINDS) {
      const { min, max } = indexRange(kind);
      for (let index = min; index <= max; index++) {
        expect(seen.has(`${kind}:${index}`), `missing geometry entry for ${kind}:${index}`).toBe(true);
      }
    }

    let expectedCount = 0;
    for (const kind of ALL_KINDS) {
      const { min, max } = indexRange(kind);
      expectedCount += max - min + 1;
    }
    expect(LAYOUT.length).toBe(expectedCount);
  });

  it("has no entry outside its kind's generated range", () => {
    for (const entry of LAYOUT) {
      const { min, max } = indexRange(entry.kind);
      expect(entry.index).toBeGreaterThanOrEqual(min);
      expect(entry.index).toBeLessThanOrEqual(max);
    }
  });

  it("gives encoder and encoder_push the same screen position (same physical knob)", () => {
    for (const entry of LAYOUT.filter((e) => e.kind === "encoder")) {
      const push = LAYOUT.find((e) => e.kind === "encoder_push" && e.index === entry.index);
      expect(push, `no encoder_push geometry for encoder ${entry.index}`).toBeDefined();
      expect(push?.cx).toBe(entry.cx);
      expect(push?.cy).toBe(entry.cy);
    }
  });
});

describe("device layout semantics", () => {
  it("matches the hardware's LED presence per kind", () => {
    expect(hasLed("encoder")).toBe(true);
    expect(hasLed("button")).toBe(true);
    expect(hasLed("side_button")).toBe(true);
    expect(hasLed("encoder_push")).toBe(false);
    expect(hasLed("fader")).toBe(false);
  });

  it("matches the gesture-validity matrix per kind", () => {
    expect(supportsGesture("encoder", "turn")).toBe(true);
    expect(supportsGesture("encoder", "press")).toBe(false);
    expect(supportsGesture("fader", "move")).toBe(true);
    expect(supportsGesture("fader", "turn")).toBe(false);
    expect(supportsGesture("button", "hold")).toBe(true);
    expect(supportsGesture("button", "turn")).toBe(false);
  });
});

describe("gestureLabel", () => {
  it("gives every gesture a friendly, distinct label", () => {
    const gestures: Gesture[] = ["turn", "press", "hold", "release", "double_press", "move"];
    const labels = gestures.map(gestureLabel);
    expect(new Set(labels).size).toBe(gestures.length);
    expect(gestureLabel("hold")).toBe("Long press");
    expect(gestureLabel("double_press")).toBe("Double press");
  });
});

describe("gestureHint", () => {
  it("has no hint for press/release when nothing defers/holds", () => {
    expect(gestureHint("press", { doubleBound: false, holdBound: false })).toBeUndefined();
    expect(gestureHint("release", { doubleBound: false, holdBound: false })).toBeUndefined();
  });

  it("hints press when double_press is bound", () => {
    expect(gestureHint("press", { doubleBound: true, holdBound: false })).toMatch(/double press/);
  });

  it("hints release only when hold/release is bound", () => {
    expect(gestureHint("release", { doubleBound: false, holdBound: true })).toMatch(/long press/);
  });

  it("always hints hold and double_press", () => {
    expect(gestureHint("hold", { doubleBound: false, holdBound: false })).toBeDefined();
    expect(gestureHint("double_press", { doubleBound: false, holdBound: false })).toBeDefined();
  });

  it("has no hint for turn/move", () => {
    expect(gestureHint("turn", { doubleBound: false, holdBound: false })).toBeUndefined();
    expect(gestureHint("move", { doubleBound: false, holdBound: false })).toBeUndefined();
  });
});
