# Item tasks: throwaway low-context workers, captured and reaped

Backlog item `f-item-tasks-throwaway-workers`, high. Status: designed, not built.

clint, 2026-10-18 23:12: "i want items to have tasks that can be associated so i can give a throw away worker the task
and have it complete the task. the idea is that multiple super low context claude instances are what i'm after. it'd
also allow each item to see 'the shit' that was done (and maybe a 'why') as well. then atrium would make sure the
throwaway minion is brought up in a temp folder and atrium could rm -rf the temp folder when the throwaway agent was
done but first capture any durable artifacts necessary on that item."

## What exists, and is reused

- **The item** (`docs/rnd/item-lifecycle-design.md`): one `work` row per backlog id on the hub, the cards attached in
  `work_card`, append-only `work_fact`, derived `work_step`, usage and cost in `work_usage`. Every launch names an item
  (`--item`, `atrium:item:<id>`).
- **The assignment** (items stage 2a, the hub `item` table): one card's run, `running`, `blocked`, `finished`, with an
  owner who gets one notice per state and unblocks with `atrium_item unblock`. The room mirrors it and pushes through an
  outbox keyed `(item_id, revision, state)`, so a hub outage loses nothing and a replay is a no-op.
- **The isolated toolset** (stage 1): no `atrium_say`, `atrium_peers`, `atrium_task` or `atrium_report`. The worker
  ends with `atrium_done` or `atrium_blocked`, and nothing else.
- **Pools and LEARNINGS.md** (stage 3): `learned` on `atrium_done`, one paragraph up to 1500 bytes, appended under a
  cap.
- **Superlean** (`docs/runtime/superlean-design.md`): a start 4.3k tokens lighter. `r-superlean-default-for-items` makes
  it the default for item workers once the idle-CPU run is done.
- **Scratch launches and reclaim** (`r-card-reclaim-on-done`, `bug-scratch-card-exits-on-done`): a scratch card wears
  `atrium:keep-open`. Reclaim removes BRIEF.md and the directory only when that leaves it empty, never `RemoveAll`,
  because a launcher made the directory and what is in it is the worker's output. Never a card clint started, never
  one whose work is not done.
- **The hub launches on a room** (`atrium_launch` with `room`, `control_relay_launch.go`). A launch is not idempotent,
  so a lost answer is `unconfirmed` and never retried. The per-room cap is `/_hub/launch-caps`.
- **Hub files** (`hub_files`, `files_api.go`): the hub's own file store.

## What is new

1. A **task**: a row on the hub that belongs to one item, with a brief, an order, a state and a result.
2. A **task folder**: a directory ATRIUM creates on the room, recorded in a room ledger row, so atrium may remove it
   whole. This is the one thing reclaim could never do, and the ledger row is what makes it allowed.
3. **Capture**: the room copies what lasts (the branch to the hub, the named files to hub files, the recap and the why
   to the task row) and the hub acknowledges it, BEFORE the folder goes.
4. A **scheduler** on the hub that starts a task when its turn comes.

## 1. The task model

A task belongs to one item. It lives on the hub beside the item, because the hub owns items. New hub table
`work_task`, added at the end of the hubstore migration slice, `CREATE TABLE IF NOT EXISTS`:

| Column | Meaning |
| --- | --- |
| `backlog_id`, `task_id` | the item and `t1`, `t2`, ... numbered by the hub. The pair is the primary key |
| `title` | one line |
| `brief` | what to do, up to 8 KB. The whole of what the worker is told about the job |
| `kind` | `scratch` (a temp folder) or `code` (a worktree). See 5 |
| `repo`, `base` | for `code`: the repository and the branch or task to start from |
| `after` | text JSON list of task ids that must be `captured` first. Empty: no order |
| `outputs` | text JSON list of file names or globs, relative to the folder, that are kept |
| `room`, `model` | where it runs and on what. Empty: the item's room, and the launch default |
| `state` | see below |
| `attempt`, `card` | the current attempt, from 1, and its card as `room~id` |
| `folder` | the task folder of the current attempt, as the room reported it |
| `branch`, `sha`, `diffstat` | for `code`: the captured branch on the hub, its tip, and `git diff --stat base...tip` |
| `line`, `why`, `learned` | from the worker's `atrium_done`. See 3 |
| `files` | text JSON list of captured files: name, size, sha256, hub file id |
| `reason` | why it is `failed`, `capture-failed` or `cancelled`, in one sentence |
| `created_by`, `created_at`, `started_at`, `ended_at` | |

