# PipeWire fixtures

`pw-dump-sample.json` is a hand-curated (not raw-dumped) subset of the
audio node graph captured live on the development machine on 2026-09-20,
trimmed to the properties the `audio.AppMatcher` in `daemon/internal/audio`
actually reads, and stripped of host-identifying fields (`application.process.user`,
`application.process.host`, `application.process.machine-id`, ALSA card
internals, etc.) that a raw `pw-dump` includes.

It exists to pin down two real edge cases the matcher must handle, both
marked with a `_comment` field (not a real PipeWire property — strip it
before feeding fixtures into anything that round-trips the JSON):

1. A stream (`java`, id 112) with none of `application.name`,
   `application.process.binary`, or `application.process.id` — only
   `node.name`. The matcher must be able to key on `node.name` alone.
2. Two applications (`vesktop`, `Pal`) that each publish **two**
   simultaneous sink-inputs. A binding targeting the app must resolve to
   every matching stream, not just the first.

See `specs/reference/environment.md` for the full context this was
captured in, and `daemon/internal/audio/matcher_test.go` (M03) for the
tests that consume this fixture.

## `focus-brave-pid-mismatch.json` (M06)

A second, separate fixture (not merged into `pw-dump-sample.json`,
whose existing tests assert exact ID lists against it) for
`daemon/internal/audio/focus_test.go`. It pins the case M06's
acceptance criteria name specifically: Brave's audio stream carries no
`application.id` at all — so matching a focused window's `resourceClass`
("brave-browser") against `AppMatcher.DesktopIDs` misses it before the
process-id question even comes up — and its
`application.process.id` differs from the focused *window*'s pid used
in the accompanying test, mirroring the real parent/child relationship
confirmed live on the dev machine (a browser's focused-window pid is
usually several process hops above the pid that owns its audio stream).
A second, unrelated stream is included so a test can assert an
over-broad match doesn't pull it in.
