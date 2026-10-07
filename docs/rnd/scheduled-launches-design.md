# Scheduled launches: a row with a schedule and a launch request, and the nightly test suite as its first use

Status: proposed, not built. clint said build later, 2026-09-30.

Origin: design, 2026-09-30, @rnd. Build later (clint, 2026-09-30). Item r-038 (`docs/backlog/runtime/r-038.md`),
from item 4 of `docs/rnd/competitor-features.md`. Built by @runtime, with a board pane from @ui.

## 1. The answer in five lines

1. A schedule is a row: when, which room, a stored launch request, an owner, and the last result. Source rules
   carry over: a failed run is reported on the row and tried again at the next time, and three failures in a row
   switch it off with the reason.
2. **It lives on the HUB, not in a room**, which is the one departure from "a row like a source". Every launch the hub
   makes goes through the launch cap, the item-dependency refusal and the deploy-hold refusal, and a room-side timer
   would go round all three. The hub also stays up while a room restarts, so it can say "room offline, tried again".
3. A missed time is run ONCE if it is still inside a catch-up window (default 2 hours), and recorded as missed
   otherwise. Never a backlog of runs.
4. An earlier run still working means this one is skipped and recorded, never a second card beside it.
5. An agent may write a schedule, but it starts DISABLED. A human turns it on, because a schedule is a standing
   decision to spend with nobody watching.

The first use is two nightly schedules, the Go suite for @merge and the UI suite for @ui, each a lean Sonnet worker
that runs one script, waits inside that one call, and reports (section 7).

## 2. The row

Hub store, migration `0005_schedule` (or the next free name when it is built, at the END of the slice):

```sql
CREATE TABLE IF NOT EXISTS schedule (
  id            TEXT PRIMARY KEY,
  label         TEXT NOT NULL,                     -- unique among live rows, shown on the board and in tags
  enabled       INTEGER NOT NULL DEFAULT 0,        -- 0 when an agent wrote it. a human sets 1
  spec          TEXT NOT NULL,                     -- section 3
  tz            TEXT NOT NULL DEFAULT '',          -- IANA name. empty is the hub machine's local zone
  room          TEXT NOT NULL,
  owner         TEXT NOT NULL,                     -- a handle, resolved at delivery, like report_to
  launch        TEXT NOT NULL,                     -- the atrium_launch input, as JSON
  heavy         INTEGER NOT NULL DEFAULT 0,        -- one heavy scheduled card per room at a time
  catch_up_secs INTEGER NOT NULL DEFAULT 7200,
  next_at       TEXT NOT NULL,
  last_run_at   TEXT NOT NULL DEFAULT '',
  last_result   TEXT NOT NULL DEFAULT '',          -- launched, skipped, deferred, missed, failed, with the reason
  last_card     TEXT NOT NULL DEFAULT '',          -- room~id of the card the last launch made
  failures      INTEGER NOT NULL DEFAULT 0,
  created_by    TEXT NOT NULL,                     -- handle@room, or 'human'
  created_at    TEXT NOT NULL,
  notes         TEXT NOT NULL DEFAULT ''
);
```

`launch` is the same input `atrium_launch` takes (cwd, title, brief, prompt, runner, model, effort, lean, tags,
theme), checked by the same code when the row is written, so a row that could never launch is refused at write and
not at 03:00. Like a source, there is nowhere in it for a credential, and `env` entries follow the launch rule that
refuses `ATRIUM_` names.

A scheduled card is tagged `origin:schedule`, `schedule:<label>` and `atrium:subagent`, its `report_to` is the
schedule's `owner`, and its title is `<label> <date>: <title>`. So it reports to a person or a director, it counts as
a worker for the launch cap and for merged-cull, and the board can group a schedule's runs.

## 3. When: a small grammar, not cron

Four forms, parsed by one pure function with its own tests:

| Spec | Means |
|---|---|
| `daily 03:00` | every day at 03:00 |
| `weekdays 07:00` | Monday to Friday at 07:00 |
| `weekly sun 04:30` | once a week |
| `every 6h`, `every 45m` | a fixed interval from the last run, at least 15 minutes |

