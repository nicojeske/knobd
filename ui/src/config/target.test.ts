import { describe, expect, it } from "vitest";

import { makeTarget, targetNeedsRef } from "./target";

describe("targetNeedsRef", () => {
  it("mirrors model.Target.Validate's needsRef table", () => {
    expect(targetNeedsRef("default_sink")).toBe(false);
    expect(targetNeedsRef("sink")).toBe(true);
    expect(targetNeedsRef("default_source")).toBe(false);
    expect(targetNeedsRef("source")).toBe(true);
    expect(targetNeedsRef("app")).toBe(true);
    expect(targetNeedsRef("group")).toBe(true);
    expect(targetNeedsRef("focused")).toBe(false);
    expect(targetNeedsRef("all_streams")).toBe(false);
  });
});

describe("makeTarget", () => {
  it("omits ref entirely for a kind that must not carry one", () => {
    const t = makeTarget("focused", "should-be-ignored");
    expect(t).toEqual({ kind: "focused" });
    expect("ref" in t).toBe(false);
  });

  it("includes ref for a kind that requires one", () => {
    expect(makeTarget("app", "vesktop")).toEqual({ kind: "app", ref: "vesktop" });
  });

  it("defaults an omitted ref to an empty string for a kind that requires one", () => {
    expect(makeTarget("sink")).toEqual({ kind: "sink", ref: "" });
  });
});
