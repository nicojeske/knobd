import { useState } from "react";

import { useConfig } from "../../state/ConfigContext";
import { useAudioGraph } from "../../state/useAudioGraph";
import type { AppMatcher } from "../../types/config";
import styles from "./AppsView.module.css";
import { GroupEditor } from "./GroupEditor";
import { MatcherEditor } from "./MatcherEditor";

/** AppsView manages AppMatchers and AppGroups, and shows the live audio
 * graph (GET /audio) so a matcher can be created from a stream that's
 * actually running right now -- with its raw, undigested property bag,
 * since what a given app publishes varies (some streams have only
 * node.name). "Group" targets are accepted here (M07's own scope) but
 * don't resolve at runtime until M08 -- see BindingEditor's own note
 * for where that's surfaced when binding to one. */
export function AppsView() {
  const { config } = useConfig();
  const { graph, loading, error, reload } = useAudioGraph();
  const [editingMatcher, setEditingMatcher] = useState<string | undefined>(undefined);
  const [creatingMatcherFrom, setCreatingMatcherFrom] = useState<Partial<AppMatcher> | undefined>(undefined);
  const [editingGroup, setEditingGroup] = useState<string | undefined>(undefined);
  const [creatingGroup, setCreatingGroup] = useState(false);
  const [creatingMatcher, setCreatingMatcher] = useState(false);

  return (
    <div className={styles.view}>
      <section className={styles.section}>
        <div className={styles.sectionHeader}>
          <h2>App matchers</h2>
          <button
            type="button"
            className={styles.button}
            onClick={() => {
              setCreatingMatcher(true);
            }}
          >
            New matcher
          </button>
        </div>
        <div className={styles.list}>
          {(config?.appMatchers ?? []).map((m) => (
            <div key={m.id} className={styles.card}>
              <div className={styles.cardMain}>
                <span className={styles.cardTitle}>{m.displayName || m.id}</span>
                <span className={styles.cardMeta}>
                  {[
                    m.binaries?.length ? `binaries: ${m.binaries.join(", ")}` : undefined,
                    m.appNames?.length ? `appNames: ${m.appNames.join(", ")}` : undefined,
                    m.nodeNames?.length ? `nodeNames: ${m.nodeNames.join(", ")}` : undefined,
                    m.desktopIds?.length ? `desktopIds: ${m.desktopIds.join(", ")}` : undefined,
                    m.mediaNameRx ? `mediaNameRx: ${m.mediaNameRx}` : undefined,
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </span>
              </div>
              <div className={styles.cardActions}>
                <button
                  type="button"
                  className={styles.button}
                  onClick={() => {
                    setEditingMatcher(m.id);
                  }}
                >
                  Edit
                </button>
              </div>
            </div>
          ))}
          {config?.appMatchers.length === 0 ? <p>No app matchers yet.</p> : null}
        </div>
      </section>

      <section className={styles.section}>
        <div className={styles.sectionHeader}>
          <h2>Groups</h2>
          <button
            type="button"
            className={styles.button}
            onClick={() => {
              setCreatingGroup(true);
            }}
          >
            New group
          </button>
        </div>
        <div className={styles.list}>
          {(config?.appGroups ?? []).map((g) => (
            <div key={g.id} className={styles.card}>
              <div className={styles.cardMain}>
                <span className={styles.cardTitle}>{g.displayName || g.id}</span>
                <span className={styles.cardMeta}>{g.matcherIds.length} matcher(s)</span>
              </div>
              <div className={styles.cardActions}>
                <button
                  type="button"
                  className={styles.button}
                  onClick={() => {
                    setEditingGroup(g.id);
                  }}
                >
                  Edit
                </button>
              </div>
            </div>
          ))}
          {config?.appGroups.length === 0 ? <p>No groups yet.</p> : null}
        </div>
      </section>

      <section className={styles.section}>
        <div className={styles.sectionHeader}>
          <h2>Live streams</h2>
          <button type="button" className={styles.button} onClick={() => void reload()} disabled={loading}>
            Refresh
          </button>
        </div>
        {error ? <p>{error}</p> : null}
        <div className={styles.list}>
          {(graph?.streams ?? []).map((s) => (
            <div key={s.ref} className={styles.card}>
              <div className={styles.cardMain}>
                <span className={styles.cardTitle}>
                  {s.displayName} {s.matcherIds?.length ? `(matched: ${s.matcherIds.join(", ")})` : ""}
                </span>
                <span className={styles.cardMeta}>{s.direction}</span>
                <pre className={styles.streamProps}>{JSON.stringify(s.props, null, 2)}</pre>
              </div>
              <div className={styles.cardActions}>
                <button
                  type="button"
                  className={styles.button}
                  onClick={() => {
                    setCreatingMatcherFrom({
                      id: s.appName ?? s.nodeName ?? s.ref,
                      displayName: s.displayName,
                      ...(s.appName ? { appNames: [s.appName] } : {}),
                      ...(s.nodeName && !s.appName ? { nodeNames: [s.nodeName] } : {}),
                    });
                  }}
                >
                  New matcher…
                </button>
              </div>
            </div>
          ))}
          {graph?.streams.length === 0 ? <p>No live streams.</p> : null}
        </div>
      </section>

      {editingMatcher ? (
        <MatcherEditor
          matcherId={editingMatcher}
          onClose={() => {
            setEditingMatcher(undefined);
          }}
        />
      ) : null}
      {creatingMatcher ? (
        <MatcherEditor
          onClose={() => {
            setCreatingMatcher(false);
          }}
        />
      ) : null}
      {creatingMatcherFrom ? (
        <MatcherEditor
          seed={creatingMatcherFrom}
          onClose={() => {
            setCreatingMatcherFrom(undefined);
          }}
        />
      ) : null}
      {editingGroup ? (
        <GroupEditor
          groupId={editingGroup}
          onClose={() => {
            setEditingGroup(undefined);
          }}
        />
      ) : null}
      {creatingGroup ? (
        <GroupEditor
          onClose={() => {
            setCreatingGroup(false);
          }}
        />
      ) : null}
    </div>
  );
}
