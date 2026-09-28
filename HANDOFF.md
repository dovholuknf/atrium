# sa58 handoff

## Phase

Step 1 of the brief (design), draft written, NOT reviewed. No code yet. No mercurius session opened yet (no session
id, nothing to resume). Handoff at about 151k context, asked by atrium-87300.

## Done

- Read BRIEF.md, backlog-2 item 58, `internal/daemon/peers.go`, `messages.go`, `a2a.go`, `contextsize.go`,
  `finish.go` (notice path), `internal/link/control_mcp.go`, `proxy.go` (roomFor, untag, serveControl),
  `announce.go`, `protocol.go`, `hub.go` (take), `room.go`, `internal/cli/control.go`, `control_peers.go`,
  `roomrun.go`.
- Asked atrium-87300 what m1mini cards run. Answer: NO atrium-control at all. Design the reply path as stdio
  `atrium control` -> local room -> link -> hub. `/_hub/mcp` stays loopback only. Also make provisioning
  (`scripts/provision-room.ps1`, item 46) register atrium-control at user scope, and say what a room without it does.
- Wrote `docs/cross-room-say-design.md` (draft). Read it first: every decision is in it.

## Decisions and why

- Grammar `name@room`, split on last `@`. Also `room~id` and `id@room`. Own room part = local. Sender always shown
  as `handle@room`, room part added by the hub from the link certificate (anti-spoof).
- New link connection kind `relay` (room dials, like `announce`), ops `say` and `peers`. Additive, no link Version
  bump. Old hub refuses the kind with a sentence, which the room turns into "hub is older".
- Cross-room says from `/_hub/mcp` are forwarded to the SENDER'S room `/v1/say`, so `peerSaid` / `MarkReported` /
  work ledger (room-side, keyed on the local sender card) stay the single record. Fallback to direct delivery for a
  caller with no room, or an old sender room (404).
- Cross-room with no sender identity is refused (it would be typed as the operator, rule 3).
- Offline target: sync try first. Definitive refusals are errors. Unreachable goes to a ROOM-SIDE `relay_outbox`
  table on the sender's room, `delivered: "held"`, drained on reaper tick, after insert, and on reattach, kept 24h.
  Notices and reports to a remote launcher use the same outbox. Hub holds nothing.
- Rule 5 needs cross-room lineage: hub `atrium_launch` gets an optional `room`, and `spawned_by` becomes
  `me@myroom` when it differs. `notifyLauncher` and `finish` (RecordReport, same transaction) enqueue for a remote
  launcher.
- `atrium_peers` gets `rooms: true` (hub-side from aggregate `/v1/tasks`, rows carry `room`), room-side via relay
  `peers`.
- stdio `atrium control`: `atrium_say` must send `from` = `ATRIUM_AGENT_NAME` (today it sends none, a rule 3 bug),
  accept the grammar via the room's `/v1/say`, and gain `atrium_report`.

## Files touched

- `docs/cross-room-say-design.md` (new, draft)
- `HANDOFF.md` (this file)

## Next steps, in order

1. Finish the design doc: check `internal/link/fanout.go` aggregate rows (line ~206, `obj["room"]`) for the peers
   list, `store.RecordReport` / `NoticeSpec` for the outbox-in-transaction change, `watchWorkers` retry behaviour,
   and how the hub answers a named room that is not attached (status code) so "unreachable" is classified right.
   Add before/after examples of what a sender and recipient see.
2. mercurius design review of the doc (`mercurius_open_session`, `mercurius_start_review_round`,
   `mercurius_collect_round`). Fold in or reject each finding with a reason, in the doc.
3. Build with tests. Link: `relay` kind in `protocol.go` hearHello, `hub.go` take, `Hub.Relay` handler, `Room.Relay`
   client. Hub: grammar in `control_mcp.go` say/peers/launch, relay server using `controlMCP.ask`/`resolvePeer`
   with the target room. Room: `/v1/say` on the board mux and `/tell` grammar in `peers.go`, `Daemon.SetRelay`
   wired in `internal/cli/roomrun.go` after `link.Room` is built, outbox table and drain, remote launcher in
   `a2a.go`/`finish.go`. CLI: stdio `control_peers.go` from + report + grammar. Script: provision-room.ps1 registers
   atrium-control at user scope. Tests with a fake second room (see `internal/link/*_test.go` for pipe hubs).
4. mercurius diff review.
5. CHANGELOG entry, new `docs/test-plan.md` section, backlog-2 item 58 status line.
6. ONE atrium_report to saorch (sa84-merger-owns-claude-main-workers-rep): shas, HUB-SIDE / ROOM-SIDE per part,
   migrations, test results, both mercurius session ids and verdicts.

Rules to keep: clear ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG before `go test`. TestRealSessionsKeepTheirText is
known noise. Builds to build.claude/. Never touch the live hub :7778 or room :7781. One-line commit subjects.
