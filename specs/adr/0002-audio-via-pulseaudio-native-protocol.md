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
