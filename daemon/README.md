# knobd daemon

Go module `github.com/njeske/knobd`. Builds to a single static binary
(`cmd/knobd`) with no CGo dependencies — see
`../specs/adr/0001-pure-go-alsa-rawmidi.md` for why that's a deliberate
constraint, not just a default.

```
cmd/knobd/          main.go — subcommand dispatch, flag parsing, config
                     load, logging, signal handling, and (M04) wiring
                     midi.Supervisor/audio.Supervisor/focus/engine/api
                     together and running them until a signal or a fatal
                     component error. configstore.go/state.go are the
                     api.ConfigStore/api.StateProvider adapters. `knobd
                     monitor` (monitor.go, M02) prints decoded MIDI
                     events; `knobd monitor-audio` (monitor_audio.go,
                     M03) prints the live PipeWire sink/source/stream
                     graph and its change events; `knobd calibrate-leds`
                     (calibrate.go, M05) sends one raw LED CC/Note
                     message and exits, for empirically confirming the
                     ring/button LED byte encoding against the physical
                     unit.
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
  schema/            generates the JSON Schema for model.Config
                      (schema.go), the OpenAPI 3.1 document for
                      daemon/internal/api's routes (openapi.go), and the
                      hardware index-range/gesture-matrix document
                      (devicelayout.go) that cmd/schemagen writes out;
                      the model package itself stays dependency-free.
  midi/              Port interface + FakePort, plus the real backend
                      (M02): discovery (discover.go), a running-status
                      byte-stream parser (parser.go), the rawmidi Port
                      (rawmidi.go), inotify hotplug watching
                      (watcher.go), and a self-healing, reconnecting
                      Supervisor (supervisor.go).
  device/            Codec interface for the X-Touch Mini's MIDI
                      encoding. Decode is implemented (M02, xtouch.go);
                      EncodeLED (M05, led.go) encodes ring/button LED
                      updates against values confirmed live via `knobd
                      calibrate-leds`.
  audio/             Backend interface + FakeBackend, and (M03) the real
                      PipeWire backend against pipewire-pulse's
                      PulseAudio-compatible protocol (pulse.go,
                      subscribe.go — see
                      ../specs/adr/0002-audio-via-pulseaudio-native-protocol.md),
                      a self-healing, reconnecting Supervisor
                      (supervisor.go, mirroring midi's), real AppMatcher
                      resolution (matcher.go), and the volume response
                      curve (curve.go).
  focus/             Provider interface + FakeProvider, plus (M04)
                      Unavailable(), the no-op Provider knobd runs with
                      until M06 lands a real KWin-script backend.
  engine/            (M04) Engine.Run: Port.Read -> Codec.Decode ->
                      gestureMachine (gesture.go, the press/hold/
                      double-press state machine) -> bindingIndex
                      (bindings.go) -> resolver (resolver.go, Target ->
                      live audio.Refs) -> a single dispatcher goroutine
                      (dispatch.go) that owns every blocking
                      audio.Backend/actions.Registry call and coalesces
                      rapid turns/fader moves. SetConfig/Snapshot
                      (state.go) are channel round trips served by the
                      same run goroutine, so there are no mutexes here;
                      (M07) SetLearnUntil (learn.go) is the same pattern
                      for MIDI learn, suppressing dispatch of every
                      decoded event while armed.
                      (M05, led.go) also pushes LED updates from that
                      same goroutine on every resolved volume/mute
                      change, rate-limited (leading edge + trailing
                      flush, see ledFlushInterval); RepaintLEDs is
                      another such round trip, for cmd/knobd's reconnect
                      hook.
  actions/           Generic action dispatch registry (registry.go) plus
                      (M04) the volume.adjust/set/mute_toggle/follow
                      handlers (volume.go) — the first real
                      actions.Handler implementations. Remaining
                      families: TODO(M08, M09, M11 — see registry.go's
                      doc comment).
  api/               (M04) GET/PUT /config and GET /state over the unix
                      socket (server.go, handlers.go, state.go); (M07)
                      GET /audio, GET /capabilities, POST/DELETE /learn,
                      and a GET /events Server-Sent Events stream
                      (hub.go, stream_sse.go, events.go) for live state,
                      learn captures, and config-change notifications.
                      The route table (routes.go) also drives OpenAPI
                      generation — see ../internal/schema.
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
