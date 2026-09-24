import { useState } from "react";

import { useConfig } from "../../state/ConfigContext";
import appsStyles from "../apps/AppsView.module.css";
import { SceneEditor } from "./SceneEditor";

/** ScenesView is basic CRUD over Config.scenes, modeled directly on
 * AppsView's Groups section -- a card list plus a Dialog editor. Per
 * specs/milestones/M08's Scope, this deliberately stays basic (create/
 * edit/delete), not a polished mixer-style scene manager. */
export function ScenesView() {
  const { config } = useConfig();
  const [editingScene, setEditingScene] = useState<string | undefined>(undefined);
  const [creatingScene, setCreatingScene] = useState(false);

  return (
    <div className={appsStyles.view}>
      <section className={appsStyles.section}>
        <div className={appsStyles.sectionHeader}>
          <h2>Scenes</h2>
          <button
            type="button"
            className={appsStyles.button}
            onClick={() => {
              setCreatingScene(true);
            }}
          >
            New scene
          </button>
        </div>
        <div className={appsStyles.list}>
          {(config?.scenes ?? []).map((s) => (
            <div key={s.id} className={appsStyles.card}>
              <div className={appsStyles.cardMain}>
                <span className={appsStyles.cardTitle}>{s.displayName || s.id}</span>
                <span className={appsStyles.cardMeta}>
                  {s.entries.length} {s.entries.length === 1 ? "entry" : "entries"}
                </span>
              </div>
              <div className={appsStyles.cardActions}>
                <button
                  type="button"
                  className={appsStyles.button}
                  onClick={() => {
                    setEditingScene(s.id);
                  }}
                >
                  Edit
                </button>
              </div>
            </div>
          ))}
          {config?.scenes.length === 0 ? <p>No scenes yet.</p> : null}
        </div>
      </section>

      {editingScene ? (
        <SceneEditor
          sceneId={editingScene}
          onClose={() => {
            setEditingScene(undefined);
          }}
        />
      ) : null}
      {creatingScene ? (
        <SceneEditor
          onClose={() => {
            setCreatingScene(false);
          }}
        />
      ) : null}
    </div>
  );
}
