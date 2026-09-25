import { useMemo, useState } from "react";

import { isBridgeError, saveConfig } from "../../api/client";
import { ACTION_SPECS, actionTypes, type ActionGroup, type ActionType } from "../../actions/specs";
import { applyGestureDrafts, findBinding, type GestureDraft } from "../../config/bindings";
import type { ControlKind, Gesture } from "../../device/layout";
import { gestureHint, gestureLabel, supportedGestures } from "../../device/layout";
import { useCapabilities } from "../../state/CapabilitiesContext";
import { useConfig } from "../../state/ConfigContext";
import type { Action } from "../../types/config";
import { ActionParamsForm } from "./ActionParamsForm";
import styles from "./BindingEditor.module.css";
import { Dialog } from "../common/Dialog";
import { GestureGlyph } from "./GestureGlyph";

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
      ...supportedGestures("encoder").map((gesture) => ({
        kind: "encoder" as const,
        gesture,
        label: gestureLabel(gesture),
      })),
      ...supportedGestures("encoder_push").map((gesture) => ({
        kind: "encoder_push" as const,
        gesture,
        label: gestureLabel(gesture),
      })),
    ];
  }
  return supportedGestures(primaryKind).map((gesture) => ({
    kind: primaryKind,
    gesture,
    label: gestureLabel(gesture),
  }));
}

