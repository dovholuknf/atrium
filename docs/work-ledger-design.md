# The work ledger: who did what, where it is, and who said it was done

Design only. Nothing here is built. Written by sa42 for the orchestrator (`atrium-87300`) and clint on 2026-09-24.
`docs/work-ledger-plan.md` is the short version for reading.

Read `docs/a2a-reliability-design.md` first. This design sits on its lineage columns, its report verb and its notice
path, and it absorbs that design's stage 2 item F9 ("a worker that dies is reported").

## What went wrong

On 2026-09-23 at 13:04 the machine crashed with sa19 and sa20, two SDK matrix workers, mid-task. A day later both
cards read `done`, and nobody knew the work was unfinished until clint asked. The orchestrator then rebuilt the
truth by hand from cards and git state into `orchestrator/AGENTS-UNFINISHED.md`: ten cards in `done`, of which two
died mid-task, four left a `wip` or HANDOFF commit that never landed, three reported a commit that is not on
`claude/main`, and one left nothing at all. That file is what this design makes unnecessary.

Three facts in the code produced it.

1. **`done` means four different things.** A card reaches `done` when a worker reports `done` (`finish.go`), when
   its session ends while its worktree still exists (`endedAs` in `session.go`), when a session leaves atrium, and
   when a human drags it. The column records that a session stopped. It was never a statement that the work is
   finished, and nothing else on the card is.
2. **Reports are not kept where the work is.** `atrium_report` overwrites one `recap` column (2000 characters)
   and writes a `submitted` event that holds `recap: true`, not the text. An `atrium_say` to a launcher stamps
   `reported_at` on the worker and queues the text as a message row on the LAUNCHER's card. Nothing links that row
   back to the worker, the launcher's terminal is where it is read, and after a `/clear` or a crash nobody reads it
   again. Events are also subject to the per-card hot window, so an event is the wrong home for anything that
   must be kept.
3. **Where the output lives is prose.** `report_sha` holds one commit and a flag for whether the worktree has it.
   A branch, a second commit, a file written outside git: all free text in a summary.

## Requirements

In clint's words:

- "A list of what agents did what, what state they are in, what their output was, and where that output is."
- "The orchestrator should be the arbiter of whether work is done or not. If you don't mark it complete, then it
  shouldn't be considered complete."

So a worker saying `done` never makes work complete, and neither does a process exiting. Only the launcher
accepting it does, or the human overruling.

## The decision in one paragraph

Every card another session launches gets a **work item**: a row in the room's store, one per card, holding the
brief, the launcher, a work state, the declared outputs, and a bounded **work log** of every report, every
message between worker and launcher, and every verdict. The work state is a second axis beside the card's column.
The column keeps meaning what the session is doing. The work state says where the work is, and only the
worker's structured report, the launcher's verdict, the human, and one automatic rule (the session ended without
a final report) move it. The board gets a Work view, `atrium_peers` and `atrium_task` return the work state, and
the room writes a readable snapshot file beside its database so the list survives without a daemon.

## Where it lives

**Atrium's store, in the room, with a file snapshot beside it.** The orchestrator's lean, taken.

- A card already outlives its process, its id survives resume, `/clear` and compaction, and the lineage columns
  that say who launched it are already on it. The ledger hangs off that identity rather than inventing another.
- `atrium_launch` launches into the caller's own room, so a launcher, its workers and their ledger rows are in
  one daemon's store and every write is local. Cross-room launches through the dispatch queue stay out of scope,
  as they are in the a2a design.
- **Two new tables, not columns on `task`.** Every column on `task` changes `taskColumns`, `scanTask` and the
  insert, and several branches edit those at once. `turn_seen` made the same call for the same reason.
- **No foreign key cascade.** A work item copies what it needs to be read alone (handle, title, worktree,
  launcher handle) when it is created, and pruning a card does not delete its item. The ledger is the record of
  what was delegated. It must outlive the card that did it. The other direction holds too: the prune sweep skips a
  card whose item is still open, because that card holds the worktree and resume id somebody may need.
- **A durable file, regenerated, not appended.** After every ledger change the room rewrites `work-ledger.md`
  beside its database (write to a temp file, rename over). It lists every item that is not closed, then items
  closed in the last seven days, in the shape of `AGENTS-UNFINISHED.md`. Any agent or person can `cat` it with the
  daemon down. It is a projection of the database, never read back, and a failure to write it is logged and
  ignored, the posture of a cold event sink.
- `atrium2 ledger` prints the same list from the database opened read-only, with `--json` for scripts, for the
  case where the snapshot is stale because the daemon died between a write and its rename.

