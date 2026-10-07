# The work ledger: who did what, where it is, and who said it was done

Design only. Nothing here is built. Written by sa42 for the orchestrator (`atrium-87300`) and clint on 2026-09-24.
The plan, appended at the end, is the short version for reading.

Read `docs/runtime/a2a-reliability-design.md` first. This design sits on its lineage columns, its report verb and its notice
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
- `atrium ledger` prints the same list from the database opened read-only, with `--json` for scripts, for the
  case where the snapshot is stale because the daemon died between a write and its rename.

Rejected: files as the source of truth. A file per worker in its worktree pollutes a git tree the worker commits
from, a central file needs a lock atrium already has in SQLite, and neither can be queried by the board.

## The data

### `work_item`, one per launched card

| Field | What it holds |
| --- | --- |
| `task_id` | The card. Primary key. Not a cascading foreign key (see above). |
| `handle`, `title`, `worktree` | Copied at creation and refreshed while the card exists, so a pruned card still reads. |
| `launcher_id`, `launcher_handle` | Who launched it, from `spawned_by_id` and `spawned_by`. Provenance. Never changes. |
| `arbiter_id`, `arbiter_handle` | Who is responsible for ruling on it now. Starts as the launcher. The human can reassign it. |
| `brief` | The head of the brief and launch prompt, bounded to 4000 characters, and `brief_path`, the `BRIEF.md` it wrote. |
| `state` | See "States". |
| `state_at`, `state_by` | When it last moved, and who moved it: `worker`, `atrium`, an arbiter handle, or `@human`. |
| `revision` | Goes up by one on every change to the item. A verdict names the revision it was given on. See "Races". |
| `generation` | Which run of the card's session this is. Goes up by one each time the session is running again. |
| `ended_generation` | The generation atrium last recorded as ended, so one death is handled once. |
| `latest_report_id` | The newest `done` report. The one a verdict rules on. |
| `accepted_report_id` | On an accepted item, the report that was accepted. Never trimmed from the log. |
| `outputs` | The latest `done` report's declared outputs as JSON, with their check results. See "Outputs". |
| `continued_in`, `continues` | The two ends of a continuation link, each at most one card. |
| `created_at` | When the card was launched. |

### `work_log`, the item's history

One row per thing said or decided about the work.

| Kind | Written when |
| --- | --- |
| `report` | The worker calls `atrium_report` or `atrium finish` with a status. Holds status, summary, ask, outputs. |
| `say` | The worker `atrium_say`s its arbiter or its launcher. The text, verbatim. |
| `instruction` | The arbiter or the launcher `atrium_say`s the worker. The text, verbatim. So a changed brief is on record. |
| `verdict` | The arbiter or the human accepts, rejects, abandons, reopens or reassigns. Holds the reason, the revision it was given on, and the report it ruled on. |
| `atrium` | Atrium moved the state itself (ended without a report, running again). One row per generation. |

Each row: `id`, `task_id`, `at`, `kind`, `by`, `status` (for reports), `text`, `outputs` (for reports),
`report_id` (for verdicts).

**Bounded, as operational history, not an audit trail.** The limits:

- Each `text` is cut at 8000 characters on a rune boundary, the report summary's existing bound.
- Each output string is cut at 400 characters, and one report's `outputs` JSON at 16 KB.
- Per item, at most 300 rows and 1 MB. Past either, the oldest rows go first in the same transaction as the
  insert, in this order: `say` and `instruction`, then `progress`, `blocked` and `question` reports, then `atrium`
  rows, then `done` reports and verdicts.
- Two rows are never trimmed: the report named by `accepted_report_id` and the report named by
  `latest_report_id`. A verdict that outlives the report it ruled on keeps the report's first 2000 characters and
  its outputs copied into its own row, so what was judged stays readable.
- `atrium` rows are one per generation, so a session that dies and is resumed a thousand times writes a thousand
  rows and then starts trimming, rather than one per exit source per death.
- The tools return at most 64 KB of log per call and page past it.

A reopen and accept loop can still fill an item to its cap. It then keeps the newest history, which is the part
somebody is looking at.

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

The checks are bounded (five seconds each, thirty for the set) and run before the write transaction, never
inside it. The transaction then rechecks the item's revision (see "Races"). A failed
check never refuses the report, the rule clint set in the a2a design's decision 5: a worker that committed
somewhere else is doing its job. The item shows `unverified` with exactly which output failed, and the launcher's
notice says so. `done` with no commits and no `no_commit` is still refused, as today.

