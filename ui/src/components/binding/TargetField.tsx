import { makeTarget, targetNeedsRef, TARGET_KINDS, type TargetKind } from "../../config/target";
import type { Target } from "../../types/config";
import styles from "./Field.module.css";

/** TargetField offers the 8 target kinds and, for the ones that need
 * one, a free-text ref. Live pickers backed by GET /audio (choosing
 * from actually-running sinks/sources/apps, and creating an app matcher
 * from a running stream) land in a later commit of this milestone --
 * this is the minimal version that already lets every target kind be
 * bound correctly. */
export function TargetField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: Target;
  onChange: (next: Target) => void;
}) {
  const needsRef = targetNeedsRef(value.kind);

  return (
    <div className={styles.row}>
      <label>{label}</label>
      <select
        value={value.kind}
        onChange={(e) => {
          onChange(makeTarget(e.target.value as TargetKind, value.ref));
        }}
      >
        {TARGET_KINDS.map((kind) => (
          <option key={kind} value={kind}>
            {kind}
          </option>
        ))}
      </select>
      {needsRef ? (
        <input
          type="text"
          value={value.ref ?? ""}
          placeholder="node name, app matcher id, or group id"
          onChange={(e) => {
            onChange(makeTarget(value.kind, e.target.value));
          }}
        />
      ) : null}
    </div>
  );
}
