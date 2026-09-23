// Screen geometry for the visual panel: where each control sits, in a
// 0-900 x 0-460 coordinate space matching the X-Touch Mini's real
// layout (8 encoders across the top, a 2x8 button grid below them, 2
// side buttons, 1 fader). Hand-written -- nothing in the daemon knows
// where a control belongs on a picture of it -- but built from loops
// rather than 35 typed-out rows, and checked for completeness against
// device/layout.ts's generated ranges by layout.test.ts.
import { indexRange, type ControlKind } from "./layout";

export type ControlShape = "knob" | "pad" | "fader";

export interface ControlGeometry {
  readonly kind: ControlKind;
  readonly index: number;
  readonly cx: number;
  readonly cy: number;
  readonly shape: ControlShape;
  readonly label: string;
}

const ENCODER_Y = 90;
const BUTTON_ROW_1_Y = 230;
const BUTTON_ROW_2_Y = 320;
const COLUMN_START_X = 80;
const COLUMN_STEP_X = 100;

function column(index1Based: number): number {
  return COLUMN_START_X + (index1Based - 1) * COLUMN_STEP_X;
}

function encoders(): ControlGeometry[] {
  return Array.from({ length: 8 }, (_, i) => {
    const index = i + 1;
    return { kind: "encoder", index, cx: column(index), cy: ENCODER_Y, shape: "knob", label: `E${index}` };
  });
}

// encoder_push shares its encoder's screen position -- it's a press on
// the same physical knob, not a separate location -- so the panel
// renders one visual element per encoder that carries both a turn
// binding (the ring) and a push binding (a center click target).
function encoderPushes(): ControlGeometry[] {
  return Array.from({ length: 8 }, (_, i) => {
    const index = i + 1;
    return { kind: "encoder_push", index, cx: column(index), cy: ENCODER_Y, shape: "knob", label: `E${index}` };
  });
}

function buttons(): ControlGeometry[] {
  const topRow = Array.from({ length: 8 }, (_, i) => {
    const index = i + 1;
    return {
      kind: "button" as const,
      index,
      cx: column(index),
      cy: BUTTON_ROW_1_Y,
      shape: "pad" as const,
      label: `${index}`,
    };
  });
  const bottomRow = Array.from({ length: 8 }, (_, i) => {
    const index = i + 9;
    return {
      kind: "button" as const,
      index,
      cx: column(i + 1),
      cy: BUTTON_ROW_2_Y,
      shape: "pad" as const,
      label: `${index}`,
    };
  });
  return [...topRow, ...bottomRow];
}

function sideButtons(): ControlGeometry[] {
  return [
    { kind: "side_button", index: 1, cx: 30, cy: BUTTON_ROW_1_Y, shape: "pad", label: "S1" },
    { kind: "side_button", index: 2, cx: 30, cy: BUTTON_ROW_2_Y, shape: "pad", label: "S2" },
  ];
}

function fader(): ControlGeometry[] {
  return [{ kind: "fader", index: 1, cx: 860, cy: 275, shape: "fader", label: "Fader" }];
}

export const LAYOUT: readonly ControlGeometry[] = [
  ...encoders(),
  ...encoderPushes(),
  ...buttons(),
  ...sideButtons(),
  ...fader(),
];

export function geometryFor(kind: ControlKind, index: number): ControlGeometry | undefined {
  return LAYOUT.find((g) => g.kind === kind && g.index === index);
}

/** encoderGeometry is geometryFor for the "encoder" kind specifically --
 * the panel's per-encoder component needs one shared position for its
 * ring (kind "encoder") and its push target (kind "encoder_push"), and
 * both are guaranteed (by construction, and checked by layout.test.ts)
 * to be identical. */
export function encoderIndices(): readonly number[] {
  const { min, max } = indexRange("encoder");
  return Array.from({ length: max - min + 1 }, (_, i) => min + i);
}
