# Review: f-launch-room e45b841e..bf3630f0 (m1mini, 2026-10-02): ROOM DEPLOY OK

One commit on claude/f-launch-room: `room` on the room-local (stdio) `atrium_launch`, carried by a new relay op
`launch` through the hub to the target room. A priority from the orchestrator and clint, since @review needs it to
place reviewers on another room. Unsigned.

## The six asks

1. **Lineage and identity: right.**
   - The launcher's room comes from the connection. `launchAcross` uses `from`, the attached room's name from its
     certificate, never a request field: `spawned_by` is `me@from`, and `spawned_by_id` is `from~id`, resolved with
     `resolvePeer` on `from`.
   - The handle itself is the session's own `ATRIUM_AGENT_NAME`, read by the stdio tool from its environment, not a
     tool argument, so the agent cannot pick it.
   - A local process posting `/v1/peers/launch` can name a handle on its own room, which is the existing same-room
     trust (the 2026-09-30 audit's H3). The audit line marks it "(claimed)", which is honest.
2. **No new reach: right.**
   - The relay is served only to attached rooms.
   - `reachable` runs before anything is posted (a 404 with the known rooms, or unreachable).
   - The gate and the cap on the target are inside `launchOnRoom`.
   - The target's own `/v1/launch` refuses `ATRIUM_*` env (daemon/launch.go:405).
   - The brief travels in the body and is written by the target. The launcher never stats `cwd` and never writes on
     its own disk.
3. **Unconfirmed: right.**
   - A 502, 503 or 504 after the post is `Unconfirmed`, never `unreachable`. The room answers 504 "may or may not
     have started… not retried".
   - A failure before the post (not attached, dial, hello) is `ErrRelayDown`: "nothing was started, nothing is held".
   - Nothing on this path queues, unlike a say.
   - Low, no change asked: a `writeJSON` error after the bytes reached the hub would read as "nothing started". The
     hub's decode fails on a partial request, so it is near enough.
4. **The extraction kept the hub tool's behaviour: right.** `launchHandler` now ends in `launchOnRoom` with
   `callerRoom := roomOf(req)` passed in where it used to read `roomOf(req)`. The gate, cap, tags, post, naming and
   notes are the same code. `LaunchStartedNote` is the old string, now a constant.
5. **Old hub and old room messages: right.** `ErrRelayOld`, an old hub's "does not know the relay op", and
   `olderRoom` on the stdio side each give a sentence naming what to update.
6. **The mutation list is real.** Three of its mutations, run here, each turned tests red:
   - lineage taken from the target fails 3 tests;
   - a gateway failure no longer marked unconfirmed fails `…FailsAfterThePostIsUnconfirmed`;
   - `reachable` removed fails 2 tests.

Tests: `go vet` is clean on link, cli and daemon. `go test` passes on link (`Launch|Relay`), cli (`Launch|Room`)
and daemon (`RelayLaunch|RoomLaunch|Relay`).

Note for the rebase: @runtime's unlanded claude/r-exit-guard touches the same exit-op files, so one of the two will
need a rebase.

Closed: none (no findings)
Open: none

Verdict: ROOM DEPLOY OK e45b841e..bf3630f0, hub-ok and room-ok.

Quality: careful. One launch path with two doors, identity from the connection, and the not-idempotent rule
carried through every layer, with tests that catch the mutations.
