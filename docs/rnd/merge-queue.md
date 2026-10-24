# atrium merge: one queue that lands claude/* on claude/main

Status: design, 2026-10-23. Backlog items `f-atrium-merge-queue`, `f-atrium-merge-queue-decisions` and
`f-atrium-merge-queue-land` are the spec, and this document folds them into one plan. It replaces the merger role of
`docs/design/merge-flow.md` (phase 2, the merge gate) and keeps the rest of that design (stages, checks, epochs).

## The shape in one paragraph

A worker pushes `claude/foo` to the hub and runs `atrium merge` (or the `atrium_merge` tool). The hub writes a row and
puts it in a queue, one queue per repository, run one request at a time and in order. For the head of the queue the
hub computes the merge itself in its own bare store with `git merge-tree --write-tree`, which needs no working tree and
runs no project code. A clean merge becomes a candidate commit under `refs/atrium/merge/<n>`. One room, the merge
room, fetches the candidate, runs `bash scripts/test-touched.sh --base <old claude/main>` in a worktree of its own and
reports the verdict and the output. On green the hub moves `claude/main` to the candidate itself, under the
repository's lock and only from the tip it merged onto, deletes `claude/foo` on the hub and on the rooms, and tells the
requester and its launcher `merged <sha>`. A conflict starts one slim merger card. Red tests go back to the requester
with the failing tests. No agent sits in the clean path and the orchestrator is in no path.

## Why the hub merges and a room tests

- **The hub holds no work, and a merge-tree is not work.** It is git plumbing over objects the hub already has, the
  same class as the epoch move (`gitsync/flowepoch.go`) and the forge fetches. It runs through the store's `Runner`,
  bounded and cancelled on stop. It never checks out a tree and never runs a script from the repository.
- **Tests are work.** They run project code, take minutes and need Go, Node and Playwright. So they run on a room,
  which already owns processes and toolchains.
- **The candidate is made where the ref move happens.** The hub computes it, the hub moves `claude/main` to it, and no
  pack ever has to travel from a room to the hub for the clean path. The room only fetches, through `HubLoopback`,
  which already serves `refs/atrium/pr/<N>` the same way.
- **`claude/main` is moved by the hub and never pushed.** Like the epoch move, it is an `update-ref` with the old
  value given, under the lock every push holds. The candidate's first parent is the old tip, so the move is always a
  fast-forward, and a tip that moved underneath is a refusal to `update-ref`, not a lost commit.

`merge-tree --write-tree` needs git 2.38. The store's floor stays `MinGit` (2.31). A hub with an older git refuses each
request with that sentence and starts nothing, so nothing is half done.

## Which room runs the tests

**The hub setting `merge_room` when it is set, else the room attached from the hub's own machine** (a loopback link
address). Neither one attached means the request waits at the head of the queue with the reason shown, and the queue
moves when one attaches. Never the branch's room.

Why: the hub's machine is where clint works and where the hub's store lives, so its room is the one whose toolchain is
kept current, and the fetch of the candidate never leaves the machine. The branch's own room would spread merge
worktrees and test runs across every machine, and two rooms testing in turn would make the verdict depend on which
machine was asked. One configured room keeps the verdict reproducible and lets clint move it with one setting
(`atrium merge room <name>`), for example to a faster build box. One room also makes "one at a time" hold for free.

The room keeps one worktree per repository at `<data dir>/merge/<host>/<owner>/<repo>`, made from its clone under
`git_root`, detached at the candidate and reset before every run. It is not a card and no session runs in it.

## The request, its states and its log

