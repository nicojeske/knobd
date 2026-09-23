import type { State } from "../../api/client";
import type { ControlKind } from "../../device/layout";
import { controlLiveInfo } from "./controlState";

const PAD_SIZE = 36;

/** Pad renders one grid or side button. Its LED mirrors
 * daemon/internal/engine/led.go's own rule exactly: only a
 * volume.mute_toggle binding drives a button's LED (lit when NOT
 * muted, matching the daemon's ledButtonUpdate), any other action type
 * bound to a button leaves it visually off, even though the control is
 * bound -- inventing a lit state the real hardware wouldn't show would
 * be lying about what the device actually does. */
export function Pad({
  kind,
  index,
  cx,
  cy,
  label,
  state,
  onSelect,
}: {
  kind: ControlKind;
  index: number;
  cx: number;
  cy: number;
  label: string;
  state: State | undefined;
  onSelect: (kind: ControlKind, index: number) => void;
}) {
  const info = controlLiveInfo(state, kind, index);

  const lit = info.tier === "live" && info.actionType === "volume.mute_toggle" && info.muted === false;
  const fill = info.tier === "unbound" ? "none" : lit ? "var(--ctl-live)" : "var(--ctl-bound)";
  const stroke = info.tier === "unbound" ? "var(--ctl-unbound)" : "var(--color-border)";

  const title = `${label}${info.actionType ? ` — ${info.actionType}` : " — unbound"}`;

  return (
    <g
      style={{ cursor: "pointer" }}
      onClick={() => {
        onSelect(kind, index);
      }}
    >
      <title>{title}</title>
      <rect
        x={cx - PAD_SIZE / 2}
        y={cy - PAD_SIZE / 2}
        width={PAD_SIZE}
        height={PAD_SIZE}
        rx={6}
        style={{ fill, stroke, strokeWidth: 1 }}
      />
      <text x={cx} y={cy + 4} textAnchor="middle" style={{ fill: "var(--color-text-muted)", fontSize: 11 }}>
        {label}
      </text>
    </g>
  );
}
