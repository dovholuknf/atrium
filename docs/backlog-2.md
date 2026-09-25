# Backlog 2.0

Items owned by this line of work. The original `docs/backlog.md` belongs to another surface; do not edit it.

Sorted: what clint paused on 2026-09-24 first, then bugs, then decided features, then open housekeeping, then the
larger designs. Inside each group, the item closest to landing comes first.

| # | Item | Group | State |
| - | ---- | ----- | ----- |
| 1 | sa58: popped-out terminal also attached on the main board | paused | DONE, `7b62aa3` `7e85a80`, deployed |
| 2 | sa60: process registry design, revised | paused | DONE, `eb887da`, Mercurius ready_to_build |
| 3 | sa59: restart gate says why it went | paused | DONE, `8872505`, deployed |
| 4 | sa61: terminals pane group drag and group colours | paused | DONE, `b12b323`, deployed |
| 5 | One atrium: one binary, Mode A and B out, the hub becomes the atrium | paused | stage 1 DONE `948d557`, deployed, stages 2-7 wait on 13 questions |
| 6 | Taking a card out of a group | bug | DONE in `b12b323`, deployed |
| 7 | The held-message `!` chip says the wrong reason | bug | not started |
| 8 | Input lag follow-ups | bug | hop split DONE `5d9ba72`: the stall is the runner side, not atrium |
| 9 | Eliminate unstyled tooltips | bug | DONE, `069c16b`, deployed, check-titles guards it |
| 10 | `atrium_say` types immediately by default | feature | decided, not started |
| 11 | Clicking `? N` or a question clears it | feature | not started |
| 12 | Keep codex up to date | feature | not started |
| 13 | Housekeeping asked, not answered | housekeeping | waiting on clint |
| 14 | Per-card notification log | design | tentative |
| 15 | Pluggable event sink, what is left | design | stages 1-2 done |
| 16 | Reviews that remember: a resident reviewer per repo, and a panel that reads once | design, HIGH PRIORITY | not started, clint out of tokens 2026-09-25 |
| 17 | A Claude subagent finishing tells clint the card is waiting on him | bug | not started, repro on `openziti/ziti` `backport/v2.0.x-ctrl-heartbeat-reconnect` 2026-09-25 |
| 18 | On the terminals tab, toasts sit top right, not over the input line | feature | not started |
| 19 | Launch (and every other submit) shows it is working and refuses a second click | bug | not started |

------------

## Paused 2026-09-24: work in flight when clint stopped the wave

**Raised 2026-09-24.** Paused to save tokens. Each worker was told to commit what it had and stop. Their cards,
branches and worktrees are kept (do not cull). Nothing here is on `claude/main` unless it says so.

### 1. sa58: a popped-out terminal also attached on the main board

`claude/popout-double-attach`, **fix done at `c26ba01`, not merged.** clint saw win32crypto-e2e live in a
popped-out window and in the main board's terminals pane at once. Cause: the hub spells a card `room~id` with
several rooms attached and bare with one, a restart re-attaches rooms one at a time so the spelling flips, and
the pop-out claim code compared raw ids. Fixed by keying on the bare id. The popped-out window wins. Left: a green
`check-board.sh` rerun (the last failure looked like the load flake) and the manual AP checks on a two-room
throwaway hub.

### 2. sa60: process registry design, revised

