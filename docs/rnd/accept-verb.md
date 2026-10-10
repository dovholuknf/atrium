# Accept is one verb (r-agent-loop point 4, with r-finish-pr-review folded in)

Status: design, 2026-10-10. Nothing here is built. Read first: `docs/rnd/card-lifecycle-design.md` (the lifecycle this
sits in, sections 6 and 7 and Interview Q3 to Q5). Code read: `internal/daemon/cull.go` (`CullProved`),
`internal/daemon/reclaim.go`, `internal/daemon/mergedcull.go`, `internal/api/close.go`, `internal/cli/close.go`,
`internal/link/control_mcp.go` (`atrium_cull`, `atrium_launch_worker`) and `internal/store/resources.go`.

## 0. The design in one paragraph

**Accept** means the work landed, the card is done, and everything the card owns is freed. It is one verb with one
path underneath. The path is close's: walk the card's inventory, ask keep, stash or delete for a worktree that holds
work nowhere else, free the rest, report the bytes. What accept adds on top of close is the **proof** that the work
landed (the branch is merged into the target) and **who may say it**. What a launch adds is the **inventory**, so that
a launched worker's worktree and branch are rows like any other and close has something to free. Then `atrium_cull`
and `atrium close` stop being two implementations and become two doors to one: cull is an alias that is kept for a
release and then removed, and close stays as the operator's door for work that did not land (abandon, not accept).

## 1. What exists today, and why it is two verbs

| | cull (`atrium_cull`, `daemon.CullProved`) | close (`atrium close`, `atrium_close`, `api.closeCard`) |
| --- | --- | --- |
| who | the launcher (an agent), or the merger for it | the operator, on a card they own |
| scope | a done, `origin:agent`, `atrium:subagent` card. Never a keep-open or investigation card | any card, including a link card |
| proof | the branch is an ancestor of `claude/main`, or sits on a `tip` proved elsewhere | none. It never refuses |
| what it frees | the runner, the worktree (no `--force`), its own branch, `BRIEF.md`, a scratch dir | what the inventory lists: worktrees, branches, refs, ports, procs, overlays, review folder |
| dirty worktree | keeps the worktree and branch, still exits the runner, still accepts | asks keep, stash or delete per worktree (Q4) |
| work item | `AcceptMerged`, then `ArchiveCulled`: the card leaves the board | the card goes to done, tagged `atrium:closed` |
| disk | not reported | `closing X frees N` |
| runs on | the room, over `POST /v1/tasks/{id}/cull` | the room, over `POST /v1/tasks/{id}/close` |

They overlap on three steps: stop the runner, remove a worktree, delete a branch. They have separate code for each
(`daemon.removeWorktree` and `api.removeWorktree`, `deleteMergedBranch` and the branch rows in `closeCard`). The two
have drifted on tonight's evidence: a cull leaves `node_modules` and every other ignored tree behind (git refuses to
remove it without `--force`, and cull never forces), while close measures them.

