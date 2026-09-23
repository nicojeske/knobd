import type { State } from "../../api/client";
import { controlLiveInfo } from "./controlState";

const TRACK_HEIGHT = 160;
const TRACK_WIDTH = 10;
const THUMB_WIDTH = 34;
const THUMB_HEIGHT = 14;

/** Fader has no LED on the real hardware (see
 * specs/reference/xtouch-mini-midi-map.md) -- unlike Encoder/Pad, this
 * never renders a lit affordance, only the thumb's position, so it
 * never teaches the user something false about the device. */
export function Fader({
  cx,
  cy,
  state,
  onSelect,
}: {
  cx: number;
  cy: number;
  state: State | undefined;
  onSelect: (kind: "fader", index: number) => void;
}) {
  const info = controlLiveInfo(state, "fader", 1);
  const percent = info.tier === "live" && info.volumePercent !== undefined ? info.volumePercent : 0;
  const clamped = Math.max(0, Math.min(100, percent));

  const trackTop = cy - TRACK_HEIGHT / 2;
  const thumbY = trackTop + TRACK_HEIGHT * (1 - clamped / 100);

  const trackColor = info.tier === "unbound" ? "var(--ctl-unbound)" : "var(--color-border)";
  const thumbColor =
    info.tier === "live" ? "var(--ctl-live)" : info.tier === "bound" ? "var(--ctl-bound)" : "var(--ctl-unbound)";

  const title = `Fader${info.actionType ? ` — ${info.actionType}` : " — unbound"}`;

  return (
    <g
      style={{ cursor: "pointer" }}
      onClick={() => {
        onSelect("fader", 1);
      }}
    >
      <title>{title}</title>
      <rect
        x={cx - TRACK_WIDTH / 2}
        y={trackTop}
        width={TRACK_WIDTH}
        height={TRACK_HEIGHT}
        rx={4}
        style={{ fill: trackColor }}
      />
      <rect
        x={cx - THUMB_WIDTH / 2}
        y={thumbY - THUMB_HEIGHT / 2}
        width={THUMB_WIDTH}
        height={THUMB_HEIGHT}
        rx={3}
        style={{ fill: thumbColor, stroke: "var(--color-border)", strokeWidth: 1 }}
      />
      <text
        x={cx}
        y={trackTop + TRACK_HEIGHT + 20}
        textAnchor="middle"
        style={{ fill: "var(--color-text-muted)", fontSize: 11 }}
      >
        Fader
      </text>
    </g>
  );
}
