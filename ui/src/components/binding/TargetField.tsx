import { useState } from "react";

import { makeTarget, targetNeedsRef, TARGET_KINDS, type TargetKind } from "../../config/target";
import { useConfig } from "../../state/ConfigContext";
import { useAudioGraph } from "../../state/useAudioGraph";
import type { Target } from "../../types/config";
import styles from "./Field.module.css";

const FREE_TEXT = "__free_text__";

/** TargetField offers the 8 target kinds, and for the ones that need a
 * ref, a live picker: sink/source list from GET /audio, app/group list
 * from the configured AppMatchers/AppGroups -- falling back to free
 * text for a device that isn't plugged in right now (Config.Validate
 * can't check that; the daemon just won't resolve it until it is). */
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
  const { config } = useConfig();
  const { graph } = useAudioGraph();
  const [freeText, setFreeText] = useState(false);

  const options: readonly { id: string; label: string }[] =
    value.kind === "sink"
      ? (graph?.sinks.map((d) => ({ id: d.id, label: d.description || d.id })) ?? [])
      : value.kind === "source"
        ? (graph?.sources.map((d) => ({ id: d.id, label: d.description || d.id })) ?? [])
        : value.kind === "app"
          ? (config?.appMatchers.map((m) => ({ id: m.id, label: m.displayName || m.id })) ?? [])
          : value.kind === "group"
            ? (config?.appGroups.map((g) => ({ id: g.id, label: g.displayName || g.id })) ?? [])
            : [];

  const currentRef = value.ref ?? "";
  const knownOption = options.some((o) => o.id === currentRef);
  const showFreeText = freeText || (currentRef !== "" && !knownOption);

  return (
    <div className={styles.row}>
      <label>{label}</label>
      <select
        value={value.kind}
        onChange={(e) => {
          setFreeText(false);
          onChange(makeTarget(e.target.value as TargetKind, undefined));
        }}
      >
        {TARGET_KINDS.map((kind) => (
          <option key={kind} value={kind}>
            {kind}
          </option>
        ))}
      </select>

      {needsRef ? (
        showFreeText ? (
          <input
            type="text"
            value={currentRef}
            placeholder="node name, matcher id, or group id"
            onChange={(e) => {
              onChange(makeTarget(value.kind, e.target.value));
            }}
          />
        ) : (
          <select
            value={knownOption ? currentRef : ""}
            onChange={(e) => {
              if (e.target.value === FREE_TEXT) {
                setFreeText(true);
                return;
              }
              onChange(makeTarget(value.kind, e.target.value));
            }}
          >
            <option value="" disabled>
              {options.length === 0 ? "(none available)" : "Choose…"}
            </option>
            {options.map((o) => (
              <option key={o.id} value={o.id}>
                {o.label}
              </option>
            ))}
            <option value={FREE_TEXT}>Type manually…</option>
          </select>
        )
      ) : null}
    </div>
  );
}
