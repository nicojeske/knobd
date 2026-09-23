// Two small, deliberately-isolated helpers for editing an optional
// field on a generated config type under exactOptionalPropertyTypes:
// that flag forbids `{ ...p, k: undefined }` for an optional key `k?:`
// -- TypeScript treats explicitly assigning `undefined` to an optional
// property as different from the property being absent, and generated
// config types always mean the latter (Go's own `omitempty` never
// serializes a JSON `null` for these fields; the daemon just doesn't see
// the key at all). Getting this wrong doesn't just fail to compile: a
// naive `{...params, curveExponent: undefined}` would serialize as
// `"curveExponent": null`, and Go would decode that as 0, silently
// changing behavior rather than "not set".
export function setField<P extends object, K extends keyof P>(p: P, k: K, v: P[K]): P {
  return { ...p, [k]: v };
}

// clearField's `as P` cast is confined to this one line: deleting a key
// from a `Partial<P>` copy leaves TypeScript unable to re-narrow it back
// to `P` on its own, even though the result is sound (P had that key as
// optional to begin with, or the caller wouldn't be clearing it).
export function clearField<P extends object>(p: P, k: keyof P): P {
  const rest: Partial<P> = { ...p };
  Reflect.deleteProperty(rest, k);
  return rest as P;
}

/** parseOptionalNumber commits a raw text-input value to an optional
 * numeric field: empty string clears the field entirely (never sets it
 * to 0), a valid number sets it, anything else is reported as invalid
 * rather than silently coerced. The naive `Number(text) || undefined`
 * is the single most likely bug this helper exists to prevent -- it
 * turns a genuine 0 into "unset", which is wrong for e.g. a 0% minPercent. */
export function parseOptionalNumber(text: string): { ok: true; value: number | undefined } | { ok: false } {
  if (text.trim() === "") return { ok: true, value: undefined };
  const n = Number(text);
  if (Number.isNaN(n)) return { ok: false };
  return { ok: true, value: n };
}

/** parseRequiredNumber is parseOptionalNumber without the "empty means
 * unset" case -- an empty or non-numeric string is always invalid. */
export function parseRequiredNumber(text: string): { ok: true; value: number } | { ok: false } {
  if (text.trim() === "") return { ok: false };
  const n = Number(text);
  if (Number.isNaN(n)) return { ok: false };
  return { ok: true, value: n };
}