Atrium checks that the outputs exist. It does not check that they landed. Landing on `claude/main` is the
launcher's judgement and it records that judgement when it accepts, optionally naming the commit the work landed
as (`landed_as`), which is checked the same way.

## States

```
   open ──────── worker: done report ────────▶ reported ── arbiter: accept ──▶ accepted
    ▲                                            │  ▲
    │                                            │  │ worker: done report (a late one, or a second one)
    │                                            │  │
    │                                            │ arbiter: reject (reason)
    │                                            ▼  │
    │                                         reopened
    │
    │ session running again (a human or launcher resumed it)
    │
   ended-without-report ◀── atrium: the current generation ended while open or reopened

   any open state ── arbiter: abandon (reason) ──▶ abandoned
   any open state ── arbiter: launches a new card with continues ──▶ superseded
   accepted or abandoned ── arbiter: reopen (reason) ──▶ reopened
```

The open states are `open`, `reported`, `reopened` and `ended-without-report`. The closed states are `accepted`,
`abandoned` and `superseded`.

| State | Meaning | Board shows |
| --- | --- | --- |
| `open` | Work handed out, no final report yet. | The last report's status (`progress`, `blocked`, `question`) as a sub-label. |
| `reported` | The worker filed a `done` report. Waiting on the arbiter. | Amber. "reported 2h ago, waiting on atrium-87300". Outputs with their checks. |
| `accepted` | The arbiter says the work is done. Closed. | Green, then out of the default view. |
| `reopened` | The arbiter rejected a done report, or reopened closed work. The reason went to the worker. | Amber, with the reason, and "no session" when nobody is there to do it. |
| `ended-without-report` | The session ended while the work was `open` or `reopened`. The crash case. | Red, at the top, with how it ended and the last thing it said. |
| `abandoned` | Given up. Closed. | Grey, with the reason. |
| `superseded` | The work continues on another card. Closed. | Grey, linked to the card that carries it. |

### Every transition

Rows are the state now, columns are what happens. A dash is refused, with a message saying why and what would
work instead. "Log" means the event is written to the work log and the state does not change.

| From | done report | other report | session ends | running again | accept | reject | abandon | reopen | continues |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `open` | `reported` | log | `ended-without-report` | log | - | - | `abandoned` | - | `superseded` |
| `reported` | `reported`, newer report replaces | log | log | log | `accepted` | `reopened` | `abandoned` | - | `superseded` |
| `reopened` | `reported` | log | `ended-without-report`, unless already ended | log | - | - | `abandoned` | - | `superseded` |
| `ended-without-report` | `reported`, marked "after the session ended" | log | log | `open` | - | - | `abandoned` | - | `superseded` |
| `accepted` | log, "after close" | log | log | log | - | - | - | `reopened` | - |
| `abandoned` | log, "after close" | log | log | log | - | - | - | `reopened` | - |
| `superseded` | log, "after close" | log | log | log | - | - | - | - | - |

What the less obvious cells mean:

- **A second `done` while `reported`** replaces the first as `latest_report_id`, bumps the revision, replaces the
  outputs, and tells the arbiter. A verdict given on the old revision is refused (see "Races").
- **A `done` after the session ended** is real: a report can be in flight through the hub when the exit is
  recorded. It moves `ended-without-report` to `reported` with a note, because the report is the stronger fact.
- **A reject on an item whose session is gone** lands in `reopened` and stays there. The sweep does not turn it into
  `ended-without-report`, because it keys on generations: that generation's end is already recorded in
  `ended_generation`. The board shows "reopened, no session" until somebody resumes or relaunches.
- **`superseded` cannot be reopened.** Reopen the successor instead. Reopening the old one would either fork the
  work or undo the link, and neither has a use.

### The rules, and who holds each one

1. **Only a `done` report moves work to `reported`.** `progress`, `blocked` and `question` reports are logged and
   shown, and leave the state alone. An `atrium_say` to the launcher still counts as a report for the silent-stop
   watchdog (a2a decision 2), and is logged, but it never moves the state, because free text cannot be checked.
