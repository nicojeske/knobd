import { describe, expect, it } from "vitest";

import type { Scene } from "../types/config";
import { removeScene, upsertScene } from "./scenes";

function scene(id: string, displayName = id): Scene {
  return { id, displayName, entries: [] };
}

describe("upsertScene", () => {
  it("appends a new scene", () => {
    const next = upsertScene([scene("a")], scene("b"));
    expect(next.map((s) => s.id)).toEqual(["a", "b"]);
  });

  it("replaces an existing scene in place, never mutating the input", () => {
    const original = [scene("a", "A"), scene("b", "B")];
    const next = upsertScene(original, scene("a", "Updated"));
    expect(next.map((s) => s.displayName)).toEqual(["Updated", "B"]);
    expect(original[0]?.displayName).toBe("A");
  });
});

describe("removeScene", () => {
  it("removes the matching scene by id", () => {
    const next = removeScene([scene("a"), scene("b")], "a");
    expect(next.map((s) => s.id)).toEqual(["b"]);
  });

  it("is a no-op when the id doesn't exist", () => {
    const original = [scene("a")];
    expect(removeScene(original, "ghost")).toEqual(original);
  });
});
