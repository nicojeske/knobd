import { listen } from "@tauri-apps/api/event";
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";

import { getState, isBridgeError, type DaemonEvent, type State } from "../api/client";

export type ConnectionStatus = "connecting" | "connected" | "unreachable";

export interface ConnectionInfo {
  status: ConnectionStatus;
  state: State | undefined;
  error: string | undefined;
}

const ConnectionContext = createContext<ConnectionInfo | undefined>(undefined);

interface ConnectionEventPayload {
  connected: boolean;
  error?: string;
}

const initialInfo: ConnectionInfo = { status: "connecting", state: undefined, error: undefined };

/** ConnectionProvider is seeded by one GET /state call and kept live by
 * push after that: ui/src-tauri/src/events.rs holds knobd's GET /events
 * connection and re-emits every frame as a "knobd:event" Tauri event,
 * plus "knobd:connection" when the connection itself drops or
 * (re-)establishes.
 *
 * The explicit seed call is load-bearing, not a leftover from before the
 * push channel existed: events.rs starts connecting in Tauri's .setup()
 * hook, which runs before the webview has loaded React at all, and the
 * daemon's own GET /events sends exactly one unconditional state
 * snapshot right on connect (see daemon/internal/api/stream_sse.go) --
 * on an otherwise-idle daemon, nothing ever triggers a second one. A
 * frontend that only ever listens would race that one-time snapshot and
 * could be stuck showing "connecting" forever despite the connection
 * being perfectly healthy underneath. GET /state has no such race. */
export function ConnectionProvider({ children }: { children: ReactNode }) {
  const [info, setInfo] = useState<ConnectionInfo>(initialInfo);

  useEffect(() => {
    let cancelled = false;

    async function seed() {
      try {
        const state = await getState();
        if (!cancelled) {
          setInfo({ status: "connected", state, error: undefined });
        }
      } catch (err) {
        if (cancelled) return;
        const message = isBridgeError(err) ? err.message : String(err);
        setInfo({ status: "unreachable", state: undefined, error: message });
      }
    }
    void seed();

    let unlistenEvent: (() => void) | undefined;
    let unlistenConnection: (() => void) | undefined;

    void listen<DaemonEvent>("knobd:event", (event) => {
      const payload = event.payload;
      if (payload.type === "state" && payload.state) {
        const nextState = payload.state;
        setInfo((prev) => ({ status: "connected", state: nextState, error: prev.error }));
      }
    }).then((unlisten) => {
      if (cancelled) {
        unlisten();
        return;
      }
      unlistenEvent = unlisten;
    });

    void listen<ConnectionEventPayload>("knobd:connection", (event) => {
      const { connected, error } = event.payload;
      if (connected) {
        // A (re)connection succeeded; re-seed in case its own one-time
        // snapshot races this listener too (the same reasoning as the
        // initial seed() above, on every reconnect, not just startup).
        void seed();
        return;
      }
      setInfo({ status: "unreachable", state: undefined, error });
    }).then((unlisten) => {
      if (cancelled) {
        unlisten();
        return;
      }
      unlistenConnection = unlisten;
    });

    return () => {
      cancelled = true;
      unlistenEvent?.();
      unlistenConnection?.();
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
