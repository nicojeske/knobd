import { createContext, useContext, useEffect, useState, type ReactNode } from "react";

import { getState, isBridgeError, type State } from "../api/client";

export type ConnectionStatus = "connecting" | "connected" | "unreachable" | "error";

export interface ConnectionInfo {
  status: ConnectionStatus;
  state: State | undefined;
  error: string | undefined;
}

const ConnectionContext = createContext<ConnectionInfo | undefined>(undefined);

// pollIntervalMs is a stopgap until the live GET /events push channel
// is wired up on this side (see specs/milestones/M07-config-ui.md's
// plan): polling GET /state is correct, just coarser than a push. 2s is
// generous enough to never contend with the daemon's own 30ms LED-flush
// cadence -- this is a "is the daemon still there, what does it report
// right now" check, not a source of live control positions.
const pollIntervalMs = 2000;

export function ConnectionProvider({ children }: { children: ReactNode }) {
  const [info, setInfo] = useState<ConnectionInfo>({
    status: "connecting",
    state: undefined,
    error: undefined,
  });

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      try {
        const state = await getState();
        if (!cancelled) {
          setInfo({ status: "connected", state, error: undefined });
        }
      } catch (err) {
        if (cancelled) return;
        if (isBridgeError(err) && err.kind === "unreachable") {
          setInfo({ status: "unreachable", state: undefined, error: err.message });
        } else {
          const message = isBridgeError(err) ? err.message : String(err);
          setInfo((prev) => ({ status: "error", state: prev.state, error: message }));
        }
      }
    }

    void poll();
    const id = window.setInterval(() => void poll(), pollIntervalMs);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);

  return <ConnectionContext.Provider value={info}>{children}</ConnectionContext.Provider>;
}

export function useConnection(): ConnectionInfo {
  const ctx = useContext(ConnectionContext);
  if (!ctx) {
    throw new Error("useConnection must be used within a ConnectionProvider");
  }
  return ctx;
}
