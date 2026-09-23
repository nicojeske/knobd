import { describe, expect, it } from "vitest";

import { cx } from "./cx";

describe("cx", () => {
  it("joins truthy class names with a space", () => {
    expect(cx("a", "b")).toBe("a b");
  });

  it("drops undefined and false", () => {
    expect(cx("a", undefined, false, "b")).toBe("a b");
  });

  it("returns an empty string for no truthy input", () => {
    expect(cx(undefined, false)).toBe("");
  });

  it("handles a single class name", () => {
    expect(cx("only")).toBe("only");
  });
});