Rejected: files as the source of truth. A file per worker in its worktree pollutes a git tree the worker commits
from, a central file needs a lock atrium already has in SQLite, and neither can be queried by the board.

## The data

### `work_item`, one per launched card

| Field | What it holds |
| --- | --- |
| `task_id` | The card. Primary key. Not a cascading foreign key (see above). |
| `handle`, `title`, `worktree` | Copied at creation and refreshed while the card exists, so a pruned card still reads. |
| `launcher_id`, `launcher_handle` | Who launched it, from `spawned_by_id` and `spawned_by`. |
| `arbiter_id` | Who may rule on it. Starts as the launcher. The human can reassign it. |
| `brief` | The head of the brief and launch prompt, bounded to 4000 characters, and `brief_path`, the `BRIEF.md` it wrote. |
| `state` | See "States". |
| `state_at`, `state_by` | When it last moved, and who moved it: `worker`, `atrium`, a launcher handle, or `@human`. |
| `outputs` | The latest declared outputs as JSON, with their check results. See "Outputs". |
| `continued_in` | For a superseded item, the card that carries the work on. |
| `created_at` | When the card was launched. |

### `work_log`, the item's history

One row per thing said or decided about the work.

| Kind | Written when |
| --- | --- |
| `report` | The worker calls `atrium_report` or `atrium finish` with a status. Holds status, summary, ask, outputs. |
| `say` | The worker `atrium_say`s its launcher. The text, verbatim. |
| `instruction` | The launcher `atrium_say`s the worker. The text, verbatim. So a brief that changed mid-work is on record. |
| `verdict` | The arbiter or the human accepts, rejects, abandons or reassigns. Holds the reason. |
| `atrium` | Atrium moved the state itself (ended without a report, resumed) or sent the launcher a notice about it. |

Each row: `id`, `task_id`, `at`, `kind`, `by`, `status` (for reports), `text`, `outputs` (for reports).

**Bounded.** Each `text` is cut at 8000 characters on a rune boundary, the report summary's existing bound. Per
item the log keeps every `verdict` and `atrium` row, the three newest `done` reports, and the newest 100 of
everything else, trimming oldest first in the same transaction as the insert. A worker that loops on `atrium_say`
cannot grow its item past a few hundred kilobytes, and nothing that decided the state is ever trimmed.

## Outputs

A final report fills in where the work is, in a shape atrium can check.

```
outputs:
  repo      the git directory the rest is relative to. defaults to the card's worktree
  branch    the branch the work is on, e.g. claude/work-ledger-design
  commits   the commits that are the work, newest first. up to 20
  paths     files the work produced, relative to repo or absolute. up to 50
  no_commit why a done has no commit, e.g. "research only, the answer is in the summary"
```

`atrium_report` gains `outputs`. The existing `sha` stays and means `commits: [sha]`, so today's callers keep
working. `atrium finish` gains `--branch` and repeated `--path`.

**Checked when reported, and again when the arbiter rules.** Each output gets a result:

- a commit: `git cat-file -e <sha>^{commit}` in `repo`, the check `finish.go` already runs;
- a branch: it exists in `repo`, and every listed commit is an ancestor of it (`git merge-base --is-ancestor`);
- a path: it exists, and when it is inside `repo` whether git tracks it.

The checks are bounded (five seconds each, the whole set capped) and run off the request path's lock. A failed
check never refuses the report, the rule clint set in the a2a design's decision 5: a worker that committed
somewhere else is doing its job. The item shows `unverified` with exactly which output failed, and the launcher's
notice says so. `done` with no commits and no `no_commit` is still refused, as today.

Atrium checks that the outputs exist. It does not check that they landed. Landing on `claude/main` is the
launcher's judgement and it records that judgement when it accepts, optionally naming the commit the work landed
as (`landed_as`), which is checked the same way.

## States

```
   open ──────── worker: done report ────────▶ reported ── arbiter: accept ──▶ accepted
    ▲                                            │
    │                                            │ arbiter: reject (reason)
    │                                            ▼
    │                                         reopened ── worker: done report ──▶ reported
    │
    │ session running again (a human or launcher resumed it)
    │
   ended-without-report ◀── atrium: session ended while open or reopened

   any open state ── arbiter: abandon (reason) ──▶ abandoned
   any open state ── a new card launched with continues ──▶ superseded
   any closed state ── arbiter: reopen (reason) ──▶ reopened
```

The open states are `open`, `reported`, `reopened` and `ended-without-report`. The closed states are `accepted`,
`abandoned` and `superseded`.

