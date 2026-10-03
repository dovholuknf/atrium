# Review: f-new-cross-room-wake 54af8c02

Range `526337e1..54af8c02`, one commit on claude/f-cross-room-wake.

- `wake` rides as a new field on the existing relay `say` op, so there is no new op.
- The hub posts it to the target card's `/message`, which already unparks on `wake: true` (since r-007, 09-29).
- A wake say that the sending room cannot relay is refused with a 503, not held.
- "local only" is gone from both schemas.

It merges onto landing cleanly and builds. Deploy: the hub and the SENDING room. The target room can be any version
that parks, because every such version takes `wake` on `/message`. Agreed.

Verdict: **HOLD on M1.** M1 is a small fix. Everything else is sound.

## How it was checked

- At the tip, `go build ./...` and `go vet` (link, cli, daemon) are clean. `go test ./internal/link/` passes, and so do
  cli `Wake|Say|Relay|Control` and daemon `Wake|Relay|Say|Park|Message|Tell`.
- Four mutants:
  - The hub dropping `body["wake"]` on the post fails 4 link tests.
  - The room holding a wake say fails `TestAWakeSayToAnUnreachableRoomIsRefusedNotHeld`.
  - The hub not turning a lost post into unconfirmed fails `TestAWakeSayWhoseResumeIsNotAnsweredIsUnconfirmed`.
  - `linkRelay.Say` dropping `Wake` (internal/cli/roomrelay.go) **survives**. That is L2.
- A probe for M1: a fake sender room answers `/v1/say` with the room's real 503 wake refusal, and I read what the
  hub's own `atrium_say` returns.
- The failures the worker named are not from this change. Each fails the same way on the tip and on landing:
  - `TestKeepaliveForkCarriesALeanCardsPromptToolsAndMCP` (the known macOS `/x` low);
  - the daemon `Host*` set (`TestHostCloseKillsItsRunners`, `TestIdleExit`, `TestReattachLosesAndRepeatsNothing` and
    8 more), from the socket path length;
  - the ptyhost and testguard packages.

## The three questions

1. **Version mixes degrade safely.** Nothing is ever delivered without the wake and without saying so.
   - **Old hub, new sending room.** The old hub's `RelayRequest` drops the unknown `wake`. The target answers
     `delivered: parked`, and the sender is told "not sent", so it is not silent. The advice in that note is wrong in
     this case (L1).
   - **New hub, old sending room.** The old room never sends `wake`, so it behaves as before.
   - **Old target room.** Every version that can park takes `wake` on `/message` (94d2951c). A room older than that
     has no parked cards.
   - **Hub tool, sender room older than `/v1/say`.** The hub delivers the say itself and passes `in.Wake`
     (`TestAHubSideWakeSayFromAnOldRoomStillResumes`).
2. **Never retried when unconfirmed: yes, on the room path.**
   - A post to the target that fails is marked Unconfirmed on the hub.
   - A relay whose answer is lost is `ErrRelayUnconfirmed`.
   - The room's `sayAcross` writes a `SayUnconfirmed` row, never calls `holdRelay` for it, and tells the sender to
     ask first.
   - The wake refusal returns before `holdRelay`, so the outbox never sees a wake say.
   - The hub-tool path gets the reverse wrong: a definite refusal reads as unconfirmed (M1).
3. **Wake does not widen who can be addressed.**
   - The hub resolves `to` with the same `resolvePeer` a plain say uses.
   - On the target, `/message` already lets any sender that can address a card wake it. The local `atrium_say` path
     has no narrower rule, so a remote sender gets exactly what a local one has.
   - **Cost:** a wake cold-starts only a card that is parked. Once it is running, more wake says are plain says.
     `peerSendsPerMinute` applies per sender on both sides, so a loop costs at most one cold start per park cycle.

## Medium

### M1: the hub's own `atrium_say` reports a refused wake say as `unconfirmed` (proven)

`controlMCP.sayAcross` (internal/link/control_relay.go) posts to the sender's room's `/v1/say`. It reads any 502, 503
or 504 as "your room did not answer in time … it may still deliver this". The new refusal in the room's `sayAcross`
(internal/daemon/relay.go, `if wake {`) is a **503**, and it means the opposite: nothing was sent or held.

The probe, with the sender room giving that 503:

    delivered="unconfirmed" note="your room did not answer in time (could not reach sg4 (not attached). nothing was
    sent or held, since a held message cannot wake a card. ...). it may still deliver this, so ask whether it
    arrived before sending it again."

That is the path a card on the hub's control uses, such as the orchestrator on sg4-control waking a parked director.
It is told the message may have arrived, so it waits to ask a card that is parked and cannot answer. That is the
stall this change exists to end.

The fix: answer the wake refusal with a code the hub does not read as "in flight", for example 409 or 424, with the
same text. Or add a field the hub checks before its 502/503/504 branch. Add a link test in which the sender room
answers with the refusal, and the hub tool returns an error, not `unconfirmed`.

## Lows

- **L1: an old hub gives the wrong advice.** When the room sent `wake` and the answer is `parked`, the hub dropped the
  field, so "send again with wake=true" sends the sender round in a loop until the rate limit stops it. In
  `sayAcross`, when `wake && res.Delivered == "parked"`, say instead "the hub is older than cross-room wake, nothing
  was delivered".
- **L2: the cli glue is untested.** `linkRelay.Say` copying `Wake` into `link.RelayRequest` is the one line between
  the room and the hub, and removing it passes every test. Add a cli test that drives `linkRelay` against a fake hub
  and checks `wake` on the wire.
- **L3: a slow resume reads as unconfirmed.** The hub's post is bounded by `controlTimeout` (8 s), and `unpark`
  launches the runner inside the post. A resume slower than that, for example a Windows cold start, still lands, but
  the sender is told "unconfirmed". That is safe, since nothing is sent twice. Say so in the change doc, or have
  `/message` queue the text before it unparks.

Atrium-Verdict: hold 526337e1..54af8c02
Quality: a small, well-placed change. It reuses the target's existing wake gate and the say op, and the rule that
refuses to hold a wake is right and well argued. M1 is the one place the hub contradicts it. m1mini commits are
unsigned.
