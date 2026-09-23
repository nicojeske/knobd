import { useMemo, useState } from "react";

import { isBridgeError, saveConfig } from "../../api/client";
import { ACTION_SPECS, actionTypes, type ActionType } from "../../actions/specs";
import { findBinding, removeBinding, upsertBinding } from "../../config/bindings";
import type { ControlKind, Gesture } from "../../device/layout";
import { supportedGestures } from "../../device/layout";
import { useCapabilities } from "../../state/CapabilitiesContext";
import { useConfig } from "../../state/ConfigContext";
import type { Action, Binding } from "../../types/config";
import { ActionParamsForm } from "./ActionParamsForm";
import styles from "./BindingEditor.module.css";
import { Dialog } from "../common/Dialog";

// Layers aren't reachable at runtime until M08 (GET /capabilities
// reports features.layers: false) -- everything in this editor targets
// layer 0, matching ProfileState.activeLayer's own "always 0 until M08."
const LAYER = 0;

interface GestureOption {
  kind: ControlKind;
  gesture: Gesture;
  label: string;
}

/** gestureOptions lists every (kind, gesture) this physical control can
 * be bound on. For an encoder this spans TWO data-model kinds sharing
 * one screen position (see device/geometry.ts): "encoder" (turn) and
 * "encoder_push" (press/hold/release/double_press) -- from the user's
 * point of view it's one knob with several gestures, so the editor
 * presents them as one list rather than requiring a separate click
 * target for the push. */
function gestureOptions(primaryKind: ControlKind): GestureOption[] {
  if (primaryKind === "encoder" || primaryKind === "encoder_push") {
    return [
      ...supportedGestures("encoder").map((gesture) => ({ kind: "encoder" as const, gesture, label: "Turn" })),
      ...supportedGestures("encoder_push").map((gesture) => ({
        kind: "encoder_push" as const,
        gesture,
        label: `Push: ${gesture}`,
      })),
    ];
  }
  return supportedGestures(primaryKind).map((gesture) => ({ kind: primaryKind, gesture, label: gesture }));
}

function buildAction(type: ActionType): Action {
  const spec = ACTION_SPECS[type];
  // The cast here is confined and sound: ACTION_SPECS is a mapped type
  // over ActionType, so spec.defaults() for a given `type` always
  // produces exactly that type's params -- but Action's own definition
  // (a discriminated union generated from config.schema.json) can't
  // express "params matches whichever type is bound at this call site"
  // without this cast at the boundary between the two.
  return { type, params: spec.defaults() } as Action;
}

