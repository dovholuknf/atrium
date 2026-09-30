# A merged worker is culled without anybody remembering to

clint, 2026-09-29, through @orchestrator: finished worker cards pile up on the board because directors forget to
cull them. The fix must be mechanical and never depend on memory. For @runtime to build.

## What is true today

- `atrium_cull` (`internal/daemon/cull.go`, item 36) does the whole job safely. It refuses a card not tagged
  `atrium:subagent` and a branch not merged into the target. It keeps a dirty worktree. It never removes the main
  checkout or deletes main or the target branch, and it runs `git worktree remove` without `--force`. What it lacks
  is a caller: somebody has to remember.
- `cull.go` records why an automatic cull was turned down once: **"a merge is not an acceptance. A branch can land
  and still be sent back, and the worker is the cheapest place to make the fix while its conversation is still
  warm."** That reason still holds. The design below keeps it, with a window in which to send the work back.
- `WorkAccepted` exists in the ledger (`internal/store/ledger.go`) and no code ever sets it.
- The repository's hooks live in the common git dir (`.git/hooks`), shared by every worktree. Only `pre-commit` is
  active.

## The decision: the merge is noticed mechanically, and the cull follows after a grace period unless it is held

Chosen over "just show merged, cull?" because a chip is one more thing to remember, and remembering is what fails.
The grace period is what keeps "merge is not acceptance" true.

1. **A git `post-merge` hook says a merge happened.** It is installed once per repository by
   `scripts/install-git-hooks.ps1` (next to the existing `pre-commit`), and it runs in whichever worktree merged:
   @merge's, or a director's merging a worker into its area branch. It does one thing, best effort, and never
   fails a merge: `atrium merged --into <the branch HEAD is on>`. Fast-forwards fire `post-merge` too. The hook runs
   no check of its own. Git stays in the room's cull code, where it already lives.
2. **The room finds the workers that merge covered.** On `merged --into X`, it looks at every card tagged
   `atrium:subagent` whose launcher's worktree is on X, or where X is `claude/main`, and runs `inspectCull(worktree, X)`,
   the same check `Cull` runs. Each card whose branch is now merged, whose status is finished (`finishedStatus`),
   whose last report was `done`, and which has no turn running and no open question, is **marked**: `merged_at`,
   `merged_into` and `merged_sha` on its work item, and `cull_at` = now + `merged_cull_grace` (default 30 minutes,
   a daemon setting, `off` disables the whole thing).
3. **Everybody who needs to know is told once.** The board shows a chip on the card, `merged · culling in 28m ·
   keep`. The launcher gets one queued message from atrium: `u-006's branch merged into claude/runtime at 3a66510.
   It will be culled at 16:40. To keep it, atrium_cull card=u-006 hold=true.` The message waits for the turn (it is
   news, not an interruption).
4. **At `cull_at` the room calls `Cull(card, X)` itself,** with every check `Cull` makes today, re-run at that
   moment: still merged, still finished, not held, no new turn since the mark. Success sets the work item
   `accepted`, which finally gives the ledger its accepted state. A dirty worktree is kept, as `Cull` already
   does, and the card leaves.
5. **Held means kept.** `atrium_cull card=<id> hold=true`, or the chip's `keep`, clears `cull_at` and puts
   `held_by` on the card. A held card is never culled automatically again, only by an explicit `atrium_cull`. Any
   new turn on the card (the launcher sent it back to fix something) also clears the mark, and the next merge marks
   it again.

## What never happens

- A director, clint's own card, a fixture or anything not tagged `atrium:subagent` is never marked. That's
  `Cull`'s own first rule.
- Nothing is culled while it is working, asking, or holding a pending permission.
- No timer looks for merges. The only trigger is the hook, and the only timer is the per-card `cull_at`, which the
  sweep (`sweep.go`) acts on when it comes due.
- A hook failure never fails a merge. `atrium merged` answers fast and ignores its errors, like every hook
  (CLAUDE.md, daemon resilience 2).

## Across rooms: the four ways cull fails today, and the fix for each

Found by @orchestrator on 2026-09-29 against m1mini and sg3. Workers on remote rooms run in the rooms' own clones,
and their branches come back to this machine through `room-git.ps1 fetch` as `<room>/claude/<id>`. So the proof that
a worker's work merged lives HERE, where the area branch is, and the worktree and branch to remove live THERE.

