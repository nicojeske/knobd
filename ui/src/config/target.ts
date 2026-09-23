// Mirrors model.Target.Validate's needsRef rule: sink/source/app/group
// require a ref, default_sink/default_source/focused/all_streams must
// not carry one at all -- not merely an empty string. makeTarget is the
// only place a Target literal should be constructed in the UI, so that
// rule is enforced in exactly one place.
import type { Target } from "../types/config";

export type TargetKind = Target["kind"];

const TARGET_NEEDS_REF: Readonly<Record<TargetKind, boolean>> = {
  default_sink: false,
  sink: true,
  default_source: false,
  source: true,
  app: true,
  group: true,
  focused: false,
  all_streams: false,
};

export function targetNeedsRef(kind: TargetKind): boolean {
  return TARGET_NEEDS_REF[kind];
}

/** makeTarget builds a Target, omitting `ref` entirely for a kind that
 * must not carry one -- under exactOptionalPropertyTypes, `{ kind, ref:
 * undefined }` is a type error, and even if it weren't, Target.Validate
 * rejects a ref on these kinds server-side, so the key must be genuinely
 * absent, not merely empty. */
export function makeTarget(kind: TargetKind, ref?: string): Target {
  if (targetNeedsRef(kind)) {
    return { kind, ref: ref ?? "" };
  }
  return { kind };
}

export const TARGET_KINDS: readonly TargetKind[] = [
  "default_sink",
  "sink",
  "default_source",
  "source",
  "app",
  "group",
  "focused",
  "all_streams",
];
