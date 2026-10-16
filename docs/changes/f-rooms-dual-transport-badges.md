Docs: none, covered by docs/fabric/multi-transport-design.md and this note

# Rooms: mTLS and ziti badges together, and an honest lag tile

## What already landed (check, do not rebuild)

- **ph1/ph2** (`room-transport-order.md`, `f-multi-transport-rooms.md`): a hub listens on several transports at once
  (`--transport direct,ziti`), a room holds an ordered ladder of rungs (a direct address, a ziti service, zrok), attaches
  on the first that answers and falls back down the ladder. The hub's listener order and addresses reach rooms live.
- **ph3** (`internal/link/pathstate.go`): every 30 seconds a room dials each rung it is not standing on and reports
  `paths` in its beat: `{kind, addr, state, rtt_ms, last_error}` with state `on | up | down | none | unlistened`. The room
  card draws one `pth` chip per kind (`direct`, `ziti`, `zrok`) from `pathBadges` in `rooms.js`. The hub's `/v1/health`
  lists its own listeners.
- The `ziti` chip clint saw on m1mini is the older one-chip `transportBadge` (what the control connection is on). m1mini
  shows no second badge because it has no direct rung that answers, so this is configuration, not missing code.

## What is still missing

1. The `direct` path chip says `direct`, while the other chip on the same card calls the same thing `mTLS`. Clint asked
   for "mtls and ziti". The path chips become `mTLS`, `ziti`, `zrok`, with the same padlock and logo icons as the
   attached-over chip.
2. The rung a room stands on is never probed, so the connected path has no round trip. Only the idle ones carry `rtt_ms`.
3. The `lag` tile is in-process timer drift p99 (`internal/roomstats/drift.go`), not network latency.

## Badge states (unchanged vocabulary, plus a label)

| state | meaning | look |
| --- | --- | --- |
| `on` | the control connection is on it | solid |
| `up` | a probe from the room answered | outlined |
| `down` | the probe did not answer, the error is in the tip | warning colour |
| `none` | the room has no rung of that kind | grey italic |
| `unlistened` | the hub is not listening on that kind | grey italic |

"up, down" in the ask maps to `on`/`up` and `down`. Both transports lit means both rungs are `on` or `up`.

## Where the state comes from

The room's own view, from `Ladder.ProbePaths`, carried in the beat and held on the hub's room row. Nothing new on the
wire. Change: `ProbePaths` also dials the `on` rung once, off the control connection, to get a real connect time, and
keeps it `on`. A failed extra dial never changes the state (the control connection is the proof), it only leaves `rtt_ms`
empty. Cost is one short extra connection per rung per 30 seconds, which is what the idle rungs already do.

## Lag tile: the choice

Both. The tile is renamed `timer drift` (tip: "how late this room's timers fire, p99 over 10 seconds. Not network
latency"), and each path chip's tip shows its round trip (`rtt_ms`, labelled "connect time"). Putting the number in the
chip text itself would crowd the card, so it stays in the tip, and the chip text gains `· 2ms` only when the room is
wide. No new stored value: `rtt_ms` is already in the beat.

## Config needed for every room to attach over both (launcher does this, nothing here touches live rooms)

- **Hub (sg4)**: `--transport direct,ziti` with the ziti identity and `--atrium-service atrium-hub`, already in
  `scripts/live/live-common.ps1`. The hub's direct listener has to be reachable by each room: bind wide
  (`--link 0.0.0.0:7779`) and `--link-advertise <lan or public address>:7779` for m1mini and sg3, which today cannot
  reach sg4 directly.
- **Each remote room (m1mini, sg3)**: a direct rung to that advertised address (picked up from the hub's list at the next
  attach, or `atrium room join <string>` once), and a ziti rung: `atrium room transports ziti --identity <jwt or json>
  --service atrium-hub`.
- **Rooms on the hub's own machine (sg4, claude-sg4)**: already direct over loopback and a ziti identity on claude-sg4;
  sg4 itself shows `here` and has no ziti rung.
- Order stays `ziti,direct,zrok` or whatever `atrium rooms transports` says. Ladder order decides which one is `on`; the
  other should show `up`.

## Test plan

- Go unit: `ProbePaths` gives the `on` rung an `rtt_ms` without changing its state, and a failing extra dial leaves
  `on` with no `rtt_ms` (fake dialers, under the 10ms budget).
- Headless `pathBadges` unit: chips read `mTLS`, `ziti`, `zrok`; `on` and `up` carry the rtt in the tip; a room with
  both `on`/`up` shows two lit chips; the lag tile reads `timer drift` and its tip says it is not network latency.
- `bootClean`, `scripts/test-touched.sh`, `gofmt -l`, `check-test-globals.go`.
- PNGs under `img/f-rooms-dual-transport-badges/`: before and after of the demo rooms row, with a README line.