1. **`atrium_cull` resolves a card only in the caller's room.** `t-003b@m1mini` and `m1mini~<id>` both answer "no
   session called". Fix: `atrium_cull` takes the same forms `atrium_say` takes (`name@room`, `alias@room`,
   `room~id`) through the same resolver, and a card on another room is routed through the hub to the room that owns
   it, the way item 68 made `atrium_exit` and `atrium_task` work. @fabric, both halves: the control tool is the hub's
   (`internal/link/control_mcp.go`, where `cullHandler` uses `resolvePeer` and `atrium_exit` already uses
   `resolveCard`).
2. **The merge proof cannot be made on the remote room.** Over the hub, `POST /v1/tasks/m1mini~<id>/cull` reaches the
   room and answers "there is no branch claude/ui to check it against", because a room's clone never has a
   director's area branch. Fix: **the proof is made where the area branch lives, and the room checks only what it
   can see.**
   - This machine's room runs the check against the fetched branch: `git merge-base --is-ancestor
     <room>/claude/<id> <into>` in the main checkout, after the fetch. The result is a proof,
     `{"into": "claude/ui", "tip": "<the fetched branch's sha>"}`.
   - The cull to the remote room carries that proof. The remote room accepts it only when its worktree's `HEAD` IS
     `tip`: nothing was committed there since the branch that merged was fetched. It then does the rest of `Cull`
     as today (the subagent tag, finished status, clean worktree, no `--force`), and deletes its local branch, whose
     commits are now in the area branch here. If `HEAD` differs, it refuses with "new commits since the merged
     branch was fetched: <sha> vs <tip>".
   - The post-merge hook of this design is where the proof starts. A merge here of `m1mini/claude/u-010` into
     `claude/ui` fires the hook on this machine, this machine's room sees that the merged ref is a fetched room
     branch, marks the remote card through the hub (the chip and the launcher's message are the same), and at
     `cull_at` sends the proofed cull. @runtime for the proof and the room-side check, @fabric for routing a cull and
     a mark to the owning room.
3. **A room whose build predates the cull endpoint answers 404** (sg3). Fix: the hub turns a 404 from a room's cull
   into `sg3 build <x> predates cull (needs <sha>). Update the room.`, never "no such card", and the mark is kept so
   the cull runs once the room is updated. The cull endpoint's commit goes into `atrium.requirements` as part of the
   binary floor (@fabric's design, question 4). @fabric.
4. **A card launched without `atrium:subagent` can never be culled** (m1mini u-010, `01a0ee82`). Two fixes, both
   mechanical:
   - **The launch path adds the tag.** `atrium_launch` called by an agent tags the new card `atrium:subagent`
     unless the caller asks for a resident with `atrium:director`, so a director cannot forget it. The launch cap
     already counts that tag, so a card that escapes it also escapes the cap, which is a second reason. @runtime.
   - **Cards already on the board:** a card with `origin:agent`, a recorded launcher (`work_item.launcher_id`) and
     no `atrium:director` tag counts as a worker for the mark and for `Cull`. The tag stays the rule for everything
     else. @runtime.

`room-git.ps1 push-base <room>` stays as it is. The hook on this machine is the one trigger for local and remote
workers alike.

## Tests (@runtime)

- A subagent card, done, whose branch a merge covered: marked, the launcher told once, culled at `cull_at`, and its
  work item `accepted`.
- The same card given a new turn inside the grace period: the mark is cleared and nothing is culled.
- Held: never culled automatically.
- A director whose area branch merged into `claude/main`: never marked.
- A branch merged, but the card still `running`: not marked. When it later reports done, the next merge marks it.
- A dirty worktree at `cull_at`: the worker leaves, and its worktree and branch are kept with the reason, as `Cull`
  does today.
- `merged --into` with the room down: the hook returns 0 and the merge is untouched.

## Owners

@runtime: `atrium merged`, the mark, the grace, the hold, the sweep acting on `cull_at`, the board chip data,
`install-git-hooks.ps1`, the merge proof and the room-side tip check,
the launch path adding `atrium:subagent`, and the `origin:agent` rule. @fabric (f-010): `atrium_cull` resolving other rooms'
cards, routing a cull, a mark and a proof to the owning room, turning a room's 404 into "update the room", and the
binary floor key. @ui: the chip and its `keep`.

Tests to add for the cross-room half: a remote worker whose fetched branch merged here is marked and then culled with
a proof. A remote worktree with a commit after the fetch refuses. `atrium_cull t-003b@m1mini` resolves. A room that
answers 404 keeps the mark, with the update message. An agent launch without the tag gets it.
