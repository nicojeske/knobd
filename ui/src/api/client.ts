// Client for knobd's local API (daemon/internal/api), reached through
// Tauri's Rust side, which proxies to the daemon's unix socket at
// $XDG_RUNTIME_DIR/knobd.sock (see specs/adr/0004-ipc-over-unix-socket.md).
// The daemon's HTTP handlers and WebSocket push channel do not exist yet
// (TODO M04/M07) — every method here is a stub so the rest of the UI can
// be built against a stable shape before the daemon side lands.

import type { Config } from "../types/config";

export class KnobdClient {
  /** TODO(M07): implement via @tauri-apps/api's invoke(), calling
   * through to a Rust command that proxies to GET /config on the unix
   * socket. */
  async getConfig(): Promise<Config> {
    throw new Error("KnobdClient.getConfig: not implemented yet (see specs/milestones/M07-config-ui.md)");
  }

  /** TODO(M07): implement via invoke(), proxying to PUT /config. */
  async saveConfig(_config: Config): Promise<void> {
    throw new Error("KnobdClient.saveConfig: not implemented yet (see specs/milestones/M07-config-ui.md)");
  }

  /** TODO(M07): implement by opening a WebSocket (through Tauri) to the
   * daemon's live-state channel, for knob positions / resolved volumes /
   * focus changes. */
  subscribeToState(_onEvent: (event: unknown) => void): () => void {
    throw new Error("KnobdClient.subscribeToState: not implemented yet (see specs/milestones/M07-config-ui.md)");
  }
}
