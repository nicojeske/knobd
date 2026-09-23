import { listen } from "@tauri-apps/api/event";
import { useEffect, useState } from "react";

import { cancelLearn, isBridgeError, startLearn, type DaemonEvent } from "../../api/client";
import { LAYOUT } from "../../device/geometry";
import type { ControlKind, Gesture } from "../../device/layout";
import { useCapabilities } from "../../state/CapabilitiesContext";
import { useConnection } from "../../state/ConnectionContext";
import { BindingEditor } from "../binding/BindingEditor";
import { Encoder } from "./Encoder";
import { Fader } from "./Fader";
import { LearnOverlay } from "./LearnOverlay";
import styles from "./Panel.module.css";
import { Pad } from "./Pad";

interface Selection {
  kind: ControlKind;
  index: number;
  preferredGesture: Gesture | undefined;
}

/** Panel is the visual X-Touch Mini: one SVG element built from
 * device/geometry.ts's LAYOUT, with each control's fill state mirroring
 * the live GET /state snapshot (see components/panel/controlState.ts).
 * encoder_push entries are skipped here -- Encoder renders both the
 * turn ring and the push target for one physical knob, since they share
 * a screen position (see device/geometry.ts and BindingEditor's own
 * gestureOptions, which spans both kinds for one dialog).
 *
 * MIDI learn: clicking "Learn" arms it (POST /learn); the overlay is
 * driven by this component's own local state -- seeded immediately from
 * the command's own response, not by waiting on the live push -- so it
 * appears the instant the click is handled rather than one push-channel
 * round trip later. A learn_input event captures the touched control
 * and opens the binding editor on it directly. */
export function Panel() {
  const { state } = useConnection();
  const { capabilities } = useCapabilities();
  const [selected, setSelected] = useState<Selection | undefined>(undefined);
  const [learnExpiresAt, setLearnExpiresAt] = useState<Date | undefined>(undefined);
  const [learnError, setLearnError] = useState<string | undefined>(undefined);

  const learning = learnExpiresAt !== undefined;

  function handleSelect(kind: ControlKind, index: number) {
    setSelected({ kind, index, preferredGesture: undefined });
  }

  async function handleStartLearn() {
    setLearnError(undefined);
    try {
      const result = await startLearn();
      setLearnExpiresAt(result.expiresAt ? new Date(result.expiresAt) : new Date(Date.now() + 15000));
    } catch (err) {
      setLearnError(isBridgeError(err) ? err.message : String(err));
    }
  }

  function handleCancelLearn() {
    setLearnExpiresAt(undefined);
    void cancelLearn();
  }

  // Client-side backstop: if learn_input never arrives (nothing was
  // touched), clear the overlay once its own deadline passes -- the
  // daemon disarms itself server-side regardless; this only affects
  // what the UI shows.
  useEffect(() => {
    if (!learnExpiresAt) return;
    const ms = learnExpiresAt.getTime() - Date.now();
    const id = window.setTimeout(
      () => {
        setLearnExpiresAt(undefined);
      },
      Math.max(0, ms),
    );
    return () => {
      window.clearTimeout(id);
    };
  }, [learnExpiresAt]);

  // While armed, listen for the captured control and open the binding
  // editor on it -- learn is one-shot (see engine/learn.go), so one
  // capture always ends the overlay.
  useEffect(() => {
    if (!learning) return;
    let cancelled = false;
    let unlisten: (() => void) | undefined;

    void listen<DaemonEvent>("knobd:event", (event) => {
      const payload = event.payload;
      if (payload.type === "learn_input" && payload.input) {
        const { control, suggestedGesture } = payload.input;
        setLearnExpiresAt(undefined);
        setSelected({ kind: control.kind, index: control.index, preferredGesture: suggestedGesture });
      }
    }).then((fn) => {
      if (cancelled) {
        fn();
        return;
      }
      unlisten = fn;
    });

    return () => {
      cancelled = true;
      unlisten?.();
    };
  }, [learning]);

  // Best-effort: leaving the panel view while learn is armed disarms it
  // rather than leaving the controller's normal behavior suppressed
  // until the daemon's own timeout catches up.
  useEffect(() => {
    return () => {
      if (learning) void cancelLearn();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- intentionally only on unmount
  }, []);

  const learnDisabledReason = !state?.device.connected
    ? "Controller disconnected"
    : capabilities && !capabilities.features.learn
      ? "Learn isn't available in this daemon build"
      : undefined;

  return (
    <div className={styles.wrap}>
      <div className={styles.toolbar}>
        <button
          type="button"
          className={styles.learnButton}
          disabled={learning || Boolean(learnDisabledReason)}
          title={learnDisabledReason}
          onClick={() => void handleStartLearn()}
        >
          Learn
        </button>
        {learnError ? <span className={styles.learnError}>{learnError}</span> : null}
      </div>
      <div className={styles.svgWrap}>
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
        {learning ? <LearnOverlay expiresAt={learnExpiresAt} onCancel={handleCancelLearn} /> : null}
      </div>
      {selected ? (
        <BindingEditor
          kind={selected.kind}
          index={selected.index}
          preferredGesture={selected.preferredGesture}
          onClose={() => {
            setSelected(undefined);
          }}
        />
      ) : null}
    </div>
  );
}
