import type { ReactNode } from "react";

import styles from "./Banner.module.css";

export interface BannerAction {
  label: string;
  onClick: () => void;
}

/** Banner is a non-blocking, dismissable-by-action strip -- used for
 * "knobd changed the configuration" (config_changed, see M07's save
 * semantics) and similar notices that must never steal focus from
 * whatever the user is doing. Never a modal: a dialog the user didn't
 * ask for is exactly the kind of interruption this is designed to
 * avoid. */
export function Banner({ children, actions }: { children: ReactNode; actions: readonly BannerAction[] }) {
  return (
    <div className={styles.banner} role="status">
      <span className={styles.message}>{children}</span>
      <span className={styles.actions}>
        {actions.map((action) => (
          <button key={action.label} type="button" onClick={action.onClick}>
            {action.label}
          </button>
        ))}
      </span>
    </div>
  );
}
