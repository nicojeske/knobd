// Hardware semantics -- which control kinds exist, their valid index
// ranges, whether they have an LED, and which gestures each supports --
// generated from daemon/internal/model (model.ControlIndexRange,
// model.ControlKind.SupportsGesture, model.ControlKind.HasLED) via
// `make schema`'s device-layout.json and copied here by `npm run
// codegen:device-layout`. This is what lets the panel and binding
// editor enforce the hardware's real constraints without hand-copying
// them: config.schema.json can express neither the index ranges nor
// the gesture-validity matrix.
import deviceLayout from "../types/device-layout.json";
import type { Binding, Control } from "../types/config";

export type ControlKind = Control["kind"];
// config.ts has no standalone Gesture export -- Binding.gesture inlines
// the union -- so it's derived here rather than duplicated.
export type Gesture = Binding["gesture"];

export interface ControlKindLayout {
  kind: ControlKind;
  minIndex: number;
  maxIndex: number;
  hasLed: boolean;
  gestures: readonly Gesture[];
}

// Cast needed once, here: TypeScript infers device-layout.json's exact
// literal shape (kind as the literal string that happens to be in the
// file, gestures as a tuple of literal strings), which is compatible
// with but not identical to the widened ControlKindLayout/ControlKind/
// Gesture types this module exports for the rest of the app to use.
const KIND_LAYOUTS: readonly ControlKindLayout[] = deviceLayout.kinds as readonly ControlKindLayout[];

export const RING_POSITIONS: number = deviceLayout.ringPositions;

export const ALL_KINDS: readonly ControlKind[] = KIND_LAYOUTS.map((k) => k.kind);

const byKind = new Map<ControlKind, ControlKindLayout>(KIND_LAYOUTS.map((k) => [k.kind, k]));

function kindLayout(kind: ControlKind): ControlKindLayout {
  const layout = byKind.get(kind);
  if (!layout) {
    // Unreachable in practice: ControlKind is exactly the union
    // device-layout.json's own generator (model.ControlKinds()) drives,
    // so every member has an entry. A thrown error here would mean the
    // generated file and the Control type genuinely disagree.
    throw new Error(`device/layout: no layout entry for control kind "${kind}"`);
  }
  return layout;
}

export function indexRange(kind: ControlKind): { min: number; max: number } {
  const layout = kindLayout(kind);
  return { min: layout.minIndex, max: layout.maxIndex };
}

export function hasLed(kind: ControlKind): boolean {
  return kindLayout(kind).hasLed;
}

export function supportedGestures(kind: ControlKind): readonly Gesture[] {
  return kindLayout(kind).gestures;
}

export function supportsGesture(kind: ControlKind, gesture: Gesture): boolean {
  return supportedGestures(kind).includes(gesture);
}
