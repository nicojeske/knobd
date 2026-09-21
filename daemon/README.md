# knobd daemon

Go module `github.com/njeske/knobd`. Builds to a single static binary
(`cmd/knobd`) with no CGo dependencies — see
`../specs/adr/0001-pure-go-alsa-rawmidi.md` for why that's a deliberate
constraint, not just a default.

```
cmd/knobd/          main.go — subcommand dispatch, flag parsing, config
                     load, logging, signal handling. `knobd monitor`
                     (monitor.go, M02) prints decoded MIDI events; `knobd
                     monitor-audio` (monitor_audio.go, M03) prints the
                     live PipeWire sink/source/stream graph and its
                     change events. The bare daemon path does not yet
                     start audio/focus/engine/api together — those are
                     still scaffolding (see below).
cmd/schemagen/       regenerates docs/config.schema.json (`make schema`).
                     A separate binary from knobd so its dependency
                     (github.com/invopop/jsonschema) never links into the
                     daemon that actually ships — see
                     ../specs/adr/0005-schema-generation-via-invopop.md.
internal/
  model/             domain types: Control, Gesture, Target, AppMatcher,
                      Action (tagged union), Binding, Config. Fully
                      implemented and tested — this is the shape
                      everything else is built around.
  config/            load/save/migrate ~/.config/knobd/config.json.
                      Fully implemented and tested.
  schema/            generates the JSON Schema for model.Config that
                      cmd/schemagen writes out; the model package itself
                      stays dependency-free.
  midi/              Port interface + FakePort, plus the real backend
                      (M02): discovery (discover.go), a running-status
                      byte-stream parser (parser.go), the rawmidi Port
                      (rawmidi.go), inotify hotplug watching
                      (watcher.go), and a self-healing, reconnecting
                      Supervisor (supervisor.go).
  device/            Codec interface for the X-Touch Mini's MIDI
                      encoding. Decode is implemented (M02, xtouch.go).
                      EncodeLED (LED output): TODO(M05).
  audio/             Backend interface + FakeBackend, and (M03) the real
                      PipeWire backend against pipewire-pulse's
                      PulseAudio-compatible protocol (pulse.go,
                      subscribe.go — see
                      ../specs/adr/0002-audio-via-pulseaudio-native-protocol.md),
                      a self-healing, reconnecting Supervisor
                      (supervisor.go, mirroring midi's), real AppMatcher
                      resolution (matcher.go), and the volume response
                      curve (curve.go). Wiring any of this into MIDI
                      input or the mapping engine: TODO(M04).
  focus/             Provider interface + FakeProvider. Real KWin-script
                      backend: TODO(M06).
  engine/            Event loop skeleton. Real gesture detection, layer
                      resolution, and dispatch: TODO(M04).
  actions/           Generic action dispatch registry (implemented).
                      Concrete handlers per action family: TODO(M03,
                      M08, M09, M11 — see registry.go's doc comment).
  api/               Unix-socket HTTP+WS server skeleton. Real handlers:
                      TODO(M04, M07).
```

Every package that talks to hardware or the desktop environment is
built behind a small interface with a fake implementation
(`midi.FakePort`, `audio.FakeBackend`, `focus.FakeProvider`) so the rest
of the daemon can be tested without the physical controller, a running
PipeWire session, or KDE — see `../CLAUDE.md`.

## Building & testing

```bash
go build ./... && go vet ./... && go test ./...
```

or from the repo root: `make build`, `make test`, `make lint`.

After changing anything under `internal/model` that affects the config's
JSON shape, regenerate the schema: `make schema` (from the repo root),
then commit the updated `docs/config.schema.json` — CI fails if it's
stale.