function optionKey(layer: number, o: GestureOption): string {
  return `${layer}|${o.kind}|${o.gesture}`;
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

const GROUP_ORDER: readonly ActionGroup[] = [
  "volume",
  "audio",
  "scene",
  "layer",
  "knob",
  "media",
  "spotify",
  "sink",
  "mic",
  "shell",
];

const GROUP_LABELS: Record<ActionGroup, string> = {
  volume: "Volume",
  audio: "Mix & duck",
  scene: "Scenes",
  layer: "Layers",
  knob: "Knob",
  media: "Media",
  spotify: "Spotify",
  sink: "System",
  mic: "Microphone",
  shell: "Shell",
};

function actionEqual(a: Action | null | undefined, b: Action | null | undefined): boolean {
  if (a == null || b == null) return (a ?? null) === (b ?? null);
  return JSON.stringify(a) === JSON.stringify(b);
}

export function BindingEditor({
  kind: primaryKind,
  index,
  preferredGesture,
  initialLayer,
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
  /** initialLayer seeds the layer selector, e.g. with whatever layer is
   * currently active on the hardware (Panel passes state.profile.
   * activeLayer) -- a starting point the user can still change, same
   * spirit as preferredGesture. */
  initialLayer: number;
  onClose: () => void;
}) {
  const { config, reload } = useConfig();
  const { capabilities } = useCapabilities();

  const [layer, setLayer] = useState(initialLayer);
  const options = useMemo(() => gestureOptions(primaryKind), [primaryKind]);
  const preferredOption = preferredGesture ? options.find((o) => o.gesture === preferredGesture) : undefined;
  const [selectionKey, setSelectionKey] = useState<string>(() => {
    const first = preferredOption ?? options[0];
    return first ? `${first.kind}:${first.gesture}` : "";
  });
  const selection = options.find((o) => `${o.kind}:${o.gesture}` === selectionKey) ?? options[0];

  // drafts holds every pending edit across every (layer, gesture) this
  // dialog session has touched, keyed by optionKey(layer, option) --
  // switching gesture or layer never discards an edit already made
  // elsewhere in the same session. A draft value of `null` means "clear
  // this gesture's binding"; absence means "unchanged from what's
  // saved."
  const [drafts, setDrafts] = useState<Map<string, Action | null>>(new Map());
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [confirmingDiscard, setConfirmingDiscard] = useState(false);

  const profile = config?.profiles.find((p) => p.id === config.activeProfileId);

  // savedActionFor/draftFor/effectiveActionFor below are pure lookups
  // against `profile`/`drafts` -- kept as plain functions (not useMemo)
  // since they're cheap and always called with the current render's
  // closures.
  function savedActionFor(l: number, o: GestureOption): Action | undefined {
    return profile ? findBinding(profile.bindings, l, { kind: o.kind, index }, o.gesture)?.action : undefined;
  }
  function draftFor(l: number, o: GestureOption): Action | null | undefined {
    return drafts.get(optionKey(l, o));
  }
  function effectiveActionFor(l: number, o: GestureOption): Action | undefined {
    const draft = draftFor(l, o);
    if (draft !== undefined) return draft ?? undefined;
    return savedActionFor(l, o);
  }
  function isModified(l: number, o: GestureOption): boolean {
    const draft = draftFor(l, o);
    if (draft === undefined) return false;
    return !actionEqual(draft, savedActionFor(l, o) ?? null);
  }

  // Not memoized: cheap (bounded by the handful of gestures one control
  // has and the handful of layers a session touches), and every draft
  // edit needs to recompute both anyway.
  let modifiedCount = 0;
  for (const o of options) {
    for (const l of new Set([layer, ...[...drafts.keys()].map((k) => Number(k.split("|")[0]))])) {
      if (isModified(l, o)) modifiedCount++;
    }
  }

  const layersInUse = (() => {
    const set = new Set<number>([0, initialLayer, layer]);
    for (const b of profile?.bindings ?? []) set.add(b.layer);
    for (const k of drafts.keys()) set.add(Number(k.split("|")[0]));
    return [...set].sort((a, b) => a - b);
  })();

  function setDraft(o: GestureOption, action: Action | null) {
    const key = optionKey(layer, o);
    setDrafts((prev) => {
      const next = new Map(prev);
      const saved = savedActionFor(layer, o) ?? null;
      if (actionEqual(action, saved)) {
        // Back to the saved value: drop the draft entirely so the
        // "modified" count stays honest instead of accumulating no-op
        // edits.
        next.delete(key);
      } else {
        next.set(key, action);
      }
      return next;
    });
  }

  function addLayer() {
    const next = (layersInUse.at(-1) ?? 0) + 1;
    setLayer(next);
  }

  async function commit() {
    if (!config || !profile) return;
    setSaving(true);
    setError(undefined);
    try {
      const byLayer = new Map<number, GestureDraft[]>();
      for (const [key, action] of drafts) {
        const [layerStr, kindStr, gestureStr] = key.split("|");
        const l = Number(layerStr);
        const list = byLayer.get(l) ?? [];
        list.push({ control: { kind: kindStr as ControlKind, index }, gesture: gestureStr as Gesture, action });
        byLayer.set(l, list);
      }
      let bindings = profile.bindings;
      for (const [l, ds] of byLayer) {
        bindings = applyGestureDrafts(bindings, l, ds);
      }
      const nextProfiles = config.profiles.map((p) => (p.id === profile.id ? { ...p, bindings } : p));
      await saveConfig({ ...config, profiles: nextProfiles });
      await reload();
      onClose();
    } catch (err) {
      setError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  function requestClose() {
    if (modifiedCount > 0 && !confirmingDiscard) {
      setConfirmingDiscard(true);
      return;
    }
    onClose();
  }

  if (!config || !profile) {
    return (
      <Dialog title={`${primaryKind} ${index}`} onClose={onClose} size="wide">
        <p>Loading configuration…</p>
      </Dialog>
    );
  }

  const implemented = new Set(capabilities?.implementedActions ?? []);
  const activeAction = selection ? effectiveActionFor(layer, selection) : undefined;
  const inheritedAction =
    selection && layer !== 0 && draftFor(layer, selection) === undefined && !savedActionFor(layer, selection)
      ? savedActionFor(0, selection)
      : undefined;
  const doubleBoundHere = options.some((o) => o.gesture === "double_press" && effectiveActionFor(layer, o));
  const holdBoundHere = options.some(
    (o) => (o.gesture === "hold" || o.gesture === "release") && effectiveActionFor(layer, o),
  );
  const hint = selection
    ? gestureHint(selection.gesture, { doubleBound: doubleBoundHere, holdBound: holdBoundHere })
    : undefined;

  const groups = new Map<ActionGroup, ActionType[]>();
  for (const t of actionTypes()) {
    const g = ACTION_SPECS[t].group;
    const list = groups.get(g) ?? [];
    list.push(t);
    groups.set(g, list);
  }

  return (
    <Dialog title={`${primaryKind} ${index}`} onClose={requestClose} size="wide">
      <div className={styles.header}>
        <span className={styles.headerLabel}>Layer</span>
        <div className={styles.layerTabs} role="tablist" aria-label="Layer">
          {layersInUse.map((l) => (
            <button
              key={l}
              type="button"
              role="tab"
              aria-selected={l === layer}
              className={l === layer ? `${styles.layerTab} ${styles.layerTabActive}` : styles.layerTab}
              onClick={() => {
                setLayer(l);
              }}
            >
              {l}
            </button>
          ))}
          <button type="button" className={styles.layerAdd} title="Add a new layer" onClick={addLayer}>
            +
          </button>
        </div>
      </div>

      <div className={styles.splitPane}>
        <div className={styles.gestureList} role="listbox" aria-label="Gesture">
          {options.map((o) => {
            const key = `${o.kind}:${o.gesture}`;
            const action = effectiveActionFor(layer, o);
            const inherited =
              layer !== 0 && draftFor(layer, o) === undefined && !savedActionFor(layer, o)
                ? savedActionFor(0, o)
                : undefined;
            const modified = isModified(layer, o);
            const selected = key === selectionKey;
            const summary = action
              ? ACTION_SPECS[action.type].label
              : inherited
                ? `from layer 0: ${ACTION_SPECS[inherited.type].label}`
                : "Add action…";
            return (
              <button
                key={key}
                type="button"
                role="option"
                aria-selected={selected}
                className={
                  selected
                    ? `${styles.gestureRow} ${styles.gestureRowSelected}`
                    : `${styles.gestureRow}${!action && !inherited ? ` ${styles.gestureRowEmpty}` : ""}`
                }
                onClick={() => {
                  setSelectionKey(key);
                }}
              >
                <GestureGlyph gesture={o.gesture} className={styles.gestureGlyph} />
                <span className={styles.gestureRowText}>
                  <span className={styles.gestureRowLabel}>{o.label}</span>
                  <span
                    className={
                      inherited && !action
                        ? `${styles.gestureRowSummary} ${styles.gestureRowInherited}`
                        : styles.gestureRowSummary
                    }
                  >
                    {summary}
                  </span>
                </span>
                <span
                  className={
                    action
                      ? `${styles.gestureDot} ${styles.gestureDotBound}`
                      : inherited
                        ? `${styles.gestureDot} ${styles.gestureDotInherited}`
                        : styles.gestureDot
                  }
                  aria-hidden="true"
                />
                {modified ? (
                  <span className={styles.modifiedMark} title="Unsaved change" aria-label="Unsaved change">
                    ●
                  </span>
                ) : null}
              </button>
            );
          })}
        </div>

        <div className={styles.detailPane} aria-label={selection ? `${selection.label} action` : "Action"}>
          {!selection ? null : (
            <>
              <h3 className={styles.detailTitle}>{selection.label}</h3>

              {inheritedAction ? (
                <div className={styles.note}>
                  Layer {layer} has no binding of its own here — layer 0's ({ACTION_SPECS[inheritedAction.type].label})
                  applies until one is added.{" "}
                  <button
                    type="button"
                    className={styles.linkButton}
                    onClick={() => {
                      setDraft(selection, inheritedAction);
                    }}
                  >
                    Override on layer {layer}
                  </button>
                </div>
              ) : null}

              <div className={styles.row}>
                <label>Action</label>
                <select
                  value={activeAction?.type ?? ""}
                  onChange={(e) => {
                    setDraft(selection, buildAction(e.target.value as ActionType));
                  }}
                >
                  <option value="" disabled>
                    {activeAction ? "(bound)" : "Choose an action…"}
                  </option>
                  {GROUP_ORDER.filter((g) => groups.has(g)).map((g) => (
                    <optgroup key={g} label={GROUP_LABELS[g]}>
                      {(groups.get(g) ?? []).map((type) => (
                        <option key={type} value={type}>
                          {ACTION_SPECS[type].label}
                          {implemented.has(type) ? "" : " (not implemented yet)"}
                        </option>
                      ))}
                    </optgroup>
                  ))}
                </select>
              </div>

              {activeAction && !implemented.has(activeAction.type) ? (
                <div className={styles.note}>
                  This daemon build has no handler for "{activeAction.type}" yet — the binding will be saved, but won't
                  do anything until a future milestone registers one.
                </div>
              ) : null}
              {activeAction && ACTION_SPECS[activeAction.type].note ? (
                <div className={styles.note}>{ACTION_SPECS[activeAction.type].note}</div>
              ) : null}
              {hint ? (
                <div className={styles.hint}>
                  <span aria-hidden="true">ⓘ</span> {hint}
                </div>
              ) : null}

              {activeAction ? (
                <ActionParamsForm
                  type={activeAction.type}
                  params={activeAction.params}
                  onChange={(nextParams) => {
                    setDraft(selection, { type: activeAction.type, params: nextParams } as Action);
                  }}
                />
              ) : (
                <p className={styles.emptyState}>Nothing is bound to {selection.label.toLowerCase()} on this layer.</p>
              )}

              {activeAction ? (
                <div className={styles.detailFooter}>
                  <button
                    type="button"
                    className={styles.linkButtonDanger}
                    onClick={() => {
                      setDraft(selection, null);
                    }}
                  >
                    Clear {selection.label.toLowerCase()}
                  </button>
                </div>
              ) : null}
            </>
          )}
        </div>
      </div>

      {error ? <div className={styles.error}>{error}</div> : null}

      <div className={styles.actions}>
        {confirmingDiscard ? (
          <>
            <span className={styles.discardPrompt}>Discard {modifiedCount} unsaved change(s)?</span>
            <div className={styles.actionsRight}>
              <button
                type="button"
                className={styles.button}
                onClick={() => {
                  setConfirmingDiscard(false);
                }}
              >
                Keep editing
              </button>
              <button type="button" className={`${styles.button} ${styles.buttonDanger}`} onClick={onClose}>
                Discard
              </button>
            </div>
          </>
        ) : (
          <>
            <span className={styles.unsavedCount}>
              {modifiedCount > 0 ? `${modifiedCount} unsaved change${modifiedCount === 1 ? "" : "s"}` : ""}
            </span>
            <div className={styles.actionsRight}>
              <button type="button" className={styles.button} onClick={requestClose} disabled={saving}>
                Cancel
              </button>
              <button
                type="button"
                className={`${styles.button} ${styles.buttonPrimary}`}
                disabled={saving || modifiedCount === 0}
                onClick={() => void commit()}
              >
                {saving ? "Saving…" : "Save"}
              </button>
            </div>
          </>
        )}
      </div>
    </Dialog>
  );
}