2. **Only the arbiter or the human rules.** `accept`, `reject`, `abandon`, `reopen` and a `continues` launch come
   from the card named in `arbiter_id`, or from the board. Anybody else is refused, told who may. A worker cannot
   rule on its own item, even if lineage makes it its own arbiter by accident.
3. **The process never closes work.** A session that exits, crashes, is killed or leaves moves `open` and
   `reopened` to `ended-without-report`, and moves nothing else. A session that ends after a `done` report leaves
   the item in `reported`, because the report is on record and the arbiter can still judge it.
4. **Atrium never resumes.** `ended-without-report` goes back to `open` only when the card's session is running
   again, and that only happens because a human or a launcher resumed it. Atrium follows that fact. It does not
   create it.
5. **A reject reaches the worker.** The reason is queued to the worker from the arbiter's handle, the same path as
   `atrium_say`, in the verdict's transaction. When the session is gone the verdict answer says nobody is there to
   hear it.
6. **Closed is closed, for the worker.** A report on a closed item is logged, marked "after close", and changes
   nothing. The arbiter can reopen `accepted` or `abandoned` work with a reason, which is how accepted work that
   turns out broken goes back on the list.
7. **The human has every arbiter verb, plus reassign.** The board offers accept, reject, abandon, reopen and
   reassign, the same transitions as the table, logged as `@human`. There is no free-form "set any state".
   A launcher that is dead, pruned or gone does not strand its workers: the board flags "arbiter gone" and the
   human rules or reassigns.
8. **The arbiter is who hears.** Every notice about an item (a report, a death) goes to the current arbiter, not to
   `spawned_by`. A reassign records who, when and why, and the new arbiter gets one digest of the item's state.
   Notices already queued to the old arbiter stay where they are and are not re-sent. Messages between the worker
   and either the arbiter or the original launcher are logged.
9. **The column is not the work.** The card's column keeps its meaning, which is what the session is doing. A
   worker's `done` report still moves its card to `done`, an exit still files it `done` or `dead`. On a launched
   card the column is labelled with the work state wherever it appears, so a `done` column never reads as
   finished work. See open question 1.

### Races

Three things can arrive at once: a worker's report, an exit, and a verdict. Each change is one SQLite transaction
(see "Transactions"), so they are ordered, and the rules below make every order safe.

- **A verdict names what it rules on.** `atrium_verdict` takes the item's `revision`, which `atrium_task` and
  `atrium_peers` return. Accept and reject also rule on `latest_report_id` at that revision. If the revision has
  moved (a newer report, a reassign, another verdict), the verdict is refused with what changed, and the arbiter
  looks again. An accept can never close a report nobody read.
- **Output checks run before the transaction.** A report's checks and an accept's `landed_as` check run outside
  any lock, then the transaction rechecks the revision and the arbiter before it writes. A report that lost that
  race is still logged, as a newer report does not erase an older one.
