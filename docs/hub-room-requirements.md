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

### Built

1. **Routing.** `roomFor(request)` reads the header, then the query parameter, then the room carried in a
   `room~id` path, then falls back to the only attached room. `internal/link/proxy.go`.
2. **Fan-out and merge** for the list endpoints, in `internal/link/fanout.go`. A table rather than a switch, so
   adding one is a row. Reads only: a write with no room to land in is refused with the rooms named. A room
   that does not answer is reported in `rooms_quiet` rather than emptying the board. `/v1/health` is merged
   separately and pessimistically, because the board decides atrium is up at all from it.
3. **The event stream**, in `internal/link/events.go`. The hub holds ONE upstream stream per room while
   anybody is watching and tears them down when nobody is. Three addresses, because an `EventSource` cannot
   set a header and so can only say what it wants in its URL:

   | address | what it carries |
   | --- | --- |
   | `/v1/events/hub` | every room, merged, ids tagged, each event carrying its `room` |
   | `/v1/events/room/<name>` | one room, untouched, exactly what that room sent |
   | `/v1/events` | whichever of the two the request turns out to mean |

   Tagged exactly when the lists are tagged, checked per event rather than per connection, so a room attaching
   under an open stream cannot leave the two describing different cards.

4. **The board**, in `internal/api/web/js/rooms.js`. The counter in the header is the selector. Scoping is one
   header added by wrapping `fetch`, and a query parameter added by wrapping `WebSocket`. A card wears its
   room as a tag in the merged view. The launch dialog asks which machine only when that is a question.

### Still to build

5. **Moving skin, auth and overlays to the hub.**

6. **An MCP panel on the runners page.** Which MCP servers a launched session gets, why, and a way to change
   it. Raised 2026-09-17 after a session came up with none and there was no way to see why from the board.

   The mechanism it has to expose, because it is not guessable: the `claude` harness carries
   `--strict-mcp-config`, which means a session gets ONLY the servers in the files named by `--mcp-config` and
   ignores the user, project and local configs a plain terminal would pick up. It was added so that servers
   which prompt for authentication could not hang a launch, and no config file was named because a relative
   `.mcp.json` is missing in most worktrees and a missing file fails startup.

   The resolution is an ABSOLUTE path, which `C:\Users\claude\.atrium\mcp.json` now is. The panel should show
   that file's servers, say that strict mode is on and what it excludes, and let the file be edited. The
   existing test `TestNoMCPConfigFileIsNamed` in `internal/store` pins the old behaviour and will need its
   reasoning updated rather than deleted: the objection was to a RELATIVE path, and it is still correct about
   that.

------------

### Running it

Ports 8000 (board), 8001 (link), 8010 and 8011 (first room), 8020 and 8021 (second room). Binary
`D:\worktrees\github\dovholuknf\atrium\hub-room\build.claude\atrium2.exe`.

The hub names its rooms, so a room is added on the hub and the name is minted into the join string. There is no
`--name` on the room: it is called what the certificate says, which is what the token asked for.

```
atrium2 hub --addr 127.0.0.1:8000 --link 127.0.0.1:8001 --dir <certs> --board <repo>\internal\api\web
atrium2 hub room add sparta --dir <certs> --link 127.0.0.1:8001
atrium2 join <token> --dir <d1> --db <d1>\atrium.db --http 127.0.0.1:8010 --agent 127.0.0.1:8011
atrium2 hub room add athens --dir <certs> --link 127.0.0.1:8001
atrium2 join <token> --dir <d2> --db <d2>\atrium.db --http 127.0.0.1:8020 --agent 127.0.0.1:8021
```

`atrium2 hub room ls` says what exists and which of them are here. `atrium2 hub room log` says what has happened
to any of them, including ones that have since been removed.

### One thing that is not a guarantee

**Order across rooms.** A merged list is every room's answer concatenated, and a merged stream is whichever
room spoke first. Within one room both keep that room's order. Across rooms there is no clock to sort by and
inventing one would be a lie about when things happened on four machines. The board sorts what it draws, which
is where the question belongs.
