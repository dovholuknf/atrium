# Branch status: landed, open, orphaned, room-only

Status: DESIGN. Nothing here is built. It is for @rnd review before any build. Backlog item `f-branch-status-view`.
Input: `docs/research/branch-audit-2026-10-12.md` on `claude/branch-audit`, and a measurement on the live hub mirror
(section 8).

## 1. The problem

The operator does not want to lose work on a branch and cannot see branches anywhere. The hub holds hundreds of
`claude/*` branches. Landing rewrites history (cherry-pick onto `claude/main`, re-sign onto origin's `main`), so every
SHA changes and ancestry calls almost every branch unmerged. A status has to be by content. The board shows only what a
human acts on, so each status below names the action.

## 2. What the hub already has

Most of the parts exist. The design is mostly joining them.

| Fact | Where | Note |
| --- | --- | --- |
| Hub branches and tips | `internal/gitsync/store.go` `View`, `BranchView{name, sha, room, card, at, released}` | read from the viewcache snapshot, no git on a GET |
| Patch-id match against a ref | `landfacts.go` `carriedBy`, `patchIDs` | per branch, ancestor first, then `merge-base`, then `git patch-id --stable` on both ranges |
| Changelog fallback | `landfacts.go` `landedByItem`, `addedChangelog` | the item's `changelog/<dept>/<date>-<id>.md` was added on `claude/main` |
| Room branches, never pushed | `hub.go` `Collect` | every 5 minutes the hub fetches each room's `claude/*` into `refs/rooms/<key>/claude/*` of the mirror bare |
| Which branch a live card is on | `internal/cli/roomrun.go` `selectLive` | a card is live while `running`, `needs-input` or `needs-permission`, matched to a repo by `RepoMatches`, branch is `Task.Branch` |
| Who pushed a branch | `git_push` table, `Pushed` in `pushed.go` | room, card, time |
| Hub pass-through lookup | `lookup.go` `URLAnswer` | `ahead` says a room has commits the hub's store lacks |

`carriedBy` is per branch and walks `merge-base..branch` and `merge-base..ref`. On a branch forked before the rewrite
that range is the whole pre-rewrite history, and the audit saw plain `git cherry` fail to finish in ten minutes. The
design therefore does not call `carriedBy` once per branch. It hashes once for all branches (section 4).

## 3. The four statuses and their exact rules

Inputs for a branch B in repository R, computed at main M (`claude/main`, the integration branch, which is also what
`LandFacts` uses):

- `C(B)` is the non-merge commits reachable from B and not from M, `git rev-list --no-merges B ^M`.
- `P(M)` is the set of `git patch-id --stable` of every non-merge commit reachable from M.
- `live(B)` is true when a live card's `Task.Branch` equals B in R (section 6).

A commit c in `C(B)` is **carried** by M when any rule holds, tried in this order and stopped at the first:

1. **Patch-id.** `patchid(c)` is in `P(M)`. This is a plain cherry-pick or a rebase without conflict.
2. **Subject.** c has no patch-id (an empty commit, a root, a walk) or none matched, and its first line equals a subject
   on M. This catches a cherry-pick whose conflict resolution changed the diff but not the message. It can be wrong
   when two commits share a generic subject, so it is recorded as the weaker evidence (`how: subject`).
3. **Item changelog.** B added a `changelog/<dept>/<date>-<id>.md` that exists at the same path on M. This catches a
   squash or a conflict-resolved pick where no per-commit rule matches. Per BRANCH, not per commit: it carries every
   commit of the branch. Recorded as `how: changelog`.
4. **Reverse apply** (phase 3 only, optional). `git apply --cached --check -R` of the commit against M's tree in a
   scratch index. If it applies, the change is on M in another form. The audit found 2 of 53. It is the only rule that
   needs a worktree or index, so it is deferred. A failure proves nothing, because a later commit may have changed the
   same lines.

The fallbacks are not interchangeable with patch-id. They are listed per branch (`how`) so the board can show "landed
(by changelog)" with a lighter chip than "landed (by patch-id)", and so a wrong call is visible.

### The status

```
nothing in C(B)                          -> landed   (how: ancestry)
every c in C(B) carried by 1, 2 or 3     -> landed
else, a room holds B with other commits  -> room-only wins over the rest, see below
else, live(B)                            -> open
else                                     -> orphaned
```

- **landed.** Every change is on M. Per commit stored, per branch derived, so a branch that gets a commit after landing
  flips back (the audit's question 2). A branch with `C(B)` empty because B is an ancestor of M is landed too.
- **open.** Not landed, and `live(B)`. Someone is on it.
- **orphaned.** Not landed, no live card. This is the list where work is lost. Carries a `kind`: `code` when any
  uncarried commit touches a path outside `docs/`, `changelog/` and `*.md`, `docs` otherwise. A docs-only orphan is a
  report or interview that can be harvested, a code orphan cannot. The path rule is one small list, kept in one place.
- **room-only.** A commit of B exists on a room and was never pushed to the hub's store. It is not a fourth way to
  compute landed. It is a second place to look. Rule: the room's tip of B (from `refs/rooms/<key>/claude/B`) has at
  least one commit not reachable from any ref in the hub's store AND not carried by M. A branch name that exists only
  on a room is room-only with `hub_sha` empty. A branch on both with the room ahead is room-only with `ahead: N`. If
  the room's commits are all carried by M, the branch is `landed` with a note `room copy only`, since only a spare
  copy is at risk (the audit's 67).

`room-only` is reported WITH the status the room's content would have (`room_status`: landed, open or orphaned), so a
room-only branch that is also dead shows both. On the board it sorts above plain orphaned, because the hub holds no copy
at all and a dead disk loses it.

Nothing is `deleted`. A branch that is gone from the hub and from every room is gone, and the view cannot show it.

## 4. Where the hub computes it, and when

**In the hub, in the gitsync package, as a snapshot like `viewcache.go`.** Never on a request. A GET reads the last
snapshot, with `computed` and `building` in the answer, exactly as `/_hub/git/repos` does (a process spawn is 50 to
100ms on Windows).

**One pass per repository, not one call per branch:**

1. `git log --no-merges -p --format='commit %H' M | git patch-id --stable` gives `P(M)` and commit to patch-id for main.
2. `git log --no-merges -p --format='commit %H' --glob='refs/heads/claude/*' --glob='refs/rooms/*/claude/*' ^M |
   git patch-id --stable` gives one patch-id per commit that is on some branch and not on M, each commit hashed once
   however many branches contain it.
3. `git for-each-ref` for every branch tip, then `git rev-list --no-merges B ^M` per branch (no diff, cheap) and a map
   lookup per commit.
4. Subject and changelog fallbacks only for branches left over, using `git log --format=%s M` once and
   `git log --diff-filter=A --name-only B ^M` per leftover.

**Cost, measured** (section 8): step 1 took 5.4 s for 3337 commits and step 2 took 10.7 s for 5455 commits, on a laptop.
Whole pass about 17 s on 349 branches. Too slow for a request, fine for a background pass.

**When:**

- **Incremental by cache.** Keep `commit sha -> patch-id` in memory and on disk (a table `branch_commit_pid` in the hub
  store, or a file beside the mirror). A commit's patch-id never changes, so after the first pass step 2 hashes only
  commits it has not seen. `P(M)` is extended by `git log M ^<last M>` only. A pass after one push hashes a handful of
  commits and costs the `for-each-ref` and a few `rev-list`s, well under a second of git.
- **Triggers.** After a push to the hub's store (the same hook that already rebuilds the viewcache), after a mirror
  pass that moved main, and after a collect that moved a room ref (`CollectRepo.Moved`). Plus the viewcache's timer as a
  backstop (20 s is too often for the full pass, so the status pass runs on its own timer, 5 minutes, and only when
  something moved).
- **Main moved.** A landing moves M, which can flip many branches from orphaned to landed at once. This is the case the
  cache pays for: only the new M commits are hashed, then every branch's uncarried set is re-tested against the
  extended `P(M)` from memory.
- **Bounded.** The same `Runner` and `CommandBound` as the rest of gitsync. A pass that fails leaves the previous
  snapshot and says why (`error`). A repository with no `claude/main` is skipped with a reason, as `LandFacts` does.
- **No state that can halt.** The snapshot is a cache, derivable from git. Losing it costs one slow pass. It is not a
  source of truth and cannot halt the hub.

## 5. How a room-only commit is learned without calling a room's forge

The hub never asks GitHub about a room's work and a room never fetches from a forge. The room already hands its
branches to the hub: `Collect` fetches `refs/heads/claude/*` (except `claude/main`) from each room over the existing
room link, into `refs/rooms/<key>/claude/*` of the hub's mirror bare, every 5 minutes and after a sync. So the commits
are already on the hub's disk, and the status pass reads them with the same `git rev-list` it uses for hub branches. No
new protocol, no new endpoint on the room, no call to any forge.

Limits, to say on the board rather than hide:

- **Only `claude/*`.** A card on a branch with another prefix is not collected. `Live` in `RoomHandler` serves live
  cards' branches for pass-through fetch but `Collect` does not use it. Phase 4 widens the collect refspec to the live
  cards' branches, or the board says "branch not named claude/*".
- **A same-machine room is skipped** (`SameMachine`), since the hub's own checkout already sees its branches. Its
  branches come from the checkout's `refs/heads`, not from `refs/rooms`. The pass reads both.
- **An offline room** keeps its last collected refs, and the row shows the collect time (`room_at`). A branch that
  exists only on a room that has never been collected is invisible to this view, and that is the one thing it cannot
  see. The audit read rooms over ssh to find these. The view answers it with a `rooms` line in the header: "room X: last
  collected 3 days ago".
- **Uncommitted work is not a commit** and never reaches the hub. A dirty worktree is a different signal (phase 4).
  The room reports a dirty count in its collect answer (`git status --porcelain` count per live worktree), not the
  hub, so no forge or ssh is involved.
- **The same name on two rooms** (the audit saw `claude/fabric` on sg4 and m1mini) is two room tips of one branch. The
  row lists each room's tip and the status uses the one with the most uncarried commits.

## 6. The live-card fact: open versus orphaned

**A live card is one whose status is `running`, `needs-input` or `needs-permission`, whose repo matches R by
`gitsync.RepoMatches`, and whose `Task.Branch` equals the branch name.** This is exactly `selectLive` in
`internal/cli/roomrun.go`, the rule that already decides which branches a room serves. Reusing it keeps "live" meaning
one thing. A parked card (no process, status kept) is live: it still owes work. A `done`, `paused`, `dead` or `backlog`
card is not.

Why Branch and not the worktree path (the audit matched `where` to a worktree path): the branch is what the hub holds,
a worktree path is a room fact, and two cards can share a worktree while only one owns the branch.

How the hub gets it: the room already knows (`liveBranches`). Two carriers, pick in review:

- **a.** The room adds `live` (branch names per repository) to its collect answer, and to the sync status it already
  returns. Cheap, one more field, and fresh at every collect.
- **b.** The hub joins the card list it already holds for the board and the peers tool.

Recommendation: **a**, because the card list on the hub is a view assembled per request from rooms, and a status that
depends on a room being asked right now would flap. A room that is offline keeps its last `live` set with its time, so
its branches stay `open` for a bounded time (`live_stale_after`, default 24 h) and then fall to `orphaned` with a note
"card's room offline since". That is the honest answer: nobody can say it is being worked on.

A done card's branch is `orphaned` if unlanded, which is the point: finishing a card that never landed is how work gets
left. The card's id and last status still show on the row (`last_card`) so the human knows whose it was.

## 7. API shape

One new hub route, read from the snapshot. Under `/_hub/git/` beside `repos` and `pushed`, so the board's existing proxy
and auth posture apply and nothing new is exposed.

```
GET /_hub/git/branches?repo=<name>&status=<s>[&status=...]&kind=<code|docs>&limit=N
```

```json
{
  "computed": "2026-10-16T14:02:11Z", "building": false, "main": {"sha": "d37bb785", "ref": "claude/main"},
  "counts": {"landed": 305, "open": 3, "orphaned": 38, "room-only": 4},
  "rooms": [{"room": "sg4", "collected": "2026-10-16T13:58:00Z", "online": true}],
  "branches": [{
    "repo": "github/dovholuknf/atrium", "name": "claude/interview-ci-scripts",
    "status": "orphaned", "kind": "docs",
    "hub_sha": "ab12cd34", "room_sha": "", "ahead": 0,
    "commits": 11, "uncarried": 11, "how": "",
    "last_commit": "2026-10-12T10:21:00Z", "files": ["INTERVIEW.md"],
    "owner": {"room": "sg4", "card": "t-1042", "pushed": "2026-10-12T10:25:00Z"},
    "live_card": null, "last_card": {"room": "sg4", "card": "t-1042", "status": "done"}
  }]
}
```

- `status` is exactly one of `landed`, `open`, `orphaned`, `room-only`. `how` is `ancestry`, `patch-id`, `subject`,
  `changelog` for a landed branch.
- `room_status` is present on `room-only`.
- `counts` is for the filtered repo, over ALL branches, so the chips show totals whatever the page of rows is.
- Branch names pass `CheckPushBranch`/`refname.go` before they are drawn, as everywhere in gitsync.
- A repository with no snapshot yet answers `computed: ""` and `branches: []`, so the page draws its empty state.
- The control MCP gets the same list as `atrium_git_url`-style read, `atrium_branches` (phase 4), so an agent can ask
  "what is orphaned" without the board.

No write route. Deleting a branch is a push of a delete through the existing, ownership-checked path, never a new one.

## 8. Measurement on the real hub mirror

Method: a scratch script (not committed) in a clone that had fetched every hub branch (`git fetch hub`). Main is the
hub's `claude/main` at `d37bb785` (4399 commits, 3337 non-merge). 349 `claude/*` branches besides `claude/main`.
The hub store holds no rooms' refs in this clone, so room-only could not be counted here (see below).

| Step | Result | Time |
| --- | --- | --- |
| patch-ids of every non-merge commit on main | 3337 | 5.4 s |
| patch-ids of every commit on a branch and not on main | 5455 | 10.7 s |
| per-branch membership, subject and changelog fallbacks, all 349 | | about 6 s of git calls and Python, unoptimised |

Status by rule, each branch decided by the first rule that landed it:

| Result | Branches |
| --- | --- |
| landed, by ancestry or by patch-id alone | 161 |
| landed, needed the subject rule | 83 |
| landed, needed the changelog rule | 61 |
| **landed total** | **305** |
| not landed, code in an uncarried commit | 21 |
| not landed, docs and reports only | 20 |
| **not landed total** | **41** |

What the numbers say:

- Patch-id alone calls 161 landed and 188 not. The fallbacks move 144 branches, so patch-id by itself is not enough
  after this history. Subject covers cherry-picks with a resolved conflict, changelog covers the rest. This is why
  the fallbacks are in the rule and not in an appendix.
- The 21 code-bearing unlanded branches are mostly one shape: 17 `wip-*` branches carry 97 commits each, 96 of them
  carried, and one snapshot commit that is not. These are salvage stashes of a worktree, so they are the first thing a
  human should look at, and they show why the `kind` split is needed. The rest are `land-script-tests`, `land-sh`,
  `wip-atrium-board-sweep` and `wip-atrium-verify`.
- The 20 docs-only are reports, reviews and interviews, as the audit found, plus the three `audit-backup-*` and this
  item's own `branch-audit`.
- The audit's count on 2026-10-12 was 187 landed and 21 unlanded of 209. Four days later it is 305 and 41 of 349, so
  the unlanded count roughly doubled in four days. That is the case for a standing view.
- Open versus orphaned within the 41 was NOT measured: cards live on the rooms and this clone cannot read them.
  The audit joined cards by hand and found the three interview branches and `ci-triage` waiting on input. Room-only was NOT measured here either, for the same reason.
  The audit's number, by its own method on 2026-10-12, was 24 room-only branches with unlanded commits and 67
  room-only with everything landed. Both are phase 1 acceptance checks on a hub that has the room refs.

False results to watch, found while measuring:

- A 20-commit-or-more branch whose only unmatched commit is a `WIP` snapshot is `orphaned: code`, correct, and loud.
  It is the one place a human must decide.
- `git log -p` over the whole of main warns "exhaustive rename detection was skipped" once main's history has a big
  commit. `patch-id` is unaffected, but the pass must pass `--no-renames` to keep the hash stable and fast.

## 9. Where the board shows it

**The repos tab** (`js/hubrepos.js`), which is hub-only and already holds the repos list with branches, in three views
(shelf, ledger, feed). Add a fourth view, `branches`, beside `requests`, and a status chip on each branch row in the
ledger. The default view stays as it is. The view reads on entry and on the refresh button, the tab's existing rule
("read when you go there"), and shows `computed` so a stale snapshot is visible.

```
 repos   [shelf] [ledger] [feed] [requests] [branches]          github/dovholuknf/atrium   computed 14:02  [refresh]
 ┌─────────────────────────────────────────────────────────────────────────────────────────────────────────────┐
 │ [ orphaned 38 ] [ room-only 4 ] [ open 3 ] [ landed 305 ]      kind: [ all ] [ code ] [ docs ]   sort: oldest  │
 ├──────────────────────────────┬───────────┬───────┬────────────┬──────────────┬──────────────────────────────┤
 │ branch                       │ status    │ ahead │ last       │ card         │ do                           │
 ├──────────────────────────────┼───────────┼───────┼────────────┼──────────────┼──────────────────────────────┤
 │ claude/wip-b4-15-dir-picker  │ ORPHANED  │ 1/97  │ 2026-10-09 │ none         │ [open diff] [land] [delete]  │
 │                              │  code     │       │            │              │                              │
 │ claude/land-sh               │ ORPHANED  │ 2/2   │ 2026-10-12 │ t-2210 done  │ [open diff] [land] [delete]  │
 │                              │  code     │       │            │              │                              │
 │ claude/fabric   (sg4,m1mini) │ ROOM-ONLY │ +6    │ 2026-10-14 │ none         │ [push to hub]                │
 │ claude/interview-ci-scripts  │ ORPHANED  │ 11/11 │ 2026-10-12 │ t-1042 done  │ [read] [keep] [delete]       │
 │                              │  docs     │       │            │              │                              │
 │ claude/f-branch-status-view  │ OPEN      │ 3/3   │ 2026-10-16 │ t-2301 live  │ (nothing: a card owns it)    │
 └──────────────────────────────┴───────────┴───────┴────────────┴──────────────┴──────────────────────────────┘
   landed is folded by default. [show landed 305]  how: patch-id 161, subject 83, changelog 61
```

- Landed is folded by default: it is the one status with no action, and the board shows what a human acts on.
- The chip counts come from `counts`, the rows from `branches`.
- `ahead` is `uncarried/commits`, and `+N` for a room tip ahead of the hub.
- A branch name is text, never HTML, and the card title is looked up from the board's card list as `hubrepos.js` does.

**Second place, phase 5: the card and the close dialog.** A card whose branch is `orphaned` or `room-only` wears a
banner, and closing it, or `cull`, warns when the close would leave a branch the hub reads as unlanded code. This is the
point of the whole view (the audit: stop the loss when a card closes, not weeks later). Warn with an explicit second
click. It does not refuse, since 20 of 41 are reports that need no pruning.

## 10. What a human does with each status

| Status | Human action | The row offers |
| --- | --- | --- |
| landed | nothing, or delete later to tidy | folded. A `delete` appears when the branch is fully landed by patch-id AND older than `git.stash_retention_days` |
| open | nothing. Someone is on it | no button. The card is a link |
| orphaned, code | decide. **Land** it (relaunch a worker on the branch, or cherry-pick), **keep** it (pin so it leaves the list), or **delete** it knowing it is gone | open diff, land, keep, delete |
| orphaned, docs | **Read** it and harvest the report, then **delete**. Or **keep** | read, keep, delete |
| room-only | **Push** it to the hub first, then treat as orphaned. Never delete: the room holds the only copy | push to hub (asks the card on that room to `atrium_git_push`, as `atrium_git_url` already says) |

`keep` is a mark, not a status: a `branch_keep(repo, branch, note, at)` row in the hub store, so a kept branch drops out
of `orphaned` into a `kept` chip and does not nag. A kept branch that later gains a commit loses its mark. Open
question 3.

`delete` goes through the existing ownership-checked push-delete path. The row's confirm names the status and the
uncarried count. It is never offered on `room-only` or on a code orphan without a second click.

## 11. Phased build, each phase landable alone

1. **The computation and a read-only API.** New file in `internal/gitsync` (`branchstatus.go`): the one pass of section
   4, the snapshot, the rules 1 to 3, `GET /_hub/git/branches`. Hub branches only (no room refs, no live set: every
   non-landed branch is `orphaned`). Integration tests on a built repository that has a rewritten main, a squash, a
   conflict-resolved pick, an empty commit. Lands alone: an API nobody calls, plus the numbers of section 8 in a log
   line. **Acceptance:** counts match section 8 on the live mirror, pass under 30 s cold and under 1 s warm.
2. **The repos tab view.** `branches` view with chips, folded landed, `how` and `kind`. Headless unit and before/after
   PNGs. Read-only. Lands alone on phase 1.
3. **Open versus orphaned.** The room adds `live` to its collect answer, the hub keeps it with a time, the snapshot
   joins it. Changes `orphaned` to `open` for live cards and adds `last_card`. Needs a room build that says it in its
   hello, and until then a room is treated as `live` unknown, shown as `orphaned` with a `?` on the chip.
4. **Room-only.** The pass also reads `refs/rooms/*/claude/*` and the same-machine checkout, adds `room-only`,
   `room_status`, the `rooms` header and the collect time. Optional: a dirty-worktree count in the collect answer, and
   the collect refspec widened to live cards' non-`claude/*` branches.
5. **Actions and the close warning.** `keep` marks, `delete` through the existing push-delete path, `push to hub` as a
   request to the card, the banner on a card and the warning in close and cull. Each button is its own small step.
6. **Optional: reverse apply** for the squash cases the first three rules miss, and `atrium_branches` on the control MCP.

## 12. Open questions

1. **Cache home.** The `commit -> patch-id` map in memory only (a restart costs one 17 s pass) or persisted (a hub
   table, new migration at the END of the slice)? Recommendation: memory first, persist only if the pass shows up in
   the hub's timing.
2. **Per-commit store or per-branch.** The doc derives branch status from commit facts, so a branch that gains a commit
   flips back. Confirm this is wanted, or whether a landed branch should stay landed once flagged.
3. **`keep`.** A mark with a note and an expiry, or a plain flag? Does a kept branch stay out of the orphaned count?
4. **Subject rule risk.** It is cheap and found 83 branches, and wrong when two commits share a generic subject
   ("Fix tests"). Accept with the weaker chip, or require subject AND same file set?
5. **`kind` path rule.** `docs/`, `changelog/`, `*.md` means docs. Does a branch with only a `REPORT.md` and a test
   change count as code? Proposal: yes, any uncarried non-doc path makes it code.
6. **Offline rooms.** 24 h before `open` falls to `orphaned` is a guess. Longer is safer for the worker, shorter is
   safer for the work.
7. **Who may delete.** Through the existing owner check a card can delete only its own branch. The board is the
   operator, which has no check. Is the delete button operator-only, and does it need the second click on every status?
8. **Multi-repo.** `flow_repo` picks one repository for the item lifecycle. Should this view cover every repository the
   hub holds, or the flow repo only until a second repository has a rewritten main?
9. **Dirty worktrees.** The audit found 42 with real edits and they are the loudest loss. Phase 4's optional count is a
   room-side `status --porcelain`. Is that in scope, or its own item?
