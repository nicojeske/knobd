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

/** gestureLabel gives every Gesture a name a non-hardware-hacker
 * recognizes -- the raw values ("hold", "double_press") read fine in
 * JSON but not in a picker. Used by the binding editor's gesture list;
 * kept here, not component-local, so it stays next to the Gesture union
 * it describes. */
export function gestureLabel(gesture: Gesture): string {
  switch (gesture) {
    case "turn":
      return "Turn";
    case "press":
      return "Press";
    case "hold":
      return "Long press";
    case "release":
      return "Release";
    case "double_press":
      return "Double press";
    case "move":
      return "Move";
  }
}

/** gestureHint gives the binding editor's timing callout for a
 * gesture, when one is worth showing -- undefined for gestures with no
 * timing subtlety (turn, move). doubleBound/holdBound tell it whether
 * the *other* gesture that shares this control's press is actually
 * bound, since a press with nothing double-bound never waits (see
 * daemon/internal/engine/gesture.go's deferPress) and a long press with
 * nothing hold/release-bound never turns into a hold (the same
 * detectHold refinement). */
export function gestureHint(gesture: Gesture, opts: { doubleBound: boolean; holdBound: boolean }): string | undefined {
  switch (gesture) {
    case "press":
      return opts.doubleBound ? "Waits up to 350 ms to rule out a double press." : undefined;
    case "hold":
      return "Hold for 600 ms. Release sooner and it's a normal press instead.";
    case "release":
      return opts.holdBound ? "Fires when you let go after a long press." : undefined;
    case "double_press":
      return "Two presses within 350 ms.";
    case "turn":
    case "move":
      return undefined;
  }
}