export function BindingEditor({
  kind: primaryKind,
  index,
  preferredGesture,
  onClose,
}: {
  kind: ControlKind;
  index: number;
  /** preferredGesture pre-selects a gesture when the editor opens --
   * used by MIDI learn to seed the gesture picker with the physically-
   * observed action (turn/move/press) without forcing it: learn
   * deliberately doesn't run the gesture machine (see
   * daemon/internal/engine/learn.go), so this is a starting point the
   * user can still change, not a claim about what they meant.
   * Required-but-nullable, not `?:`: Panel.tsx passes this from its own
   * `Selection` state, which is `Gesture | undefined` under
   * exactOptionalPropertyTypes -- an `?:` prop here would reject that
   * value explicitly passed. */
  preferredGesture: Gesture | undefined;
  onClose: () => void;
}) {
  const { config, reload } = useConfig();
  const { capabilities } = useCapabilities();

  const options = useMemo(() => gestureOptions(primaryKind), [primaryKind]);
  const preferredOption = preferredGesture ? options.find((o) => o.gesture === preferredGesture) : undefined;
  const firstOption = preferredOption ?? options[0];
  const [selectionKey, setSelectionKey] = useState<string>(
    firstOption ? `${firstOption.kind}:${firstOption.gesture}` : "",
  );
  const selection = options.find((o) => `${o.kind}:${o.gesture}` === selectionKey) ?? firstOption;

  const profile = config?.profiles.find((p) => p.id === config.activeProfileId);
  const existing =
    profile && selection
      ? findBinding(profile.bindings, LAYER, { kind: selection.kind, index }, selection.gesture)
      : undefined;

  const [action, setAction] = useState<Action | undefined>(existing?.action);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  // Re-derive the working action whenever the selected gesture changes
  // to a different existing (or empty) binding -- keyed by selection so
  // switching back and forth doesn't clobber in-progress edits.
  const activeAction = action ?? existing?.action;

  function selectOption(key: string) {
    setSelectionKey(key);
    const next = options.find((o) => `${o.kind}:${o.gesture}` === key);
    const nextExisting =
      profile && next ? findBinding(profile.bindings, LAYER, { kind: next.kind, index }, next.gesture) : undefined;
    setAction(nextExisting?.action);
  }

  function selectActionType(type: ActionType) {
    setAction(buildAction(type));
  }

  async function handleSave() {
    if (!config || !profile || !activeAction || !selection) return;
    setSaving(true);
    setError(undefined);
    try {
      const binding: Binding = {
        layer: LAYER,
        control: { kind: selection.kind, index },
        gesture: selection.gesture,
        action: activeAction,
      };
      const nextProfiles = config.profiles.map((p) =>
        p.id === profile.id ? { ...p, bindings: upsertBinding(p.bindings, binding) } : p,
      );
      await saveConfig({ ...config, profiles: nextProfiles });
      await reload();
      onClose();
    } catch (err) {
      setError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  async function handleDelete() {
    if (!config || !profile || !selection) return;
    setSaving(true);
    setError(undefined);
    try {
      const nextProfiles = config.profiles.map((p) =>
        p.id === profile.id
          ? { ...p, bindings: removeBinding(p.bindings, LAYER, { kind: selection.kind, index }, selection.gesture) }
          : p,
      );
      await saveConfig({ ...config, profiles: nextProfiles });
      await reload();
      onClose();
    } catch (err) {
      setError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  if (!config || !profile) {
    return (
      <Dialog title={`${primaryKind} ${index}`} onClose={onClose}>
        <p>Loading configuration…</p>
      </Dialog>
    );
  }

  const implemented = new Set(capabilities?.implementedActions ?? []);

  return (
    <Dialog title={`${primaryKind} ${index}`} onClose={onClose}>
      <div className={styles.row}>
        <label>Gesture</label>
        <select
          value={selectionKey}
          onChange={(e) => {
            selectOption(e.target.value);
          }}
        >
          {options.map((o) => (
            <option key={`${o.kind}:${o.gesture}`} value={`${o.kind}:${o.gesture}`}>
              {o.label}
            </option>
          ))}
        </select>
      </div>

      <div className={styles.row}>
        <label>Action</label>
        <select
          value={activeAction?.type ?? ""}
          onChange={(e) => {
            selectActionType(e.target.value as ActionType);
          }}
        >
          <option value="" disabled>
            {existing ? "(bound)" : "Choose an action…"}
          </option>
          {actionTypes().map((type) => (
            <option key={type} value={type}>
              {ACTION_SPECS[type].label}
              {implemented.has(type) ? "" : " (not implemented yet)"}
            </option>
          ))}
        </select>
      </div>

      {activeAction && !implemented.has(activeAction.type) ? (
        <div className={styles.note}>
          This daemon build has no handler for "{activeAction.type}" yet -- the binding will be saved, but won't do
          anything until a future milestone registers one.
        </div>
      ) : null}
      {activeAction && ACTION_SPECS[activeAction.type].note ? (
        <div className={styles.note}>{ACTION_SPECS[activeAction.type].note}</div>
      ) : null}
      {activeAction && "target" in activeAction.params && activeAction.params.target.kind === "group" ? (
        <div className={styles.note}>Group targets are accepted here but don't resolve at runtime until M08.</div>
      ) : null}

      {activeAction ? (
        <ActionParamsForm
          type={activeAction.type}
          params={activeAction.params}
          onChange={(nextParams) => {
            setAction({ type: activeAction.type, params: nextParams } as Action);
          }}
        />
      ) : null}

      {error ? <div className={styles.error}>{error}</div> : null}

      <div className={styles.actions}>
        <div>
          {existing ? (
            <button
              type="button"
              className={`${styles.button} ${styles.buttonDanger}`}
              disabled={saving}
              onClick={() => void handleDelete()}
            >
              Remove binding
            </button>
          ) : null}
        </div>
        <div className={styles.actionsRight}>
          <button type="button" className={styles.button} onClick={onClose} disabled={saving}>
            Cancel
          </button>
          <button
            type="button"
            className={`${styles.button} ${styles.buttonPrimary}`}
            disabled={saving || !activeAction}
            onClick={() => void handleSave()}
          >
            {saving ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
    </Dialog>
  );
}
