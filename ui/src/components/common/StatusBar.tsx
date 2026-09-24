import { cx } from "../../lib/cx";
import { useConnection } from "../../state/ConnectionContext";
import styles from "./StatusBar.module.css";

function dotClass(ok: boolean | "warn"): string {
  if (ok === "warn") return cx(styles.dot, styles.warn);
  return cx(styles.dot, ok ? styles.ok : styles.bad);
}

/** StatusBar summarizes device/audio/focus/profile/learn status from
 * the polled (soon: pushed) GET /state snapshot. Always visible: it's
 * the one place a user can tell "is knobd actually working right now"
 * regardless of which view they're on. */
export function StatusBar() {
  const { status, state, error } = useConnection();

  if (status === "unreachable") {
    return (
      <div className={styles.bar}>
        <span className={dotClass(false)} />
        <span>knobd isn't running{error ? ` (${error})` : ""}</span>
      </div>
    );
  }

  if (!state) {
    return (
      <div className={styles.bar}>
        <span className={dotClass("warn")} />
        <span>Connecting…</span>
      </div>
    );
  }

  const { device, audio, focus, profile, learn } = state;

  return (
    <div className={styles.bar}>
      <span className={styles.item}>
        <span className={dotClass(device.connected)} />
        {device.connected ? (device.name ?? "Controller") : "Controller disconnected"}
      </span>
      <span className={styles.item}>
        <span className={dotClass(audio.connected)} />
        {audio.connected ? "Audio" : "Audio disconnected"}
      </span>
      <span className={styles.item}>
        <span className={dotClass(focus.available ? true : "warn")} />
        {focus.available ? (focus.resourceClass ?? "Focus tracking") : "Focus tracking unavailable"}
      </span>
      <span className={styles.spacer} />
      {learn.active ? <span className={styles.item}>Learning…</span> : null}
      <span className={styles.item}>{profile.activeProfileId || "no profile"}</span>
      {profile.activeLayer !== 0 ? <span className={styles.item}>layer {profile.activeLayer}</span> : null}
    </div>
  );
}
