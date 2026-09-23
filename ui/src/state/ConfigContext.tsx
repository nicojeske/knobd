import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";

import { getConfig, isBridgeError } from "../api/client";
import type { Config } from "../types/config";

export interface ConfigInfo {
  config: Config | undefined;
  error: string | undefined;
  loading: boolean;
  /** reload re-fetches GET /config. Every editor should call this after
   * a successful save rather than trusting its own local copy -- see
   * M07's save-semantics design (per-dialog apply, re-fetch on
   * config_changed). */
  reload: () => Promise<void>;
}

const ConfigContext = createContext<ConfigInfo | undefined>(undefined);

export function ConfigProvider({ children }: { children: ReactNode }) {
  const [config, setConfig] = useState<Config | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);
  const [loading, setLoading] = useState(true);

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      const cfg = await getConfig();
      setConfig(cfg);
      setError(undefined);
    } catch (err) {
      setError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return <ConfigContext.Provider value={{ config, error, loading, reload }}>{children}</ConfigContext.Provider>;
}

export function useConfig(): ConfigInfo {
  const ctx = useContext(ConfigContext);
  if (!ctx) {
    throw new Error("useConfig must be used within a ConfigProvider");
  }
  return ctx;
}
