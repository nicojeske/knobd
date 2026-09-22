# ADR 0004: Daemon ↔ UI IPC over a unix domain socket, not a TCP port

**Status**: Accepted

## Context

`knobd-ui` (the Tauri configuration app, M07) needs to read and write
the daemon's live config, and receive a stream of live state (knob
positions, resolved volumes, focus changes) to display. This requires
some form of local IPC between two separate OS processes (the daemon
runs as a `systemd --user` service; the UI is a separate app the user
opens on demand).

## Decision

The daemon serves HTTP + WebSocket on a **unix domain socket** at
`$XDG_RUNTIME_DIR/knobd.sock` (falling back to a `/tmp` path if
`$XDG_RUNTIME_DIR` is unset — see `daemon/internal/api.SocketPath`'s
TODO), created with `0600` permissions (owner-only). The Tauri app's
Rust side connects to this socket directly and proxies requests
to/from the webview via Tauri commands (`ui/src-tauri/src/main.rs`);
the webview itself never has raw socket access, consistent with Tauri's
security model.

## Alternatives considered

- **A TCP port on localhost** (e.g. `127.0.0.1:8733`): simpler to debug
  with a browser, but on a shared multi-user machine, *any* local user
  can connect to a Node/localhost TCP port unless it's bound to a
  specific interface and firewalled — and any web page open in any
  browser on the machine can also attempt to reach it (mitigated by
  `Access-Control-Allow-Origin`/CORS in the browser sense, but not by
  anything at the socket level, and Electron/Tauri webviews don't always
  enforce the same-origin protections a "real" browser tab does).
  Avoiding a TCP port avoids this whole class of concern, and a bearer
  token adds complexity without adding real security once you'd have to
  document "don't lose this token" anyway.
- **A TCP port + a random-per-launch auth token**: works, but is the
  same amount of engineering as a unix socket for a strictly worse
  security property (still discoverable/connectable by anything on the
  loopback interface, just requires guessing or leaking the token
  first).
- **D-Bus**: idiomatic on this platform (and already used for
  ADR 0003's focus tracking) and would avoid inventing a wire protocol,
  but a full D-Bus interface for streaming live per-control state
  updates (potentially high-frequency, e.g. fader moves) is a more
  awkward fit than a WebSocket, and would tie the UI's API shape
  tightly to D-Bus's own type system rather than plain JSON.

## Consequences

- No authentication token to generate, store, or rotate — the operating
  system's file permissions are the entire access control mechanism.
- The UI can only be used by the same local user the daemon runs as,
  which matches the product's actual use case (personal desktop tool,
  not a multi-user shared service) — this is a feature, not a
  limitation, for this project.
- `daemon/internal/api` needs to create the socket directory if it
  doesn't exist and clean up a stale socket file left behind by an
  unclean shutdown before binding — a small but real implementation
  detail for M04.
- Debugging the API by hand needs `curl --unix-socket` instead of a
  plain URL — slightly less convenient than a TCP port, judged an
  acceptable cost for the security property.

## Update (M07)

The live-state channel shipped as **Server-Sent Events** (`GET /events`,
compact JSON per frame, `event: <type>\ndata: <json>\n\n`), not
WebSocket as originally written above. The decision this ADR actually
made — unix socket, not TCP; filesystem permissions as the entire
access control mechanism; the Rust side holds the connection and the
webview never touches the socket — is unchanged. Only the streaming
transport differs from what "HTTP + WebSocket" implied, for reasons
specific to what M07 actually needed:

- The channel is strictly one-directional (daemon → UI). Entering MIDI
  learn is `POST /learn`, not a client→server frame, so there was never
  a use for WebSocket's bidirectionality here.
- SSE needs no new dependency on either side: the daemon already needs
  an HTTP client/server (`net/http`) for `GET`/`PUT /config`, and the
  Tauri bridge already needs `hyper` for the same reason. WebSocket
  would have added `coder/websocket` (or similar) daemon-side and
  `tokio-tungstenite` in the Rust bridge, plus an awkward handshake URL
  for a connection that isn't really going to a URL at all.
- SSE preserves this ADR's `curl --unix-socket` debuggability
  consequence (above) for the streaming endpoint too:
  `curl -N --unix-socket $XDG_RUNTIME_DIR/knobd.sock http://localhost/events`
  prints live frames. A WebSocket upgrade has no equivalent one-liner.
- An SSE response is an ordinary long-lived HTTP response, not a
  hijacked connection — `http.Server.Shutdown` (with a `BaseContext`
  tying request contexts to the daemon's shutdown context) accounts for
  it automatically. A hijacked WebSocket connection would need the hub
  to track and close every connection itself for graceful shutdown to
  work at all.

All hub/framing logic lives behind a transport-agnostic interface in
`daemon/internal/api` (see `M07-config-ui.md`), specifically so that if
bidirectional push is ever actually needed, swapping the transport is
one file, not a redesign.