One table, `merge_request`, in a new migration at the end of the hub's slice. A row is never deleted, so the history is
the table. Columns: `n` (the queue order), `repo`, `branch`, `sha` (the tip asked for), `requester` (card and room),
`launcher` (card and room, read from the requester's lineage when it is asked), `state`, `base` (the `claude/main` it
was merged onto), `candidate`, `landed` (the sha `claude/main` became), `room` (where it was tested), `reason`,
`failing` (the failing test names, one per line), `output` (the test output, the last 64 KiB, the head of the file
kept when cut), `merger` (the merger card, if any), `of` (the request a merger's request continues), and the times
`asked_at`, `started_at`, `tested_at`, `ended_at`.

| State | Means | Moves to |
| --- | --- | --- |
| `queued` | waiting its turn | `merging`, `refused`, `cancelled` |
| `merging` | the hub is computing the merge | `testing`, `conflict`, `landed` (already in), `refused`, `queued` |
| `testing` | the merge room runs `test-touched.sh` on the candidate | `landed`, `red`, `refused`, `queued`, `cancelled` |
| `conflict` | the merge did not apply, a merger card was started | `landed` (through the merger), `refused` |
| `landed` | `claude/main` is the candidate, with its sha | end |
| `red` | tests failed on the candidate, the failing tests named | end |
| `refused` | anything else, with a sentence: not pushed, sha moved, no merge room, old git, the merger gave up | end |
| `cancelled` | the requester took it back while queued, or the operator while queued or testing | end |

The store rules, in `hubstore/merge.go`, follow the rest of the package. A state only moves along the table. An end
state is never left. One open request (`queued`, `merging`, `testing`, `conflict`) per repository and branch, so a
second `atrium merge` of the same branch answers the open row. A refusal is `refuse(...)`, and anything else from the
database halts the hub as every other method does. The output is text the room sent, cut by the room to 64 KiB
before it is sent and again before it is written. Failing names and reasons are one line, with escape sequences and
control characters taken out on both sides.

**`testing` is never left to wait on its own.** Each tick the hub asks the merge room `/v1/git/merge/status` for every
`testing` request. `running` keeps it, a verdict is applied, and a room that has no run of it past two minutes, or
does not answer past the test bound and ten minutes, has it queued again. A request queued again because the hub
restarted or the room lost the run spends no retry. A restart re-queues what it left `merging` before the first
schedule. Only the run's own verdict is heard: each run carries a token from the hub, and a run a newer one replaced
does not report. The same verdict again is answered as taken, and a store that cannot record a verdict answers 503
so the room sends it again. `claude/main` already at the candidate counts as landed, so that resend lands it once.

**A broken merge room or hub git holds the queue and refuses nobody.** What is wrong with the room (no clone, no
bash, the hub's git out of reach, a worktree it cannot make) is the verdict `cannot`, not `error`. The hub queues the
request again without spending its retry and holds the repository with the reason on the strip, for a wait that
doubles from a minute to sixteen. A git failure in the hub's own merge does the same. Only `ErrRefused` refuses, and
a merge cut short by the hub stopping is left for the next start. The hub logs each hand-off and each verdict.

`atrium merge --list` and the board read the same rows: newest first, the open ones on top.

## Asking: `atrium merge` and `atrium_merge`

- **CLI**, in a worktree: `atrium merge` asks for the current branch at `HEAD`, after checking that the hub has that
  branch at that sha (`git ls-remote hub`), so "push first" is answered before anything is queued. `--branch` names
  another. `atrium merge --list [--all]`, `atrium merge show <n>` (with the output), `atrium merge cancel <n>`,
  `atrium merge room [name]` (operator), `atrium merge land` (operator, below).
- **MCP**, `atrium_merge` with `action` `request`, `list`, `show` or `cancel`. It is in the worker set
  (`workerTools`), since the workers are the ones that ask. `land` and `room` are not actions of the tool at all.
- The requester is the card that asked, from the link (`hubAsk` sets `by` and `room` as it does for the backlog), never
  from the body. A card on another room asks through its room's `hub` relay, which gains `/_hub/merge`.
- The hub refuses a branch that is not `claude/*`, is `claude/main`, is not in the hub's store, or whose tip is not the
  sha asked.

## Telling

Every end is told to the requester and to its launcher with the hub's existing cross-room delivery (`deliverAs`), as
`atrium-merge@hub`, kind `needs` for red, conflict and refused and `fyi` for landed. One line each:

- `merged claude/foo as 1a2b3c4 on claude/main (#12)`
- `red: claude/foo on claude/main failed TestX, TestY. atrium merge show 12`
- `refused: claude/foo is at 9f8e, not the 1a2b you asked to merge. push again`

A requester that is gone is not an error: the row holds the answer and the launcher still hears.

## A conflict: one slim merger

When `merge-tree` reports conflicts the hub launches one card on the merge room with `launchOnRoom`, as the task
scheduler does. Its prompt is the smallest that works: the request number, the branch, the conflicted paths that
`merge-tree` named, and four steps. It is launched with `repo` and a new branch `claude/merge-<n>` off `claude/foo`,
merges `claude/main` into it, fixes the conflict, runs `test-touched.sh`, pushes `claude/merge-<n>` and runs
`atrium merge`. That request carries `of: <n>`, goes to the HEAD of the queue, and is landed by the clean path like any
other, so the merger never pushes `claude/main` and the gate stays shut. When it lands, request `n` becomes `landed`
with the same sha and both branches are deleted.

A merger that ends with `atrium_blocked`, or whose request comes back red, makes request `n` `refused` with its reason,
and that goes back to the requester to fix, as the original item asked. One merger per request, never retried.

## Deleting the branch

On `landed` the hub deletes `refs/heads/claude/foo` in its store (and `claude/merge-<n>` for a merger) and asks every
attached room to delete the local branch of that name. A room deletes it only when no worktree has it checked out and
it points at a commit `claude/main` now contains. A branch still checked out, typically by the requester's own
worktree, is left and named in the request's log, and the room deletes it when that card's worktree is removed. A
room that is not attached does it at its next sync, from the hub's list of landed branches. The one-time sweep of old
branches is `o-branch-sweep`, not this.

## Landing origin/main: "land now"

Never automatic: no count, timer or idle trigger. A **Land now** button on the board and `atrium merge land` do the same
thing, and both are operator-only (`edge.LocalOperator` on the hub's machine).

1. The queue is paused, so nothing lands on `claude/main` while the batch is cut. A land is refused while a request
   is merging or testing, and while the last land is not on `main` yet.
2. The batch is every `landed` request since the last land. One commit is made, its tree `claude/main`'s tree, its
   parent hub `main` (the mirror of `origin/main`), its message one subject line (`internal/land` refuses a body),
   authored as the operator from the operator's global git config and signed when that config signs.
3. The commit is put on the hub's branch `land/<n>`, and the answer carries the one `git push` that puts it on
   `origin`'s `main`. The operator runs it. The board shows the pending land with a button that copies it.
4. `claude/main` is moved onto the commit, and its old tip is kept under `refs/atrium/lands/<n>` first, so no
   commit becomes unreachable. Its tree is the same, so nothing is replayed. This is not the epoch move:
   `EpochMove` checks that every commit is on the new tip as itself or as a cherry-pick, and a squash is neither.
5. The queue resumes. The land is a row of its own (`merge_land`: when, by, the commit, the requests), and each
   request in it shows `on main` beside its sha once the hub's `main` has the commit.

**Atrium does not write to a forge.** The push was the recommendation, but the hub's rule is that it only reads a
forge and no answer came to change it, so this is the fallback: the button and the CLI do steps 1, 2 and 4 and hand
the operator the push. Making it the push later is step 3 alone.

## The board

The Terminals tab first, and only what a person can act on:

- A strip above the terminal list when something wants a person: a `red` or `refused` request whose requester is gone,
  a `conflict` whose merger is blocked, a queue held because no merge room is attached. Each row names the branch, the
  state, the age, and one action (open the test output, cancel, set the merge room).
- A **Land now** button with the count of landed requests not yet on `main`, shown only when there is at least one.
- The full queue and history (state, timestamps, the output) behind one link from the strip, in a dialog, so the tab
  is not filled by a log nobody acts on.

## What happens to the merger role

The queue becomes the merger. `RoleMerger` stops being a way to move `claude/main`: `isMerger` in
`receive_rules.go` is removed, so the only movers of `claude/main` are the operator and the hub itself (the queue and
the epoch move). `RoleSet` of `merger` is refused with "the merge queue is the merger now: atrium merge". The CHECK
constraint and any existing rows are left alone, since a shipped migration is never edited. Two things read the
role: an item may no longer be owned by `merger` (asking for it is refused the same way, an item that has it keeps
it), and `atrium_flow epoch_close` now needs the orchestrator instead.
`docs/working-here.md` and the worker briefs change from `git push hub HEAD:claude/main` to `atrium merge`.

## Slices, one commit each

1. **The rows.** Migration `merge_queue` (`merge_request`, `merge_land`), `hubstore/merge.go` with the states, the
   one-open rule, the cap on output, and the tests.
2. **The git.** `gitsync/merge.go`: `MergeCandidate` (merge-tree, commit-tree, `refs/atrium/merge/<n>`, the conflicted
   paths), `MergeLand` (the locked fast-forward from `base`), `DeleteBranch`, the git version check. Integration tests
   on real repositories.
3. **The room's half.** `POST /v1/merge/run` (fetch the candidate, the merge worktree, run the script with a bound,
   report events and output to `/_hub/merge/report` over the relay) and `POST /v1/merge/prune` (delete a landed local
   branch when it is safe).
4. **The queue.** `link/merge.go`: the routes under `/_hub/merge`, the runner (one per repository, serial, a tick and a
   kick like the task scheduler), the room choice and the `merge_room` setting, telling, the hub relay path.
5. **Asking.** `atrium merge` and `atrium_merge`.
6. **Conflicts.** The slim merger launch, `of`, the head of the queue, the merger's end.
7. **The board.** The Terminals tab strip and the history dialog.
8. **Land now.** `merge_land`, the batch commit, `land/<n>`, the move of `claude/main`, the button and
   `atrium merge land`.
9. **The merger role retired.** `isMerger` out, `RoleSet` refusing it, the docs and briefs.

## Open points, one recommendation each

1. **Which room.** `merge_room`, else the hub's machine's room. Reason above.
2. **The forge push on land now.** Push, operator-only, with the fallback above. Needs clint's yes, since it changes
   a stated hub rule.
3. **claude/main moved while testing** (an operator push). Merge again and test again, once. A second move refuses the
   request, so a busy operator cannot make the queue spin.
4. **Branch protection beyond `claude/*`.** None. A `main` or tag request is refused like a push to them.