| State | Meaning | Entered by | Board shows |
| --- | --- | --- | --- |
| `open` | Work handed out, no final report yet. | Atrium, at launch. | The last report's status (`progress`, `blocked`, `question`) as a sub-label. |
| `reported` | The worker filed a `done` report. Waiting on the arbiter. | The worker, `atrium_report status=done`. | Amber. "reported 2h ago, waiting on atrium-87300". Outputs with their checks. |
| `accepted` | The arbiter says the work is done. Closed. | The arbiter, or the human. | Green, then out of the default view. |
| `reopened` | The arbiter rejected a done report. The reason went to the worker. | The arbiter, or the human. | Amber, with the rejection reason. |
| `ended-without-report` | The session ended while the work was `open` or `reopened`. The crash case. | Atrium, on the session's `exited` event. | Red, at the top, with how it ended and the last thing it said. |
| `abandoned` | Given up. Closed. | The arbiter, or the human, with a reason. | Grey, with the reason. |
| `superseded` | The work continues on another card. Closed. | Atrium, when a launch names this card in `continues`. | Grey, linked to the card that carries it. |

### The rules, and who holds each one

1. **Only a `done` report moves work to `reported`.** `progress`, `blocked` and `question` reports are logged and
   shown, and leave the state alone. An `atrium_say` to the launcher still counts as a report for the silent-stop
   watchdog (a2a decision 2), and is logged, but it never moves the state, because free text cannot be checked.
2. **Only the arbiter or the human closes work.** `accept`, `reject` and `abandon` come from the card named in
   `arbiter_id`, or from the board. A verdict from anybody else is refused with who may give one. A worker cannot
   rule on its own item, even if it is its own arbiter by some accident of lineage.
3. **The process never closes work.** A session that exits, crashes, is killed or leaves moves `open` and
   `reopened` to `ended-without-report`, and moves nothing else. A session that ends after a `done` report leaves
   the item in `reported`, because the report is on record and the arbiter can still judge it. The log gets a
   line saying the session ended.
4. **Atrium never resumes.** `ended-without-report` goes back to `open` only when the card's session is running
   again, and that only happens because a human or a launcher resumed it. Atrium follows that fact. It does not
   create it.
5. **A reject reaches the worker.** The reason is queued to the worker from the arbiter's handle, the same path as
   `atrium_say`. When the worker's session has ended the item still moves to `reopened`, the verdict answer says
   nobody is there to hear it, and the board shows "reopened, no session". The arbiter resumes or relaunches.
6. **Closed is closed, for the worker.** A report on an `accepted`, `abandoned` or `superseded` item is logged,
   marked "after close", and changes nothing. The arbiter can `reopen` a closed item, with a reason, which is how
   accepted work that turns out to be broken goes back on the list.
7. **The human outranks everyone.** Every verdict is available on the board, and the human can move any state to
   any other, logged as `@human`. A launcher that is itself dead, pruned or gone does not strand its workers: the
   board flags "arbiter gone" and the human rules or reassigns the arbiter.
8. **The column is not the work.** The card's column keeps its meaning, which is what the session is doing. A
   worker's `done` report still moves its card to `done`, an exit still files it `done` or `dead`. On a launched
   card the column is labelled with the work state wherever it appears, so a `done` column never reads as
   finished work. See open question 1.

### Which cards get an item

**Cards launched by another session.** That is `atrium_launch`, marked by the `origin:agent` tag with lineage in
`spawned_by`. A card started from the board's launch dialog (`@human`) and a session that joined on its own have
nobody to rule on them but clint, and giving every one of those an item would give him a queue of verdicts he did
not ask for. The board can track any card by hand ("track this work"), which creates an item with the human as
arbiter. See open question 2.

A worker that launches its own workers is their arbiter. Items nest the way lineage does.

### Continuing work on a new card

sa19 and sa20 were resumed on 2026-09-24 as new sessions. `atrium_launch` gains an optional `continues`, a card id.
The new card's item copies the old one's brief and links back to it, and the old item moves to `superseded` with
`continued_in` set. Nothing is lost and the list shows one open item for the work, not two.

## What each surface returns

### The Work view on the board

A tab beside the board, reading every room's ledger through the hub.

- **One row per item.** Handle and title, launcher, state and how long it has been in it, the last report (status,
  first line of the summary, when), outputs with a mark per check, and the brief's first line.
- **Default filter: not closed.** `ended-without-report` rows first and red, then `reported` (oldest first,
  because they are waiting on somebody), then `reopened`, then `open`. A toggle shows closed items, paged.