- **Retries are idempotent.** A report carries a client id (the MCP call's id, or a hash of the payload and the
  card's generation), and the same id twice is one row. A verdict retried at the same revision after it applied is
  answered with the result it already had.
- **An exit and a resume are told apart by generation.** An exit event carries the generation it belongs to. A late
  exit from an old generation, arriving after the card is running again, is logged and moves nothing.

### Transactions

`Store.guard` retries contention and halts on any other error. It is not a transaction, and today `finish` writes
the recap, the sha, the event, the status and `reported_at` as separate statements. The ledger needs more.

- **A new store helper runs a function inside `BEGIN IMMEDIATE`**, under `guard`, and commits or rolls back as one.
  Every ledger change uses it: the item update, its log row, the durable notice (below), and for an exit the
  `exited` event row itself.
- **The db event sink writes inside the caller's transaction** when there is one. Cold sinks fan out after commit,
  best effort as today.
- **A refusal is not a storage error.** A verdict refused for a stale revision, a missing reason or the wrong
  caller returns a typed result from inside the transaction with a clean rollback, never an error `guard` would
  read as a reason to halt.
- `finish` moves its existing writes into the same transaction as the ledger's. That is a change to a working path,
  so it has its own tests for a failure between each pair of writes.

### Liveness

A card's session is **live**, **gone** or **unknown**, and only **gone** ends work.

- **Live**: atrium owns its runner, or its pid is alive, or a hook was heard in the current generation within the
  reaper's quiet window.
- **Gone**: an `exited` event for the current generation, or a pid that is dead, or an owned runner that exited.
- **Unknown**: none of those. The case the reaper deliberately leaves alone: a `needs-input` card with no pid that
  atrium does not own, or a shelved window-mode session whose process atrium never held.

An item whose session is unknown keeps its state. The Work view shows "liveness unknown since 13:04", and
`atrium_peers mine` says the same, so a launcher sees it. Atrium does not invent an exit to make the list tidy.

### Which cards get an item

**Cards launched by another session.** That is `atrium_launch`, marked by the `origin:agent` tag with lineage in
`spawned_by`. A card started from the board's launch dialog (`@human`) and a session that joined on its own have
nobody to rule on them but clint, and giving every one of those an item would give him a queue of verdicts he did
not ask for. The board can track any card by hand ("track this work"), which creates an item with the human as
arbiter, and is also how a launched card with lost lineage is adopted. See open question 2.

A worker that launches its own workers is their arbiter. Items nest the way lineage does.

### Continuing work on a new card

sa19 and sa20 were resumed on 2026-09-24 as new sessions. `atrium_launch` gains an optional `continues`, a card id.

- **Only the old item's arbiter or the human may continue it**, the same rule as any verdict.
- **The link is written after the new card exists**, in one transaction that checks the old item is open and has
  no `continued_in`, creates the new item with `continues` set, copies the brief and the arbiter, and moves the old
  item to `superseded`. A second continuation of the same item loses that check and is refused. A cycle is refused.
- **A launch that fails leaves the old item as it was.** A launch that creates a card and then fails to start leaves
  a successor item in `ended-without-report`, visible, rather than an old item superseded by nothing.

## Pruned cards

Ledger operations address an item by its id, which is the card's id, and never need the card row. `atrium_task` and
`atrium_verdict` fall back to the ledger when the card is gone and say so.

A pruned card's item can be read and ruled on. Reopening one is allowed and leaves it `reopened` with no session
and no card to resume: the answer says so and the board offers "relaunch with continues", which is the only way to
put somebody on it again.

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
- **Actions for the human**: accept, reject, abandon, reopen and reassign, exactly the transitions in the table,
  each sent with the revision the row was drawn at. Reject, abandon and reopen cannot be sent without a reason.
- **Liveness** shows beside the state when it is `unknown`, with since when.
- **The main board** gets a work chip on each launched card (`reported`, `ended`, `reopened`) and nothing else, so
  the columns stay what they are.

### `atrium_peers`

Each peer gains a `work` object when it has an item: `state`, `revision`, `arbiter`, `liveness`, `last_report`
(`status`, `at`, first line), `unverified` (bool). And a `mine` filter, which the a2a design's stage 3 already
names: it lists every item the caller is arbiter of **that is not closed, whatever the card's column**, so an ended
worker is in the list rather than hidden with the `done` and `dead` cards `atrium_peers` leaves out by default.
This is the orchestrator's replacement for building `AGENTS-UNFINISHED.md` by hand.

### `atrium_task`

Gains a `work` block: state, revision, who moved it and when, arbiter, liveness, the brief's head,
`latest_report_id`, outputs with their check results, and the newest ten log entries. `log: true` returns the log,
paged at 64 KB. It answers from the ledger alone when the card has been pruned. The tool description drops "atrium
never records what the session said" for the part that is now recorded: its reports and its messages to and from
its launcher. Its terminal output is still not recorded.

### `atrium_verdict`, new

The arbiter's one verb, in the shape of `atrium_report`.

```
card      the worker's card id or handle              required
verdict   accept | reject | abandon | reopen          required
revision  the item revision this verdict was made on  required
reason    why                                         required for reject, abandon, reopen
landed_as the commit the work landed as               optional, for accept. checked like an output
```

Refused, with the text saying what to fix, when the caller is not the arbiter, when a reason is missing, when the
revision has moved (the answer says what changed), or when the transition is not in the table. The answer says
whether a reject reached a live session. Reassign is on the board only, because moving responsibility between
agents is the human's call.

### `atrium_report`

Gains `outputs`. Its description gains one line: a `done` report hands the work to your launcher for a verdict,
it does not close it.

## Recovery after a crash

What atrium does by itself, and where it stops.

1. **The exit is noticed where it always is.** After a crash the room restarts, the reaper finds each card's
   process gone and writes its `exited` event. The exit paths that write one today: the reaper (pid gone, and
   silent with no pid while `running`), session end, supervisor exit, the orphan reaper, leave, and shelve.
   Several can fire for one death, which is why the ledger keys on generation, not on the event.
2. **The ledger moves in the same transaction.** Appending an `exited` event for the item's current generation,
   when `ended_generation` is behind it, moves the item from `open` or `reopened` to `ended-without-report`, sets
   `ended_generation`, writes one `atrium` log row and the durable notice (step 4), all in one transaction. One
   place, so no exit path can forget it, and a second exit source for the same death is a no-op. The item keeps the
   last report and last message as they were.
3. **A sweep reconciles the rest.** On daemon start and on the reaper's tick, every open item is checked against
   the card's liveness (see "Liveness"), archived cards and pruned cards included. A card that is gone for its
   current generation with no end recorded is moved as in step 2. A card whose liveness is unknown keeps its
   state and is shown as unknown. Nothing is moved on a guess.
4. **The arbiter is told once, durably.** The notice is inserted into the arbiter's message queue inside the
   transaction that moved the item, keyed on `(item, generation, "ended")`, so it is one notice per death and a
   crash between the move and the notice cannot happen: both commit or neither does. Delivery is the queue's
   existing job (typed when the arbiter's terminal is free, carried by a hook otherwise), so an arbiter that died
   in the same crash reads it when resumed. This fixes a gap in `notifyLauncher` today, which records the notice as
   sent before it queues it, so a crash between the two loses it. Reports use the same durable path. The text:
   `sa19 ended without a final report (process is gone at 13:04). last report: progress 12:51 "feature list
   committed, starting go-vs-c". outputs: none. card <id>`.
5. **The board is told once**, with a toast keyed the same way, and the row stays red in the Work view until
   somebody rules. A board that was not open at the time sees the red row, which is the durable half. No backoff
   nag: nothing is burning, the process is gone.
6. **Atrium stops there.** It never resumes a card, never relaunches one, and never moves an item to any closed
   state. The launcher or the human reads the list and decides: resume, relaunch with `continues`, or abandon.

Written into the launcher's SessionStart digest (a2a stage 3) as well: a launcher that comes back after a crash
is told which of its items ended without a report before it does anything else.

## How it keeps the resilience rules

From `CLAUDE.md`, "Resilience guarantees (daemon)".

1. **Storage failure halts.** Ledger writes on the report, verdict and message paths run in one transaction
   through the store and return its error, the way `handleFinish` does. The move on `exited` shares a transaction
   with the event, so a store that cannot move the item cannot record the exit either. Refusals (stale revision,
   wrong caller) are results, not errors, so they never halt anything.
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

The backfill runs in Go (`backfill.go`), not SQL, and only for cards created in the last 14 days that carry
`origin:agent` and lineage. Older launched cards stay out of the ledger: flagging a year of history as
`ended-without-report` would bury the ten cards that matter.

It is restart-safe: items are inserted with `INSERT OR IGNORE` on the card id, and a setting records that the
backfill finished, so a crash part way runs it again without doubling anything.

Its evidence is thin and it says so. It does NOT use `LatestAgentReports`, which reads only seven days, skips
`progress`, `blocked` and `question`, and does not decode a report's status. It runs its own bounded query over the
cohort's `submitted` events and parses `kind`, `report` and `status` from each. The hot window may already have
dropped a card's events, and `recap` and `report_sha` may describe different reports, so every backfilled item is
marked `inferred` and shows so.

- a card whose newest parsed agent report is `done` becomes `reported`, with `report_sha` as its outputs when the
  report named that sha, and no outputs otherwise;
- a card that is gone without one becomes `ended-without-report`;
- a live card becomes `open`, and a card of unknown liveness becomes `open` shown as unknown;
- the recap, when there is one, becomes a log row labelled "recap at backfill", not a report, because nothing ties
  it to a particular report.

This gives the orchestrator a starting list to rule on. It is not a reconstruction of `AGENTS-UNFINISHED.md`: that
file used git state in other repos, which these fields do not hold. A launched card whose lineage was never
recorded is adopted with "track this work".

## Stages

Each stage lands on its own and is labelled by where it deploys.

1. **The ledger records (ROOM-SIDE).** The transaction helper and the transaction-aware db sink, migration,
   backfill, items created at launch, generations, reports, says and instructions logged, `done` report to
   `reported`, the move on `exited` and the liveness sweep, the durable `ended` notice, the snapshot file,
   `atrium ledger`.
2. **The arbiter rules (HUB-SIDE and ROOM-SIDE).** `atrium_verdict` with revisions, the verdict endpoint and its
   rules, the reject reaching the worker, `atrium_peers` `work` and `mine`, `atrium_task` `work`.
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
- **The log is bounded.** Send 500 `atrium_say`s from a worker. Expect: at most 300 rows, the says trimmed first,
  every verdict kept. Then loop reopen, report, accept 200 times. Expect: the cap holds, the accepted and latest
  reports are still there, and old verdicts keep their copy of the report they ruled on.
- **A verdict on a stale revision is refused.** Read the item, have the worker report `done` again, then accept at
  the old revision. Expect: refused, naming the newer report. Accept at the new revision. Expect: `accepted`, with
  `accepted_report_id` the newer report.
- **One death, many exits.** Kill a supervised worker so the supervisor and the reaper both write `exited`.
  Expect: one `atrium` row, one notice.
- **A crash between writes loses nothing.** Fail the store between the item update and the notice insert (a test
  hook). Expect: both rolled back, and the sweep on the next tick moves the item and queues the notice.
- **A late exit does not end resumed work.** Resume an ended worker, then deliver an `exited` event from the old
  generation. Expect: logged, the item stays `open`.
- **Unknown stays unknown.** A `needs-input` worker with no pid that atrium does not own, and a shelved window-mode
  worker. Expect: both keep their state and show liveness unknown.
- **A reject after exit stays reopened.** Worker reports `done`, then exits, so the item stays `reported`. Reject
  it. Expect: `reopened`, "no session", and the sweep leaves it there.
- **A continuation needs the arbiter.** A third session launches with `continues` on somebody else's worker.
  Expect: refused. Two continuations of one item at once. Expect: one wins, the other is refused.
- **The snapshot survives the daemon.** Stop the room. Expect: `work-ledger.md` lists the open items, and
  `atrium ledger` prints the same from the database.
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

## Appendix: review

Mercurius was unreachable (its MCP server refused the connection), so the review came from a codex session
(`sa42-review`, card `01a0d368`), read only, against this document and the code it cites. Verdict: needs changes,
C1 to C4 before implementation.

| Ref | Finding | Taken | What changed |
| --- | --- | --- | --- |
| C1 | `Store.guard` is not a transaction, so "the same guard" promised an atomicity it cannot give. | yes | "Transactions": a `BEGIN IMMEDIATE` helper, a transaction-aware db sink, cold fan-out after commit, refusals as results not halts. |
| C2 | A verdict named only a card, so it could close a report nobody read, or race a new report. | yes | `revision` and `latest_report_id` on the item, verdicts name the revision, checks outside the transaction and a recheck inside, `accepted_report_id`, idempotent retries. "Races". |
| C3 | `notifyLauncher` records a notice before queuing it, and exits from several sources carry different times. | yes | The notice is queued in the transaction that moves the item, keyed on `(item, generation)`. Generations make one death one event. |
| C4 | `continues` let anybody close an item, and could race or fail half way. | yes | Arbiter or human only, one transaction with an open and not-yet-continued check, failed launches leave the old item alone. |
| C5 | The state matrix missed a late `done`, a second `done`, and a reject after exit. Human "any to any" had no board actions. | yes | A full transition table. Human gets the arbiter's verbs plus reassign, no free-form set. |
| C6 | The sweep missed no-pid waiting workers and could treat a shelved window-mode session as dead. | yes | "Liveness": live, gone, unknown. Only gone ends work. The sweep covers archived and pruned cards. |
| C7 | The backfill promised more than the stored fields can give. | yes | Its own parsing query, items marked `inferred`, restart-safe, no claim of matching the hand audit, adoption for lost lineage. |
| A1 | The log kept verdict and atrium rows forever, so it was not bounded. | yes | A hard cap per item with a trim order, verdicts copy the report they ruled on, one atrium row per generation, byte bounds on outputs and on tool answers. |
| A2 | Reassigning the arbiter left notices going to the launcher. | yes | Launcher is provenance, arbiter is who hears. Reassign is logged and the new arbiter gets a digest. |
| A3 | Pruned and superseded items had undefined reopen behaviour. | partly | Ledger operations work without the card row, and a pruned reopen points at relaunch with `continues`. Refused: reopening a superseded item. It would fork the work or undo the link, and the successor is where to reopen. |

## Appendix: The work ledger, the plan

Merged from `docs/runtime/work-ledger-plan.md` on 2026-10-07.

The full design is the body of this file, above.

### What goes wrong today

- A card reaches `done` when a worker says so, when its session exits, or when somebody drags it. So `done` says a
  session stopped, not that the work is finished.
- A worker's report goes into the launcher's terminal and nowhere a reader can find it later. After a crash or a
  `/clear` it is gone.
- Where the work lives (branch, commits, files) is prose inside that report.
- So when sa19 and sa20 died in a crash, their cards still read `done`, and the orchestrator rebuilt the truth by
  hand a day later.

### What changes

- Every card another session launches gets a work item in atrium's store. It holds the brief, who launched it, a
  work state, where the output is, and a log of every report, every message between worker and launcher, and
  every verdict.
- The work state sits beside the card's column and does not replace it. The column says what the session is
  doing. The work state says where the work is.
- A worker's `done` report moves the work to `reported`, waiting on the launcher. Nothing closes until the launcher
  accepts it, rejects it back to the worker with a reason, or abandons it with a reason. The human can overrule
  any of these.
- A verdict names the version of the work the launcher looked at. If the worker reported again in the meantime,
  the verdict is refused, so nothing gets accepted unread.
- A session that ends before a `done` report, by crash, kill or exit, moves its work to `ended-without-report`. This
  happens in the same database transaction that records the exit, so no exit path can miss it, and one death counts
  once however many parts of atrium notice it.
- When atrium cannot tell whether a session is alive, it says "unknown" and leaves the work where it is. It never
  invents an exit to tidy the list.
- After a crash, atrium flags those items and shows them red on the board. It queues one notice to each launcher
  in the same transaction, so the notice cannot be lost, and a launcher that died too reads it when it comes back.
  Atrium never resumes or relaunches anything. The launcher or the human decides.
- A final report names its outputs in a fixed shape: repo, branch, commits, files. Atrium checks that each one
  exists and marks what it could not find. It still accepts the report, and the launcher judges it.
- Work that moves to a new card, as sa19 and sa20 did, links the old item to the new one, so the list shows one
  open piece of work, not two. Only the launcher or the human can do that.
- The log is capped per item. It trims chatter first and always keeps the report that was accepted. Items outlive
  their cards.
- A one-time backfill puts the last 14 days of launched cards on the list, marked as inferred, because the old
  records are thin.
- The room also rewrites a `work-ledger.md` file beside its database on every change, so the list can be read with
  the daemon down.

### What you see

- A Work view on the board: every launched card, its launcher, its state and for how long, its last report, its
  outputs with a mark per check. Ended-without-report rows first and red, then work waiting on a launcher.
- Opening a row shows the whole history, which is what used to scroll away in the orchestrator's terminal.
- The main board gets a small work chip on each launched card, and the columns stay as they are.
- `atrium_peers mine` gives a launcher its open work, including workers whose sessions are gone. That replaces
  building `AGENTS-UNFINISHED.md` by hand. `atrium_task` shows one item's state, outputs and recent log.
- A new `atrium_verdict` tool is the launcher's one verb: accept, reject, abandon or reopen. Only the launcher or
  the human can use it on an item.

### Open for you

- A worker's `done` report still moves its card to the `done` column, with the work chip saying `reported`.
  Recommended, so the board does not ring you for the launcher's decision.
- Cards you launch from the board get no work item unless you ask for one with "track this work". Recommended.
- Work waiting on a launcher does not nag anybody. The board shows how long it has waited. Recommended.

### Build stages

1. The ledger records: transactions, reports, messages, the ended-without-report flag, the crash notice, the file.
2. The launcher rules: `atrium_verdict`, and the work state in `atrium_peers` and `atrium_task`.
3. Structured outputs: branch, commits and files on the report, each one checked.
4. The Work view on the board, with verdict buttons for you.
5. Continuing work on a new card, tracking any card by hand, and handing an item to a new arbiter.