Five-field cron is left out on purpose. Nobody on this board has asked for "the second Tuesday", every form above
reads aloud, and a parser dependency buys only the cases nobody uses. A form can be added the day one is needed.

`next_at` is worked out from NOW when a run happens, not from the time that was due, so a hub that was down for a day
does not fire a burst. A clock time that falls in a daylight-saving gap runs at the first valid minute after it, and
one that happens twice runs once, at the first.

## 4. What happens at a due time

The hub's scheduler ticks every 30 seconds and does nothing unless a row is due. For each due, enabled row, in turn:

1. **Overlap.** If `last_card` is still working (not `done`, `dead` or `shelved`, read through the proxy as
   `atrium_task` reads a card), the run is SKIPPED: `skipped: <card> from <time> is still running`. Not a failure.
2. **Heavy.** If the row is `heavy` and another heavy scheduled card is still working on the same room, the run is
   DEFERRED and tried every 5 minutes inside the catch-up window. This is the machine-load rule (one heavy job on a
   machine at a time) for the jobs atrium starts itself. It cannot see a test run a director started by hand, which
   is why the nightly rows sit at hours nobody is working.
3. **Launch**, through the same function `atrium_launch` calls, with the schedule as the caller. Then every refusal
   `atrium_launch` can give applies unchanged, and each is recorded on the row:
   - the room's launch cap is full: DEFERRED, tried every 5 minutes inside the catch-up window, then MISSED,
   - the room is offline, or under a deploy hold (`docs/rnd/room-deploy-hold-design.md`): DEFERRED the same way,
   - the title names an item with an open gate (`docs/runtime/item-dependencies-design.md`): SKIPPED, with the gate,
   - anything else (the cwd is gone, the runner is missing): FAILED.
4. **Record.** `last_run_at`, `last_result`, `last_card`, `next_at`. Three FAILED in a row switch the row off with
   the reason, and the owner is told once. Skipped, deferred and missed never count as failures, since none is the
   schedule's fault.

**Missed runs across a restart.** On hub start, a row whose most recent due time is after `last_run_at` is run once
if that due time is inside `catch_up_secs`, and marked `missed <time>` otherwise. `catch_up_secs = 0` means never
catch up, for a job where "late" is worse than "not at all".

**Who is told.** A launch tells nobody: the card reports to its owner when it is done, and that is the one message.
A switch-off tells the owner once. Skips and misses are on the row and the board, and tell nobody, because a message
for every skipped night is the noise a schedule is meant to remove.

## 5. The surface

- MCP tool `atrium_schedule`, full class only: `list`, `add` (always written disabled), `edit` (a change to `spec`,
  `room`, `launch` or `owner` disables it again), `run_now` (enabled rows only, same checks), `remove`.
- `GET/POST /_hub/schedules`, `POST /_hub/schedules/{id}/enable` (board only, refuses `X-Atrium-Agent`, with the same
  caveat item dependencies states: tidiness, not a boundary, and audited as `schedule-enable`).
- CLI: `atrium schedule list|add|enable|disable|run-now|rm`.
- Board (@ui): a Schedules pane on the hub board. One row each: label, spec in words, room, owner, next time, last
  result and a link to the last card, an enable switch and "run now". A disabled row an agent wrote says who wrote it.

## 6. Resilience

- A schedule is a suggestion, never durable state. A scheduler failure is logged on the row and can halt nothing.
- The tick reads the store only. The one network call is the launch itself, bounded like every control call.
- Nothing retries faster than every 5 minutes, and nothing retries outside the catch-up window.
- A hub store failure is the hub's existing halt, and the scheduler stops with the rest.

## 7. First use: the nightly full test suite

Two rows, both `heavy`, one hour apart so they never share the machine:

| Label | Spec | Room | Owner | What the worker runs |
|---|---|---|---|---|
| `nightly-go` | `daily 02:00` | claude-sg4 | `merge` | `pwsh -File scripts/nightly-suite.ps1 -Part go` |
| `nightly-ui` | `daily 03:00` | claude-sg4 | `ui` | `pwsh -File scripts/nightly-suite.ps1 -Part ui` |

The UI row is owned by @ui because only @ui runs the UI tests (clint, 2026-09-29), and a nightly run is still a run.

**`scripts/nightly-suite.ps1` holds all the logic**, so it can be run by hand exactly as the schedule runs it, and the
worker's job is to call it and read its answer. Built with r-038:

- Runs in a dedicated worktree, `D:/worktrees/claude/atrium/nightly`, moved to the tip of `claude/main` (detached) at
  the start. It never commits, and never touches a director's worktree.
- Clears `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` first, which a test run inside a room session otherwise
  inherits and fails on.
- `-Part go` runs `bash scripts/ci.sh`. `-Part ui` runs the UI suite the way @ui runs it. Each writes its whole
  output to `<atrium dir>/nightly/<date>-<part>.log` and prints ONE last line: `PASS <part> <sha> <duration>` or
  `FAIL <part> <sha> <n> failing: <names>`, with the exit code to match.
- Keeps the last 14 logs and deletes older ones.

**The worker** is a lean Sonnet worker at low effort. Its brief, which is the row's `launch.brief`:

> You are the nightly `<part>` run for `<date>`. Run `pwsh -File scripts/nightly-suite.ps1 -Part <part>` in this
> directory, as ONE call, and wait for it. It can take 40 minutes. Do not poll and do not narrate.
> If its last line starts `PASS`: call `atrium_report` with status `done`, the line as the summary, and `no_commit`
> "nightly run". Then exit.
> If it starts `FAIL`: read only the failing tests' names and first lines in the log it names, and the previous
> night's log beside it. Report `done` with: which tests fail, which of them also failed last night, and the commits
> since the last `PASS` (`git log --oneline <last-pass-sha>..<sha>`). `TestRealSessionsKeepTheirText` fails on this
> machine's own data and is known noise: name it apart. Do not fix anything, and do not rerun anything. Then exit.

**What a night costs.** A green night is one lean session that makes one tool call, waits inside it for free, and
reports once. A red night adds reading a bounded slice of a log. Both show up in `session_usage` under the card, so
after a week the real figure is on the board. If a green night proves too dear, stage 2's precheck (section 8) takes
green nights to zero tokens.

**Before it is turned on** a human enables both rows, after one `run_now` of each has been read.

## 8. Later (named, not promised)

- **A precheck**: a command the ROOM runs (a source-shaped row in the room's own store, so the hub never sends a
  command to run), with `launch when: fails | passes`. The nightly suite then launches a worker only on a red night,
  with the failing lines written into its brief. It needs a room build and a design for the room-side half.
- **Budgets** from r-037 on a schedule's launch, so a scheduled worker that runs away is stopped by the tree budget.
- **`skipped` streaks** reported to the owner (for example seven skips in a row), if a silent skip turns out to hide a
  stuck card.

## 9. Tests

- Grammar: each form, the 15-minute floor, daylight-saving gap and repeat, `tz` honoured, `next_at` from now.
- Write: an agent's `add` is disabled, an `edit` of the launch disables it again, a launch that could never run is
  refused at write, an `ATRIUM_` env name is refused.
- Due: overlap skips, heavy defers while another heavy card works, a full cap defers then misses, an offline room and
  a held room defer, an open item gate skips with the gate, a missing cwd fails.
- Three failures switch the row off and tell the owner once. Skips, defers and misses do not count.
- Restart: a due time inside the window runs once, one outside is `missed`, `catch_up_secs = 0` never catches up,
  and a day of downtime makes one run, not a burst.
- The card: tags, title, `report_to` the owner, counted by the launch cap.
- `scripts/nightly-suite.ps1`: moves the worktree to the tip, prints one last line in the stated form with a matching
  exit code, and keeps 14 logs.