- **Grouped by arbiter**, so the orchestrator's items read as its list and a sub-worker's items sit under it.
- **Opening a row** shows the whole work log: the brief, every report, every message between worker and launcher,
  every verdict with its reason. This is what used to scroll away in the launcher's terminal.
- **Actions for the human**: accept, reject with a reason, abandon with a reason, reassign the arbiter, open the
  card. Reject and abandon cannot be sent without a reason.
- **The main board** gets a work chip on each launched card (`reported`, `ended`, `reopened`) and nothing else, so
  the columns stay what they are.

### `atrium_peers`

Each peer gains a `work` object when it has an item: `state`, `arbiter`, `last_report` (`status`, `at`, first line),
`unverified` (bool). And a `mine` filter, which the a2a design's stage 3 already names: it lists every item the
caller is arbiter of **that is not closed, whatever the card's column**, so an ended worker is in the list rather
than hidden with the `done` and `dead` cards `atrium_peers` leaves out by default. This is the orchestrator's
replacement for building `AGENTS-UNFINISHED.md` by hand.

### `atrium_task`

Gains a `work` block: state, who moved it and when, arbiter, the brief's head, outputs with their check results,
and the newest ten log entries. `log: true` returns the whole bounded log. The tool description drops "atrium
never records what the session said" for the part that is now recorded: its reports and its messages to and from
its launcher. Its terminal output is still not recorded.

### `atrium_verdict`, new

The arbiter's one verb, in the shape of `atrium_report`.

```
card      the worker's card id or handle              required
verdict   accept | reject | abandon | reopen          required
reason    why                                         required for reject, abandon, reopen
landed_as the commit the work landed as               optional, for accept. checked like an output
```

Refused, with the text saying what to fix, when the caller is not the arbiter, when a reason is missing, or when
the transition is not in the table. The answer says whether a reject reached a live session.

### `atrium_report`

Gains `outputs`. Its description gains one line: a `done` report hands the work to your launcher for a verdict,
it does not close it.

## Recovery after a crash

What atrium does by itself, and where it stops.

1. **The exit is noticed where it always is.** After a crash the room restarts, the reaper finds each card's
   process gone and writes its `exited` event. Every exit path already writes that event (reaper, session end,
   supervisor exit, orphan reaper, leave, shelve).
2. **The ledger moves in the same transaction.** Appending an `exited` event moves the card's item from `open` or
   `reopened` to `ended-without-report`, inside the store, under the same guard. One place, so no exit path can
   forget it. The log records how it ended (`reaper: process is gone`, `session hook: exit`) and the item keeps the
   last report and last message as they were.
3. **A sweep catches what the event missed.** On daemon start and on the reaper's tick, any `open` or `reopened`
   item whose card is `done`, `dead` or `shelved` with no live runner is moved the same way. This covers a card
   that reached its column by a path that wrote no `exited` event, and databases from before the ledger.
4. **The launcher is told once.** Through `notifyLauncher`, source `ended`, keyed on the exit event's time, so it
   is one notice per death however many times the sweep sees it. The notice is queued, so a launcher that died in
   the same crash reads it when it is resumed. The text: `sa19 ended without a final report (process is gone at
   13:04). last report: progress 12:51 "feature list committed, starting go-vs-c". outputs: none. card <id>`.
5. **The board is told once**, with a toast, and the row stays red in the Work view until somebody rules. No
   backoff nag: nothing is burning, the process is gone.
6. **Atrium stops there.** It never resumes a card, never relaunches one, and never moves an item to any closed
   state. The launcher or the human reads the list and decides: resume, relaunch with `continues`, or abandon.

Written into the launcher's SessionStart digest (a2a stage 3) as well: a launcher that comes back after a crash
is told which of its items ended without a report before it does anything else.

## How it keeps the resilience rules

From `CLAUDE.md`, "Resilience guarantees (daemon)".

1. **Storage failure halts.** Ledger writes on the report, verdict and message paths go through the store and
   return its error, the way `handleFinish` does. The move on `exited` is inside the event append, so a store that
   cannot move the item cannot record the exit either.
2. **A hook never fails a session.** No hook gains work. The say path's ledger write sits in the message
   endpoint, which a tool call reaches, not a hook, and its failure is logged beside `peerSaid`'s. The Stop hook
   is untouched.
3. **`/activity` stays fire and forget.** Nothing here touches it.
4. **Shutdown is bounded.** No new goroutine. The sweep rides the reaper's tick. The output checks are bounded
   `git` calls on the request that asked for them.
