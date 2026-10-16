# Script minion: a per-item shepherd that costs no model tokens

Item `rd-script-minion`, approved by clint on 2026-10-15. The question: can the per-item minion of
`docs/runtime/item-minion.md` be a program instead of a model session? This is a design. Nothing here is built.

## What the minion is for, measured

The recipe in `docs/runtime/item-minion.md` has seven steps: check the item, make a worktree on a room, launch a worker,
collect its branch, cherry-pick and verify, report and wait for "accepted", land and clean up. Counted by what each step
needs, nearly all of it is mechanical.

| Step | Needs judgement? |
| --- | --- |
| 0 check the item against main | partly: "is this already built" is a reading task |
| 1 worktree, 2 launch | no: fixed commands with the item id, room and sha as inputs |
| 3 collect | no: `atrium rooms git collect <room>` and a ref read |
| 3 "check it against the item, not the worker's say-so" | yes, once, at the end |
| 4 cherry-pick, BOM strip, gofmt, globals, test-touched | no, until a conflict or a red test |
| 4 report, wait for "accepted" | no: waiting is a poll |
| 5 ff-only land, 6 close, clean up, log line | no |

Measured costs to beat. A per-item sonnet minion sits at about 14.8k context at its first turn, and every later turn
re-reads that plus everything since. The earlier say-relay minion, a resident session woken by `atrium_say`, cost about
720k cache-read tokens per turn. A waiting minion pays that on every wake, and a wake happens for every notice, so the
cost of an item is roughly (turns) x (context), and most turns are "nothing to do yet".

A script pays none of it. It waits in a Go process and spends tokens only when a step genuinely needs reading.

## The lean spike, and why it does not settle this

The spike ran a lean sonnet minion (cards `minion-bug-room-auth-revoked-silent*@claude-sg4`). Finding 1: a lean launch's
atrium-control has the worker tool set only. It has no `atrium_backlog`, `atrium_launch_worker`, `atrium_git_sync` or
`atrium_git_collect`. So a lean minion cannot run the recipe through MCP at all. It would have to shell out to
`atrium backlog`, `atrium launch` and `atrium rooms git collect`, and each of those is a model turn that pays context to
run a command with no decision in it.

That finding argues for the script, not against it. The commands exist as a CLI (`atrium launch --isolated --item
--item-owner --onto --step`, `atrium rooms git sync|collect`, `atrium close`, `atrium backlog`). A script calls them
directly and the missing MCP tools stop mattering. Lean is the right model setting for the few steps that stay with a
model (recommendation 11), and the wrong place to run a state machine.

## Recommendations

### 1. Build it as one command, `atrium shepherd run <item>`, a Go process

- **Choice:** a cobra command in `internal/cli` with the logic in a new `internal/shepherd` package. One shepherd per
  item. Each phase is a step behind a small interface, so a test drives the machine with a fake runner and no
  repository.
- **Rejected:** a shell or PowerShell script. The recipe already spans three shells (Windows rooms, m1mini bash, the
  staging worktree) and has been bitten by `/tmp` differing between them and by commands that silently continue in the
  wrong directory. Go has one `exec` and can assert the HEAD line after `git worktree add` instead of hoping someone
  reads it.
- **Rejected:** a hub-hosted goroutine from day one. It is where this ends up (recommendation 5) but it ties the first
  build to a hub deploy, and the staging worktree and the main checkout are a machine's files, not the hub's.

### 2. The phases are a fixed sequence, and each one is idempotent

- **Choice:** `check`, `worktree`, `launch`, `wait`, `collect`, `stage`, `hold`, `land`, `close`. Every phase can be run
  twice and finds its own work already done (the worktree exists at the sha, the branch ref is already collected, the
  land branch is already main plus the commits). This is what makes recommendation 5 safe.
- **Rejected:** a free-form plan the shepherd improvises. The recipe's "things that went wrong" list is the proof that
  the failures are known. A fixed list gets a fixed check.

### 3. Launch the worker as an item, owned by `human`, through `atrium launch`

