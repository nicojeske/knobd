import { useEffect, useState } from "react";

import styles from "./LearnOverlay.module.css";

/** LearnOverlay covers the panel while MIDI learn is armed. The
 * "won't do anything" line is deliberate: dispatch suppression during
 * learn (see daemon/internal/engine/learn.go) is user-visible behavior,
 * and hiding it would look like a bug rather than what it is. */
export function LearnOverlay({ expiresAt, onCancel }: { expiresAt: Date | undefined; onCancel: () => void }) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const id = window.setInterval(() => {
      setNow(Date.now());
    }, 200);
    return () => {
      window.clearInterval(id);
    };
  }, []);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") onCancel();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [onCancel]);

  const remainingMs = expiresAt ? expiresAt.getTime() - now : 0;
  const remainingSeconds = Math.max(0, Math.ceil(remainingMs / 1000));

  return (
    <div className={styles.overlay} onClick={onCancel} role="dialog" aria-modal="true" aria-label="Learning MIDI input">
      <div className={styles.title}>Touch the control you mean…</div>
      <div className={styles.countdown}>{remainingSeconds}s</div>
      <div className={styles.hint}>Your controller won't do anything else while learning.</div>
      <button
        type="button"
        className={styles.cancel}
        onClick={(e) => {
          e.stopPropagation();
          onCancel();
        }}
      >
        Cancel
      </button>
    </div>
  );
}
