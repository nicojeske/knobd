# M07: Config UI

## Status

Not started.

## Depends on

M04 (a real API to talk to). Benefits from, but doesn't strictly need,
M05 (live LED-equivalent state to mirror in the UI) and M06 (focused-app
assignment to surface).

## Goal

A Tauri desktop app, launched from a tray icon, where every control on a
visual X-Touch Mini panel can be clicked to bind, MIDI-learn works
(physically touch a control on the real device to select it in the UI
instead of clicking), and profiles/bindings/app matchers/groups can be
managed without hand-editing `config.json`.

## Scope

**In**: `rustup` + Tauri Linux prerequisites actually installed and
`ui/`'s Tauri build working end to end (currently blocked — see
`specs/reference/environment.md`), Tauri commands proxying to
`daemon/internal/api`'s unix socket (replacing every "not implemented"
stub in `ui/src/api/client.ts`), a visual representation of the
controller's layout (from `specs/reference/xtouch-mini-midi-map.md`),
click-to-bind, MIDI learn (listen for the next controller event and use
it as the selection instead of a click), live state display over
WebSocket, app/group pickers backed by `GET /streams`-equivalent live
data, profile management, replacing `ui/src/types/config.ts`'s
hand-written mirror with generated types from `docs/config.schema.json`/
`docs/openapi.json`.

**Out**: anything the daemon itself doesn't support yet — this
milestone is a client for M04–M06/M08's surface, not a place to sneak in
new daemon behavior. Packaging/distribution of the UI app is M12.

## Design

Install the Rust toolchain (see
`specs/reference/environment.md`'s installation snippet) and confirm
`npm run tauri dev` actually launches a window before building
anything else. Then work outward from `ui/src/api/client.ts`'s stubs:
implement the underlying Tauri commands in `ui/src-tauri/src/main.rs`
(a unix-socket HTTP client — `hyper`+`hyperlocal` or a thin custom one,
per `Cargo.toml`'s TODO comment) one method at a time, replacing each
stub as its daemon-side support exists.

Type generation: once `daemon/internal/api` has real routes and
`docs/config.schema.json`/`docs/openapi.json` are generated (M01's
deferred item, M04's routes), regenerate `ui/src/types/config.ts` (or
replace it with a generated file entirely) rather than continuing to
hand-maintain it — its own header comment says as much.

The visual controller panel and MIDI-learn flow are new UI-only work
with no direct daemon-side dependency beyond the WebSocket state feed
and a way to say "the next raw input event, tag it as a selection"
(likely a small daemon-side "learn mode" toggle — worth a short design
note in this file once decided, since it's new API surface not covered
by M04).

## Data model changes

Likely a small addition to `daemon/internal/api` for a "learn mode"
endpoint/message, not to `daemon/internal/model` itself. Record the
actual shape here once designed.

## Acceptance criteria

- [ ] `npm run tauri dev` and `npm run tauri build` both work on the
      development machine.
- [ ] Every method in `ui/src/api/client.ts` has a real implementation,
      none throw "not implemented."
- [ ] Clicking a control on the visual panel opens a binding editor for
      it; saving persists to the daemon and is reflected on the physical
      device without restarting either process.
- [ ] MIDI learn: click "learn," then turn/press the physical control
      you mean, and the UI selects the right one.
- [ ] The tray icon's "Configure..." (or equivalent) menu item opens the
      window; closing the window doesn't kill the daemon.
- [ ] `ui/src/types/config.ts` is generated, not hand-maintained (or
      explicitly deferred here with a reason, if generation turns out to
      need more design work than expected).

## Verification

Manual, UI-driven: install the Rust toolchain, `npm run tauri dev`, and
walk through binding an encoder to an app via the UI, confirming it
takes effect live; use MIDI learn to bind a second control; restart the
daemon and confirm both bindings persisted.

## Risks & open questions

- Rust/Tauri toolchain setup is untested on this machine as of
  planning — first real risk of this milestone is just getting a Tauri
  window to open at all.
- Type-generation tooling choice (from JSON Schema/OpenAPI to TS) isn't
  picked yet — evaluate options (`openapi-typescript`, `json-schema-to-
  typescript`, etc.) once M01's schema-generation item is actually done.
