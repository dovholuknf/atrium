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
   for "mtls and ziti". **mTLS only when true.** A direct rung always dials TLS (`Direct.Dial`, `direct.go`), but it
   presents a client certificate only when the room has enrolled (`room.crt` and `room.key` loadable in `clientTLS`).
   So `PathState` gains `mtls bool`, set by the rung when that certificate loads. The chip reads `mTLS` (padlock) when
   true and stays `direct` otherwise, an unenrolled room or an older build that sends no flag. The sg4 rooms are checked
   at build time: the loopback rung is the same `Direct` dial, so it is TLS too, and whether it carries a client cert
   depends on enrolment, which the flag reports rather than assumes.
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
wire but the `mtls` flag. Change: `ProbePaths` also dials the `on` rung once, off the control connection, for a real
connect time, and keeps it `on`. A failed extra dial never changes the state, it only leaves `rtt_ms` empty.

**The probe is bounded.** A ziti `Dial` ignores its context, so `dialWithin` alone cannot stop a hung dial from piling up
(the pileup bug already fixed once this session). Each rung keeps a "probe in flight" flag. A round skips a rung whose
last probe has not returned and reports that rung's previous `rtt_ms` marked stale (`stale: true`, shown in the tip as
"last measured"). The probe never takes a `maxDataDials` slot and never calls `dataFailed`: it is observation, outside
the data dial accounting. This applies to the idle rungs too, since they have the same hazard.

## Lag tile: the choice

Both. The tile is renamed `timer drift` (tip: "how late this room's timers fire, p99 over 10 seconds. Not network
latency"), and each path chip's tip shows its round trip (`rtt_ms`, labelled "connect time"). No new stored value:
`rtt_ms` is already in the beat.

## Config needed for every room to attach over both (the launcher does this, nothing here touches live rooms)

- **Hub (sg4)**: `--transport direct,ziti` with the ziti identity and `--atrium-service atrium-hub`, already in
  `scripts/live/live-common.ps1`.
- **Each remote room (m1mini, sg3)**: a ziti rung, `atrium room transports ziti --identity <jwt or json> --service
  atrium-hub`. This needs no extra yes.
- **claude-sg4**: already has its ziti identity and the loopback direct rung.
- Order stays whatever `atrium rooms transports` says. Ladder order decides which is `on`, the other should show `up`.

### Needs clint's yes, and is NOT in the launcher's to-do list

For m1mini and sg3 to also attach by direct mTLS, the hub's link listener has to leave loopback: `--link 0.0.0.0:7779`
plus `--link-advertise <address>:7779`. That puts the port on the LAN. What guards it: the listener is TLS 1.3, the hub
refuses control and data from any peer without a client certificate it signed itself (the hello handler, `direct.go`),
and a room the hub has no record of cannot attach. Without that yes the two remote rooms show ziti `on` and direct
`down` or `none`, which is the honest picture.

## Test plan

- Go unit: `ProbePaths` gives the `on` rung an `rtt_ms` without changing its state, and a failing extra dial leaves
  `on` with no `rtt_ms` (fake dialers, under the 10ms budget).
- Go unit: a blocking dialer, 10 probe rounds, at most 1 dial in flight per rung, the skipped rung reports its previous
  rtt as stale, and no probe takes a data dial slot or counts toward `dataFailed`.
- Go unit: `mtls` is true only for a direct rung with a client certificate.
- Headless `pathBadges` unit: chips read `mTLS` (only when `mtls` is true, else `direct`), `ziti`, `zrok`; `on` and `up`
  carry the rtt in the tip; a room with both `on`/`up` shows two lit chips; the lag tile reads `timer drift` and its tip
  says it is not network latency.
- `bootClean`, `scripts/test-touched.sh`, `gofmt -l`, `check-test-globals.go`.
- PNGs under `img/f-rooms-dual-transport-badges/`: before and after of the demo rooms row, with a README line.
