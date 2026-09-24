import { useState } from "react";

import { isBridgeError, saveConfig, spotifyLogin, spotifyLogout } from "../../api/client";
import { useConfig } from "../../state/ConfigContext";
import { useConnection } from "../../state/ConnectionContext";
import appsStyles from "../apps/AppsView.module.css";
import styles from "./SpotifyView.module.css";

const REDIRECT_URI = "http://127.0.0.1/callback";

/** SpotifyView is the Spotify tab: a Client ID field (config.spotify.
 * clientId -- public under PKCE, see model.SpotifySettings' own doc
 * comment, so it's fine living in config.json like everything else
 * here), the one-time developer-dashboard setup instructions, and a
 * Connect/Disconnect button driven by GET /state's SpotifyState (the
 * same Available-gated shape MediaView/the focus status use). */
export function SpotifyView() {
  const { config, reload } = useConfig();
  const { state } = useConnection();
  const spotify = state?.spotify;
  const [clientIdDraft, setClientIdDraft] = useState<string | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | undefined>(undefined);

  if (!config || !spotify) {
    return (
      <div className={appsStyles.view}>
        <p>Connecting…</p>
      </div>
    );
  }

  const clientId = clientIdDraft ?? config.spotify.clientId;

  async function saveClientId(next: string) {
    if (!config) return;
    try {
      await saveConfig({ ...config, spotify: { ...config.spotify, clientId: next } });
      await reload();
    } catch (err) {
      console.error("SpotifyView: failed to save Client ID", isBridgeError(err) ? err.message : err);
    }
  }

  async function connect() {
    setBusy(true);
    setActionError(undefined);
    try {
      await spotifyLogin();
      // The daemon makes a best-effort attempt to open the authorize
      // URL in a browser itself; GET /state's next push carries
      // loginUrl as the fallback shown below regardless.
    } catch (err) {
      setActionError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  async function disconnect() {
    setBusy(true);
    setActionError(undefined);
    try {
      await spotifyLogout();
    } catch (err) {
      setActionError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={appsStyles.view}>
      <section className={appsStyles.section}>
        <div className={appsStyles.sectionHeader}>
          <h2>Connection</h2>
        </div>
        <div className={styles.status}>
          {spotify.authorized ? (
            <p>
              Connected{spotify.user ? ` as ${spotify.user}` : ""}.{" "}
              <button type="button" className={appsStyles.button} disabled={busy} onClick={() => void disconnect()}>
                Disconnect
              </button>
            </p>
          ) : (
            <p>
              Not connected.{" "}
              <button
                type="button"
                className={appsStyles.button}
                disabled={busy || clientId === ""}
                onClick={() => void connect()}
              >
                Connect
              </button>
              {clientId === "" ? " Set a Client ID below first." : null}
            </p>
          )}
          {spotify.loginInProgress ? (
            <p className={styles.pending}>
              Waiting for Spotify to redirect back…
              {spotify.loginUrl ? (
                <>
                  {" "}
                  If a browser tab didn't open,{" "}
                  <a href={spotify.loginUrl} target="_blank" rel="noreferrer">
                    open this link
                  </a>
                  .
                </>
              ) : null}
            </p>
          ) : null}
          {spotify.lastError ? <div className={appsStyles.cardMeta}>{spotify.lastError}</div> : null}
          {actionError ? <div className={appsStyles.cardMeta}>{actionError}</div> : null}
        </div>
      </section>

      <section className={appsStyles.section}>
        <div className={appsStyles.sectionHeader}>
          <h2>Spotify Developer Application</h2>
        </div>
        <div className={styles.field}>
          <label htmlFor="spotify-client-id">Client ID</label>
          <input
            id="spotify-client-id"
            type="text"
            value={clientId}
            placeholder="from developer.spotify.com/dashboard"
            onChange={(e) => {
              setClientIdDraft(e.target.value);
            }}
            onBlur={() => {
              if (clientIdDraft !== undefined && clientIdDraft !== config.spotify.clientId) {
                void saveClientId(clientIdDraft);
              }
              setClientIdDraft(undefined);
            }}
          />
        </div>
        <div className={styles.instructions}>
          <p>
            One-time setup, at{" "}
            <a href="https://developer.spotify.com/dashboard" target="_blank" rel="noreferrer">
              developer.spotify.com/dashboard
            </a>
            :
          </p>
          <ol>
            <li>Create an app (any name/description).</li>
            <li>
              Add <code>{REDIRECT_URI}</code> as a Redirect URI — no port number: knobd binds a random loopback port per
              login and Spotify allows that for a portless registered loopback address (it rejects{" "}
              <code>localhost</code> outright).
            </li>
            <li>Enable the Web API under "Which API/SDKs are you planning to use?".</li>
            <li>
              Copy the Client ID into the field above. No client secret is needed or stored — knobd uses PKCE, and the
              refresh token it gets back is stored via your system's Secret Service (KWallet/gnome-keyring), never in
              config.json.
            </li>
            <li>
              Since Spotify's February 2026 Developer Mode changes, the app owner's account needs Spotify Premium, and
              Developer Mode allows up to 5 authorized users.
            </li>
          </ol>
        </div>
      </section>
    </div>
  );
}
