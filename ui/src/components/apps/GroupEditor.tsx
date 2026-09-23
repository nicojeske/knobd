import { useState } from "react";

import { isBridgeError, saveConfig } from "../../api/client";
import { useConfig } from "../../state/ConfigContext";
import type { AppGroup } from "../../types/config";
import editorStyles from "../binding/BindingEditor.module.css";
import styles from "../binding/Field.module.css";
import { Dialog } from "../common/Dialog";

export function GroupEditor({ groupId, onClose }: { groupId?: string; onClose: () => void }) {
  const { config, reload } = useConfig();
  const existing = config?.appGroups.find((g) => g.id === groupId);

  const [id, setId] = useState(existing?.id ?? "");
  const [displayName, setDisplayName] = useState(existing?.displayName ?? "");
  const [matcherIds, setMatcherIds] = useState<Set<string>>(new Set(existing?.matcherIds ?? []));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  function toggleMatcher(matcherId: string) {
    setMatcherIds((prev) => {
      const next = new Set(prev);
      if (next.has(matcherId)) next.delete(matcherId);
      else next.add(matcherId);
      return next;
    });
  }

  async function handleSave() {
    if (!config || id === "") return;
    setSaving(true);
    setError(undefined);
    try {
      const group: AppGroup = { id, displayName: displayName === "" ? id : displayName, matcherIds: [...matcherIds] };
      const others = config.appGroups.filter((g) => g.id !== id);
      await saveConfig({ ...config, appGroups: [...others, group] });
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
      await saveConfig({ ...config, appGroups: config.appGroups.filter((g) => g.id !== existing.id) });
      await reload();
      onClose();
    } catch (err) {
      setError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog title={existing ? `Edit group: ${existing.id}` : "New app group"} onClose={onClose}>
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
      <div className={styles.row}>
        <label>Matchers</label>
        {config?.appMatchers.length ? (
          config.appMatchers.map((m) => (
            <label key={m.id} style={{ display: "block", fontWeight: "normal" }}>
              <input
                type="checkbox"
                checked={matcherIds.has(m.id)}
                onChange={() => {
                  toggleMatcher(m.id);
                }}
              />{" "}
              {m.displayName || m.id}
            </label>
          ))
        ) : (
          <p>No app matchers configured yet -- create one in the Apps &amp; Groups tab first.</p>
        )}
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