- **Choice:** `atrium launch --isolated --item <id> --item-owner human --runner claude --model sonnet --cwd <wt>
  --tags atrium:item:<id> --prompt "Read BRIEF.md and do the task."`. Atrium item mode already does the routing: the
  worker's `atrium_done` and `atrium_blocked` become one keyed notice per `(card, generation, result)` and an item state
  (`running`, `blocked`, `finished`) held on the hub, mirrored in the room with an outbox, so a hub outage loses nothing
  (`docs/changes/items-stage2a.md`, test 6). The worker cannot say, so it cannot wander off to chat.
- The recipe says "do NOT pass `item: true`" because a recipe minion needed the worker to `atrium_say` and
  `atrium_done` to it. That reason goes away. The shepherd is not a session, so it is not the owner, and it needs no
  notice. It reads state.
- **Rejected:** owner = the orchestrator session. A session owner gets notices by queued message, which wakes a model
  for
  every `blocked` and `finished`. That is the 720k turn this design exists to remove.
- **Rejected:** a new owner kind `shepherd`. The owner is who gets paged. The human is the right page for the rare
  `blocked` and costs no schema change. The board already shows human-owned items under "items for you".

### 4. Wait by polling the hub item, not by being woken

- **Choice:** the `wait` phase calls the hub's item read (`/_hub/items`, the same read `atrium_item get` makes) every
  few
  seconds with backoff to a minute. `finished` moves on to `collect`. `blocked` moves to a `held-blocked` hold with the
  ask text. A card that goes `dead` with the item still `running` is a failure the shepherd reports, and 2b's "finished
  for an item whose worker ended with no result" is the hook that will make that a state.
- A poll is one small HTTP call and no model. The hub is the one source of truth, so a shepherd restarted mid-wait
  loses nothing.
- **Rejected:** subscribing to the owner notice or the SSE stream. It couples the shepherd to delivery semantics that
  exist to wake a session. A poll cannot miss a message, a stream can.
- **Rejected:** `atrium_say` from the worker to the shepherd. Item workers cannot say, by decision 4 of
  `docs/rnd/atrium-items-decisions.md`.

### 5. State is a hub row, written before each step

- **Choice:** one row per shepherd in a hub table (a new migration at the end of the slice), holding the item id, the
  phase, the room, the base sha, the worktree path, the land branch and the last error. It is written before a phase
  runs, so a killed shepherd resumes at the phase it was in and the idempotent phase repeats safely. The first build
  keeps the process on the operator's machine. Moving it into `atrium run` later is a change of who calls the same
  function.
- **Rejected:** state in a file next to the main checkout, like `MINION-LOG.md`. A file is per machine, and the thing
  that needs to know a shepherd exists is the board and the other directors. "Cards, not agents": a shepherd outlives
  its process, so its fact belongs on disk beside the backlog.
- **Rejected:** state in memory, resumed by re-deriving from git. Most of it can be derived, but "was this accepted" and
  "which sha did the human see" cannot.

### 6. The stage phase is the recipe's step 4, verbatim, with exit codes

- **Choice:** `git switch -C claude/land-<id> claude/main`, `git cherry-pick claude/main..<ref>`, strip any byte-239 BOM
  from changed `.md` files, `gofmt -l internal/ cmd/` must be empty, `go run scripts/check-test-globals.go` must exit 0,
  then `scripts/test-touched.sh`. Each step's exit code and its last lines go in the packet (recommendation 11). The
  known-failing tests are read from a data file in the repo, not from the prose of a brief.
- A 20ms unit-budget miss on a disk test the branch did not touch gets one automatic rerun, recorded in the packet as a
  rerun. This is the rule the recipe already has for item `t-unit-budget-load-headroom`.
- **Rejected:** trusting `atrium_done`'s sha and landing it. The recipe's own warning is that a worker has said "done"
  on work that was already on main. The script cannot read that, but it can refuse an empty cherry-pick range and hand
  the case to recommendation 12.
