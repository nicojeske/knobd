import { useState } from "react";

import { isBridgeError, saveConfig } from "../../api/client";
import { useConfig } from "../../state/ConfigContext";
import type { Profile } from "../../types/config";
import styles from "../apps/AppsView.module.css";
import { ProfileEditor } from "./ProfileEditor";

/** ProfilesView lists every Profile and lets the user create, rename,
 * duplicate, delete and activate one. Only one profile is active at a
 * time (Config.ActiveProfileID) -- activating is a plain field write on
 * the existing config, not a dialog, since it's non-destructive and
 * reversible in one click either way. Rename/duplicate/delete go through
 * ProfileEditor, matching AppsView's matcher/group editors. */
export function ProfilesView() {
  const { config, reload } = useConfig();
  const [editingProfile, setEditingProfile] = useState<string | undefined>(undefined);
  const [duplicating, setDuplicating] = useState<Profile | undefined>(undefined);
  const [creating, setCreating] = useState(false);
  const [activating, setActivating] = useState<string | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);

  async function handleActivate(id: string) {
    if (!config || id === config.activeProfileId) return;
    setActivating(id);
    setError(undefined);
    try {
      await saveConfig({ ...config, activeProfileId: id });
      await reload();
    } catch (err) {
      setError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setActivating(undefined);
    }
  }

  return (
    <div className={styles.view}>
      <section className={styles.section}>
        <div className={styles.sectionHeader}>
          <h2>Profiles</h2>
          <button
            type="button"
            className={styles.button}
            onClick={() => {
              setCreating(true);
            }}
          >
            New profile
          </button>
        </div>
        {error ? <p>{error}</p> : null}
        <div className={styles.list}>
          {(config?.profiles ?? []).map((p) => {
            const isActive = p.id === config?.activeProfileId;
            return (
              <div key={p.id} className={styles.card}>
                <div className={styles.cardMain}>
                  <span className={styles.cardTitle}>
                    {p.displayName || p.id}
                    {isActive ? " (active)" : ""}
                  </span>
                  <span className={styles.cardMeta}>{p.bindings.length} binding(s)</span>
                </div>
                <div className={styles.cardActions}>
                  <button
                    type="button"
                    className={isActive ? styles.button : `${styles.button} ${styles.buttonPrimary}`}
                    disabled={isActive || activating === p.id}
                    onClick={() => void handleActivate(p.id)}
                  >
                    {isActive ? "Active" : activating === p.id ? "Activating…" : "Activate"}
                  </button>
                  <button
                    type="button"
                    className={styles.button}
                    onClick={() => {
                      setDuplicating(p);
                    }}
                  >
                    Duplicate…
                  </button>
                  <button
                    type="button"
                    className={styles.button}
                    onClick={() => {
                      setEditingProfile(p.id);
                    }}
                  >
                    Edit
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      </section>

      {editingProfile ? (
        <ProfileEditor
          profileId={editingProfile}
          onClose={() => {
            setEditingProfile(undefined);
          }}
        />
      ) : null}
      {creating ? (
        <ProfileEditor
          onClose={() => {
            setCreating(false);
          }}
        />
      ) : null}
      {duplicating ? (
        <ProfileEditor
          duplicateFrom={duplicating}
          onClose={() => {
            setDuplicating(undefined);
          }}
        />
      ) : null}
    </div>
  );
}
