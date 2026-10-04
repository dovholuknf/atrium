# f-pr-placement report

Branch `claude/f-pr-placement`, based on claude/main 5fc5b390.

## What I built

**Hub claim table.** `internal/hubstore/prclaim.go` and migration `0010_pr_claim` (added at the end of the slice).
One row per key, `host/org/repo/number` lower case, with the owner room name, a source, the time, and two columns for
the offline warning. `ClaimPR` is first-claim-wins and answers the owner either way. `MovePRClaim` is the only thing
that changes an owner.

**Placement.** `internal/link/prclaim.go`, `placePRRoom`. The least busy online room, where busy is the count of cards
that are running, needs-input or needs-permission in the room's own `/v1/tasks`, asked when a claim is placed (no
standing index). Rooms marked for deletion are skipped. A room that did not answer is passed over unless none did.
Checkout presence does not rank a room.

**Tie-break: stable order by room name.** The hub is told no CPU figure by any room today. Getting idle CPU would mean
widening a room report or adding a new call, so I did not. If you want CPU, the seam is the `load` struct in
`placePRRoom`.

**Claim flow.**
- A room reaches the hub for a claim on the existing `git` kind connection at `/_claim/pr`. The hub names the asker
  from the hello, never from a header the room wrote. No new listener.
- `api.postPR` asks before `CreatePR`, and outside `prOpMu` so a hub that forwards to another room cannot deadlock.
- Owner is the asker: it makes the row and the run. Owner is another room: it makes nothing, answers 200 with
  `held_by` and `claim: folded into <room>`. If the hub placed the key elsewhere and the ask carried the paste, the hub
  forwards the `POST /v1/prs` to the owner over the proxy it already has. If that forward fails the hub gives the key to
  the asker, since nothing was made on the owner.
- A room that already has a live row for the PR says `held`, so the hub records it as owner rather than placing it
  elsewhere.
- Hub unreachable (including a hub that predates this): the row is made with `claim: pending` (room migration
  `0081_pr_claim`). `Server.ReconcilePRClaims` asks again from `Room.OnAttach`, so it is event driven and there is no
  timer. If another room owns the key meanwhile, a queued row is aborted with "folded into O", and a started one is left
  and labelled, never deleted.
- A paste at the hub with two or more rooms and none named (it used to be the `needsARoom` 409) goes to the least busy
  room, which then claims.

**Pulls view fold.** `foldPRRows` in `pulls.go`. Two rows with one key on different rooms show as the claim's owner
(the earliest when nothing claimed it), the others are listed on it as `folded` and removed from the tab counts. Rows of
one room are never folded.

**Offline owner is a warning and never a move.** `prWarnSweep`, run from the growler's existing 30 second ticker (no new
timer). It raises a growler titled `WARNING: PR <key> is on <room>, which is offline`, once per offline spell, after the
room has been seen gone for 2 minutes (`prWarnGrace`) so a network blink or a hub restart raises nothing. It ends when
the room is back or the claim is moved.

**Manual move.** `POST /_hub/pr-claims/move {key, to}` (same cross-origin gate the change request writes use) and
`Proxy.MovePRClaim`. `GET /_hub/pr-claims` lists claims.

## Decisions

1. **There is no existing card move or `moved_to` code path** (room handoff is still design and backlog only), so there
   was nothing to hook. I built the hub-side update as `MovePRClaim` and the route above. The handoff item should call
   it when a PR's card moves. Moving the row's data between rooms is not done here.
2. **The alert uses the growler** (the board's existing persistent alert) under the existing `question` reason, because
   the `growl.reason` CHECK constraint would need a table rebuild for a new reason. The WARNING is in the title. No
   board JS change was made, so it draws as a question-class growler. If you want a distinct severity or reason that is
   a growler migration plus growl.js.
3. The claim table has no `row_id` column (the design listed one). The key is the identity and the hub never needs the
   row id. Say if you want it bound.
4. The hub places at claim time, including when the asker is a different room from the winner. That is section 6 step 3.

## Not done

- No idle-CPU tie-break (see above).
- No forge calls, PR worktrees, login alert (f-forge-access), Bitbucket or GitLab, per the brief.
- No board JS and no docs/backlog.md edit.
- A bare `/v1/prs` POST on a single-room hub is unchanged and the room still claims, so a one-room hub gets a claim row
  too.

## Tests

- `go test ./internal/link -run 'TestSamePR|TestPlacement|TestUnscopedPaste|TestOfflineOwner|TestManualMove|TestPullsViewFolds'`
  passes. Real hub and real link with two fake rooms: same PR from both is one row and one run on the least busy room,
  placement and tie, an offline owner raises one warning and does not move, a manual move updates the claim and ends
  the warning, the pulls fold.
- `go test ./internal/api -run 'TestAnotherRoomsPR|TestAnUnreachableHub'` passes: another room's key makes no row,
  pending, and reconcile (claimed, and folded with abort).
- `go build ./...` clean. Full-package results for hubstore, store, api and link are below.
