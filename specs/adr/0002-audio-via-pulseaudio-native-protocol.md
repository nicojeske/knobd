# ADR 0002: Audio control via the PulseAudio native protocol against PipeWire

**Status**: Accepted

## Context

The development machine (and, per the project's stated intent, the
target environment generally) runs **PipeWire** as the audio server, but
exposes a **PulseAudio-compatible native protocol server**
(`pipewire-pulse`) at `$XDG_RUNTIME_DIR/pulse/native` — confirmed via
`pactl info` reporting `Server Name: PulseAudio (on PipeWire 1.6.8)`.
`pactl`, `wpctl`, and `pw-cli`/`pw-dump` are all present.

knobd needs to: enumerate sinks/sources/streams, read/set per-stream
volume and mute, and be notified of changes (an app opening/closing a
stream, another program changing a volume) without polling — turning a
knob should feel instant, and the LED ring (M05) needs to reflect
external volume changes too (e.g. from the app's own volume slider).

## Decision

Implement `daemon/internal/audio`'s real backend against the
**PulseAudio native protocol** via `github.com/jfreymuth/pulse`'s
`proto` subpackage (pure Go, no CGo, confirmed present on the Go module
proxy at v0.1.3), talking to the `pipewire-pulse` socket. This protocol
supports subscription to change events, which a `pactl` shell-out
approach cannot do without polling.

If, during M03 implementation, `pulse/proto` turns out to be missing an
operation knobd needs (the doc comment on `audio.Backend` specifically
flags `SetSinkInputVolume`/`SetSinkInputMute` and subscription as things
to confirm early), fall back to shelling out to `pactl` for that
specific operation, behind the same `audio.Backend` interface — the rest
of the daemon should not need to know which one is in use.

**M03 update — confirmed, no fallback needed.** Checked against both
pkg.go.dev and v0.1.3's actual source before building anything on top of
it, then against this system's live `pipewire-pulse` with a throwaway
probe (two real `paplay` streams, a live `SetSinkInputVolume`/
`SetSinkInputMute` round-trip, and a live `Subscribe`): every operation
knobd needs is present —
`SetSinkInputVolume`/`SetSinkInputMute`/`SetSinkVolume`/`SetSinkMute`/
`SetSourceVolume`/`SetSourceMute`/`SetSourceOutputVolume`/
`SetSourceOutputMute`, `GetSinkInputInfoList`/`GetSourceOutputInfoList`,
and `Subscribe`/`SubscribeEvent` with a working, promptly-delivered
change-event stream (confirmed live: an externally-triggered `pactl`
volume change showed up in `knobd monitor-audio`'s output in well under a
second, no polling). The `pactl` fallback was not needed anywhere.

Four library behaviors, not documented anywhere obvious, shape
`daemon/internal/audio/pulse.go`'s design and are worth recording here so
a future reader isn't surprised by them:

- **`proto.Client` has no `Close` method.** The only way to stop its
  internal read loop is to close the `net.Conn` `proto.Connect` hands
  back alongside the client.
- **A timed-out `Request` doesn't clean up after itself.** `proto.Client`
  defaults to a 1-second request timeout (raised to 5s here); on timeout,
  `Request` returns but leaves its pending reply registration in place,
  so a late reply can still write into the caller's reply struct after
  the caller has moved on. The code never reuses or reads a reply struct
  after a failed `Request`.
- **`proto.Client.Callback` runs inline in the read loop and blocks it
  until it returns** — a `Request` call from inside `Callback` can never
  see its own reply. The subscribe-event callback only ever hands events
  off to a channel; a separate goroutine does the round-trip lookups.
- **Connection loss isn't always signaled.** The read loop only fires a
  `ConnectionClosed` callback on `io.EOF`; other failures (a reset
  connection, a protocol parse error) just exit silently. `pulse.go`
  additionally treats a non-`proto.Error` `Request` failure as terminal
  and runs a periodic `GetServerInfo` heartbeat, so a silent death is
  still noticed.

Also confirmed live: this system's negotiated protocol version is **32**
(the client library's own cap, via `client.SetVersion`'s
`Min(ourCap, serverOffered)` — the server itself offers 35), which is
comfortably above every version-gated field the implementation needs
(`Properties` at v13, `VolumeWritable` at v20 for sink-inputs and v22 for
source-outputs).

## Alternatives considered

- **Shell out to `pactl` for everything**: simplest to implement, but no
  event subscription (`pactl subscribe` exists but is line-oriented text
  parsing of a CLI tool's output, fragile across versions) and a process
  spawn per volume change is unnecessary overhead for something that
  needs to feel instant when a physical knob turns.
- **PipeWire's native protocol directly** (via a Go binding, or CGo
  against `libpipewire`): more "correct" long-term, and gives access to
  things Pulse's compatibility layer doesn't expose, but no mature
  pure-Go PipeWire client library was found, and CGo would violate ADR
  0001's static-binary rationale. Worth revisiting if the Pulse
  compatibility layer proves insufficient (e.g. if it turns out not to
  reliably expose every property knobd's `AppMatcher` needs — see
  `testdata/pipewire/pw-dump-sample.json`'s two edge cases).

## Consequences

- Depends on the PipeWire distribution shipping `pipewire-pulse`, which
  is effectively universal on current Linux desktops but is a real
  dependency to document (see
  `specs/reference/environment.md` and the top-level README's
  Requirements section).
- Some PipeWire-native concepts (e.g. `node.description` vs. Pulse's
  sink "description" field) may not map perfectly through the
  compatibility layer; M03 should treat property availability as
  best-effort, matching the "props are a bag of hints, not a fixed
  struct" design already reflected in `audio.Stream`.
- A `pactl`-shell-out fallback implementation, if needed, must satisfy
  the same `audio.Backend` interface — this is already how the
  interface is designed (see `daemon/internal/audio/backend.go`'s
  package doc comment), so no rework should be needed if M03 has to use
  it.
