// Renders an encoder's LED ring as a set of radial ticks. This is a
// proportional UI approximation of device.EncodeLED's mode-2 ("fill")
// encoding, not a byte-for-byte reproduction of it: the real ring lights
// physical LEDs 2 through position+1 of 13 segments (11 addressable);
// here, litCount of totalCount ticks are lit, which reads the same to a
// user (a ring that's roughly N/11 full) without needing to replicate
// the exact segment-numbering offset. See
// specs/reference/xtouch-mini-midi-map.md's LED section for the real
// encoding.
const SWEEP_DEG = 270;
const INNER_RADIUS = 30;
const OUTER_RADIUS = 38;
const TICK_WIDTH = 3;

export function Ring({
  cx,
  cy,
  litCount,
  totalCount,
  litColor,
  dimColor,
}: {
  cx: number;
  cy: number;
  litCount: number;
  totalCount: number;
  litColor: string;
  dimColor: string;
}) {
  const start = -SWEEP_DEG / 2;
  const step = totalCount > 1 ? SWEEP_DEG / (totalCount - 1) : 0;

  const ticks = Array.from({ length: totalCount }, (_, i) => {
    const angle = ((start + step * i) * Math.PI) / 180;
    const x1 = cx + INNER_RADIUS * Math.sin(angle);
    const y1 = cy - INNER_RADIUS * Math.cos(angle);
    const x2 = cx + OUTER_RADIUS * Math.sin(angle);
    const y2 = cy - OUTER_RADIUS * Math.cos(angle);
    return (
      <line
        key={i}
        x1={x1}
        y1={y1}
        x2={x2}
        y2={y2}
        stroke={i < litCount ? litColor : dimColor}
        strokeWidth={TICK_WIDTH}
        strokeLinecap="round"
      />
    );
  });

  return <g>{ticks}</g>;
}
