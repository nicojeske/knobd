import { describe, expect, it } from "vitest";

import type { Binding } from "../types/config";
import { bindingKey, findBinding, findDuplicateKeys, removeBinding, upsertBinding } from "./bindings";

const enc1 = { kind: "encoder" as const, index: 1 };
const enc2 = { kind: "encoder" as const, index: 2 };

function binding(layer: number, index: number, gesture: Binding["gesture"], stepPercent: number): Binding {
  return {
    layer,
    control: { kind: "encoder", index },
    gesture,
    action: { type: "volume.adjust", params: { target: { kind: "default_sink" }, stepPercent } },
  };
}

describe("bindingKey", () => {
  it("combines layer, control kind, control index, and gesture", () => {
    expect(bindingKey(0, enc1, "turn")).toBe("0|encoder|1|turn");
    expect(bindingKey(1, enc2, "turn")).toBe("1|encoder|2|turn");
  });
});

describe("findBinding", () => {
  it("finds the matching binding", () => {
    const bindings = [binding(0, 1, "turn", 2), binding(0, 2, "turn", 3)];
    const found = findBinding(bindings, 0, enc2, "turn");
    expect(found?.action.params).toMatchObject({ stepPercent: 3 });
  });

  it("returns undefined when nothing matches", () => {
    const bindings = [binding(0, 1, "turn", 2)];
    expect(findBinding(bindings, 0, enc2, "turn")).toBeUndefined();
  });

  it("resolves a duplicate key last-one-wins, mirroring bindingIndex", () => {
    const bindings = [binding(0, 1, "turn", 2), binding(0, 1, "turn", 99)];
    const found = findBinding(bindings, 0, enc1, "turn");
    expect(found?.action.params).toMatchObject({ stepPercent: 99 });
  });
});

describe("upsertBinding", () => {
  it("appends when no binding matches the key", () => {
    const bindings = [binding(0, 1, "turn", 2)];
    const next = binding(0, 2, "turn", 3);
    const result = upsertBinding(bindings, next);
    expect(result).toHaveLength(2);
    expect(result[1]).toBe(next);
  });

  it("replaces in place when a binding already matches the key", () => {
    const original = binding(0, 1, "turn", 2);
    const bindings = [original, binding(0, 2, "turn", 3)];
    const next = binding(0, 1, "turn", 99);
    const result = upsertBinding(bindings, next);
    expect(result).toHaveLength(2);
    expect(result[0]).toBe(next);
  });

  it("never mutates the input array", () => {
    const bindings = [binding(0, 1, "turn", 2)];
    const frozen = Object.freeze(bindings.slice());
    upsertBinding(frozen, binding(0, 1, "turn", 99));
    expect(frozen[0]).toMatchObject({ action: { params: { stepPercent: 2 } } });
  });
});

describe("removeBinding", () => {
  it("removes only the matching binding", () => {
    const bindings = [binding(0, 1, "turn", 2), binding(0, 2, "turn", 3)];
    const result = removeBinding(bindings, 0, enc1, "turn");
    expect(result).toHaveLength(1);
    expect(result[0]).toMatchObject({ control: enc2 });
  });
});

describe("findDuplicateKeys", () => {
  it("finds no duplicates in a clean binding list", () => {
    const bindings = [binding(0, 1, "turn", 2), binding(0, 2, "turn", 3)];
    expect(findDuplicateKeys(bindings)).toEqual([]);
  });

  it("finds a key that appears more than once", () => {
    const bindings = [binding(0, 1, "turn", 2), binding(0, 1, "turn", 3), binding(0, 2, "turn", 4)];
    expect(findDuplicateKeys(bindings)).toEqual(["0|encoder|1|turn"]);
  });
});
