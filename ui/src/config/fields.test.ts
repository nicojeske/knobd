import { describe, expect, it } from "vitest";

import { clearField, parseOptionalNumber, parseRequiredNumber, setField } from "./fields";

interface Params {
  target: string;
  curveExponent?: number;
}

describe("setField / clearField", () => {
  it("setField sets a value without mutating the input", () => {
    const p: Params = { target: "a" };
    const next = setField(p, "curveExponent", 2);
    expect(next).toEqual({ target: "a", curveExponent: 2 });
    expect(p).toEqual({ target: "a" });
  });

  it("clearField removes the key entirely, not just sets it to undefined", () => {
    const p: Params = { target: "a", curveExponent: 2 };
    const next = clearField(p, "curveExponent");
    expect("curveExponent" in next).toBe(false);
    expect(JSON.stringify(next)).toBe(JSON.stringify({ target: "a" }));
  });
});

describe("parseOptionalNumber", () => {
  it("empty string clears the field (never becomes 0)", () => {
    expect(parseOptionalNumber("")).toEqual({ ok: true, value: undefined });
    expect(parseOptionalNumber("   ")).toEqual({ ok: true, value: undefined });
  });

  it("parses a valid number", () => {
    expect(parseOptionalNumber("42")).toEqual({ ok: true, value: 42 });
    expect(parseOptionalNumber("0")).toEqual({ ok: true, value: 0 });
    expect(parseOptionalNumber("-3.5")).toEqual({ ok: true, value: -3.5 });
  });

  it("rejects non-numeric text", () => {
    expect(parseOptionalNumber("abc")).toEqual({ ok: false });
  });
});

describe("parseRequiredNumber", () => {
  it("rejects an empty string", () => {
    expect(parseRequiredNumber("")).toEqual({ ok: false });
  });

  it("parses a valid number, including zero", () => {
    expect(parseRequiredNumber("0")).toEqual({ ok: true, value: 0 });
    expect(parseRequiredNumber("7")).toEqual({ ok: true, value: 7 });
  });
});
