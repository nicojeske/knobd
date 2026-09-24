import { useState } from "react";

import { isBridgeError, saveConfig } from "../../api/client";
import { useConfig } from "../../state/ConfigContext";
import type { Scene, SceneEntry, Target } from "../../types/config";
import editorStyles from "../binding/BindingEditor.module.css";
import styles from "../binding/Field.module.css";
import { TargetField } from "../binding/TargetField";
import { Dialog } from "../common/Dialog";

const DEFAULT_TARGET: Target = { kind: "default_sink" };

function defaultEntry(): SceneEntry {
  return { target: DEFAULT_TARGET, volumePercent: 50, muted: false };
}

/** SceneEditor is basic CRUD over one Scene, modeled on GroupEditor: an
 * id/displayName pair plus a list of entries (each a TargetField, a
 * volume percent, and a muted checkbox). It only edits a scene's
 * *definition* -- which targets it names and their saved level/mute --
 * never live audio state; that only ever changes via scene.save fired
 * from the physical control (see specs/milestones/M08's Design
 * section: saving never adds entries here either, matching the
 * daemon's own rule). */
export function SceneEditor({ sceneId, onClose }: { sceneId?: string; onClose: () => void }) {
  const { config, reload } = useConfig();
  const existing = config?.scenes.find((s) => s.id === sceneId);

  const [id, setId] = useState(existing?.id ?? "");
  const [displayName, setDisplayName] = useState(existing?.displayName ?? "");
  const [entries, setEntries] = useState<SceneEntry[]>(existing?.entries ?? []);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  function updateEntry(index: number, next: Partial<SceneEntry>) {
    setEntries((prev) => prev.map((e, i) => (i === index ? { ...e, ...next } : e)));
  }

  function removeEntry(index: number) {
    setEntries((prev) => prev.filter((_, i) => i !== index));
  }

  function addEntry() {
    setEntries((prev) => [...prev, defaultEntry()]);
  }

  async function handleSave() {
    if (!config || id === "") return;
    setSaving(true);
    setError(undefined);
    try {
      const scene: Scene = { id, displayName: displayName === "" ? id : displayName, entries };
      const others = config.scenes.filter((s) => s.id !== id);
      await saveConfig({ ...config, scenes: [...others, scene] });
      await reload();
      onClose();
    } catch (err) {
      setError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  async function handleDelete() {
    if (!config || !existing) return;
    setSaving(true);
    setError(undefined);
    try {
      await saveConfig({ ...config, scenes: config.scenes.filter((s) => s.id !== existing.id) });
      await reload();
      onClose();
    } catch (err) {
      setError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog title={existing ? `Edit scene: ${existing.id}` : "New scene"} onClose={onClose}>
      <div className={styles.row}>
        <label>ID</label>
        <input
          type="text"
          value={id}
          disabled={Boolean(existing)}
          onChange={(e) => {
            setId(e.target.value);
          }}
        />
      </div>
      <div className={styles.row}>
        <label>Display name</label>
        <input
          type="text"
          value={displayName}
          onChange={(e) => {
            setDisplayName(e.target.value);
          }}
        />
      </div>

      <div className={editorStyles.note}>
        Entries are the targets scene.save will capture the live mix of, and scene.apply will restore -- saving never
        adds new entries on its own; add/remove them here.
      </div>

      {entries.map((entry, i) => (
        // Index as key is fine here: entries have no stable id of their
        // own, and this list is only ever appended to or spliced by user
        // action within this same render tree, never reordered
        // externally.
        <div key={i} className={styles.row}>
          <TargetField
            label={`Entry ${i + 1}`}
            value={entry.target}
            onChange={(next) => {
              updateEntry(i, { target: next });
            }}
          />
          <input
            type="number"
            min={0}
            max={100}
            value={entry.volumePercent}
            onChange={(e) => {
              updateEntry(i, { volumePercent: Number(e.target.value) });
            }}
          />
          <label style={{ fontWeight: "normal" }}>
            <input
              type="checkbox"
              checked={entry.muted}
              onChange={(e) => {
                updateEntry(i, { muted: e.target.checked });
              }}
            />{" "}
            Muted
          </label>
          <button
            type="button"
            className={editorStyles.button}
            onClick={() => {
              removeEntry(i);
            }}
          >
            Remove
          </button>
        </div>
      ))}
      <div className={styles.row}>
        <button type="button" className={editorStyles.button} onClick={addEntry}>
          Add entry
        </button>
      </div>

      {error ? <div className={editorStyles.error}>{error}</div> : null}
      <div className={editorStyles.actions}>
        <div>
          {existing ? (
            <button
              type="button"
              className={`${editorStyles.button} ${editorStyles.buttonDanger}`}
              disabled={saving}
              onClick={() => void handleDelete()}
            >
              Delete
            </button>
          ) : null}
        </div>
        <div className={editorStyles.actionsRight}>
          <button type="button" className={editorStyles.button} onClick={onClose} disabled={saving}>
            Cancel
          </button>
          <button
            type="button"
            className={`${editorStyles.button} ${editorStyles.buttonPrimary}`}
            disabled={saving || id === ""}
            onClick={() => void handleSave()}
          >
            {saving ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
    </Dialog>
  );
}
