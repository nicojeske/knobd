# knobd daemon

Go module `github.com/njeske/knobd`. Builds to a single static binary
(`cmd/knobd`) with no CGo dependencies — see
`../specs/adr/0001-pure-go-alsa-rawmidi.md` for why that's a deliberate
constraint, not just a default.

```
cmd/knobd/          main.go — flag parsing, config load, logging, signal
                     handling. Does not start MIDI/audio/focus/engine/api
                     yet — those are still scaffolding (see below).
internal/
  model/             domain types: Control, Gesture, Target, AppMatcher,
                      Action (tagged union), Binding, Config. Fully
                      implemented and tested — this is the shape
                      everything else is built around.
  config/            load/save/migrate ~/.config/knobd/config.json.
                      Fully implemented and tested.
  midi/              Port interface + FakePort. Real rawmidi backend and
                      device discovery: TODO(M02).
  device/            Codec interface for the X-Touch Mini's MIDI
                      encoding. Real implementation: TODO(M02) (input),
                      TODO(M05) (LED output).
  audio/             Backend interface + FakeBackend + AppMatcher
                      resolution signature. Real PipeWire backend:
                      TODO(M03).
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