Two columns on `work`, with `ADD COLUMN` (the runner swallows "duplicate column name"): `task_context` (up to 2 KB,
written by the owner, which every task's brief carries) and `task_parallel` (default 2).

**States**, in a `CHECK`:

| State | Means | Next |
| --- | --- | --- |
| `pending` | waiting for its `after` tasks, or for a slot | `running`, `held`, `cancelled` |
| `held` | the owner or clint stopped it from starting | `pending`, `cancelled` |
| `running` | a worker is on it. A blocked worker shows through its assignment, as today | `capturing`, `failed`, `cancelled` |
| `capturing` | the worker called done, and the room is capturing | `captured`, `capture-failed` |
| `captured` | done: everything that lasts is on the item, and the folder is gone | terminal |
| `capture-failed` | done, but something could not be kept. The folder stays | `capturing` (retry), `captured` (discard) |
| `failed` | the worker ended without a done. The folder stays | `pending` (retry, a new attempt) |
| `unconfirmed` | the launch went out and its answer was lost | `running` once the card reports in, or `failed` by hand |
| `cancelled` | stopped by the owner | terminal |

Every change is also a `work_fact` (`task-started`, `task-done`, `task-captured`, `task-capture-failed`,
`task-reaped`), so the record is append-only like the rest of the item and the row is a cache of it.

**Adding tasks.** `atrium_item` gains the actions `task_add {id, title, brief, kind, after, outputs, repo, base, room,
model}`, `task_list {id}`, `task_hold`, `task_release`, `task_cancel` and `task_retry {id, task}`. Who may: the item's
owner, any card attached to the item as orchestrator or relay, and the board (as the human). The board adds through
the same hub route. A task added to an item whose owner is `human` notifies nobody when it ends, and shows on the
Items tab.

## 2. Super low context

A task worker gets exactly this, and nothing else:

- the **isolated toolset** of stage 1, so it cannot say, read peers or report. It ends with `atrium_done` or
  `atrium_blocked`, and `atrium_blocked` reaches the item's owner the way stage 2a does.
- **superlean** once `r-superlean-default-for-items` lands. Until then, lean. `superlean: false` on the task brings
  one back lean.
- a **BRIEF.md**, in this order: the task's title and brief, the item's title and `task_context`, the folder and what
  survives it (the `outputs` list, and for `code` the branch), the item's learnings (below), and the ending lines.
- **not** the backlog body, the item's history, other tasks' recaps, a handoff or a launcher's name. A task that needs
  something from an earlier task says so in its brief, or names the earlier task's output file, which the brief then
  lists by hub file id and which the room places in the folder before the launch.

**The item's learnings.** A task worker's `learned` is appended to the item on the hub, not to a LEARNINGS.md in its
folder, because the folder is about to go. The next task's brief carries the newest entries up to 4 KB. The cap rule of
stage 3 holds: 50 entries or 20 KB on the item, an alert at the cap, nothing dropped.

A `scratch` task folder holds no repository, so no `AGENTS.md` and no project `CLAUDE.md` load. That is most of the
saving. A `code` task loads the repository's `AGENTS.md` like any worktree does, and cutting that is outside this item.

The relation to scratch launches: a task folder is a scratch directory atrium made, not a launcher. The scratch launch
keeps working as it is, kept open on done, never removed whole.

## 3. The record

A task keeps:

- **what**: `line`, the one line `atrium_done` already takes, and the room's recap (`recap.go`, bounded).
- **why**: a new optional `why` on `atrium_done`, one paragraph up to 1000 bytes, shown under the line. The task brief's
  ending lines ask for it. A done without one is still a done.
- **commits**: for `code`, the branch, its tip and the diffstat, read by the hub from its own mirror, never from the
  worker's word.
- **files**: what was captured, with sizes and hashes, each one opening in the hub's file viewer.
- **cost**: the card's tokens and dollars from `work_usage`, which the item already ingests per card.
- **the card**, archived, with its transcript, because cards are durable. Only the folder goes.

The item's detail shows the tasks as a list with this record (7).

## 4. The lifecycle

**The folder.** New room setting `task_root`. Its default is `$WORKTREE_ROOT/atrium-tasks` when `WORKTREE_ROOT` is set.
With neither set, the room refuses a task launch with a sentence naming the setting, and never falls back to a temp
directory or the home folder. That keeps every task off `C:` on sg4, where `WORKTREE_ROOT` is on `D:`. A `task_root`
that is a drive root, the home directory or an ancestor of it, or inside the room's state directory is refused when it
is set.

A task folder is `<task_root>/<backlog id>/<task id>-a<attempt>-<ulid>`. The room creates it, refusing one that exists,
and writes a ledger row in a new room table `task_dir` (end of the room's migration slice): path, backlog id, task id,
attempt, card, created at, state (`live`, `kept`, `removed`) and the reason it was kept.

**The launch.** The hub's scheduler (6) asks the task's room to launch with a new `task` block on the launch body
(backlog id, task id, attempt, kind, outputs, and the hub file ids to place). The room makes the folder, places the
inputs, writes BRIEF.md and launches in it with `--item <backlog id>`, the isolated set and the tag
`atrium:task:<task id>`. The card is attached to the item as role `task`. A lost answer is `unconfirmed`, never sent
again, the rule every hub launch keeps.

**Done.** `atrium_done` from a task card closes it like any worker (`finish()`), asks the runner to leave, and starts
capture on the room. The task goes `capturing`.

**Capture**, in order, each step on the room:

1. Wait until the runner has exited, up to 60 seconds, so nothing is still writing.
2. For `code`: the worktree must be clean (untracked files that are not ignored count as dirty), and every commit
   since `base` must be on the task branch. The room pushes the branch to the hub through its own forwarder, with the
   task card on the push, and reads the hub's tip back. The hub's tip must equal the local one.
3. For each `outputs` entry: resolve it with `internal/safepath` inside the folder (symlinks followed, nothing outside),
   and upload it to hub files under `items/<backlog id>/<task id>/`. Bounds: 20 files, 5 MB each, 20 MB per task. A
   pattern that matches nothing is not a failure. One that matches past a bound is.
4. Post the result (line, why, learned, recap, branch, tip, files) to the hub through the item outbox, keyed `(backlog
   id, task id, attempt, captured)`, so a replay is a no-op. The hub writes the row and the facts, and acknowledges.
5. Only after that acknowledgement: remove the folder (below), set the ledger row to `removed`, and push `task-reaped`.

**A capture that fails keeps the folder and says so.** Any step that fails stops there. The task goes `capture-failed`
with the step and the reason ("the worktree has 2 uncommitted files", "the hub's tip is 3f1c0de, the folder's is
9a0b1c2", "out/big.csv is 41 MB, over 5 MB"). The ledger row goes `kept` with the same reason, the card keeps
`atrium:keep-open`, and the owner gets one notice. Nothing is deleted. From the board the human can retry the capture,
or discard: discard captures nothing more, removes the folder and marks the task `captured` with the reason
"discarded by <who>".

**The removal.** By ledger row only, never by a path from a request. The path must resolve inside `task_root`, must
not be `task_root` itself, and must not be a link. The walk removes every entry, and a link or a junction inside is
removed as a link, never followed. A folder whose removal fails partway is `kept` with the error, and the next sweep
tries again.

**A worker that ends without a done** (dead, or exited with no report) is `failed`. The folder stays, under H4: work
that is not done stays for the operator. Retry starts attempt 2 in a NEW folder. The old folder stays `kept` until the
human removes it from the item, or the task is captured, after which atrium removes every older attempt's folder of
that task, since the work they held was redone and captured.

**A blocked worker** keeps its folder and its card, and is answered through `atrium_item unblock` as today.

**Cancel** asks the worker to leave, captures what can be captured (outputs, and for `code` a branch push of what was
committed), and removes the folder when that succeeds. A failed capture on cancel keeps the folder, as above.

## 5. Code tasks

The two shapes differ in the folder and in what lasts:

| | `scratch` | `code` |
| --- | --- | --- |
| folder | an empty directory under `task_root` | `git worktree add` under `task_root`, from the room's clone of `repo` |
| starts from | the placed input files | `base`: a branch, or `t<n>` for that task's captured tip |
| branch | none | `claude/<backlog id>-<task id>`, pushed to the hub on capture |
| lasts | outputs, recap, why | the branch, outputs, recap, why |
| clean check | none | clean worktree, every commit pushed |
| removal | the removal above | `git worktree remove`, then the removal above for what remains, then the local branch deleted |

The room's clone of `repo` comes from the hub (`atrium_git_url` and fetch), like every worker's, and is never the main
checkout: a worktree is added from it, and `inspectCull`'s refusal of the main checkout is untouched.

**Reaping and the cleanup rules.** A task card is reaped by capture, not by `atrium_cull` and not by the merged sweep,
because capture is a stronger proof than either: the hub holds the branch, so nothing on the room is the only copy.
The rules of `r-card-reclaim-on-done` stay true for every other card. Cleanup only what atrium created holds: the folder
and the worktree are removed by the `task_dir` row atrium wrote, and nothing else on the room is touched. The card
stays, archived.

The **hub branch** stays as the artifact until the item lands. When the item's `landed` step is done, the hub prunes
`claude/<backlog id>-t*` branches whose patches are on `claude/main` by patch id, the rule
`b-landed-workers-never-reclaimed` sets out, and keeps any that are not, saying so on the item.

## 6. Parallelism

- Tasks with no `after` may run at once. A task starts when every task in its `after` is `captured`, the item has
  fewer than `task_parallel` tasks running (default 2, at most 8), and the room is under its launch cap. The room cap
  counts task cards like any worker, and a task that waits on it stays `pending` with the reason on its row.
- The scheduler is on the hub. It runs on a task change, an item change and the 30 second growl tick. It launches the
  oldest ready task first, by `task_id`.
- **Results merge back by the item's owner, or by a task.** Atrium never merges. A chain (`t2` with `after: [t1]` and
  `base: t1`) builds one line of history, so the last task's branch is the item's branch. Siblings on one repo each
  have their own branch off `base`, and the item lists them as "to integrate". The integration is an ordinary `code`
  task with `after` naming them and a brief that says to merge them, so it is also low context and also captured.
- A `scratch` task's files from an earlier task are inputs by hub file id (2), so no task reads another's folder.

## 7. The board

Only what a human acts on. The Items tab's item detail gains a **Tasks** section above the timeline's later steps:

- One row per task: the title, a state chip, the card chip (opens the archived card), the `line` with the `why` folded
  under it, the branch and tip chips for `code`, the captured files (each opens in the hub viewer), and tokens and
  dollars from the card.
- The row's buttons are the actions that state allows, and nothing else: **hold** or **release** on `pending`,
  **cancel** on `pending`, `held` and `running`, **retry** on `failed`, **retry capture** and **discard** on
  `capture-failed`, **remove folder** on a kept older attempt. `capture-failed` and a task blocked on the human are
  orange (`--wait`), since they wait on the human. Nothing else is orange.
- An **add task** form at the foot: title, brief, kind, after (a pick from the item's tasks), outputs, and for `code`
  the repo and base. It posts to the same hub route as `atrium_item task_add`.
- The item's list row gains `tasks 3/5` beside the step bar, orange when a task waits on the human.

No new tab, and no folder paths on screen except on a kept folder, where the path is what the human needs to look.

## 8. Migrations

Per `AGENTS.md`, added at the end of each slice and tolerant of already being there:

- **hub:** `work_task` and its index on `(backlog_id, state)`. Then `work.task_context` and `work.task_parallel` with
  `ADD COLUMN`.
- **room:** `task_dir` and its index on `state`.

An older hub ignores the new table and columns, and an older room refuses a launch body with a `task` block it does not
know, which the hub shows on the task as `failed` with that room's sentence, naming the room as too old.

## 9. Tests

With no live hub, using the existing link harness (a hub and one room on loopback) and temp directories:

- **Model:** `task_add` numbering, `after` naming an unknown task refused, a cycle refused, the states' `CHECK`.
- **Scheduler:** `after` order, `task_parallel` honoured, the room cap leaves a task `pending` with its reason, held
  tasks never start, an unconfirmed launch is never sent again.
- **Brief:** contains the task brief, `task_context`, outputs and learnings within their caps, and never the backlog
  body or another task's recap.
- **Capture, scratch:** outputs uploaded and the folder gone only after the hub's acknowledgement. Hub down at step 4:
  the folder stays, the outbox replays on reconnect, then the folder goes. A file over 5 MB: `capture-failed`, folder
  kept, one notice. An output symlink pointing outside the folder: refused, `capture-failed`.
- **Capture, code:** a dirty worktree, an unpushed commit, and a hub tip that differs each give `capture-failed` with
  the folder kept. A clean one pushes, removes the worktree and the local branch, and keeps the hub branch.
- **Removal:** a junction or symlink inside the folder pointing at another directory: the link goes, the target is
  untouched. A ledger path outside `task_root`, or `task_root` itself: refused. A folder with no ledger row is never
  removed.
- **Folder root:** no `WORKTREE_ROOT` and no `task_root` refuses the launch. A `task_root` at a drive root, home or the
  state dir is refused.
- **Failed and retry:** a worker that dies leaves `failed` and its folder. Retry makes a new folder, and capture of
  attempt 2 removes both.
- **Cards:** a task card is archived with its history after reaping, and a card clint started is never a task card.
- **Board** (headless, mocked `/_hub/items/<id>`): the buttons per state, orange only for the two human states, the add
  form posts the right body.

## 10. Build stages

1. **T1, scratch tasks, serial:** `work_task`, `task_dir`, `atrium_item task_*`, the launch `task` block, capture and
   removal for `scratch`, the `why` field, item learnings, one task at a time per item. Board read-only list.
2. **T2, code tasks:** worktrees, the clean and push checks, hub branch pruning on landed.
3. **T3, parallel:** `after`, `task_parallel`, the scheduler on the room cap, inputs by hub file id.
4. **T4, board actions:** the buttons and the add form.

T1 needs `r-superlean-default-for-items` only to be lighter, not to work.
