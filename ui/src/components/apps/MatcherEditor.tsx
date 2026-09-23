import { useState } from "react";

import { isBridgeError, saveConfig } from "../../api/client";
import { useConfig } from "../../state/ConfigContext";
import type { AppMatcher } from "../../types/config";
import { Dialog } from "../common/Dialog";
import styles from "../binding/Field.module.css";
import editorStyles from "../binding/BindingEditor.module.css";

function toList(text: string): string[] {
  return text
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s !== "");
}

function fromList(list: readonly string[] | undefined): string {
  return (list ?? []).join(", ");
}

/** MatcherEditor creates or edits one AppMatcher. All five criteria
 * fields OR together (a stream matches if ANY populated field matches
 * it) -- case-insensitively, except mediaNameRx, which model.AppMatcher's
 * own doc comment warns is case-sensitive and unanchored Go RE2 (e.g.
 * "Pal" also matches "Palworld"). */
export function MatcherEditor({
  matcherId,
  seed,
  onClose,
}: {
  matcherId?: string;
  seed?: Partial<AppMatcher>;
  onClose: () => void;
}) {
  const { config, reload } = useConfig();
  const existing = config?.appMatchers.find((m) => m.id === matcherId);

  const [id, setId] = useState(existing?.id ?? seed?.id ?? "");
  const [displayName, setDisplayName] = useState(existing?.displayName ?? seed?.displayName ?? "");
  const [binaries, setBinaries] = useState(fromList(existing?.binaries ?? seed?.binaries));
  const [appNames, setAppNames] = useState(fromList(existing?.appNames ?? seed?.appNames));
  const [nodeNames, setNodeNames] = useState(fromList(existing?.nodeNames ?? seed?.nodeNames));
  const [desktopIds, setDesktopIds] = useState(fromList(existing?.desktopIds ?? seed?.desktopIds));
  const [mediaNameRx, setMediaNameRx] = useState(existing?.mediaNameRx ?? seed?.mediaNameRx ?? "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  const binariesList = toList(binaries);
  const appNamesList = toList(appNames);
  const nodeNamesList = toList(nodeNames);
  const desktopIdsList = toList(desktopIds);
  const hasAnyCriterion =
    binariesList.length > 0 ||
    appNamesList.length > 0 ||
    nodeNamesList.length > 0 ||
    desktopIdsList.length > 0 ||
    mediaNameRx !== "";

  async function handleSave() {
    if (!config || id === "" || !hasAnyCriterion) return;
    setSaving(true);
    setError(undefined);
    try {
      const matcher: AppMatcher = {
        id,
        displayName: displayName === "" ? id : displayName,
        ...(binariesList.length > 0 ? { binaries: binariesList } : {}),
        ...(appNamesList.length > 0 ? { appNames: appNamesList } : {}),
        ...(nodeNamesList.length > 0 ? { nodeNames: nodeNamesList } : {}),
        ...(desktopIdsList.length > 0 ? { desktopIds: desktopIdsList } : {}),
        ...(mediaNameRx !== "" ? { mediaNameRx } : {}),
      };
      const others = config.appMatchers.filter((m) => m.id !== id);
      await saveConfig({ ...config, appMatchers: [...others, matcher] });
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
      await saveConfig({ ...config, appMatchers: config.appMatchers.filter((m) => m.id !== existing.id) });
      await reload();
      onClose();
    } catch (err) {
      setError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog title={existing ? `Edit matcher: ${existing.id}` : "New app matcher"} onClose={onClose}>
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
        <label>Binaries (application.process.binary)</label>
        <input
          type="text"
          value={binaries}
          placeholder="comma-separated"
          onChange={(e) => {
            setBinaries(e.target.value);
          }}
        />
      </div>
      <div className={styles.row}>
        <label>App names (application.name)</label>
        <input
          type="text"
          value={appNames}
          placeholder="comma-separated"
          onChange={(e) => {
            setAppNames(e.target.value);
          }}
        />
      </div>
      <div className={styles.row}>
        <label>Node names (node.name)</label>
        <input
          type="text"
          value={nodeNames}
          placeholder="comma-separated"
          onChange={(e) => {
            setNodeNames(e.target.value);
          }}
        />
      </div>
      <div className={styles.row}>
        <label>Desktop IDs (application.id)</label>
        <input
          type="text"
          value={desktopIds}
          placeholder="comma-separated"
          onChange={(e) => {
            setDesktopIds(e.target.value);
          }}
        />
      </div>
      <div className={styles.row}>
        <label>Media name pattern (regex, case-sensitive, unanchored)</label>
        <input
          type="text"
          value={mediaNameRx}
          onChange={(e) => {
            setMediaNameRx(e.target.value);
          }}
        />
      </div>
      {!hasAnyCriterion ? <div className={editorStyles.note}>At least one criterion is required.</div> : null}
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
            disabled={saving || id === "" || !hasAnyCriterion}
            onClick={() => void handleSave()}
          >
            {saving ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
    </Dialog>
  );
}
