import type { ReactNode } from "react";

import { cx } from "../../lib/cx";
import styles from "./Dialog.module.css";

/** Dialog is a plain modal shell: a dimmed backdrop (click to cancel)
 * and a centered panel. No portal, no focus trap library -- this is a
 * small desktop app with a handful of dialogs, not a component library. */
export function Dialog({
  title,
  onClose,
  children,
  size = "default",
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  /** size widens the panel for content that needs more horizontal room
   * (the binding editor's two-pane gesture layout); every other caller
   * omits it and keeps today's width. */
  size?: "default" | "wide";
}) {
  return (
    <div className={styles.backdrop} onClick={onClose}>
      <div
        className={cx(styles.panel, size === "wide" && styles.panelWide)}
        onClick={(e) => {
          e.stopPropagation();
        }}
        role="dialog"
        aria-modal="true"
        aria-label={title}
      >
        <div className={styles.header}>
          <h2 className={styles.title}>{title}</h2>
          <button type="button" className={styles.close} onClick={onClose} aria-label="Close">
            ×
          </button>
        </div>
        <div className={styles.body}>{children}</div>
      </div>
    </div>
  );
}
