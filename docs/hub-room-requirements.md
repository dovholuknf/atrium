# The hub is a multi-tenant view of rooms

Requirements, given by clint on 2026-09-17 after looking at the first build and finding it did not match what
he wanted. **These override `docs/hub-room-plan.md` where they disagree**, and they override
`docs/federation-design-v2.md` outright on the aggregate question.

Written down verbatim in substance because the next session needs them before it needs anything else.

------------

## What he asked for

1. **All UI components are driven from the hub.** A room CAN serve a board, but it generally will not be used.
   The room's loopback board becomes an off-by-default fallback rather than a second place to look.

2. **Landing on the hub must say what it is.** Right now the only sign a room is attached is the
   `reconnecting` dialog, which reads as broken. Replace it with something that says: this is the atrium hub,
   `1/1 rooms` or `0/1 rooms` attached, you may run agents here but probably will not, and the rooms are where
   the agents are.

3. **Starting an agent picks a room.** With one room, use it and do not ask. With more than one, the launch
   dialog has to know which.

4. **The room counter is a selector.** Picking a room scopes the ENTIRE UI to it: from then on it is that
   room's atrium, with no room column, no room field and nothing asking again.

5. **No selection means all rooms.** The hub shows status across every room and every agent as one board.

6. **So the hub is a multi-tenant view of rooms.** Pick a tenant and focus, or see the whole.

7. **Transport is ancillary noise.** A badge at most. Never a concept in the UI.

8. **UI changes mean stopping the hub, and that must not interrupt a room's sessions.** Already built and
   proven.

------------

## Answers he gave to the open questions

- **A card shows its room in the aggregate view, probably as a TAG**, so it can be filtered with the existing
  chips later. Implementation is open.
- **Room-scoped config is edited on a page dedicated to that room**, so the "write to all rooms" problem
  mostly does not arise.
- **The hub is also a room if it wants to be.** Rooms being hubs is a later question.
- **Do NOT install anything over `C:\Users\claude\.atrium\bin\atrium.exe`.** The iteration happens outside the
  running atrium on purpose. A new hub and room may be brought up and maintained freely, on **ports 8000 and
  up**, so nothing is confused with the 7xxx daemons.

------------

## The line that falls out of it

Not "it depends per setting". It is:

> **Anything about THE BOARD belongs to the hub. Anything about A MACHINE belongs to a room.**

- **Hub:** the skin, `/v1/auth`, `/v1/overlays`. One board, so these cannot be per-room, and publishing the
  board is the hub's job now.
- **Room:** harnesses, rules, fixtures, sources, recognisers, actions, themes, and the machine-shaped settings
  (browse roots, editor, terminal, shell, scrollback).

That removes the aggregate-write question rather than answering it.

------------

## The one contradiction with what is built

Scoped mode is a byte proxy: the hub forwards to one room and understands nothing. **Aggregate mode cannot
be.** You cannot forward `/v1/tasks` to four rooms and concatenate bytes.

So the hub is two things:

| mode | what the hub is |
| --- | --- |
| scoped | a dumb pipe. Nothing parsed, nothing merged, answers arrive untouched |
| aggregate | a fan-out and a merge. It parses payloads and mints card identities across stores |

This is what `federation-design-v2.md` ruled out, and the requirement overrides it knowingly.

------------

## Where the work stopped

`internal/link/rooms.go` is written: the header and query parameter a board uses to name a room, and the
`room~id` scheme that lets an aggregate card be clicked without the board knowing rooms exist.

**The design decision already taken there, so it does not get re-litigated:** the hub rewrites card ids on the
way out (`sg4~01a0...`) and undoes it on the way in. Every per-card URL the board builds from a listed id then
carries its room, so attach, files, messages and the rest route correctly with no change to the board's
JavaScript. Two rooms can mint the same card id, so without it a click in the aggregate view reaches whichever
room answered first.

### Still to build

1. **Routing.** `roomFor(request)` reading the header then the query parameter, then scoped-proxy or aggregate.
2. **Fan-out and merge** for the list endpoints: `/v1/tasks`, `/v1/permissions`, `/v1/waiting`, tagging each
   row with its room and rewriting its id.
3. **The event stream.** `/v1/events` has to be fanned out and merged too, or the aggregate board is static.
   This is the biggest single piece.
4. **The board.** The room chip in the header, as a selector, sending `X-Atrium-Room`. The room tag on a card.
   The launch dialog's room field when there is more than one.
5. **Moving skin, auth and overlays to the hub.**

### Running it

Ports 8000 (board), 8001 (link), 8010 (room), 8011 (room agent). Binary
`D:\worktrees\github\dovholuknf\atrium\hub-room\build.claude\atrium2.exe`.
