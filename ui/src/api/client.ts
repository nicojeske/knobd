// Client for knobd's local API, reached through Tauri's Rust side
// (ui/src-tauri/src/commands.rs and socket.rs), which proxies to the
// daemon's unix socket at $XDG_RUNTIME_DIR/knobd.sock -- see
// specs/adr/0004-ipc-over-unix-socket.md. The webview never has raw
// socket access; every function here is a thin invoke() wrapper.
//
// Two generators feed this file's types, and they don't cross:
// `Config` and its nested types come from docs/config.schema.json ->
// ../types/config.ts. Everything else the API surfaces (State,
// AudioGraph, Capabilities, the learn/event payloads, ErrorResponse)
// comes from docs/openapi.json -> ../types/api.ts. See docs/README.md
// for why api.ts also contains an unused, independently-generated
// `Config` component -- it is never imported from here.

import { invoke } from "@tauri-apps/api/core";

import type { Config } from "../types/config";
import type { components } from "../types/api";

export type State = components["schemas"]["State"];
export type AudioGraph = components["schemas"]["AudioGraph"];
export type Capabilities = components["schemas"]["Capabilities"];
export type LearnState = components["schemas"]["LearnState"];
export type DaemonEvent = components["schemas"]["Event"];
export type DaemonErrorCode = components["schemas"]["ErrorResponse"]["code"];

/** DaemonStatus mirrors ui/src-tauri/src/commands.rs's DaemonStatus --
 * Rust-side-only, since it describes this bridge's own connectivity,
 * not anything the daemon itself returns. */
export interface DaemonStatus {
  socketPath: string;
  reachable: boolean;
  // serde serializes Option::None as JSON null (no #[serde(skip_serializing_if)]
  // on this field), not an absent key -- null, not undefined.
  error: string | null;
}

/** BridgeError mirrors ui/src-tauri/src/error.rs's BridgeError exactly
 * (the `#[serde(tag = "kind")]` discriminant): a rejected invoke() call
 * throws this shape verbatim, never an Error instance. Three variants
 * because each needs a different UI response: Daemon (a real
 * {code,message} answer -- code is typed against the OpenAPI-generated
 * ErrorResponse union, so a new daemon error code becomes a compile
 * error at any exhaustive switch over it), Unreachable (the daemon
 * isn't running -- the one case api.ErrorResponse's own codes cannot
 * express), and Protocol (an unparseable or malformed response). */
export type BridgeError =
  | { kind: "daemon"; status: number; code: DaemonErrorCode; message: string }
  | { kind: "unreachable"; path: string; message: string }
  | { kind: "protocol"; message: string };

/** isBridgeError narrows a caught invoke() rejection -- which is typed
 * `unknown` by @tauri-apps/api -- to BridgeError. */
export function isBridgeError(err: unknown): err is BridgeError {
  return (
    typeof err === "object" &&
    err !== null &&
    "kind" in err &&
    (err.kind === "daemon" || err.kind === "unreachable" || err.kind === "protocol")
  );
}

export function daemonStatus(): Promise<DaemonStatus> {
  return invoke<DaemonStatus>("daemon_status");
}

export function getConfig(): Promise<Config> {
  return invoke<Config>("get_config");
}

export function saveConfig(config: Config): Promise<void> {
  // Not invoke<void>(...): ESLint's no-invalid-void-type flags `void` as
  // a call expression's own type argument unconditionally (unlike
  // `Promise<void>`, which is fine) -- .then(() => undefined) gets the
  // same Promise<void>-compatible result without fighting the rule.
  return invoke("save_config", { config }).then(() => undefined);
}

export function getState(): Promise<State> {
  return invoke<State>("get_state");
}

export function getAudio(): Promise<AudioGraph> {
  return invoke<AudioGraph>("get_audio");
}

export function getCapabilities(): Promise<Capabilities> {
  return invoke<Capabilities>("get_capabilities");
}

/** startLearn arms MIDI learn; timeoutMs omitted (or 0) means the
 * engine's own default (see daemon/internal/engine.DefaultLearnTimeout).
 * exactOptionalPropertyTypes forbids passing `{ timeoutMs: undefined }`
 * where the Rust side's Option<u64> would happily accept an absent key,
 * so the argument object is built conditionally instead. */
export function startLearn(timeoutMs?: number): Promise<LearnState> {
  return invoke<LearnState>("start_learn", timeoutMs === undefined ? {} : { timeoutMs });
}

export function cancelLearn(): Promise<void> {
  return invoke("cancel_learn").then(() => undefined);
}

export type SpotifyLoginResponse = components["schemas"]["SpotifyLoginResponse"];
export type SpotifyPlaylist = components["schemas"]["SpotifyPlaylist"];
export type SpotifyDevice = components["schemas"]["SpotifyDevice"];

/** spotifyLogin starts (or restarts) the OAuth PKCE flow; the daemon
 * also makes a best-effort attempt to open the returned authorizeUrl in
 * a browser itself (see spotifyProvider.xdgOpen in cmd/knobd), so this
 * URL is mainly a fallback for the Spotify tab to show/copy. */
export function spotifyLogin(): Promise<SpotifyLoginResponse> {
  return invoke<SpotifyLoginResponse>("spotify_login");
}

export function spotifyLogout(): Promise<void> {
  return invoke("spotify_logout").then(() => undefined);
}

export function spotifyPlaylists(): Promise<SpotifyPlaylist[]> {
  return invoke<SpotifyPlaylist[]>("spotify_playlists");
}

export function spotifyDevices(): Promise<SpotifyDevice[]> {
  return invoke<SpotifyDevice[]>("spotify_devices");
}