`claude/process-registry-design`, **doc done at `00f8c4e`, not merged.** sa56's design
(`docs/process-registry-design.md`, on `claude/main`) revised with clint's answers: long-running services only,
a process never outlives its runner, no restart ever, only the owner stops it and others ask the owner. Adds a
section on the Windows firewall prompts (new exe paths listening on all interfaces: go test binaries in
`%TEMP%\go-build*`, per-worktree `build.claude`, atrium's own `:7777`/`:7778`/`:7801` defaults). Mercurius
`s_Klruhz3XfqAr` found one blocker, fixed with a gated `proc-exec` launcher that joins a room-owned job object
before it spawns. No second round ran. Open: processes on window-mode and joined cards (default: allow while the
reaper watches the pid), and gate as Bash or as a tool of its own (default: Bash). Build after One atrium.

### 3. sa59: the restart gate says why it went

`claude/gate-counts-every-board`, **WIP `e4052a2`.** At 12:57 a gated hub-only deploy printed "nobody is using a
board" with clint's board open. The first diagnosis (the gate misses single-room streams) was wrong: the hub audit
at 16:57:56Z says "restarting after a countdown nobody paused", and every stream path goes through feeds. The
script printed the same line for every go. The WIP makes a go answer say why and adds a path test for every
stream route. Left: docs, CHANGELOG, test-plan AQ, AM1's expected text, a non-WIP subject, and removing
`D:\tmp\gate59\old`.

### 4. sa61: terminals pane group drag and group colours

`claude/term-groups-drag-color`, **WIP `a6a9d3f`, a diagnostic probe only.** Found: the terminals-pane group
headings never wear `--ghue`, so they show a grey name with no hue. The stack, the board and tag chips recolour
correctly on harbour, daylight and website. The fix: put `--ghue` on `.tgroup`/`.tnest` the way `.stackgroup`
has it. Reload and a second window untested. Drag-to-reorder of custom group headings not started (write
`p.groups` the way `moveCustomGroup` does).

### 5. One atrium

`docs/one-atrium-plan.md`, **on `claude/main`, plan only.** Mode A and Mode B out, one `atrium` binary, "the hub"
becomes "the atrium" in text people read, `atrium run` starts the atrium and its room. Seven stages, a cutover for
this machine with rollback, 11 CLAUDE.md edits listed for clint. 13 Open Questions wait on clint (the
orchestrator's OWED table, row C1). Carries a live bug: the room's `daemon.json` names `atrium2.exe`, so the
board's "install hooks" writes hook lines that cannot run.

------------

## Bugs

### 6. Taking a card out of a group

**Raised 2026-09-24.** clint cannot find a way to remove a card from a custom group. A group is a tag, so the fix
is a way to drop that tag from where the card is filed: the card's menu (`out of group`), a drag out to
`untagged`, or both, on the stack, the board and the terminals pane. Fits with sa61's work.

### 7. The held-message `!` chip says the wrong reason

**Raised 2026-09-24** (clint screenshot `.atrium/incoming/20260924-133410-pasted.png`, 13:34). On a running card
with an EMPTY input line it said "delivers when your input line is clear and idle. clear or submit your line to
receive it now", which blames the line when the wait is really for the turn to end. Say which condition is
holding it. Show the COUNT of held messages on the chip (`! 2`), the way `? N` counts open questions. Most of
this goes away with item 10.

### 8. Input lag follow-ups

**Raised 2026-09-24.** 14:18 sample (clint, board at localhost:7778): 165 keys, p50 9.1ms, p95 ~90ms, max 268ms.
Every slow key is `wire` (hub, room, pty, runner redraw), none is send, parse or paint, and no fetch was in
flight. Spikes cluster over ~3s, then 4-10ms.

- Line up the hub's and the room's own hop logs for a slow key (ATRIUM_DEBUG_INPUTLAG is pinned on) to say which
  hop grows.
- `socket had N bytes unsent` counts the key's OWN frame (18 bytes = `{"t":"in","d":"x"}`), read just after send.
  Subtract it, or read it before send, so the line only appears when something was really queued.
- A console filter of `[atrium` hides every `[inputlag]` line, which made the logging look broken. Consider one
  prefix.

### 9. Eliminate unstyled tooltips

**Raised 2026-09-24.** The `!` chip uses a native `title` tooltip: plain white box, system font, no theme. Find
every native `title` tooltip on the board and replace it with the board's styled tooltip (the one help bubbles
and `data-tip` use, if one exists, or build one), on every skin. Add a `check-board.sh` rule that fails on a new
bare `title=` in `internal/api/web/` unless it is allowlisted (form controls where the browser tooltip is the
accessible name).

------------

## Decided features

### 10. `atrium_say` types immediately by default

**Decided 2026-09-24.** clint expected a say to land mid-turn the way his own typing does, and it waits for the
turn to end (the N7 rule), so a "stop now" to four workers reached none of them.

- The default becomes IMMEDIATE: typed as soon as the input line is empty and no dialog is open. Reuse the
  restart wake's gate (empty line, keyboard quiet, no dialog). Claude Code queues typed input mid-turn and reads
  it at its next step.
- The old behaviour becomes an option, `when: "done"`, for messages that should not disturb a worker mid-thought.
- Whether a runner takes typed input mid-turn is a RUNNER SETTING on its row on the runners page, seeded from the
  runner profile (claude: yes, codex: to be checked). A runner set to no falls back to done.
- The board gets an "immediately" button beside send.
- `internal/daemon/peers.go` and CLAUDE.md "Out of scope" both say peer text is never typed mid-turn. Both need
  rewording with this, and the CLAUDE.md files are clint's.

### 11. Clicking `? N` or a question clears it

**Raised 2026-09-24.** clint expects open questions he has looked at and clicked to go away. Decide whether a
click marks them answered or only seen, and do the same for `! N`.

### 12. Keep codex up to date

**Raised 2026-09-24.** Not started.

A codex session on the board stopped at start to update itself: `Updating Codex via npm install -g @openai/codex...`
inside the card's terminal, with nothing else to show for it. Claude already gets an update card
(`claude code: <old> to <new>`) from `internal/daemon/runnerupdate.go`, which reads the installed version from the
package metadata and the published one with one HTTP request.

- **A codex update task,** the same shape as claude's: a card saying `codex: <old> to <new>` when a newer
  `@openai/codex` is published, with the update as its action.
- **A "keep codex up to date" setting,** off by default. On, atrium runs the update itself between sessions, never
  while a codex session is running, and records the result on the card.
- **Codex updating itself inside a launch** should not look like a hang. Either the setting keeps it current so this
  never happens, or the launch passes whatever flag codex has to skip its own startup update.
- Check what `runnerupdate.go` already does for codex before building: its header names `codex --version`.

------------

### 12a. A resumed card keeps its old pid, and a live card reads done

**Raised 2026-09-25.** Counting `claude.exe` against the board: all 22 were the room's children, but six cards
resumed at the 17:08 room restart still recorded their OLD pid (aug-revisions, discourse-6087, sa58, sa59, sa60,
win32crypto-e2e), and four of them (win32crypto-e2e, sa58, sa59, sa60) read status `done` while their runner was
alive. Likely the same false `exited` event sa62 got at 16:23. The pid should update on every runner start, and a
card with a live runner must not read done. Anything that judges liveness by pid is misled today.

------------

## Housekeeping

### 13. Asked, not answered

**Raised 2026-09-24.** Waiting on clint:

- Delete worktrees already merged into `claude/main` (128 under `D:\worktrees\claude\atrium\`, several GB),
  and each future one when its work is accepted.
- Restart saNN numbering at sa01 after sa99.
- `Set-NetFirewallProfile -NotifyOnListen False`, clint's machine-wide call on the firewall prompts.

Accepted and on `claude/main` but NOT deployed: the process registry design doc and the one-atrium plan
(docs only).

------------

## 14. Per-card notification log

**Raised 2026-09-21. TENTATIVE - clint floated it, unsure it is worth it ("not sure about that one but maybe").**
Not started.

### The idea

A card accumulates notifications over its life: a peer message held/deferred and re-warned on backoff (see the
peer-message injection work), a permission asked, a going-down, an audit event. Today a notification fires once as
a transient toast (and toasts have been vanishing too fast to read), so a human who was not looking never learns it
happened. The board has a global notification history (the bell). This item is a PER-CARD view of that: open a card
and see the notifications it has raised, newest first, so "what has this session been trying to tell me" is
answerable after the fact rather than only in the moment.

### Why it might not be worth it

The global bell history plus the new per-card held-message indicator may already cover the need. The event log
(and the pluggable event sink above) already records `notified` events per card, so this could be a thin read view
over data that exists rather than new storage. Decide whether a dedicated per-card log earns its place or whether
filtering the existing history by card is enough. clint has not committed to building it.

------------

## 15. Pluggable event sink: get the audit trail out of the primary database

**Raised 2026-09-18.** Stages 1 and 2 done. The offsite sink and a live swap are left.

### Status

Done:

- **Stage 1** (in `02fe769`, landed 2026-09-18): the `EventSink` seam, the `db` hot sink, the rolling-JSONL `file`
  cold sink, the `event_sink` setting, the opt-in per-card hot window `event_window_bytes`, `rolled_off` on a
  card's event feed, and incremental auto_vacuum for fresh databases.
- **Stage 2, routing** (`1e03180`): `event_cold_kinds` names kinds that go to the cold sinks only. Off by default.
  The card's detail dialog says which kinds and whether older events rolled off.
- **Stage 2, compact** (`1070cbf`): `atrium2 db compact --in <db> --out <db> [--window-bytes N] [--drop-kinds
  k,...]`, an offline `VACUUM INTO` copy switched to incremental auto_vacuum.

Left:

- The `offsite` sink.
- The permission table, which is now the largest thing in the file (see the measurement). Nothing trims it.
- Swapping a compacted copy into place. Today that is a manual step with the room stopped.
- Turning a bound on by default, with a documented value.

### Measurement, 2026-09-24

A copy of the live room database, 57 MB (59.8 MB file plus a 4 MB `-wal`), 203 cards:

| what                                  | bytes on disk | rows   |
|---------------------------------------|---------------|--------|
| `event` table                         | 25.8 MB       | 62,287 |
| `event` indexes                       | 8.3 MB        |        |
| `permission` table                    | 20.8 MB       | 23,242 |
| `permission` indexes                  | 4.4 MB        |        |
| everything else                       | 0.3 MB        |        |

Event payload bytes per kind: `perm-decided` 8.6 MB (24,711 rows), `perm-requested` 5.7 MB (23,242), `prompted`
0.46 MB, `launched` 0.45 MB, `status-changed` 0.29 MB (8,097), `exited` 0.16 MB, the rest under 0.05 MB each.
There are **no `output` events**: no code path writes that kind any more. One card, `main:atrium`, holds 32k
events (8.8 MB of payload) and 13.6k permission rows (7.4 MB of command and details). The next nine cards hold
0.14 to 0.59 MB each. In the permission table, `details` is 8.3 MB and `command` 3.8 MB; `Edit` and `Write`
requests are 8.8 MB of that between them, since their details carry the file content.

`atrium2 db compact` on that copy:

| options                                                  | result  | events kept |
|----------------------------------------------------------|---------|-------------|
| none                                                     | 54.8 MB | 62,287      |
| `--window-bytes 262144`                                  | 37.7 MB | 28,974      |
| `--drop-kinds perm-requested,perm-decided`               | 28.7 MB | 14,334      |
| both                                                     | 27.8 MB | 11,572      |

### Decisions, stage 2

- **Route by kind, not `output` alone.** The brief was to route `output` cold if it was the bulk. It does not
  exist, and permission traffic is the bulk, so the setting takes any list of kinds. `output` fits it if it ever
  comes back.
- **No cold sink, no routing.** `event_cold_kinds` without a cold sink in `event_sink` is ignored and logged,
  rather than dropping events with nowhere to go.
- **`created` and `submitted` stay in the db.** `HistoryRolledOff` reads `created` and `LatestAgentReports` reads
  `submitted` from the table. Naming them is logged and skipped live, and refused by the compact.
- **The dialog says, it does not rebuild.** With perm events routed cold, the timeline shows no permission rows
  and a line naming the missing kinds. The Permissions pane still lists every decision from the permission table.
  Rebuilding timeline rows from that table is possible, but it is a second source for one view.
- **Compact refuses an open database by taking an exclusive lock with no wait.** A running room holds the file,
  so the lock fails at once. The lock is held through the `VACUUM INTO`, so nothing opens the input part way.
- **Compact never replaces the input.** The input is the archive for anything the copy drops. The swap stays a
  human step with the room stopped.
- **The permission table is not trimmed.** It is 25 MB with indexes and feeds the board's decisions list. Trimming
  it needs its own retention decision. Recorded as left.

### The problem

Every card keeps a full event history in the SQLite `event` table: `created`, `submitted`, `prompted`,
`perm-requested`, `perm-decided`, `status-changed`, `output`, `notified`, `launched`, `exited`, `compacted`.
`output` events carry chunks of terminal text. Across a long-lived board this is most of the database: a 94-card
board sat at ~40 MB, and almost none of that is the cards themselves.

A database is the wrong home for this. It is append-only, high volume, write-once, and read rarely. It is cold
audit data sharing a file with the hot operational state the board needs on every request, so the thing that
must stay small and fast is dragged down by the thing that only matters months later. What this data wants is a
log that rolls and can be shipped to long-term durable storage an operator chooses, then dropped locally.

### The seam that already exists

Two methods in `internal/store/tasks.go` are the whole surface:

- `AppendEvent(taskID, kind, payload)` writes one event.
- `Events(taskID, limit)` reads a card's recent events back, for the detail dialog and for anything deriving
  state from history.

Everything that logs an event goes through the first. Everything that reads history goes through the second.
A sink abstraction wraps exactly these two.

### The design

**An `EventSink` interface, and the store's table becomes one implementation of it.**

```
type EventSink interface {
    Append(taskID string, e Event) error
    Recent(taskID string, limit int) ([]Event, error)  // may be unsupported; see the split
}
```

**The hot/cold split is the crux.** Two different questions hide in the event log:

- HOT: "what are this card's last N events", which the board asks on every card open. Needs fast, indexed,
  local reads. Small and bounded.
- COLD: "everything that ever happened, kept forever somewhere durable", which nothing on the board reads. Big,
  write-once, ship-and-forget.

So sinks compose rather than one replacing the other:

- A **hot sink** serves `Recent`. Bounded: the last N events per card, or the last few days, in SQLite (or a
  small ring). This is what keeps the primary database small: it holds a window, not all of history.
- One or more **cold sinks** are write-only durability and do not serve `Recent`. They roll and ship.

### The sink options to build

1. **`db` (default).** What exists today, but bounded to a hot window so it stops growing without limit. Nothing
   changes for anyone who does not opt in.
2. **`file`.** Append JSONL, one line per event, rolled by size and by day, under a logs directory. A reader
   tails the current files to serve `Recent` when the db window is not the hot sink. Files roll so an operator
   can move a closed file off the machine and delete it.
3. **`offsite` (S3 / object store, or an event-stream endpoint).** Write-only, asynchronous, batched, best
   effort. Does not serve `Recent`. A plugin boundary here: S3 first, but the shape (open a batch, flush, close)
   is the same for an HTTP webhook, a Kafka topic, or whatever a data lake ingests. Atrium holds the NAME of a
   command or endpoint that has a credential and never the credential itself, the same rule overlays already
   follow.

Composition: `hot=db, cold=[file, s3]` is a real configuration. Reads hit `db`; writes fan out to all three,
cold ones async.

### Resilience, which is not optional here

A cold sink that is slow or down MUST NOT block a hook or a tool call. This is the daemon's existing posture: a
hook must never fail a session, `/activity` is fire-and-forget, storage failure halts rather than degrades.
Cold-sink writes are buffered and flushed on their own goroutine, and under backpressure they drop with a
counted, logged loss rather than stalling the session. The hot sink stays synchronous and on the halt path,
because the board depends on it and a lie there is worse than a stall.

### Configuration

A hub or room setting, `event_sink`, naming the hot sink and the cold sinks, defaulting to `db` alone so a
fresh install and every existing one behave exactly as now. Per the observed-versus-overrides rule, this is an
override a human types; nothing infers it.

### Retention falls out of it

With history in rolling files or shipped offsite, the primary database holds only the hot window, so it stays
small on its own and pruning stops being the only lever. The existing "delete finished cards for good" pruning
still applies to the hot store; the cold trail is retained by whatever policy the file roller or the data lake
enforces, which is where retention belongs.

### Migration and rollout

- Default `db`, bounded window off by default at first so nothing shrinks under anyone without them asking, then
  a follow-up that turns the bound on with a documented default.
- `file` and `offsite` are opt-in.
- No schema break: the `event` table stays; it just stops being unbounded, and stops being the only sink.

### Open questions

- What the hot window is measured in: last N events, last D days, or a size cap per card. Probably a size cap,
  matching how scrollback is already bounded.
- Whether `Recent` must ever read from a cold sink (for a card whose hot window rolled off but which somebody
  opens). Leaning no: the board shows "history rolled off, see the archive at <where>", the same way an offline
  room shows what it last said rather than pretending.
- Whether output events belong in the event log at all, or are a separate stream from the start. They are the
  bulk and the least like an audit event. (2026-09-24: moot for now. No code writes `output` events; see the
  measurement above.)

### Reclaiming space the bound leaves behind

The hot window stops the database growing, but it does not shrink a file that already grew. SQLite frees pages
inside the file when rows are deleted or rolled off and reuses them for new writes, so the file stays at its high
water mark. A 40 MB file that pruned down to a few MB of live data keeps sitting at 40 MB. Only `VACUUM` rebuilds
the file and returns the space to disk.

The catch is that `VACUUM` needs exclusive access. A room holds its database open to run the agents' terminals,
so vacuuming in place means taking the room down, which kills those terminals. That is the wrong price for
reclaiming disk. Options to design for, in rough order of preference:

- **`auto_vacuum=INCREMENTAL` from the start**, with `PRAGMA incremental_vacuum` run on a timer against free
  pages. This trims the file gradually while the room stays up, at the cost of some write overhead and a decision
  made at database creation (it cannot be turned on for an existing file without one full rebuild). New rooms
  could adopt it now.
- **A `VACUUM INTO` copy plus swap on a clean handoff**: the room writes a compacted copy while live, then swaps
  it in during a controlled restart when the terminals are already parked (a scheduled maintenance window, or the
  reload-design binary swap that already restarts on a build id). Reuses machinery that exists rather than a new
  stop-the-world path.
- **Accept the high water mark** once the hot window bounds growth. If the file plateaus at a bounded size, never
  reclaiming is a fine answer and the simplest one. This is the default until the plateau proves too large.

The event sink makes this smaller either way: move the bulk (`output` and old audit rows) out to files or
offsite, and the primary database plateaus low enough that shrinking it stops mattering.

## 16. Reviews that remember (HIGH PRIORITY)

Raised by clint 2026-09-25 during a `review-panel` run on openziti/ziti PR #4480 (a v2.0.x backport). Four
reviewers (go-security-reviewer, codebase-steward, functional-tester, nonfunctional-tester) each ran for 5.5 minutes
and read 104k to 133k tokens, mostly the same files: `context.go`, `controller.go`, `multi.go`, `router.go`.

### Why it happens

A Claude Code subagent starts with an empty conversation on every call. Only a `fork` inherits the caller's
context. So every reviewer in a panel rediscovers the same diff and the same neighbours on its own, and nothing it
learned survives to the next PR in the same repo.

### Two separate fixes

1. **Within one review: read once.** The conductor reads the diff and its neighbours once, writes a context digest,
   and the reviewers start from it (or run as forks sharing one cached prefix) and open source only to verify a
   finding. Size the panel to the diff: a small backport gets one or two reviewers, not four. This is a change to the
   `review-panel` skill in dotfiles, not to atrium. Measure tokens on one PR before and after.
2. **Across reviews: a resident reviewer per repo.** A standing atrium session whose folder follows clint's scm layout
   (`<host>/<org>/<repo>`, subprojects inside), keeping what it learned in a persistent CLAUDE.md and state files
   there. The next PR on that repo starts from that knowledge instead of from zero. This is the "personas that learn
   per repo" idea parked in `docs/far-backlog.md`, and the reason to unpark it.

### Open

- Where the per-repo state lives (dotagents persona folder, the scm worktree, or atrium) and who reviews its edits.
- How a resident reviewer keeps its knowledge current when the repo moves under it.
- Whether fix 1 alone is enough for most PRs.

## 17. A Claude subagent finishing tells clint the card is waiting on him (bug)

Raised by clint 2026-09-25. A `review-panel` run starts 3 to 5 Claude Code subagents (the Task tool, not atrium
sessions) inside one card. As each subagent finishes, the board tells clint the card is done and waiting on him,
while the parent session is still mid-turn collecting the other reviewers. One review produces 3 to 5 false "waiting
on you" alerts.

Repro, live when filed: the card in `D:\worktrees\github\openziti\ziti\pr-4480`, branch
`backport/v2.0.x-ctrl-heartbeat-reconnect`, during a review panel on PR #4480.

Expected: a subagent ending is not the card's turn ending. Only the parent session's own Stop may move the card to
waiting or raise the alert.

To find out first: which hook event the subagent's end arrives as (a `SubagentStop`, or a `Stop` carrying the
subagent's session or agent id), and which atrium path turns it into the alert (`atrium turn --event end`, the
activity hook, or the post-every-Stop change from N11). The fix is to recognise a subagent's end and ignore it for
status and notifications, with a fake-runner test that raises parent and subagent stops.

## 18. On the terminals tab, toasts sit top right (feature)

Raised by clint 2026-09-25 with a screenshot (`.atrium/incoming/20260925-124402-pasted.png`): a "sa65 ... is ready /
finished its turn and wants your next instruction" toast in the bottom right covered the terminal's input line and
status bar, exactly where he was typing.

Wanted: while the terminals view is showing, the toast stack anchors top right. Other views keep bottom right.

Watch for:
- The paste indicator (`#t-pasting`, top right of `#term-pane`, from BB) and any other top-right terminal chrome. The
  stack must not cover them, or they move.
- The restart countdown and paused toasts (sticky, hubrestart.js) ride the same stack. They move with it.
- A popped-out terminal window is a terminal too: same rule there.
- Switching view while toasts are up moves the stack without dropping or re-animating them.
- Stack order: newest nearest the anchor, so newest at the top when anchored top.

## 19. Launch shows it is working and refuses a second click (bug)

Raised by clint 2026-09-25 with a screenshot (`.atrium/incoming/20260925-141113-pasted.png`): the "resume claude code"
dialog on a card in `D:/worktrees/github/openziti/desktop-edge-win/sec-report-sep`, "pick up where it left off"
ticked. He clicked `launch`, saw nothing change, and clicked again. The first click launched. The second raised an
error.

Wanted:
- The moment `launch` is pressed it shows a spinner and a working label, and it and the dialog's other actions are
  disabled until the request answers. Success closes the dialog. Failure re-enables it and says why.
- A second submit is refused in the board, not left for the daemon to reject. The daemon side should also be safe:
  a second resume or launch of a card that is already starting answers with the first request's result rather than
  an error (an idempotency key per dialog open, or "already starting" treated as success).

Then sweep the board for the same race on every button that fires a request: new agent, resume, terminate, remove,
shelve, rule save, settings save, `use it` on a theme, file upload, message send, the permission approve / deny
buttons, source and harness saves, and anything else. One shared helper (busy state + in-flight guard) rather than
per-button code. Headless tests that double-click each and assert one request.


------------
