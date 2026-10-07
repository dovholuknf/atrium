# f-new-hub-fetch-pass: a fetch at the hub is passed through to the room that has the work

Status: BUILT on claude/f-hub-fetch-pass (hub half held by @review on M1, a stalled pass-through with no deadline, and fixed in the next commit), with @review's OK on the room half (453e754a, landed as 3212e158) and the hub
half and the lows' decisions waiting for @review. Not landed. Owner @fabric, with @runtime reading the room's served set.
Design: `docs/fabric/hub-forge-design.md` 3.3, 3.4 and the stage 3 row of 7.

Done:
- the room's served set as a pure function (`ServedHide`), written per request, with `allowFilter` and every
  sha-in-want setting off and environment-only config;
- the hub's `/git/room/<room>/<repo>.git/...` route: protocol v0, `shallow`/`deepen`/`filter` refused before it is passed
  on, `503 <room> is not connected`, 6 fetches a minute per reader and 2 running per room, the board's reach rules,
  and a log line (room, repo, reach); nothing is stored;
- the review's lows: a card on the clone's own `main` is not served, a parked card stays served, cards are matched on
  org and host when recorded.

Left:
- `atrium git setup` (the `insteadOf` lines, `followRedirects=initial`, the token header): cut as not small. It needs
  the room list, the operator token and a yes/no prompt. The `room/<room>` URL form works without it.
- A card on a room asking for a pass-through over the link is stage 1's forwarder and answers 404 here.
- Matching cards to a repository on the FULL name for every card waits for stage 2's scm path.
- The real run on sg3 and m1mini (`docs/changes/f-new-hub-fetch-pass.md`, step 1) is the orchestrator's.
