import { isBridgeError, saveConfig } from "../../api/client";
import { useConfig } from "../../state/ConfigContext";
import { useConnection } from "../../state/ConnectionContext";
import appsStyles from "../apps/AppsView.module.css";

/** MediaView lists every currently-known MPRIS player (live, from
 * GET /state -- there is nothing to configure about a player itself,
 * only whether it's ignored) plus an "Ignore" toggle per player that
 * edits Config.Media.IgnorePlayers, the same live-state-plus-config-
 * edit shape ScenesView/AppsView use elsewhere. A ref currently ignored
 * but not running still appears (state.media always lists every
 * ignored ref -- see cmd/knobd's mediaStateOf), so it can be
 * un-ignored even while its app is closed. */
export function MediaView() {
  const { config, reload } = useConfig();
  const { state } = useConnection();
  const media = state?.media;

  async function toggleIgnored(ref: string, ignored: boolean) {
    if (!config) return;
    const current = config.media.ignorePlayers;
    const next = ignored ? [...current, ref] : current.filter((r) => r !== ref);
    try {
      await saveConfig({ ...config, media: { ...config.media, ignorePlayers: next } });
      await reload();
    } catch (err) {
      // Errors here are surfaced the same way the rest of the config
      // editors' fire-and-forget saves do: logged, not blocking the
      // toggle's own optimistic re-render (the next GET /state/
      // GET /config will show the true state either way).
      console.error("MediaView: failed to update ignorePlayers", isBridgeError(err) ? err.message : err);
    }
  }

  if (!media) {
    return (
      <div className={appsStyles.view}>
        <p>Connecting…</p>
      </div>
    );
  }

  if (!media.available) {
    return (
      <div className={appsStyles.view}>
        <p>Media transport is unavailable (no D-Bus session bus, or MPRIS discovery failed to start).</p>
      </div>
    );
  }

  return (
    <div className={appsStyles.view}>
      <section className={appsStyles.section}>
        <div className={appsStyles.sectionHeader}>
          <h2>Media players</h2>
        </div>
        <div className={appsStyles.list}>
          {media.players.map((p) => (
            <div key={p.ref} className={appsStyles.card}>
              <div className={appsStyles.cardMain}>
                <span className={appsStyles.cardTitle}>
                  {p.identity ?? p.ref}
                  {p.ref === media.selected ? " (selected)" : ""}
                </span>
                <span className={appsStyles.cardMeta}>
                  {p.status ?? "unknown"}
                  {p.title ? ` · ${p.title}` : ""}
                  {p.artist ? ` — ${p.artist}` : ""}
                  {!p.canSeek ? " · no seek" : ""}
                </span>
              </div>
              <div className={appsStyles.cardActions}>
                <button
                  type="button"
                  className={appsStyles.button}
                  onClick={() => {
                    void toggleIgnored(p.ref, !p.ignored);
                  }}
                >
                  {p.ignored ? "Un-ignore" : "Ignore"}
                </button>
              </div>
            </div>
          ))}
          {media.players.length === 0 ? <p>No MPRIS players running right now.</p> : null}
        </div>
      </section>
    </div>
  );
}