5. **`/finish` follows the hook posture.** An agent atrium does not know still gets `ok` and nothing recorded.
6. **The snapshot is best effort.** A write that fails is logged, never retried in a loop, and never fails the
   change that triggered it.

## Migration and backfill

One migration at the end of the slice, `NNNN_work_ledger` with the next free number when it is built, creating
both tables with `CREATE TABLE IF NOT EXISTS`. Nothing edits an existing migration.

The backfill runs in Go (`backfill.go`), not SQL, once per database, and only for cards created in the last 14
days that carry `origin:agent` and lineage. Older launched cards stay out of the ledger: flagging a year of history
as `ended-without-report` would bury the ten cards that matter.

- a card whose last agent report was `done` becomes `reported`, with `report_sha` as its outputs;
- a card in `done`, `dead` or `shelved` without one becomes `ended-without-report`;
- a live card becomes `open`;
- the recap, when there is one, becomes the item's first `report` log row.

Run on today's database, that yields `AGENTS-UNFINISHED.md` as a query.

## Stages

Each stage lands on its own and is labelled by where it deploys.

1. **The ledger records (ROOM-SIDE).** Migration, backfill, items created at launch, reports, says and
   instructions logged, `done` report to `reported`, the move on `exited` and the sweep, the `ended` notice, the
   snapshot file, `atrium2 ledger`.
2. **The arbiter rules (HUB-SIDE and ROOM-SIDE).** `atrium_verdict`, the verdict endpoint and its rules, the reject
   reaching the worker, `atrium_peers` `work` and `mine`, `atrium_task` `work`.
3. **Structured outputs (HUB-SIDE and ROOM-SIDE).** `outputs` on `atrium_report` and `atrium finish`, the branch and
   path checks, `landed_as`.
4. **The Work view (HUB-SIDE).** The tab, the row detail, the human's verdicts, the chip on the main board.
5. **Continuing and tracking (HUB-SIDE and ROOM-SIDE).** `continues` on `atrium_launch` and `superseded`, "track this
   work" on the board, arbiter reassignment.

## Test plan

Each scenario runs in a throwaway room, never against the live board, and moves into `docs/test-plan.md` when its
stage ships.

- **A crash is flagged.** Launch a worker, let it report `progress`, kill its process. Expect: item
  `ended-without-report`, one `ended` notice on the launcher's queue naming the progress report, a red row.
- **A done report does not close.** Worker reports `done` with a sha. Expect: item `reported`, card column `done`,
  the Work view amber, `atrium_peers mine` still lists it.
- **Only the arbiter rules.** A third session calls `atrium_verdict accept`. Expect: refused, naming the arbiter.
  The launcher accepts. Expect: `accepted`, gone from `mine`.
- **A reject reaches the worker.** Launcher rejects with a reason. Expect: `reopened`, the reason typed or queued to
  the worker from the launcher's handle, then a new `done` report moves it back to `reported`.
- **An exit after a report leaves it reported.** Worker reports `done`, then exits. Expect: `reported`, one log row
  saying the session ended.
- **Nothing resumes by itself.** Restart the room with an `ended-without-report` item. Expect: still ended, no
  process started. Resume the card by hand. Expect: `open`, with a log row naming who resumed it.
- **The log is bounded.** Send 500 `atrium_say`s from a worker. Expect: 100 `say` rows kept, every verdict and
  `atrium` row kept.
- **The snapshot survives the daemon.** Stop the room. Expect: `work-ledger.md` lists the open items, and
  `atrium2 ledger` prints the same from the database.
- **Outputs are checked, not refused.** Report `done` with a commit from another repo and a missing path. Expect:
  accepted as `reported`, marked `unverified` naming both, and the notice says so.
- **A pruned card keeps its item.** Prune a card with an `accepted` item. Expect: the item and its log still read.
  Try to prune a card whose item is `reported`. Expect: skipped.

## Open questions for clint

1. Should a worker's `done` report stop moving its card to the `done` column, and park it in `needs-input` with
   reason `reported` instead? Recommendation: no. The column is the session, and `needs-input` rings the human for
   something that is the launcher's to answer. The chip carries the work state.
2. Should cards launched from the board's own dialog get an item automatically, with clint as arbiter?
   Recommendation: no, "track this work" on demand, so he is not handed a verdict queue.
3. Should a `reported` item that waits long on its launcher nag anybody? Recommendation: no nag to the launcher
   (it costs tokens every time), the board shows the age, and the SessionStart digest lists it.
