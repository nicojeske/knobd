import { createContext, useContext, useEffect, useState, type ReactNode } from "react";

import { getCapabilities, isBridgeError, type Capabilities } from "../api/client";

export interface CapabilitiesInfo {
  capabilities: Capabilities | undefined;
  error: string | undefined;
}

const CapabilitiesContext = createContext<CapabilitiesInfo | undefined>(undefined);

/** CapabilitiesProvider fetches GET /capabilities once: which of the 21
 * action types have a registered handler, which target kinds resolve,
 * and which optional features are on. Only the running daemon knows
 * this (the registry is assembled at cmd/knobd startup), so it's fetched
 * rather than hard-coded -- and it changes only across a daemon restart,
 * so a one-time fetch (unlike ConnectionContext's live push) is honest,
 * not a stopgap. */
export function CapabilitiesProvider({ children }: { children: ReactNode }) {
  const [info, setInfo] = useState<CapabilitiesInfo>({ capabilities: undefined, error: undefined });

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const capabilities = await getCapabilities();
        if (!cancelled) setInfo({ capabilities, error: undefined });
      } catch (err) {
        if (cancelled) return;
        setInfo({ capabilities: undefined, error: isBridgeError(err) ? err.message : String(err) });
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, []);

  return <CapabilitiesContext.Provider value={info}>{children}</CapabilitiesContext.Provider>;
}

export function useCapabilities(): CapabilitiesInfo {
  const ctx = useContext(CapabilitiesContext);
  if (!ctx) {
    throw new Error("useCapabilities must be used within a CapabilitiesProvider");
  }
  return ctx;
}
