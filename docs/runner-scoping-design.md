# Room-scoped runners

The new-agent dialog offers the same runners on every room. Selecting room `claude-sgg`, picking `claude code`,
and launching failed with `claude is not on PATH` because sgg has no claude. Offering a runner a room cannot
start, then failing at exec, is the bug. A runner is not a universal preset. It is configured per machine, and the
picker must offer only the runners the selected room actually supports.

## Where runner definitions live today

Each room is its own daemon with its own store. `internal/store/harness.go` holds the `harness` table, seeded once
from `DefaultHarnesses()`. So a room already HAS its own runner rows, edited on that machine and no other. There is
no global runner catalog.

`internal/api/discover.go` `listHarnesses` serves that room's rows. Each row is wrapped in a `harnessView` that
adds `found`, the resolved path of `Cmd` from `exec.LookPath`, which is the same lookup launching does. So a room
already reports, per runner, whether its command resolves on that room's PATH.

## How the hub carries each room's list

`internal/link/fanout.go` lists `/v1/harnesses` in the `merged` table. Two modes:

- SCOPED (`X-Atrium-Room` set, or the board pinned to one room): the hub is a byte pipe. `/v1/harnesses` returns
  that one room's rows untouched. No `room` field, because there is only one.
- AGGREGATE (no room named): the hub fans out `/v1/harnesses` to every attached room, stamps `room` on each row,
  and concatenates. Harness ids are NOT tagged, because two rooms both having a `claude` runner is not a
  collision. They are two runners told apart by the `room` on the row.

So runner rows are ALREADY per-room and ALREADY carry `room` and `found`. The data the picker needs to scope
itself is already on the wire. What is missing is the board using it.

## The seam

In `internal/api/web/js/fixtures.js`, `openLaunch` fills the runner dropdown (`l-harness`) from
`allHarnesses.filter(h => h.enabled)`, which in aggregate mode is EVERY room's enabled runners. Separately,
`fillLaunchRoom` fills the room selector (`l-room`) from `hubRooms`. The two controls are independent. So on a hub
with two or more rooms you can pair room B with a runner that only room A has, and the launch is dispatched to B
with `X-Atrium-Room: B` and fails at exec. That independence is the whole bug.

## HUB-ONLY fix: filter the picker by the selected room

Board JS only, deployable on a hub restart. No room change.

1. When the room field is shown (aggregate, two or more rooms), filter `l-harness` to rows whose `room` matches
   `l-room`'s value, and to runners that are usable on that room, meaning `enabled && found`. A runner the room
   does not have `found` is not silently offered.
2. Rebind `l-room.onchange` to refilter `l-harness` and re-ask the per-runner model question, since changing the
   room changes which runner is selected.
3. When a room has no usable runner, say so in the dialog and point at that room's runners page, rather than
   letting the launch fail later.
4. Scoped or single-room mode is unchanged. Rows carry no `room`, there is one machine, and the full local
   enabled list is correct as-is.

This resolves the reported failure end to end, because a runner is only offered for a room where its command
already resolves.

## ROOM-SIDE gap (PARKED): explicit per-runner binary path

`found` today is PATH resolution only. atrium2 resolves a runner's binary against the room process PATH and
nothing else. On sgg, claude installed to `C:/Users/localai/.local/bin/claude.exe` but the room could not see it
because that directory was not on the room process PATH, so the only fix was relaunching the room with PATH
patched. PATH-only resolution means a supported runner reads as unavailable purely because of where the room
process was started.

The real "configured as to what it supports" fix is to let a runner carry an explicit binary path, per room:

- Add a `bin_path` field to `Harness` (`internal/store/harness.go`, plus the schema migration and the scan and
  save columns).
- `listHarnesses` resolves `found` from `bin_path` first (does this path exist on this room), then falls back to
  PATH resolution of `Cmd`. An explicit path that exists is authoritative.
- Launch uses `bin_path` when set instead of relying on PATH.

This makes availability detection reliable and independent of the room process PATH, and it is what lets a room
declare a runner it supports even when that runner is not on PATH. It is a store schema and daemon change, so it
is ROOM-SIDE and PARKED for a planned room restart. The hub-only picker filter lands first and works against
today's PATH-derived `found`. When `bin_path` ships, the same picker filter keeps working, because it reads
`found` either way.
