import { listen } from "@tauri-apps/api/event";
import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";

import { getConfig, isBridgeError, type DaemonEvent } from "../api/client";
import type { Config } from "../types/config";

export interface ConfigInfo {
  config: Config | undefined;
  error: string | undefined;
  loading: boolean;
  /** reload re-fetches GET /config. Every editor should call this after
   * a successful save rather than trusting its own local copy. */
  reload: () => Promise<void>;
  /** changedExternally is true once a config_changed push has arrived
   * (a physical long-press, a SIGHUP reload, or another window's save)
   * that this session hasn't reloaded past yet. The UI surfaces this as
   * a dismissable banner rather than reloading silently -- silently
   * replacing `config` out from under an open editor would discard
   * whatever the user was mid-way through typing. See
   * specs/milestones/M07-config-ui.md's save-semantics design: per-
   * dialog apply (already narrows the window to milliseconds) plus this
   * banner is the whole of it -- no revision/conflict diffing. */
  changedExternally: boolean;
  dismissChanged: () => void;
}

const ConfigContext = createContext<ConfigInfo | undefined>(undefined);

export function ConfigProvider({ children }: { children: ReactNode }) {
  const [config, setConfig] = useState<Config | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [changedExternally, setChangedExternally] = useState(false);
  // A config_changed push fires for EVERY successful SetConfig, including
  // this session's own PUT /config -- otherwise every save would
  // immediately show its own resulting push back to itself as "changed
  // externally." reload() (always called right after this session's own
  // save) marks a short suppression window; a push landing inside it is
  // assumed to be the echo of our own write, not someone else's.
  const suppressUntilRef = useRef(0);
  const SUPPRESS_MS = 1500;

  const reload = useCallback(async () => {
    suppressUntilRef.current = Date.now() + SUPPRESS_MS;
    setLoading(true);
    try {
      const cfg = await getConfig();
      setConfig(cfg);
      setError(undefined);
      setChangedExternally(false);
    } catch (err) {
      setError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  const dismissChanged = useCallback(() => {
    setChangedExternally(false);
  }, []);

  useEffect(() => {
    // Fetch-on-mount: reload's first synchronous statement (setLoading(true))
    // re-affirms the initial `useState(true)` value, which React bails out
    // of re-rendering for (same value, Object.is-equal) -- the "cascading
    // render" this rule guards against doesn't apply to a single leaf
    // effect whose synchronous setState is a no-op the first time it runs.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void reload();
  }, [reload]);

  useEffect(() => {
    let cancelled = false;
    let unlisten: (() => void) | undefined;

    void listen<DaemonEvent>("knobd:event", (event) => {
      if (event.payload.type === "config_changed" && Date.now() >= suppressUntilRef.current) {
        setChangedExternally(true);
      }
    }).then((fn) => {
      if (cancelled) {
        fn();
        return;
      }
      unlisten = fn;
    });

    return () => {
      cancelled = true;
      unlisten?.();
    };
  }, []);

  return (
    <ConfigContext.Provider value={{ config, error, loading, reload, changedExternally, dismissChanged }}>
      {children}
    </ConfigContext.Provider>
  );
}

export function useConfig(): ConfigInfo {
  const ctx = useContext(ConfigContext);
  if (!ctx) {
    throw new Error("useConfig must be used within a ConfigProvider");
  }
  return ctx;
}
