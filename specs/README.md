# specs

knobd is built spec-driven: every milestone is designed here before it's
implemented. This directory is the source of truth for *why* the daemon
is shaped the way it is and *what "done" means* for each remaining
milestone — not a duplicate of what the code already says. When a spec
and the code disagree, that's a bug in the spec (update it) unless the
code diverged from it by mistake (fix the code).

## Layout

```
reference/    Facts: the hardware protocol, the dev environment, the
              full brainstormed action catalog. Not milestones —
              things every milestone can cite instead of re-deriving.
adr/          Architecture Decision Records: the load-bearing decisions
              behind the daemon's structure, and why the alternatives
              were rejected.
milestones/   M01-M12: one file per unit of work, in dependency order.
```

## Milestone status

| # | Milestone | Status | Depends on |
|---|---|---|---|
| [M01](milestones/M01-foundations.md) | Foundations | Done | — |
| [M02](milestones/M02-midi-transport.md) | MIDI transport | Done | M01 |
| [M03](milestones/M03-audio-control.md) | Audio control | Done | M01 |
| [M04](milestones/M04-mapping-engine-daemon.md) | Mapping engine & daemon | Done | M02, M03 |
| [M05](milestones/M05-led-feedback.md) | LED feedback | Done | M04 |
| [M06](milestones/M06-focus-tracking.md) | Focus tracking | Done | M04 |
| [M07](milestones/M07-config-ui.md) | Config UI | Done (hand verification with hardware still open) | M04 |
| [M08](milestones/M08-layers-groups-scenes.md) | Layers, groups, scenes | Not started | M04, M07 |
| [M09](milestones/M09-media-transport-mpris.md) | Media transport (MPRIS) | Not started | M04 |
| [M10](milestones/M10-spotify-web-api.md) | Spotify Web API | Not started | M09 |
| [M11](milestones/M11-extended-actions.md) | Extended actions | Not started | M04 |
| [M12](milestones/M12-packaging.md) | Packaging | In progress (hand verification with hardware still open) | M02–M09 (at least) |

M04 is the line where the project becomes actually usable: turn a knob,
an application's volume changes. Everything before it is plumbing;
everything after it is width (more actions, nicer UI, LEDs, other
integrations) rather than depth.

## Spec template

Each milestone file uses these sections, in this order:

1. **Status** — Not started / In progress / Done, plus the date it last
   changed status.
2. **Depends on** — which other milestones must be done first, and why.
3. **Goal** — one paragraph: what becomes true when this ships.
4. **Scope** — an explicit in/out list. The "out" list matters as much
   as the "in" list; it's what stops a milestone from quietly absorbing
   the next one.
5. **Design** — the approach, referencing existing code/types by path
   and existing ADRs by number rather than re-explaining them.
6. **Data model changes** — new/changed types in `daemon/internal/model`
   or the config schema, if any.
7. **Acceptance criteria** — a checklist. A milestone is Done when every
   box is checked, not before.
8. **Verification** — concrete steps: commands to run, what to click,
   what physical action on the controller to perform and what should
   happen.
9. **Risks & open questions** — named, not buried in prose.

## Conventions

- Milestones are numbered in the order they unlock each other, not
  necessarily the order you have to build them — M09 (media transport)
  and M11 (extended actions) only depend on M04 and could be built in
  either order, or in parallel with M05/M06.
- A spec references code by path (`daemon/internal/model/action.go`),
  not by pasting code into the spec. Specs go stale slower that way.
- If implementing a milestone reveals the spec was wrong about something
  non-trivial, fix the spec in the same change — don't let it silently
  drift from what got built.
