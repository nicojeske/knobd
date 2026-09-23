import { useConfig } from "../../state/ConfigContext";
import { useConnection } from "../../state/ConnectionContext";
import styles from "./DiagnosticsView.module.css";

/** DiagnosticsView is a plain, no-daemon-dependency-beyond-what-exists
 * window onto the raw GET /state and GET /config payloads -- genuinely
 * useful during M07's own build-out (nothing else can show live state
 * yet), and worth keeping afterward as a "what does knobd actually
 * think is going on" panel for the finished app too. */
export function DiagnosticsView() {
  const connection = useConnection();
  const { config, error: configError, loading: configLoading, reload } = useConfig();

  return (
    <div className={styles.view}>
      <section className={styles.section}>
        <h2>Connection</h2>
        <p>
          status: <strong>{connection.status}</strong>
          {connection.error ? <span className={styles.error}> — {connection.error}</span> : null}
        </p>
      </section>

      <section className={styles.section}>
        <h2>GET /state</h2>
        {connection.state ? <pre>{JSON.stringify(connection.state, null, 2)}</pre> : <p>No snapshot yet.</p>}
      </section>

      <section className={styles.section}>
        <h2>
          GET /config{" "}
          <button type="button" onClick={() => void reload()} disabled={configLoading}>
            Reload
          </button>
        </h2>
        {configError ? <p className={styles.error}>{configError}</p> : null}
        {config ? <pre>{JSON.stringify(config, null, 2)}</pre> : <p>No config loaded yet.</p>}
      </section>
    </div>
  );
}
