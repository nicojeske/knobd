import { useState } from "react";

import { isBridgeError, saveConfig } from "../../api/client";
import { useConfig } from "../../state/ConfigContext";
import type { Profile } from "../../types/config";
import editorStyles from "../binding/BindingEditor.module.css";
import styles from "../binding/Field.module.css";
import { Dialog } from "../common/Dialog";

/** ProfileEditor creates a profile, renames one, or (via `duplicateFrom`)
 * creates one seeded with another profile's bindings. All three share one
 * dialog because they differ only in how `bindings` and the initial field
 * values are seeded -- the save path is identical.
 *
 * Deleting the active profile, or the last remaining one, is refused here
 * rather than left to `Config.Validate` -- an empty profile list or a
 * dangling `activeProfileId` are conditions the daemon would reject outright,
 * and the reason ("this is the only profile" / "activate another profile
 * first") is worth surfacing next to the button rather than as a PUT error. */
export function ProfileEditor({
  profileId,
  duplicateFrom,
  onClose,
}: {
  profileId?: string;
  duplicateFrom?: Profile;
  onClose: () => void;
}) {
  const { config, reload } = useConfig();
  const existing = config?.profiles.find((p) => p.id === profileId);
  const bindings = existing?.bindings ?? duplicateFrom?.bindings ?? [];

  const [id, setId] = useState(existing?.id ?? "");
  const [displayName, setDisplayName] = useState(
    existing?.displayName ?? (duplicateFrom ? `${duplicateFrom.displayName} copy` : ""),
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  const isActive = Boolean(existing) && config?.activeProfileId === existing?.id;
  const isLastProfile = (config?.profiles.length ?? 0) <= 1;
  const idTaken = !existing && (config?.profiles.some((p) => p.id === id) ?? false);

  async function handleSave() {
    if (!config || id === "" || idTaken) return;
    setSaving(true);
    setError(undefined);
    try {
      const profile: Profile = { id, displayName: displayName === "" ? id : displayName, bindings };
      const others = config.profiles.filter((p) => p.id !== id);
      await saveConfig({ ...config, profiles: [...others, profile] });
      await reload();
      onClose();
    } catch (err) {
      setError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  async function handleDelete() {
    if (!config || !existing || isActive || isLastProfile) return;
    setSaving(true);
    setError(undefined);
    try {
      await saveConfig({ ...config, profiles: config.profiles.filter((p) => p.id !== existing.id) });
      await reload();
      onClose();
    } catch (err) {
      setError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  const deleteDisabledReason = isActive
    ? "Activate a different profile before deleting this one"
    : isLastProfile
      ? "A config needs at least one profile"
      : undefined;

  return (
    <Dialog
      title={existing ? `Edit profile: ${existing.displayName}` : duplicateFrom ? "Duplicate profile" : "New profile"}
      onClose={onClose}
    >
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
      {idTaken ? <div className={editorStyles.error}>A profile with this ID already exists.</div> : null}
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
      {duplicateFrom ? (
        <p className={editorStyles.note}>
          Copies all {bindings.length} binding(s) from "{duplicateFrom.displayName}".
        </p>
      ) : null}
      {error ? <div className={editorStyles.error}>{error}</div> : null}
      <div className={editorStyles.actions}>
        <div>
          {existing ? (
            <button
              type="button"
              className={`${editorStyles.button} ${editorStyles.buttonDanger}`}
              disabled={saving || Boolean(deleteDisabledReason)}
              title={deleteDisabledReason}
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
            disabled={saving || id === "" || idTaken}
            onClick={() => void handleSave()}
          >
            {saving ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
    </Dialog>
  );
}