- **Rejected:** running the whole suite here. `AGENTS.md` says the whole suite runs after landing, by whoever lands.

### 7. Nothing lands until a person or the orchestrator says `accept`

- **Choice:** the `hold` phase writes the packet to the shepherd row and posts a human notice (the existing human notice
  path), then waits. The only inputs that move it are `atrium shepherd accept <id>`, `reject <id> [reason]` and
  `correct <id> <text>`. An orchestrator session reaches them through `atrium_item` verbs added with the build, so it
  never needs a shell. The rule "nothing lands before accepted" is enforced by the state machine, because `land` is
  unreachable from any phase but `held` and an accept stamped with who gave it.
- **Rejected:** auto-land when every check is green. Auto mode trades interruption for review (`AGENTS.md`), and landing
  on `claude/main` is the finish line the human asked to own. A green run is evidence, not an acceptance.
- **Rejected:** accept by `atrium_say` text to the shepherd. A script parsing free text for "accepted" is the failure
  this design avoids. An accept is a verb with an id.

### 8. Land with `git merge --ff-only`, and nothing else touches the main checkout

- **Choice:** after accept, rebase the land branch on `claude/main` if main moved (and re-run the stage checks when the
  rebase changed anything), then from the main checkout run `git merge --ff-only claude/land-<id>`. A non-ff result is a
  fatal for the shepherd, not a retry loop. It goes back to `held` with a note that main moved. Then park the staging
  worktree on `claude/land-ci-green` and delete the land branch, as the recipe does.
- The commit author for a landing that is not a plain cherry-pick is Clint's, as the recipe says.
- **Rejected:** `git push` or a merge commit. The bar is a fast-forward onto `claude/main`.

### 9. Close and clean up in a fixed order, and treat each as best effort after the land

- **Choice:** after the land succeeds, in order: backlog status `done` (unless the item says its done bar is a live
  check, then leave it open and say so in the packet), `atrium close` of the worker card, `git worktree remove --force`
  and `git branch -D` on the room, the `MINION-LOG.md` line, then mark the shepherd row `closed`. Each failure is logged
  on the row and does not undo the land. A landed item with a stale worktree is a chore. An unlanded item that was
  "cleaned up" is a loss.
- **Rejected:** cleanup before accept. A rejected item needs its worktree for the correction.

### 10. Corrections from clint reach the worker as `atrium`, by the same path as an unblock

- **Choice:** two cases, because the item's state decides what is allowed.
  - **The worker is `blocked`.** `atrium_item unblock <id> "<answer>"`. This exists. Atrium types the answer as sender
    `atrium`, the worker never learns who asked, and the item returns to `running` when the worker's turn starts.
  - **The worker has finished and the human wants a change** (the more common correction). `atrium shepherd correct <id>
    <text>` queues the text to the worker's card as sender `atrium`. Stage 1 allows a say TO an item (only a say FROM a
    running item is refused), and a finished worker's card still holds its context and its worktree because the recipe
    does not close it until `close`. The shepherd moves back to `wait`, and the item's turn start flips the state to
    `running` again. A card that has since exited gets a fresh `atrium launch --resume` against the same worktree and
    the correction as the prompt.
  - **Clint typing in the terminal** works already, and the board shows it: a typed turn starts the item's turn.
- The correction is also appended to the packet history, so the next `hold` shows what changed since the last one.
- **Rejected:** the shepherd relaying clint's words through a model. That puts a model between a human and the worker
  for
  a copy operation.
- **Rejected:** giving the worker `atrium_say` back. Decision 4 removed it on purpose, so a worker cannot route around
  its owner.
- **Needs a decision from clint:** whether `correct` on a worker whose runner has exited should resume it or start a new
  one. This design picks resume, because the context is the value. The first build may only support the live-card case.

### 11. What still needs a model, and why: reading, once, at the end

- **Choice:** the script builds an evidence packet for every hold: the commit list, the diffstat, a grep on main for the
  item's named symbols, whether the cherry-pick range was empty, each check's result, and the reruns. The decision to
  accept is a person or the orchestrator reading that packet. No model is in the loop for the common case.
