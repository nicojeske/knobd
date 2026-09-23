import { LAYOUT } from "../../device/geometry";
import type { ControlKind } from "../../device/layout";
import { useConnection } from "../../state/ConnectionContext";
import { Encoder } from "./Encoder";
import { Fader } from "./Fader";
import styles from "./Panel.module.css";
import { Pad } from "./Pad";

/** Panel is the visual X-Touch Mini: one SVG element built from
 * device/geometry.ts's LAYOUT, with each control's fill state mirroring
 * the live GET /state snapshot (see components/panel/controlState.ts).
 * encoder_push entries are skipped here -- Encoder renders both the
 * turn ring and the push target for one physical knob, since they share
 * a screen position (see device/geometry.ts). */
export function Panel() {
  const { state } = useConnection();

  function handleSelect(kind: ControlKind, index: number) {
    // The binding editor lands in a later commit of this milestone;
    // this is a placeholder hook point until it exists.

    console.info(`panel: selected ${kind} ${index}`);
  }

  return (
    <div className={styles.wrap}>
      <svg viewBox="0 0 900 400" className={styles.svg} role="img" aria-label="X-Touch Mini control panel">
        {LAYOUT.filter((g) => g.kind !== "encoder_push").map((g) => {
          if (g.kind === "encoder") {
            return (
              <Encoder
                key={`${g.kind}-${g.index}`}
                index={g.index}
                cx={g.cx}
                cy={g.cy}
                state={state}
                onSelect={handleSelect}
              />
            );
          }
          if (g.kind === "fader") {
            return <Fader key={`${g.kind}-${g.index}`} cx={g.cx} cy={g.cy} state={state} onSelect={handleSelect} />;
          }
          return (
            <Pad
              key={`${g.kind}-${g.index}`}
              kind={g.kind}
              index={g.index}
              cx={g.cx}
              cy={g.cy}
              label={g.label}
              state={state}
              onSelect={handleSelect}
            />
          );
        })}
      </svg>
    </div>
  );
}
