# Event sink, stage 3: the permission table

Item 15 in `docs/backlog-2.md`. Design only. Nothing here is built.

Mercurius session `s_Y271hycEv2HC`: round 1 `needs_changes` (the review could not tell trimmed from never there,
fixed by the trim marker), round 2 `ready_to_build`. Building waits on clint's answers to the open questions at the
end.

Stages 1 and 2 bounded the `event` table and gave it somewhere else to go. The 2026-09-24 measurement found the
other half of the file sitting beside it with no bound at all: the `permission` table, 20.8 MB of rows and 4.4 MB of
indexes out of a 57 MB database. After a `--window-bytes 262144 --drop-kinds perm-requested,perm-decided`
compact, the event table is small and the permission table is most of what is left. This stage is about that table.

## Why this one next, ahead of offsite and the swap

- **It is the largest thing left, and nothing touches it.** The event table can already be bounded and routed.
  The permission table grows by one row per tool call on every gated card, for as long as the card lives. The
  card holding the most, `main:atrium`, is a resident card that is never pruned, so card prune never reaches it.
- **It needs no credential and no new process.** Offsite needs the "name of a command, never the credential"
  question answered first, and a live swap needs the reload path. This is two statements on a timer we already run.
- **It needs one small migration and no rebuild.** An `UPDATE` and a `DELETE` on columns that exist, driven by the
  `permission_pending` index on `(decided_at, requested_at)` that exists, plus two `ADD COLUMN`s on `task` for the
  trim marker (see "The trim marker").

## What reads the table, which is what decides what may go

| reader | what it needs | where |
|---|---|---|
| the pending queue | pending rows, with `details` (the "what changes" diff) | `PendingPermissions`, `stack.js:885` |
| a room's relay to the hub | pending rows, `details` cut to `roomDetailsMax` | `rooms.go:378` |
| replay, chain step 1 | a decided row by `(task_id, dedup_key)` | `RecordPermission` |
| the Permissions pane | the newest decided rows, `History(limit)`, default 100 | `api.go:1511` |
| the auto-mode review | EVERY decided row for one card, folded by command | `ReviewTask` |
| one request by id | the row | `GetPermission` |

Two facts fall out of that table:

1. **`details` on a DECIDED row is never read.** The only renderer is the pending queue. The row keeps it forever
   anyway, and it is the biggest column: 8.3 MB of the 20.8 MB, mostly `Edit` and `Write`, whose details carry
   the file content.
