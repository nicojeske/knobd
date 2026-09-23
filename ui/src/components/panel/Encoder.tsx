import type { State } from "../../api/client";
import { RING_POSITIONS } from "../../device/layout";
import { controlLiveInfo } from "./controlState";
import { Ring } from "./Ring";

const KNOB_RADIUS = 24;

function litSegmentCount(percent: number): number {
  const raw = Math.round((percent / 100) * RING_POSITIONS);
  return Math.max(0, Math.min(RING_POSITIONS, raw));
}

/** Encoder renders one physical encoder as a single visual element
 * carrying both its turn binding (the ring) and its push binding (the
 * knob itself is the click target for encoder_push) -- see
 * device/geometry.ts's doc comment for why they share one screen
 * position. */
export function Encoder({
  index,
  cx,
  cy,
  state,
  onSelect,
}: {
  index: number;
  cx: number;
  cy: number;
  state: State | undefined;
  onSelect: (kind: "encoder" | "encoder_push", index: number) => void;
}) {
  const turnInfo = controlLiveInfo(state, "encoder", index);
  const pushInfo = controlLiveInfo(state, "encoder_push", index);

  const litCount =
    turnInfo.tier === "live" && turnInfo.volumePercent !== undefined ? litSegmentCount(turnInfo.volumePercent) : 0;

  const knobFill =
    turnInfo.tier === "live" || pushInfo.tier === "live"
      ? "var(--ctl-live)"
      : turnInfo.tier === "bound" || pushInfo.tier === "bound"
        ? "var(--ctl-bound)"
        : "var(--ctl-unbound)";

  const label = `Encoder ${index}${turnInfo.actionType ? ` — turn: ${turnInfo.actionType}` : ""}${pushInfo.actionType ? ` — push: ${pushInfo.actionType}` : ""}`;

  return (
    <g
      style={{ cursor: "pointer" }}
      onClick={() => {
        onSelect("encoder", index);
      }}
    >
      <title>{label}</title>
      <Ring
        cx={cx}
        cy={cy}
        litCount={litCount}
        totalCount={RING_POSITIONS}
        litColor="var(--ctl-live-fill)"
        dimColor="var(--ctl-unbound)"
      />
      <circle
        cx={cx}
        cy={cy}
        r={KNOB_RADIUS}
        style={{ fill: knobFill, stroke: "var(--color-border)", strokeWidth: 1 }}
      />
      <text
        x={cx}
        y={cy + KNOB_RADIUS + 16}
        textAnchor="middle"
        style={{ fill: "var(--color-text-muted)", fontSize: 11 }}
      >
        E{index}
      </text>
    </g>
  );
}
