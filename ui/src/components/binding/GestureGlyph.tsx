import type { Gesture } from "../../device/layout";

/** GestureGlyph draws a tiny, monochrome pictogram for a gesture in the
 * binding editor's gesture list -- a quick visual anchor next to the
 * text label so the six rows read as distinct shapes at a glance, not
 * just a stack of similar-looking text. Pure inline SVG (currentColor
 * strokes/fills, no icon font/library dependency), sized to sit inline
 * with a row's text. */
export function GestureGlyph({ gesture, className }: { gesture: Gesture; className: string | undefined }) {
  const common = { width: 18, height: 18, viewBox: "0 0 18 18", className, "aria-hidden": true } as const;

  switch (gesture) {
    case "turn":
      return (
        <svg {...common}>
          <path d="M4 6.5A6 6 0 0 1 14.5 5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
          <path d="M14.5 5 L14.5 2 L17 4.5 Z" fill="currentColor" />
          <path
            d="M14 11.5A6 6 0 0 1 3.5 13"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
          />
          <path d="M3.5 13 L3.5 16 L1 13.5 Z" fill="currentColor" />
        </svg>
      );
    case "press":
      return (
        <svg {...common}>
          <circle cx="9" cy="9" r="4.5" fill="currentColor" />
        </svg>
      );
    case "hold":
      return (
        <svg {...common}>
          <circle cx="9" cy="9" r="3.5" fill="currentColor" />
          <circle cx="9" cy="9" r="7" fill="none" stroke="currentColor" strokeWidth="1.4" strokeDasharray="3 2.5" />
        </svg>
      );
    case "release":
      return (
        <svg {...common}>
          <circle cx="9" cy="9" r="3.5" fill="none" stroke="currentColor" strokeWidth="1.4" />
          <path
            d="M9 1.5 L9 5.5 M6.5 4 L9 1.5 L11.5 4"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.4"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      );
    case "double_press":
      return (
        <svg {...common}>
          <circle cx="6" cy="9" r="3.25" fill="currentColor" />
          <circle cx="13" cy="9" r="3.25" fill="currentColor" opacity="0.55" />
        </svg>
      );
    case "move":
      return (
        <svg {...common}>
          <rect x="3" y="4" width="12" height="10" rx="2" fill="none" stroke="currentColor" strokeWidth="1.4" />
          <rect x="5.5" y="10" width="7" height="2" rx="1" fill="currentColor" />
        </svg>
      );
  }
}