**The gap that matters.** The inventory is written by `open` (294b5891) and by `atrium_own`, `atrium_port` and
`atrium_proc_start`. A launch writes nothing. A launched worker's worktree was made by the launcher with `git
worktree add` and the card records only its cwd. So `atrium close` on a launched worker frees almost nothing, and
cull is the only verb that knows the worktree and branch, because it reads them from the cwd instead of from rows.
That is the reason there are two verbs, and it is the one thing to fix.

**In flight tonight.** A shared `internal/wtfree` package so cull also removes ignored leftovers and reports bytes
freed. This design takes it as the one place a worktree is removed, and has close use it too (phase 1). Also
`atrium_launch_worker` cards (`lifetime: work`) are exited, not culled, when their branch merges
(`mergedcull.go`, `exitMergedWork`). That stays, and the marked-then-culled path in `mergedcull.go` calls accept
(section 8).

## 2. What accept means

Three things have to be true, and accept checks each on the room instead of trusting the caller:

1. **The work landed.** For a card with a branch, the branch is merged into the target (`claude/main` by default, or
   `into`), or sits on a `tip` proved on another machine. This is `inspectCullProved`, unchanged.
2. **The card is done.** `reclaimBlock` still applies when the caller is a launcher: the card's last report is done.
   An operator may accept a card that never reported, because they are the one who knows.
3. **Everything it owns is freed.** The inventory is walked in close's order: runner, reviews, processes and ports and
   overlays, worktrees, then branches, then refs. A row that does not free stays live with `freed_err`, and the card
   says what is left.

Then the work item goes to accepted (`AcceptMerged`), the card is archived off the board as cull does today
(`ArchiveCulled`), and the history stays: the card, its transcript, the inventory rows with `freed_at`, a review's run
folder minus `src/`.

**Accept is not close.** Close is the operator saying the work is over, whether or not it landed: abandoned, replaced,
or reviewed and not wanted. It never proves anything and never accepts a work item. So accept is "landed, and clean
up" and close is "gone, and clean up". Both end in the same freeing code, and that is the point.

## 3. Who can say it

| caller | may accept | checked by |
| --- | --- | --- |
| the launcher of the card (an agent) | an `origin:agent` card it launched, done, tagged `atrium:subagent`, not `atrium:keep-open` or `atrium:investigation` | `reclaimBlock`, as today |
| the operator (`atrium accept`, the board) | any card, including a card they started | nothing beyond the proof below |
| the card itself | never. Its own done report is not the acceptance | the hub reads who is calling (`cullHandler` does this today) |
| the merger acting for a launcher | as the launcher, once the launcher has said so | as today |

The operator's accept still needs the merge proof. An operator who wants to throw away unmerged work uses close, which
asks keep, stash or delete. This keeps the meaning of "accepted" in the work item's history honest: it always means the
branch landed.

## 4. What it refuses, and what it offers

Each refusal names the thing and offers the next step, so a caller is never left with a bare no.

- **Branch not merged.** Refused as a whole, so the worker is still there to be told why. The answer lists the commits
  on the branch that are not on the target (count and the first few subjects) and offers: merge it first, `hold` the
  card, or `atrium close` (operator) to abandon it with stash.
- **Dirty or unpushed worktree.** The branch is merged, so uncommitted files are what is left. Today cull exits the
  runner, keeps the worktree and still accepts. Under this design:
  - the **operator** is asked per worktree, keep, stash or delete, with no default (Q4). This is close's ask.
  - a **launcher agent** cannot answer a dialog, so the call is refused with the dirty list and a parameter is offered:
    `dirty: keep|stash|delete`, no default. `keep` leaves the worktree and branch on disk, listed on the card as kept,
    and accepts. `stash` pushes to the hub as `stash/<card short id>/<branch>` (Q5) and removes. `delete` removes as
    it is and says so. A launcher that does not know what the dirty files are should read them first, and the refusal
    says how (`git -C <worktree> status`).
  - the one file skipped is the `BRIEF.md` atrium wrote, as today.
- **Not the caller's card.** Not tagged `atrium:subagent`, not launched by the caller, or a keep-open card: refused with
  the tag and how to remove it, as `reclaimBlock` says now.
- **Protected.** The main checkout, `main`, `master`, `claude/main` and the target are never removed
  (`protectedBranch`).
- **A new context running** (`errCullNewContext`): try again, as today.
- **The runner will not stop.** Nothing is removed and the answer says to try again.

A preview is part of the verb: `GET /v1/tasks/{id}/accept` answers what would be freed, kept and asked, and the proof,
without changing anything. The CLI and the board show it before the confirm. An agent may skip the preview and call
the verb, which is the one press its launcher already makes with cull.

## 5. Does a launch record the worktree and branch as inventory

**Yes. A launch writes the card's worktree and branch as inventory rows, so that one path (close's) frees everything.**
This is the change that lets cull's own code go.

- **When.** `atrium_launch` and `atrium_launch_worker` record rows after the card exists, when `cwd` is a linked
  worktree (not the repository's main checkout), on a branch that is not protected, and the card carries
  `atrium:subagent`. A card started in a plain scratch directory gets a `dir` row for it instead (what
  `reclaimPlain` removes today). A card started in the main checkout, or by a human's own terminal, records nothing,
  which keeps the rule from `r-card-inventory` that a thing that was already there is not the card's.
- **Rows.** `worktree` (the path), `branch` (repo path and branch), and for the `BRIEF.md` atrium wrote a `dir` row
  whose free is `os.Remove` of that one file (it is untracked and is the one file atrium owns in the tree). `detail`
  says `launch` so the board can tell how the row got there.
- **Whose is it.** The launcher made the worktree and handed it over, so the worker's card owns it. Accepting the card
  is the launcher saying it is done with it. That is the same claim cull makes today when it removes a worktree the
  launcher made.
- **Cards launched before this lands** have no rows. Accept falls back to reading the cwd, exactly as cull does now,
  and writes the rows it finds as it goes so the history is uniform (section 10, phase 2). The fallback is deleted
  once no live card lacks rows.
- **Cost.** One insert per launch and a `git rev-parse` to classify the cwd. No size is measured at launch. Size is
  measured at accept, as close does.

The other option, leaving launch alone and having accept read the cwd forever, is rejected: it keeps two ways to learn
what a card owns, and the second one cannot see a port, a process or an overlay an agent added with `atrium_own`.

## 6. What happens to cull and close

| verb | fate | why |
| --- | --- | --- |
| `atrium_cull` (MCP) and `POST /v1/tasks/{id}/cull` | **alias of accept**, same parameters (`card`, `into`, `tip`, `hold`), kept for one release with a deprecation note in its description, then removed | launchers, the merger and `mergedcull.go` call it today |
| `atrium_cull hold=true` and `/cull/hold` | **kept**, renamed `atrium_accept hold=true` later. It is about the automatic path, not the verb | the hold cancels the marked-then-culled sweep |
| `atrium_accept` (MCP), `atrium accept <card>`, `POST /v1/tasks/{id}/accept`, `GET` for the preview | **new**, the one verb | this design |
| `atrium close`, `atrium_close`, `POST /v1/tasks/{id}/close` | **kept**, the operator's abandon path (no proof, never refuses) | work that did not land still has to be freed. Its freeing is the shared path |
| `closeCard` and `daemon.CullProved`'s removal steps | **one function**, `freeInventory` (section 7) | two implementations drifted |

Closing a card with a merged branch by hand must not be a worse experience than accepting it. So `atrium close` on a
card whose branch is merged says so and suggests `atrium accept`, and does the close if confirmed (the work item is
then left unaccepted, which is the operator's choice).

## 7. One path underneath

`freeInventory(ctx, card, answers) (result)` is the close walk, moved out of `api.closeCard` so the daemon can call
it. Today `daemon.Cull` and `api.closeCard` sit on two sides of the `internal/api` and `internal/daemon` boundary. The
daemon holds the supervisor (stopping a runner) and the merge proof. The API holds the store, the hub stash and the
progress broadcasts. The function takes what it needs as a small interface (stop the runner, stash to hub, free one
row) so the daemon's accept and the API's close both call it. Where the code lives is for the building worker to
choose, and the rule is that no step is written twice.

- **Worktree removal** is `wtfree` (in flight): `git worktree remove` without `--force`, then, only for a tree git has
  already agreed is clean apart from ignored files, a removal of the ignored leftovers under safepath, and the bytes
  they held. Accept and close both call it. Nothing is ever forced past a dirty tracked or untracked file.
- **Order and failures** are close's. A row that fails stays live with `freed_err`, and the rest carry on.
- **Accept is `freeInventory` plus the proof before it and the work item after it.** Nothing else.

## 8. The automatic paths keep working

- **`lifetime: work` cards** (`atrium_launch_worker`) are exited when their branch merges (`exitMergedWork`). They
  stay on the board, with the worktree, until accepted. This is unchanged: the exit frees a launch-cap slot, and the
  launcher's accept frees the disk.
- **merged_cull_grace.** The sweep that culls a marked worker after the grace calls accept as the launcher's
  agent (`d.Accept(card, into, "")` in place of `d.Cull`), with `dirty: keep`. An automatic path never stashes or
  deletes. A dirty worktree is kept and listed, and the report in section 9 says so.
- **The merge hook** still only marks. A merge is not an acceptance, and the launcher keeps the grace period in which
  to send the work back.

## 9. Reporting the disk freed

- The result carries `freed_bytes` (what the rows held, ignored trees included) and a line per row with its bytes.
  `wtfree` supplies the bytes. Sizes are measured immediately before the removal, not read from a stale row, which is
  what close does for a worktree today.
- The CLI prints `accepted X: freed 412 MB (worktree 409 MB, branch, BRIEF.md)`, and `kept` lines for anything it left
  (`kept: worktree D:/... (3 changed files, answer: keep)`).
- The MCP answer has the same fields, so the launcher agent can tell its human in one sentence.
- The card's history gets one `notified` event with the freed total, as close's event does, so the daily housekeeping
  report (Q7) can sum what was reclaimed this week.
- A failed or partial free reports the bytes that did go and the rows that did not. No row is reported freed that is
  not gone from disk.

## 10. Merged branches that are not the card's own

Accept deletes the card's own branch and nothing else. Other merged branches (a dead worker's branch whose card is
long archived, a branch a human made) are **not pruned by accept**, for three reasons. A branch name is not an
ownership claim, so deleting one that no inventory row names deletes somebody's work. The check "merged into
`claude/main`" is true of a branch somebody has just created from main and not yet committed to. And `git branch -d`
would need to run against every repository on the room.

What the design does instead:

- **The sweep lists them** (r-inventory-sweep, section 8 of the lifecycle design): a branch that is merged into the
  target, has no worktree and no card row, and has had no commit for 7 days is a *leftover* with a button. Nothing is
  deleted without a press.
- **The daily housekeeping report** (Q7) counts them per repository.
- Pruning is therefore a decision the operator makes from a list, not something accept does as a side effect.

## 11. The PR review card (r-finish-pr-review)

A review's card is a link card: `lifetime: link`, started by open with an inventory that already holds the worktree,
the fetched `refs/atrium/pr/<N>`, the branch when open made it, and the review row. Close frees these today
(section 7 of the lifecycle design, as built). Accept folds in like this:

- **A PR card is accepted when its PR merged or closed.** The proof is not an ancestor check against `claude/main`.
  It is the forge's answer through the hub (`/_forge/pr`): state `merged` or `closed`. The hub asks the forge once per
  accept and the answer is recorded on the card. A PR that is still open is refused, with the offer of close (the
  reviewer is done, the PR is not).
- **What is freed** is exactly close's: the review stopped and archived (its run folder set aside as
  `<dir>-closed-<ts>` minus `src/`), the worktree, branch and ref. The hub claim stays and the reopen is a re-paste
  (Q3).
- **Dirty or unpushed.** A review worktree is usually clean. When it is not, the operator is asked as in Q4. A
  launcher never accepts a link card, because no launcher made it.
- **Open or accepted findings** are a warning and not a block, as close has them today. They stay in the review's
  history.
- **Close keeps the PR button.** The pulls row's "close" for a walker stays, and a merged PR's row offers "accept"
  beside it that shows the freed total. Both call the same path.
- **The disk figure** on the pulls row is already measured (`r-card-inventory`). After an accept the row shows what
  was freed instead.

This leaves close's `lifetime: link` rule alone: a link card never closes on its own report, and accept is the
operator's word, or the housekeeping report's button when the forge says the PR landed.

## 12. A phased build plan

Each phase is one worker-sized change, a branch `claude/<id>` off `claude/main`. Tests are scoped to the package named.

| phase | item | change | files | depends |
| --- | --- | --- | --- | --- |
| 1 | `r-wtfree-both` | close's `removeWorktree` calls `wtfree` as cull's does, so a close removes ignored leftovers and both report bytes. No new verb | `internal/api/close.go`, `internal/wtfree/*`, `internal/daemon/cull.go` | the in-flight `wtfree` |
| 2 | `r-launch-records-inventory` | a launch writes `worktree`, `branch` and the `BRIEF.md` file rows for an `atrium:subagent` card in a linked worktree, plus a `dir` row for a scratch cwd. `atrium close` on a launched worker now frees its worktree | `internal/link/control_mcp.go` (`launchHandler`), `internal/daemon/launch*.go`, `internal/store/resources.go`, `internal/api/close.go` (the `launch` detail) | 1 |
| 3 | `r-free-inventory-shared` | move the close walk to `freeInventory` behind a small interface. `api.closeCard` calls it, nothing changes for the operator. The result gains `freed_bytes` | `internal/api/close.go`, new `internal/inventory/free.go`, tests in `internal/api/close_*_test.go` | 1, 2 |
| 4 | `r-accept-verb` | `POST` and `GET /v1/tasks/{id}/accept`, `daemon.Accept` (proof, `reclaimBlock`, `freeInventory`, `AcceptMerged`, `ArchiveCulled`), the `dirty` parameter for launchers, the cwd fallback for cards with no rows. `Cull` and `CullProved` become wrappers | `internal/daemon/cull.go` (renamed `accept.go`), `internal/daemon/reclaim.go`, `internal/api/api.go` (routes), `internal/api/accept.go` | 3 |
| 5 | `r-accept-doors` | `atrium accept <card>` and `atrium_accept`. `atrium_cull` and `/cull` stay as aliases and say so. The CLI prints the freed total and the kept lines | `internal/cli/accept.go`, `internal/cli/close.go` (the shared printing), `internal/link/control_mcp.go`, `docs/agent-loop/*` where it names cull | 4 |
| 6 | `r-accept-pr-card` | a link card accepted when the forge says merged or closed, via the hub's forge ask. The pulls row's "accept" and the freed total | `internal/api/accept.go`, `internal/link/prclaim.go`, `internal/web/*` (pulls row), `internal/api/close.go` (reviewNotes) | 4 |
| 7 | `r-merged-sweep-accepts` | `cullDue` and the merged marking call `Accept` with `dirty: keep`. The daemon setting keeps its name `merged_cull_grace` | `internal/daemon/mergedcull.go` | 4 |
| 8 | `r-cull-removed` | drop `atrium_cull`, `/cull` and `Cull` once no caller is left. Keep `hold` under its own name | `internal/link/control_mcp.go`, `internal/api/api.go`, `internal/daemon/cull.go` | 5, 7, one release |
| 9 | `r-merged-branch-leftovers` | the sweep lists merged branches no card owns, with a button | `internal/api/sweep.go`, `internal/web/*` | lifecycle phase 7 (`r-inventory-sweep`) |

Phases 1 to 3 change no verb and can each be reverted alone. Phase 4 is the one that adds behaviour, and phases 5 to 7
move callers onto it.

## 13. Test notes

- A launched worker with a clean merged worktree and an ignored `node_modules` is accepted, the worktree is gone, and
  the answer reports the bytes `node_modules` held.
- The same worker with an unmerged branch is refused, with the commit list, and nothing is removed.
- The same worker with a dirty file: a launcher call with no `dirty` is refused with the file named, `dirty: keep`
  accepts and keeps the worktree, `dirty: stash` lands `stash/<short id>/<branch>` on the hub and removes the tree.
- An operator's `atrium accept` on a keep-open card works, and a launcher's does not.
- A card launched before phase 2 (no rows) is accepted through the cwd fallback and its rows are written.
- A PR card with the forge saying `merged` is accepted and its run folder is set aside.
- `atrium close` on the same worker frees the worktree and the branch, which it did not before phase 2.

## Open questions for clint

1. **Is accept for a launcher refused on a dirty worktree, or does it keep and accept as cull does today?** Cull
   exits the worker and leaves the dirty tree. Recommended: refuse, and offer `dirty: keep|stash|delete` with no
   default. A silent keep is how a worktree sits on disk for a month with nobody knowing, and Q4 already says no
   default for the operator. The automatic grace sweep is the exception and keeps, because nobody is there to answer.
2. **Does a launch record the worktree and branch as inventory?** Recommended: yes, for an `atrium:subagent` card in
   a linked worktree, as in section 5. It is the change that retires cull's own removal code.
3. **May the operator accept a card an agent launched, and a card the operator started?** Recommended: yes to both,
   with the merge proof still required. An operator who wants to discard unmerged work uses close.
4. **Alias or remove `atrium_cull`?** Recommended: alias for one release with a note in its description, then remove
   (phase 8). Launch texts and the merger call it today, so it cannot go in the same change.
5. **Should close stay as a separate verb, or become `accept --abandon`?** Recommended: keep close. "Abandon" and
   "accept" are different decisions with different histories, and a single verb with a flag that changes whether a
   work item is marked accepted is the kind of overloading that caused two verbs here.
6. **Are merged branches that no card owns pruned by accept?** Recommended: no. List them in the sweep and the daily
   report, delete on a press (section 10).
7. **Is a PR card accepted on the forge's word that the PR merged or closed?** Recommended: yes, one forge ask through
   the hub at accept, and an open PR is refused with the offer of close. Atrium still does not poll.
8. **Is the freed-bytes figure in the card's history and the daily report?** Recommended: yes. One event per accept
   with the total, and the report sums it for the week, so reclaiming disk is visible and not only silent.