- What a script cannot do is judge fit. "Does this diff do what the item asked, and not something near it" is the
  recipe's own warning and it is a reading task. Board work also needs an image looked at. That stays with whoever
  accepts. It is small, one read of about a page, not a resident session waiting.
- **Rejected:** a resident minion per item for that read. It is the cost this item exists to remove.
- **Rejected:** no model anywhere, accept sight unseen. The human asked for review as the bar.

### 12. A one-shot judge for exceptions only, launched lean, with no resident context

- **Choice:** on a cherry-pick conflict, a red test that is not in the known list, an empty range, or a diff the grep
  cannot match to the item, the shepherd launches ONE lean sonnet judge with the packet and the item body as its brief
  and
  `atrium_done` as its only exit. Its verdict, a paragraph, goes into the packet. A judge cannot edit the land branch.
  A conflict it proposes a resolution for goes to the human as a diff to approve.
- A lean launch costs about 14.8k at the first turn. That is paid once per exception, not once per wait. For the
  conflict case, a resolution is exactly the job a model is good at and a script is not.
- **Rejected:** letting the script resolve a conflict by taking the worker's side. That can silently drop what main
  gained.
- **Rejected:** sending exceptions to the worker. It is finished, and a worker rebasing its own branch is how a land
  branch ends up not a plain cherry-pick of what was reviewed.

### 13. The lean spike's finding is handled by not needing those tools

- **Choice:** the shepherd calls the CLI and the hub's HTTP surface, never the MCP. The judge in recommendation 12 needs
  only the worker tool set, which is what a lean launch gives. Nothing here asks to widen a lean launch's tools.
- **Rejected:** adding `atrium_backlog` and the git tools to the lean set. It would make the lean launch a bigger
  context at its first turn and invite exactly the improvised recipe this design replaces.

### 14. The board shows shepherds last, and it is not on the critical path

- **Choice:** the first build is CLI only. A later board block lists held shepherds with the packet and three buttons,
  `accept`, `reject`, and a text box for `correct`, over the same hub endpoints. The board is a view.
- **Rejected:** building the board first. A button that calls a verb that does not exist yet is the order that was just
  rebuilt on the pulls tab, and it gives the human a way to accept before there is anything trustworthy to accept.

## Cost of the design

- A shepherd sits in a poll loop at near zero. The only model spend is the worker's own work, which was never the
  problem, plus one lean judge per exception, plus the human's read.
- Compare per item. The resident say-relay minion: one wake per notice at about 720k cache-read tokens each, three to
  six notices per item. The lean per-item sonnet: about 14.8k at start, growing per command, with no tools for the
  recipe. The shepherd: none for the sequence, about 15k for each exception.

## Risks

- **Bit rot.** A recipe in a markdown file gets read and adapted. A program does not adapt. The first rooms to differ
  (the m1mini bash paths, the Windows worktree roots) go in a room table of paths that the shepherd reads, and the
  recipe's room table is the source of its first version.
- **Landing is the dangerous step.** Hence one verb, ff-only, an accept stamped with a handle, and no phase that can
  reach it by accident.
- **The staging worktree is shared.** Two shepherds cannot use `D:\worktrees\minion-ssh-default` at once. The first
  build
  takes a lock row, and a second shepherd waits in `held-queued`. Parallel staging worktrees are a later change.

## Build items filed

| id | dept | covers |
| --- | --- | --- |
| `rnd-shepherd-core` | rnd | recommendations 1 to 6: the command, the phases, the state row, launch, wait, collect, stage |
| `rnd-shepherd-accept-land` | rnd | recommendations 7 to 10: accept, reject, correct, the ff-only land, close and cleanup |
| `rnd-shepherd-judge` | rnd | recommendations 11 to 13: the packet and the one-shot judge for exceptions |
| `rnd-shepherd-board` | board | recommendation 14: the board block |

Order: core, then accept-land, then judge. The board block follows accept-land.