2. **The audit line already survives the row.** `DecidePermissionBy` copies tool, command, decision, reason and
   `by` onto the `perm-decided` event on purpose ("an audit line has to say what was allowed without a join
   against a table that prune can empty"). So a decided row deleted from this table is still recorded in the event
   log, and in the file sink when one is configured.

## The design

Two independent bounds, each its own setting, each OFF by default, both run from the sweep timer that already runs
archive and prune.

### Bound 1: shed `details` from decided rows

`perm_details_after`, in seconds, `off` by default. A decided row older than that has `details` set to `''`.
Nothing else on the row changes.

- Only `decided_at IS NOT NULL` rows. A pending row keeps its diff for as long as somebody might need it to decide.
- Cheap and nearly free of meaning: nothing reads the column afterwards (fact 1).
- Floor of one hour, same as `prune_after`, so a mistyped value cannot empty the diff of a request somebody is
  looking at in another tab while it is being decided.

### Bound 2: delete decided rows past an age

`perm_keep_after`, in seconds, `off` by default. A decided row older than that is deleted.

- **Never a pending row.** An agent is blocked on it.
- **Never a row younger than seven days, whatever the setting.** Replay (chain step 1) returns an exact-key row
  "for as long as the row lives". The case that needs it is an answer lost in transit: the daemon decided, died
  before the reply left, and the agent re-posts on reconnect. A store halt parks the agent on its backoff, and a
  halt can last until somebody reads the board. Seven days is well past any halt somebody leaves in place, and it
  is a floor rather than a default so the setting cannot undercut it. A row deleted past that is asked again,
  which is the safe failure: a person sees it.
- **The review says so, from a marker, not from what is left.** See the next section.
- `History(limit)` needs nothing. It reads the newest rows and the trimmed ones are the oldest.

### The trim marker

The rows that survive a delete cannot say that older ones existed. The oldest remaining `decided_at` looks the
same whether rows before it were trimmed or were never there, and a card whose every decided row was deleted has
nothing left to read at all. Its review would come back empty and clean, which is the one answer the auto-mode
review must never give falsely.

So the delete records what it removed, on the card:

- `task.perm_trimmed_through`, RFC3339 text, `''` by default. The newest `decided_at` among the rows deleted so
  far. Every decision at or before it may be gone, and every decision after it is still in the table.
- `task.perm_trimmed_count`, integer, `0` by default. How many decided rows were deleted, in total.

Both are written in the SAME transaction as each batch's `DELETE` (`tx.go`), so a crash between the two cannot
leave rows gone with no marker. A batch takes the oldest eligible rows in `decided_at` order,
so for each card the deleted rows are always its oldest ones, and `perm_trimmed_through` is an honest boundary that
only ever moves forward, to the newest row of that card the batch removed. The offline compact updates the same
two columns on the copy, so a compacted room's reviews say the same thing.

The migration is two `ADD COLUMN`s at the end of the slice. The runner already swallows "duplicate column name",
and no `CHECK` or `UNIQUE` changes, so there is no table rebuild.

`ReviewTask` reads both columns alongside the rows and returns `trimmed_through` and `trimmed_count` even when no
rows remain. The review pane then says "N earlier decisions, up to <time>, were trimmed. The event log has the
audit line". `Total` stays the count of rows present, and the pane shows the trimmed count beside it rather than
adding the two, because the unattended and blocked fractions can only be computed over rows that are still there.
Same posture as `rolled_off` in stage 1: say it, do not rebuild it.

Card prune deletes the card and the marker with it, which is correct: there is no review left to annotate.

### Where and how it runs

A `trimPermissions()` step in `sweep.go`, beside `pruneOld()`, reading both settings the way `pruneAfter()` does:
unset or `off` is off, a read failure is off, under the floor is logged and off.

Each tick handles a **bounded batch**, 500 rows per statement, by `id IN (SELECT id ... ORDER BY decided_at LIMIT
500)`, and loops at most 10 statements per bound per tick, so one tick touches at most 5,000 rows of each kind. The store has one connection (`MaxOpenConns(1)`), and a single `DELETE`
over twenty thousand rows would hold it while every hook waits. Leftover rows go on the next tick. The freed
pages go back to disk through the incremental vacuum loop that already runs.

Each statement goes through `s.guard`, so a failure halts like every other write. It is not a best-effort cold
path: it writes the primary database.

One log line per tick that changed something: `[atrium] trimmed N permission detail(s), deleted M decision(s)`.
No event is written per row, because an event per deleted row would be the table moving into the other table.

### The offline copy

`atrium2 db compact` grows `--perm-details-after <dur>` and `--perm-keep-after <dur>`, the same two bounds with the
same floors, applied to the copy before the `VACUUM INTO`. That gives an existing oversized room one controlled
step to shrink, and the live sweep keeps it small afterwards.

## What this does not do

- It does not trim the `command` column. Review folds by exact command, and a command is what was allowed.
- It does not route permission rows to a cold sink. The event log already carries the audit line, and a second
  cold path for the same facts is a second place for them to disagree.
- It does not turn either bound on. "Turning a bound on by default" stays on the item 15 list, and the open
  questions below ask what the numbers should be.
- It does not touch the event table's own `perm-requested` and `perm-decided` rows. Those are stage 2's
  `event_cold_kinds`.

## Tests to write when it is built

- A decided row past `perm_details_after` loses `details` and keeps everything else. A pending row of the same age
  keeps its details.
- A decided row past `perm_keep_after` is deleted, a pending one of any age is not, and a decided row inside the
  seven-day floor is not whatever the setting says.
- An exact-key replay inside the floor still returns the decision after a trim tick.
- `ReviewTask` reports `trimmed_through` and `trimmed_count` after a trim and nothing before one.
- A card whose EVERY decided row is past `perm_keep_after`: after the trim, `ReviewTask` returns zero rows and still
  reports the trimmed count and boundary.
- The marker and the delete commit together: a batch whose marker update fails leaves every row in place.
- The migration applied twice to a copy of the live database leaves both columns once, with their defaults.
- The batch bound: 1,200 eligible rows take three statements, and a tick stops at its cap with the rest left.
- Unset, `off`, garbage and under-the-floor settings all do nothing.
- Compact with `--perm-keep-after` updates `perm_trimmed_through` and `perm_trimmed_count` on the copy, and leaves
  the input untouched.
- Compact with both flags on a copy of the live database: count the rows and bytes kept. Record the numbers in
  item 15's measurement table.

## Open questions for clint

1. **Shed details at decision time instead of on a timer?** Nothing reads them after the decision. The case for
   keeping them a while is the "a model reads the review and summarises what a session changed" idea in CLAUDE.md,
   which would want the diffs. If that idea is dead, shedding on decision is simpler and needs no setting at all.
2. **Default values when these get turned on.** A proposal: `perm_details_after` 7 days and `perm_keep_after` 90
   days. The first is cheap to lose, the second is the auto-mode review of a card nobody pruned.
3. **Should a resident card be treated differently?** `main:atrium` holds 13.6k of the 23k rows because it never
   ends. A card that ends is pruned whole by `prune_after` in the end. A resident one only ever loses rows to this.
   One setting for both is simpler. A separate, longer age for pinned or resident cards is possible.
4. **Is seven days the right replay floor?** It is sized to "a halt nobody noticed for a week". Shorter frees rows
   sooner, and the cost of undercutting it is one question asked twice.
5. **Is the trim marker on the review enough, or does the Permissions pane need a line too?** It shows the newest
   100 rows, so it only reaches trimmed rows on a board with almost no traffic.
