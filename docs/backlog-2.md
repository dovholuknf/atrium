# Backlog 2.0

Items owned by this line of work. The original `docs/backlog.md` belongs to another surface; do not edit it.

Sorted: what clint paused on 2026-09-24 first, then bugs, then decided features, then open housekeeping, then the
larger designs. Inside each group, the item closest to landing comes first.

| # | Item | Group | State |
| - | ---- | ----- | ----- |
| 1 | Popped-out terminal also attached on the main board | paused | by sa58, DONE, `7b62aa3` `7e85a80`, deployed |
| 2 | Process registry design, revised | paused | by sa60, DONE, `eb887da`, Mercurius ready_to_build |
| 3 | Restart gate says why it went | paused | by sa59, DONE, `8872505`, deployed |
| 4 | Terminals pane group drag and group colours | paused | by sa61, DONE, `b12b323`, deployed |
| 5 | One atrium: one binary, Mode A and B out, the hub becomes the atrium | paused | stage 1 DONE `948d557`, deployed, stages 2-7 wait on 13 questions |
| 6 | Taking a card out of a group | bug | DONE in `b12b323`, deployed |
| 7 | The held-message `!` chip says the wrong reason | bug | DONE with item 10, `1ff7503` |
| 8 | Input lag follow-ups | bug | hop split DONE `5d9ba72`: the stall is the runner side, not atrium |
| 9 | Eliminate unstyled tooltips | bug | DONE, `069c16b`, deployed, check-titles guards it |
| 10 | `atrium_say` types immediately by default | feature | DONE, `04c2095`, not deployed. Also covers item 7's reason and count |
| 11 | Clicking `? N` or a question clears it | bug | not started, 2026-09-25: the click selects the row instead |
| 12 | Keep codex up to date | feature | not started |
| 13 | Housekeeping asked, not answered | housekeeping | waiting on clint |
| 14 | Per-card notification log | design | tentative |
| 15 | Pluggable event sink, what is left | design | stages 1-2 done |
| 16 | Reviews that remember: a resident reviewer per repo, and a panel that reads once | design, HIGH PRIORITY | not started, clint out of tokens 2026-09-25 |
| 17 | A Claude subagent finishing tells clint the card is waiting on him | bug | DONE, merged `29d45f5`, needs a hook binary rebuild and a room restart. Test plan BN |
| 18 | On the terminals tab, toasts sit top right, not over the input line | feature | DONE, `910b186` |
| 19 | Launch (and every other submit) shows it is working and refuses a second click | bug | DONE, `eb1603e` board, `50db006` daemon |
| 20 | Selecting the terminal that is already attached re-renders its whole history | bug | DONE, `cf2fc2b` |
| 21 | A card stuck on `running` after a lost Stop gets a "looks idle" badge from its silent terminal | bug | not started |
| 22 | Copy on select copies every find match (ctrl-shift-f) | bug | DONE, `54fb554` |
| 23 | Keep idle Claude cards' prompt caches warm, stop at break-even | feature | by sa78, DONE, merged `a917535`, deployed 2026-09-28 |
| 24 | The "not replayed here" notice opens or loads the pre-restart history | feature | by sa81, DONE, `ddbeb9c` `cda7ae5`, the daemon half needs a room restart |
| 25 | False STUCK alert after a slash command and a restart; stuck mark on the card; gear setting | bug | by sa80, DONE, `d4803aa` `42258ef` `2a4ae89`, the fix needs a room restart |
| 26 | Toasts pop and disappear in the same second | bug | DONE by sa83, `c843139`, not merged |
| 27 | A say to a session that has gone waits forever, blaming the input line | bug | DONE by sa83, `04bad69`, Esc Esc `d1c2454`, chip tip `dc06056`, not merged |
| 28 | The full headless board run fails most of the time on `claude/main` | bug | DONE by sa83, `39c76dc` `a74c95e` `2d47379`, not merged |
| 29 | Lean workers, a launched worker starts with only what it needs | feature | by sa85, DONE, `1043e35` `42b2221` `ac59568` `764e2a8`, the room half needs a room restart |
| 30 | Peer review on the mercurius protocol, run in an atrium session | design | not started, 2 open questions |
| 31 | STUCK fires on a worker whose turn ended while it waits on background runs | bug | not started |
| 32 | A queued say from http-support never produced a backlog entry, and nothing can say why | bug | not started |
| 33 | Watching a terminal holds every message to it, word deletes miscount, a gate debug readout | bug | sa89, started 2026-09-28 |
| 34 | Every MCP tool call skips atrium's permission gate | bug | DONE 2026-09-28 in dotfiles, uncommitted, live through the hooks symlink |
| 35 | A card has a name you mention it by, like `@dotfiles` | feature | sa89, started 2026-09-28 |
| 36 | A finished worker stays up until somebody closes it | bug | DONE by sa36, merged `c3dc597`, deployed `66717c5` |
| 37 | Token and context use on record for every session, shown only in a card's details | feature | DONE, sa90 merged. sa94: Claude subagent rows, needs a room restart. Test plan BT5 |
| 38 | A restart resumes only the cards that were working | feature | waits on 37 |
| 39 | Keep-alive warms the cards you mark, not every idle card | feature | waits on 37 |
| 40 | The launch cap counts only `atrium:subagent` cards | bug | sa91, started 2026-09-28 |
| 41 | A resident session owes its launcher a report for every prompt, from anybody | bug | not started |
| 42 | A running card wears the `!` chip for a message held until its turn ends | bug | DONE by sa42, merged, needs room and hub restarts |
| 43 | A worker's finished turn shows "nobody has looked" to clint, although its launcher read the report | bug | not started |
| 44 | A gear checkbox: no notifications from agent-launched cards, on by default | feature | not started |
| 45 | Every card shows its context size, a launcher hears once past a threshold | feature | sa87 built it to clint's decision on `claude/context-size`, not merged |
| 46 | Provision a machine as a room over ssh, from one command and later from the board | feature | stage 1 sa92, started 2026-09-28 |
| 47 | A resident session's alias defaults from its name, can be read and set, and heads the terminal title bar | feature, HIGH | DONE by sa47, merged `5b3d9e5`, deployed `66717c5` |
| 48 | `atrium_launch` takes a model and a thinking effort | feature | DONE by sa48, merged, needs room and hub restarts |
| 49 | The orchestrator can appear on every room | design | deep backlog, not started |
| 50 | Views of agents, beyond groups | design | not started |
| 51 | Five kept worktrees show 48 commits not matched on `claude/main` | housekeeping | DONE, all five safe, deleted 2026-09-28 |
| 52 | A pinned strip with cards from two rooms orders only one room | bug | not started |
| 53 | `setViewport` and `dropViewport` compute under `r.mu` and apply outside it | bug | not started, never reproduced |
| 54 | Terminal test suite part 2: `screen.go` against xterm.js | feature | not started |
| 55 | Launched runners inherit ATRIUM_DEBUG_INPUTLAG from the room | bug | DONE by sa55, merged, needs a room restart |
| 56 | Every dialog is sleek, one skinned design, starting with card details and the room's edit-agents screen | feature, design first, HIGH | DONE by sa56, merged `a1ab1c2`, deployed `66717c5` |
| 57 | The live room leaks memory | bug, HIGH | DONE by sa57, merged `aa74dd5`, deployed `66717c5` |
| 58 | `atrium_say` reaches cards on other rooms, `name@room` | feature, HIGH | DONE by sa58, merged, needs hub and room restarts |
| 59 | Spike on m1mini: more than one room per machine, and a blocked room that drains | design, spike | deep backlog, not started |
| 60 | The stdio control MCP has sa48's launch fields but no "room is older" warning | housekeeping | not started |
| 61 | A fake 45s hub echo in the lag log from the idle ping and pong | bug | not started |
| 62 | A worker that ends its turn without a report reaches its orchestrator every time | bug, HIGH | DONE by sa62, merged `f22115e`, deployed `66717c5` |
| 63 | Starting onto an existing card goes to the wrong room | bug, HIGH | not started |
| 64 | A card cannot stop being lean | bug, HIGH | DONE by sa64, merged, needs room and hub restarts |
| 65 | A deploy's revert snapshot is named after the hub's build, not the file it copies | bug | not started |
| 66 | New context: capture state, clear, and wake, from one click or one key | feature | DONE by sa66, merged `b4af8ed`, deployed `66717c5` |
| 67 | A first-run dialog eats a launched card's first prompt | bug, HIGH | DONE by sa67, merged `421ce3c`, deployed `66717c5` |
| 68 | `atrium_exit` and `atrium_task` do not take a card on another room | bug | DONE by sa68, merged `3cbbaad`, deployed `66717c5` |
| 69 | The details popover opens in one second, under the pointer, on the terminals and stack tabs too | feature | DONE by sa69, merged `1e2b702`, deployed `66717c5` |
| 70 | Keep-alive is invisible until it has spent something, and one card overspent its budget | feature, bug | DONE by sa70, merged `dc3c3a2`, deployed `66717c5` |
| 71 | "New context" on the terminals tab's right-click menu | feature | end of backlog, clint unsure it is useful |
| 72 | One hover on a card, not two | feature | DONE by sa72, merged `66717c5`, deployed `66717c5` |
| 73 | A keep-alive fork carries the card's launch args, so lean cards can warm | feature | not started |
| 74 | A long reply loses lines in the middle on the board's terminal | bug | sa74, paused at `ba761d6` (old SHA), rebase onto `66717c5` |
| 75 | sg3 as a room, and machine bootstrap reuses the operator's shared folder under `localai` | feature | sa75, parked. Provision script merged `c3dc597` |
| 76 | A worktree helper that links every CLAUDE.md, so workers get project rules | bug, HIGH, FIRST | not started, @merge |
| 77 | A merge pipeline that does not conflict or rerun | feature, HIGH | not started, @merge, after 76 |
| 78 | The details popover's token labels mislead | bug | not started |
| 79 | The notification drawer can turn notifications off | feature | design filed, queued behind 78, 44 and 43 |
| 80 | Real-time token burn and usage charts | feature, TONIGHT | design filed, ahead of 43 and 79 |

------------

## Paused 2026-09-24: work in flight when clint stopped the wave

**Raised 2026-09-24.** Paused to save tokens. Each worker was told to commit what it had and stop. Their cards,
branches and worktrees are kept (do not cull). Nothing here is on `claude/main` unless it says so.

### 1. A popped-out terminal also attached on the main board

`claude/popout-double-attach`, **fix done at `c26ba01`, not merged.** clint saw win32crypto-e2e live in a
popped-out window and in the main board's terminals pane at once. Cause: the hub spells a card `room~id` with
several rooms attached and bare with one, a restart re-attaches rooms one at a time so the spelling flips, and
the pop-out claim code compared raw ids. Fixed by keying on the bare id. The popped-out window wins. Left: a green
`check-board.sh` rerun (the last failure looked like the load flake) and the manual AP checks on a two-room
throwaway hub.

### 2. Process registry design, revised

`claude/process-registry-design`, **doc done at `00f8c4e`, not merged.** sa56's design
(`docs/process-registry-design.md`, on `claude/main`) revised with clint's answers: long-running services only,
a process never outlives its runner, no restart ever, only the owner stops it and others ask the owner. Adds a
section on the Windows firewall prompts (new exe paths listening on all interfaces: go test binaries in
`%TEMP%\go-build*`, per-worktree `build.claude`, atrium's own `:7777`/`:7778`/`:7801` defaults). Mercurius
`s_Klruhz3XfqAr` found one blocker, fixed with a gated `proc-exec` launcher that joins a room-owned job object
before it spawns. No second round ran. Open: processes on window-mode and joined cards (default: allow while the
reaper watches the pid), and gate as Bash or as a tool of its own (default: Bash). Build after One atrium.

### 3. The restart gate says why it went

`claude/gate-counts-every-board`, **WIP `e4052a2`.** At 12:57 a gated hub-only deploy printed "nobody is using a
board" with clint's board open. The first diagnosis (the gate misses single-room streams) was wrong: the hub audit
at 16:57:56Z says "restarting after a countdown nobody paused", and every stream path goes through feeds. The
script printed the same line for every go. The WIP makes a go answer say why and adds a path test for every
stream route. Left: docs, CHANGELOG, test-plan AQ, AM1's expected text, a non-WIP subject, and removing
`D:\tmp\gate59\old`.

### 4. Terminals pane group drag and group colours

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

**2026-09-25, now a bug.** clint clicked the `? 1` chip on the `zrok-research` row in the terminals pane
(`.atrium/incoming/20260925-141307-pasted.png`). It did not clear. The click fell through to the row and selected
the terminal instead. The chip must take its own click (stopPropagation) and clear, without selecting the row. The
wasted repaint that selecting caused is item 20.

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

Found 2026-09-28: it is the parent's own `Stop`, not a subagent's. The review panel starts its subagents in the
background, so the parent's turn ends with them still working, and each report wakes the parent, which reads it and
stops again. The pr-4480 transcript shows five parent Stops during the panel (16:34:06Z to 16:41:31Z), each after a
subagent hand-back. `atrium turn` already ignored `SubagentStop`. A probe with a live Claude Code showed the parent's
Stop payload lists the running subagents in `background_tasks`, and that `SubagentStop` fires for more than the
agents started: the pr-4480 run logged four within a second, a minute before any reviewer finished.

Fixed: the Stop hook sends the count of running subagents, and while it is above zero the room keeps the card
running and silent, and ignores an `idle_prompt` notification. The Stop that leaves none running moves the card. Codex
has no `background_tasks` in its Stop payload, so it is unchanged.

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

## 20. Selecting the attached terminal re-renders its whole history (bug)

Raised by clint 2026-09-25 alongside item 11. Clicking a row in the terminals pane for the terminal that is ALREADY
attached re-attaches it and replays the full scrollback (up to the newest 4 MB of pre-restart history, from N3).
That is slow and wasteful, and it moves the scroll position.

Wanted: a click on the row already attached and live is a focus, not a re-attach. Only a different card, a dead
socket, or an explicit "reattach" should replay history. Check the other paths that can land on the same card too:
the notification landing helper `landOnAlert` (item from sa73, `js/toasts.js`), a `#term=` hash, the theme preview
`use it`, and the popped-out window's return. Headless test: a second select of the attached card opens no new
socket and writes nothing to xterm.

## 21. A card stuck on `running` after a lost Stop looks idle (bug)

Raised by clint 2026-09-25 with a screenshot (`.atrium/incoming/20260925-154851-pasted.png`): the card `tlsuv GHSA:
verify_cert_ca proof of exploit` in `D:/worktrees/github/openziti/tlsuv/ghsa-verify-cert-ca` read `running` with
activity `thinking` (confirmed from `atrium_peers`) while its terminal showed the turn done at 15:45 and an empty
prompt. clint's read: a hook did not land. Typing something into the agent cleared it.

### The signal

Atrium owns the terminal. While Claude Code works, its spinner line redraws every second, so the pty produces
output. At an idle prompt it stops. So `running` plus a silent pty for 20 to 30 seconds is strong evidence the turn
ended and the Stop never arrived. Confirm from the last screen frame (an empty input box, no spinner line) so a long
silent Bash command is not mistaken for idle. Each runner draws differently: codex needs its own idle signature.

### What it does

- A new activity badge, not a status change: the stored status stays, per "status is a column, activity is a badge".
- Its own icon, distinct from running and from waiting on you: the spinner freezes into a hollow ring with a gap, in
  the warn colour. The spinner must stop, or the board claims work it cannot see. Tooltip: "no turn-end from the
  agent. its screen has been idle for Ns."
- It raises the waiting alert, worded as a guess: "looks idle (no turn-end received)".
- Any pty output or keystroke puts the real icon back. A late Stop settles the card as normal.
- Every firing is logged, since a missing Stop is a bug somewhere and the count says how often hooks drop.

### First

Find out why the existing silent-stop check behind the stuck alerts (`settings-spine.js`, the "stuck" list) did not
fire here. It may read hook timing only, not pty output. And find why this Stop was lost: check the room log for the
card around 15:45 on 2026-09-25 (missing, failed, or overwritten by a late fire-and-forget `/activity` post).

## 22. Copy on select copies every find match (bug)

Raised by clint 2026-09-25: with copy on select on, the terminal find bar (ctrl-shift-f) copies to the clipboard. It
should not.

Cause: the search addon highlights a match by SELECTING it. `termSearch.findNext` / `findPrevious`
(`js/terminal-links.js:490-491`, including the live re-search `onWriteParsed` schedules at `:440`) fire
`term.onSelectionChange`, and `js/terminal.js:816-818` copies any selection when `copyOnSelect` is set. Every
keystroke in the find box, and every output line while it is open, overwrites the clipboard.

Fix: copy on select answers only a selection the user made with the pointer. Either set a guard around the
find calls, or copy on the pointerup that ends a drag rather than on every selection change. Headless test: with
copy on select on, typing in the find bar and stepping matches leaves the clipboard untouched, and a drag still copies.

## 23. Keep idle Claude cards' prompt caches warm, and stop at break-even (feature)

Raised by clint 2026-09-27. A cache write after a gap of more than an hour cost $160 of $1,674 last week (9.6%),
and 37 of the 66 were on contexts of 200k or more. Design: `docs/cache-keepalive-design.md`, Mercurius session
`s_E1mI65LNulw1`, ready_to_build in round 2. Built on `claude/cache-keepalive`. Test plan section BL.

What it does: a forked headless resume of an idle card's conversation (`claude -p --resume --fork-session
--no-session-persistence`, every tool refused by a hook, one turn, no user or project settings) reads the cached
prefix shortly before it expires. The card's terminal and transcript are never touched. A card stops by itself
once its refreshes since it went idle cost an eighth of one full 1h rewrite, with a logged toast and a `❄ cold`
chip. Default on for new Claude cards, set in the gear. Each card has a switch in its menu.

Open:

- Deploy needs a room restart.
- In a hub's ALL view the gear's switch saves like the other room settings (sweep, prune). Check it lands on a
  board with more than one room.
- BL1, the manual fork probe, must be re-run after a Claude Code upgrade that changes sessions, settings sources,
  hooks or caching.
- Re-derive the 1/8 budget from the transcripts once more data is in: the resume hazard came from 597 idle
  stretches over 21 days, all Claude Code sessions on this machine, not only board cards.
- Codex is out: OpenAI caching has no write premium and no client TTL. A separate item if that changes.

## 24. The "not replayed here" notice is a button (feature)

Raised by clint 2026-09-28. An attach whose pre-restart history was cut to the newest 4 MB starts with a grey line
(`carryReplayNotice`, `internal/daemon/carryover.go:119`): "[atrium] ---- older output from before the restart is not
replayed here. all of it is under the terminal's cog, history from before the restart ----". Reaching that history
means knowing where the cog is and which entry it is.

Wanted: the notice is clickable in the terminal, the way file paths already are (`registerLinkProvider` in
`js/terminal-links.js:48`). Two actions, both on the one line:

- **open** the full pre-restart history in the same viewer the cog's "history from before the restart" entry opens.
  Small: a link provider that matches the notice text and calls that entry's handler.
- **load it in**: replay the whole file into this terminal above the live output. xterm cannot insert above its
  scrollback, so this means a reset and a re-attach that asks the daemon for the full carryover instead of the newest
  `carryReplayMax` (an attach parameter, for example `?carry=all`). It pays the cost `carryReplayMax` was added to
  avoid, about 1.4s for a 23 MB file, so say the size on the button and show the paste-style spinner while it lands.

Watch for:

- Match the notice by a marker the daemon controls, not by its English text, or a reworded notice breaks the link.
  An OSC 8 hyperlink with an `atrium:` scheme around the words is one way. The link provider must refuse that scheme
  anywhere else in the output, so a program cannot print a fake one.
- A new attach parameter is a new endpoint for a lent session (`overlay_guest.go` allowlist). A guest must not get
  `carry=all` unless it already gets the history.
- The popped-out window takes the same path.

**Built by sa81 on `claude/carry-notice-link`, 2026-09-28.** The notice is its own frame ahead of the replay, since
the screen model keeps no links. `open all of it` and `load all NMB in here` are OSC 8 `atrium:carry/...` links with
a per-socket nonce the board sends as `?link=`. The board drops any `atrium:` link without it at parse time. `load`
re-attaches with `?carry=all` under the paste spinner. A guest gets the old line, and `?carry=all` from a guest is
a 403. Test plan BO, headless section `carryLink`, Go tests in `internal/daemon/carry_notice_test.go`. The daemon
half needs a room restart. The board half is HUB-SIDE and safe alone.

## 25. A false STUCK alert, a stuck mark on the card, and a setting for it (bug)

Raised by clint 2026-09-28 with two screenshots (`.atrium/incoming/20260928-075641-pasted.png`, `-075738-`): a
desktop notification said `tlsuv GHSA: verify_cert_ca proof of exploit (red -> green) is STUCK: it stopped without
reporting`, while the card sat idle at an empty prompt and its row showed nothing wrong. Built on
`claude/stuck-indicator` by sa80. Test plan section BM.

Cause, from the event store. The card reported at 2026-09-25 20:40Z and its last turn ended at 21:53Z. On
2026-09-27 13:16Z the orchestrator typed `/model claude-opus-5-5` into seven cards within 1.3 seconds, as operator
messages with no sender. A built-in slash command runs no model turn, so no status change and no Stop followed, but
the `prompted` event stamped `prompted_at`. From then on the card owed a report (a prompt newer than the report),
and the watchdog's silent-stop check asked for nothing else: `needs-input`, owes a report, a `waiting_since`. The
room restart at 2026-09-28 07:50 resumed the card (done, then needs-input), which gave it a fresh `waiting_since`, so
the backoff started again from one minute and rang. Only this card of the seven was agent-launched. The other six
are human cards the watchdog does not watch.

Fix (ROOM-SIDE): a silent stop needs a turn that ended after the last prompt. A new `turn_end` table (migration
0062) stamps the moment a card goes from `running` or `needs-permission` to `needs-input`, seeded from the event log.
A slash command and a resume never write it. The turn's end is also the escalation's clock, so a restart does not
restart the backoff.

Board (HUB-SIDE): a stuck card wears a stopped-clock mark in the warn colour on the stack, the board and the terminal
strip, with a styled tooltip saying why and since when. It goes when the room clears the escalation, which is when
the card moves. The gear's `stuck agents` setting: alert me and mark the card (default), only mark the card, or off.
Stored with the other alert settings in `atrium.sound`.

Open:

- A card marked `dead` whose Stop hook still arrives goes `dead` to `needs-input` and records no turn end, because a
  resume takes the same transition. Rare. It then reads not stuck.
- The headless full run does not call `keepaliveSection` (sa78's). Only its HEADLESS_ONLY entry exists.

## 26. Toasts pop and disappear in the same second (bug)

Raised by clint 2026-09-27: toasts appear and are gone within a second, too fast to read. Also noted in passing under
item 14 on 2026-09-21, so this is not new.

`js/toasts.js` gives a toast 9s (30s for a permission), so something else removes it early. Find what. Candidates to
check first: the dedupe that replaces the last toast (`toasts.js` near `:297`), a cap on the stack, a view switch or
re-render that rebuilds `#toasts`, `landOnAlert` or a dialog close that clears it, the restart covers, and the new
top-right placement (`placeToasts`, item 18). Reproduce on the live board with the terminals view and the stack view.

Fix: a toast stays its full life unless the user dismisses it or clicks it. Hovering pauses the timer. Headless test:
raise a toast, then do each thing found above, and it is still on screen after 5s.

Done 2026-09-28 by sa83 (`c843139`). Three causes. `reapToasts` took down a keyed toast the poll after its card
stopped waiting, which a held message typed in at turn end does inside a second. The cap removed the oldest toast the
moment a fourth arrived. Nothing held a toast under the pointer. The view switch, dialogs and `placeToasts` were
checked and take nothing. An answered toast now says so and lives out its 9 seconds, a full stack queues until a
toast leaves, and hovering pauses the clock. Headless `toastLives`, test plan BX.

## 27. A say to a session that has gone waits forever, blaming the input line (bug)

Raised 2026-09-26. The orchestrator said something to sa69 after its runner had exited. The answer was `queued`, and
the card then wore `! 1` for over eleven hours with `held_for: line`, although the card was `done` with pid 0 and had no
input line to wait on.

Expected: a say to a card with no running session answers `undeliverable` with a note (resume it first), or is held
with `held_for: no session` and says so on the chip. A held message on a card that is deleted or finished does not
stay forever. Decide whether a held message expires or is dropped when the card's runner ends.

Done 2026-09-28 by sa83 (`04bad69`). The cause: the SessionEnd hook forgot the chip, but a runner that outlived
its session stayed in the supervisor. The next backoff retry found the gate shut and `noteHeld` set `held_for: line`
again, aged from the first hold. Chosen: `undeliverable`, with a note to resume first, and nothing queued. A held
message is dropped from the on-screen retry when the session ends or the runner exits. It stays queued for a resumed
session's hooks, because the sender was told `queued`. Gone means `done` or `dead` with no live pid, so a worker that
reported done and still runs is still reached. Also: Esc Esc on a Claude prompt now counts as clearing the line
(`d1c2454`). A lone Esc matched nothing in the keystroke count, so only control-c released held messages. The chip
tooltip reads as clint asked (`dc06056`). Test plan BY. ROOM-SIDE apart from the tooltip.

## 28. The full headless board run fails most of the time on `claude/main` (bug)

Raised 2026-09-27 by sa69. The full `scripts/test-board-headless.js` run failed on `claude/main` at `0305a19` in 2 of 3
runs, and on the paste-spinner branch in 4 of 5. The common failure is `page.waitForFunction: Timeout 30000ms` thrown in
the main flow, not a named section, so it cannot be run alone. Seen once each: "a cancelled countdown stayed on screen"
(`restartGate`, passes alone), "a terminated pinned terminal's right-click menu offered no dismiss action", "a fresh
history load did not go back to one page".

A check that fails most runs hides real failures. Find the wait that times out, name it, move main-flow checks into
named sections so each can run alone, and make the flaky waits wait on a condition rather than a clock.

Done 2026-09-28 by sa83 (`39c76dc`, `a74c95e`, `2d47379`). The wait was the skin-scope check's
`waitForFunction` for `sandstone`. A save raced the load's own settings reads, and a read answered first painted the
old skin back. A throw now names its line. `skinScope`, `skinHeal` and `history` are sections. 34 waits passed
`{timeout}` as the arg, so they took the 30s default. Board races fixed under the other flakes: history renders out
of order, a save in flight losing the skin, a terminal connected after it closed. The restart gate waits for every
stream to reopen, and settings-once counts its own page's reads.

## 29. Lean workers: a launched worker starts with only what it needs (feature)

Raised 2026-09-28 by clint. A worker started by `atrium_launch` inherits none of its launcher's conversation, yet it
booted with the operator's whole setup, and its first request was ~40k tokens. sa85 measured every lever in Claude
Code 2.1.283 and built a lean launch: `atrium_launch` now starts a claude worker with the user settings source
dropped, a filtered copy of the user settings (permissions, env and hooks kept), atrium-control and mercurius as its
only MCP servers, 22 tools disallowed, a short worker system prompt and auto-memory off. `mcp: [...]` adds servers,
and `lean: false` launches as before. The same `-p` probe drops from 35,970 to 11,029 tokens in a worktree.

See `docs/lean-workers-design.md` for the numbers and what a lean worker loses, and `docs/test-plan.md` section BP.
Left: the end-to-end check through `atrium_launch` after a room restart, and the ~5.9k of system tools that
dropping the user source adds for no reason found yet.

## 30. Peer review on the mercurius protocol, run in an atrium session (design)

Raised 2026-09-28 by clint, brief written by the mercurius `http-support` session and copied to
`docs/peer-review-brief.md`. A mercurius reviewer is structured, bounded, calibrated and logged per round, but it sees
only a snapshot and cannot read neighbours or run `go test`. An atrium session has tools and is watchable, but its
review output is free-form and unrecorded. The brief lists six mercurius pieces to adopt: the JSON output contract, the
finding budget, calibration from `mercurius.yaml`, the code-review prompt, per-round records with dispositions, and a
fresh read-only reviewer.

The recommended shape is option A: mercurius adds an `atrium` reviewer beside `codex`, `claude` and `pi`, and owns the
protocol. Atrium owns the runner. Atrium must provide a non-interactive launch with model, cwd, a read-only tool policy
and a prompt, a completion signal, the final output as raw text, and a session id to link. Option B, atrium
re-implements the protocol, duplicates it and drifts. Related: item 16, reviews that remember.

Open:

- Does the reviewer see a worktree pinned at a SHA, or the live tree?
- Does a failed schema validation get one repair turn in the session, or fail the round as mercurius does today?

The brief cites `prompt.BuildCodeReview` in mercurius `internal/prompt/prompt.go`, which exists only on the uncommitted
`http-support` branch as of 2026-09-28.

## 31. STUCK fires on a worker whose turn ended while it waits on background runs (bug)

Raised 2026-09-28 by clint, from a screenshot. sa83 ended its turn at 09:55 with five headless runs going in the
background, and the board marked it STUCK three minutes later: "it stopped without reporting". It was not stuck. It
reported progress when asked. A turn that ends with background work still running is waiting, not stopped. Find
whether atrium can see background tasks, from the Stop hook payload or the runner's process tree, and hold the alert
while they run.

## 32. A queued say from http-support never produced a backlog entry, and nothing can say why (bug)

Raised 2026-09-28 by clint. About 09:22 local, the mercurius `http-support` session wrote a brief and sent a say to
"the claude/main:atrium session (handle atrium)", asking for a backlog card and a reply with its id. It reported the
say as queued, because the target was mid-tool-call. No card was filed and no reply went back. Item 30 was filed by
hand later, after clint pasted the sender's own summary into the atrium session.

What is known:

- The atrium session's handle today is `atrium-87300`, not `atrium`. Whether `atrium` resolved to this card, to another
  card, or to nothing is not recorded anywhere the receiver can read.
- The atrium session ran `/clear` between the send and clint's question. If the say arrived before the clear, the
  model read it and the clear erased it, which is a lost message from the operator's point of view.
- `atrium_task` events on the receiving card reach back only a few minutes, so they cannot show whether the say was
  delivered or when.

What is wanted:

- A say's lifecycle on record: sent, the handle it resolved to, queued, delivered, and the channel (terminal, hook, end
  of turn). Both the sender and the receiver can look it up afterwards.
- A handle that no longer matches exactly answers with the candidates, as `atrium tell` already does, instead of
  queuing to a guess.
- A request that asks for a reply shows as owed on the receiving card until it is answered, so a `/clear` or a compact
  cannot drop it without a mark.

## 33. Watching a terminal holds every message to it, and nothing shows why (bug)

Raised 2026-09-28 by clint, from a screenshot. Two says to saorch sat queued for minutes behind "delivers when your
input line is clear and idle". Its input line was empty. clint had only clicked into the terminal.

The cause is in `runner.noteOperatorTyped` (`internal/daemon/supervisor.go`). It counts every byte the attach socket
carries as operator typing. Claude Code turns on focus reporting, so focusing or leaving the terminal makes xterm.js
send `ESC [ I` or `ESC [ O`. The counter skips `ESC` and counts `[` and `I` as two typed characters. Only Enter,
control-c or control-u zero it. Mouse reports and terminal query replies very likely count the same way.

What is wanted:

- Only keystrokes count. Sequences the terminal sends on its own account (focus, mouse, device and cursor reports)
  count nothing.
- A word delete is a word delete. Ctrl+Backspace sends `0x08` and is counted as one character, so deleting a word
  leaves the line reading as part written. Keep the line's text rather than a count, and apply the word rules for
  Ctrl+Backspace, Alt+Backspace and Ctrl+W. Arrow keys, history recall and tab completion can still make the model
  drift. The readout below is how that drift gets seen.
- A debug readout in the terminal view, behind a toggle, on the line above "ctrl-c copies a selection, interrupts
  otherwise ...". It shows what atrium thinks is in the line, the count, time since the last keystroke, and whether the
  gate is open or closed and why. It is for clint and an agent debugging together.

An earlier report very likely has the same cause. sa85's turn ended at 09:45
and it sat idle at its prompt. Three messages were due to it: one `when: done` from about 09:10, and two immediate
ones from about 09:46 and 09:50, both answered `queued, not typed yet`. None was typed until clint typed `u waiting?`
about 09:55. Then all three went in mid-turn and sat in Claude Code's own queue, so sa85 did nothing for about 9
minutes. clint typing and pressing Enter zeroes the count, which fits. The room log for card
`01a0e80e-b80b-7b81-874b-0d1dde930bba` between 09:45 and 09:56 can confirm it. Also check that a `when: done`
message is delivered at the turn end it waited for.

## 34. Every MCP tool call skips atrium's permission gate (bug)

Raised 2026-09-28. The dotfiles `atrium-perm-hook.ps1` exits early for every `mcp__*` tool, which was there so Mode A's
own `submit` was not gated. Mode A is gone, so `atrium_exit` and `atrium_say` fell to Claude Code's own prompt, or to
its auto-mode classifier, which refused both. The dotfiles session is removing the skip. The fix lives in dotfiles,
not here. This entry is so the reason is findable.

## 35. A card has a name you mention it by, like `@dotfiles` (feature)

Raised 2026-09-28 by clint: "a 'how this llm is referenced' type of field on each of the atrium owned sessions so that
i can mention @dotfiles in the same sort of way i would @ mention a custom agent".

Handles today are made up by the board: `dotfiles-41800`, `sa84-merger-owns-claude-main-workers-rep`. Nobody types
those. What is wanted:

- A short alias on each card, chosen by the operator and shown on the card. `atrium_say`, `atrium peers` and
  `atrium tell` accept it wherever they accept a handle.
- A worker's alias defaults from its title prefix (`sa89`), and a resident's from what it was named (`saorch`).
- An alias is unique among live cards. Taking one that is in use is refused and names the holder.
- Mentioning `@alias` in a prompt to a session is enough for that session to address the card. Whether the board also
  routes a typed `@alias ...` line on its own is an open question.

Item 32's handle mismatch (`atrium` versus `atrium-87300`) is the same gap seen from the sending side.

## 36. A finished worker stays up until somebody closes it (bug)

Raised 2026-09-28 by clint: "once a job is done the worker should be culled. plain and simple". sa82, sa85, sa86 and
sa88 sat idle at needs-input after their branches were merged, and filled the launch cap of 10 so a new worker could
not start. When the merger lands a worker's branch and the work is accepted, the worker is asked to leave and its
worktree and branch are removed.

Status: built by sa36 as the `atrium_cull <card>` control tool, merged, not deployed. Test plan CL.

Why a tool and not a trigger on the merge. A merge is not an acceptance: a branch can land and still be sent back, and
the worker is the cheapest place to fix it while its conversation is warm. So calling `atrium_cull` is the acceptance,
made by the orchestrator or the merger acting for it, and a worker cannot cull itself. The room adds what it can check
on its own and refuses the whole cull otherwise: the card is tagged `atrium:subagent`, and its branch is merged into
`claude/main` (or `into`). It then asks the runner to leave and waits for it, removes the worktree only when git
reports it clean apart from atrium's own `BRIEF.md` (`git worktree remove`, never `--force`), and deletes the branch
only once the worktree is gone and it still reads as merged. The main checkout, main, master and the target branch are
never touched. A worktree with uncommitted changes is kept with its branch, the worker still leaves, and the answer
says why. `internal/daemon/cull.go` has the whole reasoning.

## 37. Token and context use on record for every session, shown only in a card's details (feature)

Raised 2026-09-28 by clint: "i definitely want to keep track of claude sessions and token use and context use and
all that ... don't show me unless i click on the details of the card but i want atrium to be able to track it for
history's sake".

clint runs about 21 Claude cards holding 2 to 4 million tokens of context between them, and a restart, a resume or a
keep-alive round can spend a lot without anything saying so. What is wanted:

- Per session, over time: input, output, cache write (5m and 1h), cache read, and context size, from the runner's
  own transcript usage records. `keepalive.go` already reads these for the cache TTL. Reuse that reader.
- Every spend is attributed to what caused it where atrium can tell: the operator, a say, a restart wake, a keep-alive
  refresh, a resume. That attribution is what makes items 38 and 39 decidable.
- Kept with the card's history, so it outlives the card and the daemon.
- Shown ONLY in the card's details. Nothing on the card face, the list or a toast. sa87 (context size on every card)
  conflicts with this and has to be reconciled.
- The first use: measure one room restart, cache writes per card before and after, to learn whether a resume misses
  the cache.

Status: built by sa90 and merged. Test plan BT.

Subagents, 2026-09-28 (sa94). clint: "calude subagent - yes. atrium subagent no (as it's a separate thing)" and "as
long as it doesn't skew/double count". What a card's Claude Code subagents (the Task tool) spend is a row of its own,
cause `subagent`, written at the same Stop as the turn's row. The room reads `<session>/subagents/agent-*.jsonl`
beside the transcript (a workflow's agents a level down), which is what Claude Code writes on this machine today, and
the `isSidechain` lines older Claude Code wrote into the main transcript. Each file has its own cursor, replies are
kept one per message id, and a reply the turn's row holds is never also a subagent's. An atrium-launched worker is
its own card with its own rows and is not counted into its launcher. Needs a room restart. Test plan BT5.

Every current Claude model is priced on a usage row: Haiku 4.5 and Sonnet 5 are in `usageOnlyPrices`, from the
pricing page on 2026-09-28, and not in keep-alive's table, which is also the list of models keep-alive may refresh.
The details' `turns` counts only the card's own turns, and each cause has its own line, so subagent requests and
keep-alive refreshes are read apart.

The one known undercount, not fixed. A cursor skips a reply stamped at or before the last reply it already counted.
So a reply is lost when its line reaches the file AFTER a read that counted a later-stamped reply through the same
cursor. That read happens 1.5 seconds after a Stop and takes every reply stamped before the Stop, so the lost line
has to be stamped before the Stop and still be off disk 1.5 seconds after it. It can happen in two places:

- The inline layout (older Claude Code). Every inline subagent in the main transcript shares one cursor, so two
  subagents running at once can interleave: A's line stamped at t1 lands after the read that counted B's line at
  t2, later than t1.
- The first read after a daemon restart. Every subagent file starts from the one time of the last `subagent` row, so
  a line in file A stamped before the newest reply counted from file B, and not on disk when that row was written,
  is skipped.

A file of the newer layout is one subagent's conversation, written in order, so its own cursor cannot skip a line.
Nothing is ever counted twice this way. The miss only undercounts. A fix would be a cursor per inline `agentId` and a
last-counted time per file on record, kept for when a miss is seen.

Retention, later. clint: "let it grow forever for now but let's plan some way to clean it eventually". The rows are
kept forever for now. A row is a few hundred bytes, so 21 cards at a few hundred turns a day is on the order of a
megabyte a month. Options for later, none built:

- Roll rows older than N days into one row per card, day and cause, with the sums kept and the per-turn detail
  dropped. The totals and the by-cause split in the details stay right.
- Delete a card's rows when the card sweep removes the card, or some weeks after, for cards nobody reopens.
- A size cap: past N rows, or N megabytes, roll up or delete the oldest first.

## 38. A restart resumes only the cards that were working (feature)

Raised 2026-09-28. A room restart resumes every supervised card. Cards that were mid-turn or have queued prompts need
that. An idle card could stay parked until the operator attaches or types. Decide after item 37 shows what a resume
costs.

## 39. Keep-alive warms the cards you mark, not every idle card (feature)

Raised 2026-09-28. Keep-alive (item 23) refreshes every idle Claude card on the 1-hour cache, 5 minutes before
expiry, until break-even. With about 21 cards that is about 21 full-context cache reads an hour, including cards
nobody returns to. Decide after item 37 shows what keep-alive spends.

## 40. The launch cap counts only `atrium:subagent` cards (bug)

Raised 2026-09-28 by clint: "it should be only atrium:subagent". `runningForCap` in `internal/link/control_mcp.go`
counts every running supervised card carrying the `origin:agent` tag, which every `atrium_launch` stamps. So one cap
of 10 covered every orchestrator's launches and the resident merger together, and a launch was refused while only
seven workers were up. The cap counts running cards tagged `atrium:subagent` and nothing else. `origin:agent` stays
as the doer signal it already is.

Also wanted, from an earlier report of the same refusal: the refusal lists what
it counted, with title, launcher and status, so the caller and clint can see what to free. On 2026-09-28 saorch had
`origin:agent` removed from its tags as a workaround, which also stops its silent-stop notices (item 41), because
`agentLaunched` reads that tag.

## 41. A resident session owes its launcher a report for every prompt, from anybody (bug)

Raised 2026-09-28. saorch (sa84) is a resident merger that the orchestrator launched. sa81 sent it a report, and
the orchestrator sent it a copy. Each is a prompt, saorch ended both turns without saying anything to the
orchestrator, and the orchestrator got two `ended its turn without reporting` notices seven seconds apart. Each
notice is a full orchestrator turn over a large context, and neither said anything the orchestrator needed.

The notice is keyed on `PromptKey()` in `silentStop` (`internal/daemon/a2a.go`), so every prompt from any sender
makes a launched card owe a report. That fits a one-shot worker and does not fit a resident session whose prompts
come from its own workers.

The same happens to a worker that waits on its own background job. At 09:34 sa83 ran five headless runs under a
monitor. Each monitor event woke it for a turn that ended "still waiting", and the orchestrator got a notice for
each one, 23 seconds apart.

Expected: a card owes its launcher a report only for a prompt the launcher sent (its first prompt, or an
`atrium_say` from the launcher). A message from any other session, or a turn the session's own background task or
monitor woke, does not make it owe one. Test plan AB2 grows a case: a message from a third session, then a silent
stop, gives the launcher no notice.

## 42. A running card wears the `!` chip for a message held until its turn ends (bug)

Raised by clint 2026-09-28 with a screenshot (`.atrium/incoming/20260928-092222-pasted.png`). sa83 was running,
with its spinner on the card and Claude working in the terminal, and the card showed the warn-coloured `!` chip.
The tooltip: "message from atrium-87300 waiting 37m - waits for the session's turn to end, because it was sent to
arrive when the turn is done". The chip is accurate and reads as "this needs you", which is wrong: nothing on that
card needs clint, and the card is working.

Expected: the `!` is reserved for something that needs the human. A message held for a running card's turn end
shows as a quiet queued mark in the card's own colour, not the warn colour, and the tooltip keeps its wording.
Decide whether a `when: done` message held longer than some bound falls back to the next tool call.

**Status, 2026-09-28, sa42: built on `claude/held-chip-intent`, merged.** Test plan CC. A hold is quiet when every
held message waits for the turn, as its sender asked or because the runner takes no input mid-turn. It wears `✉` or
`✉ N` in the neutral chip, with no pulse and no alert, and the tip names the one rule that holds it. The `!` stays for
the line, a dialog, an immediate message queued behind a done one, and a turn wait past an hour. The room decides
which (`held_quiet`, `held_turn` in `noteHeld`), and the board only draws it. The bound does not change delivery: a
done message past it still waits for the turn and does not fall back to the next tool call.

## 43. A worker's finished turn shows "nobody has looked" to clint, although its launcher read the report (bug)

Raised by clint 2026-09-28 with a screenshot (`.atrium/incoming/20260928-092324-pasted.png`). sa82's card shows the
unseen dot: "this session's last turn ended and nobody has looked at it since". sa82 is an agent-launched worker
(`origin:agent`), and its report went to its launcher. Nobody human needs to look, so the dot asks clint for
attention the work does not need.

Expected: on a card launched by an agent, a turn that ends with a report or a message to the launcher counts as
seen. A turn that ends silently still shows the dot, alongside the stuck mark from item 25.

### Design, 2026-09-28 (@ui)

The fix is in the room, not the board. The dot is `seen.unseen`, worked out in `internal/store/seen.go` from
`turn_seen.turn_ended_at` against `seen_at`. The board only draws it (`js/seen.js`). Hiding it on the board would
leave `atrium_task` and every other reader of `unseen` still saying nobody looked.

- **Where.** The Stop path in `internal/daemon/messages.go` that lets a turn end already calls `silentStop` and then
  `noteTurnForSeen`. A card that is `agentLaunched` and does NOT owe a report (`!t.OwesReport()`, so `reported_at`
  is at or after `prompted_at`) is marked seen with a new via, `SeenLauncher` (`"launcher"`). NOT as a second step
  after `noteTurnForSeen`: that function stores `d.unseen` and publishes the card, so the board would see the dot
  for a moment on exactly the turns this hides, and could notify on it. Instead `noteTurnForSeen` takes an
  auto-seen via, records the turn end, marks it seen, leaves `d.unseen` clear, and publishes ONCE at the end.
  (Mercurius round 1, concern C1.)
- **What counts as reported.** Exactly what already sets `reported_at`: `peerSaid` (an `atrium_report` or an
  `atrium_say` to the launcher) and the relay's cross-room equivalent. A notice atrium wrote about the worker
  (`notifyLauncher`) is not a report and does not count, the same rule `silentStop` uses. So a silent stop still wears
  the dot, and the two marks can never disagree: a card is either silent (dot, and its launcher is told) or it
  reported (no dot).
- **Questions are not answered.** `MarkSeen` touches only `seen_at`, never `answered_at`. A worker whose last turn
  asked clint Open Questions keeps its `? N` chip. The launcher reading the report is not an answer from clint.
- **A report to a launcher that is gone.** `reported_at` is set when the sender spoke, not when the launcher read it,
  and the launcher's card may have exited. Counted as seen anyway: the worker did its part, and the launcher going
  away is the launcher's card's problem, shown on that card. Open for the review.
- **The board.** The dot's tooltip is unchanged, since it is no longer shown in this case. The details' seen line
  (if it shows `seen_via`) reads `launcher` as "its launcher got the report".
- **Tests.** `internal/daemon/seen_test.go`: an agent-launched card that reports then stops is not unseen, via is
  `launcher`. One that stops without reporting is unseen and the silent stop notice goes. A human-launched card that
  stops is unseen whatever it said. Questions stay open after a launcher-seen turn. A reported worker's Stop
  publishes the card once, and never with `unseen` true.

## 44. A gear checkbox: no notifications from agent-launched cards, on by default (feature)

Raised by clint 2026-09-28: "I don't need notifications from them." A worker an agent launched reports to its
launcher, so its turn ends, waits and stuck alerts reach clint as noise. The workers' own launcher already hears
through `notifyLauncher`.

Expected: a checkbox under `notifications` in the gear, "don't notify me about cards an agent launched", ticked by
default. Ticked, a card with the `origin:agent` tag raises no toast, no desktop notification and no sound. Its
marks on the card stay, and so does the toast log entry, so nothing is lost. A permission request from such a card
still notifies, because it blocks until a human answers. A card's own notification override beats the checkbox.

## 45. Every card shows its context size, and a launcher hears once past a threshold (feature, sa87)

Raised by clint 2026-09-28. sa87 built it on `claude/context-size` (ee68bc8),
not merged. Workers grow to 200k and 300k tokens of context, and every turn past that re-reads all of it.

As built: every Claude card shows its context size on the card and in the terminal header, in the warn colour past a
gear threshold (default 150k), read from the transcript like activity. An agent-launched card's launcher gets one
notice through `notifyLauncher` the first time it crosses the threshold.

CONFLICT with item 37: later the same day clint said usage is shown "only in a card's details". The launcher notice
fits. The number on the card face does not. sa90 reports what should change when both land.

**Status, 2026-09-28, sa87: built on `claude/context-size` to clint's decision, not merged.** The number is off the
card face, the stack, the terminal bar and the terminals list. A card past the gear threshold wears a small warn
mark, no number. The gear threshold (`context_threshold_k`, default 150) and the one notice per crossing to an
agent-launched card's launcher stay: keyed on the session id and stored, so a restart does not send it again, and
re-armed when the card is seen back under the line. The number is in a compact details view, one body in
`js/peek.js`, reached three ways: two seconds on a card or stack row, `details` on the card menu, and a `details`
expando on the terminal's shortcut strip that slides a drawer up. It reads item 37's `GET /v1/tasks/{id}/usage`
when it opens and never otherwise. See `docs/test-plan.md` section BZ.

## 46. Provision a machine as a room over ssh, from one command and later from the board (feature)

Raised 2026-09-28 by clint, replacing `claude/sgg-provision` (77241f0, a `start-claude-sgg.ps1` that ssh'd to sgg and
ran `atrium2.exe room`): "i want it to be made 1000% fucking generic ... slick, simple, easy. optionally even doable
FROM ATRIUM ITSELF.... that'd be my ideal situation... i provide claude (or whatever agent) ssh access and atrium
provisions the agent."

Stage 1, sa92: one generic script. Given `user@host` it detects the OS and puts the matching atrium binary on the
machine without admin, using the no-admin paths in `docs/packaging.md`. It installs autostart, joins that room to
this hub, and checks the runners it asked for (claude first, then codex and others) are present and can start. It
can be run again, prints one line per step, and has an `-Remove`. It is proven on claudevm first, then on clint's
other machine by clint.

Stage 2, later: the same thing from the board. An "add a machine" dialog takes an ssh target, and atrium runs
stage 1 and shows each step. Credentials follow the overlays rule: atrium names the ssh command, it never holds the
key.

**Status, 2026-09-28, sa92: stage 1 built and proven on Windows, Linux and macOS.** `scripts/provision-room.ps1
user@host` does the whole of stage 1. See `docs/packaging.md` "Provisioning a room over ssh, from the hub" and
`docs/test-plan.md` section BU. For stage 2 the step lines are `provision <step> <status> <detail>` and the exit
codes are listed at the top of the script.

clint's answers, and what the script does with them:

- **Ziti and zrok hubs are joined.** Ziti: the operator gives the remote's enrollment JWT (`-ZitiJwt` or
  `-ZitiJwtCommand`). It travels as a file and is enrolled on the remote with a key made there, in process when the
  remote has no `ziti` CLI. zrok: the running hub writes its share to `zrok-share` in its key folder so `atrium rooms
  token` can mint a zrok join string. The remote's own `zrok2 enable` takes the account token, so the script names
  that command and stops with exit 7.
- **Runners are report only by default.** `-Install claude,codex` fetches from the vendor after a trust warning, puts
  `~/.local/bin` on the user's PATH if needed, and `-Remove` takes both back out.
- **Linger is off by default** in `scripts/atrium-service.sh`. `ATRIUM_LINGER=1` or `-Linger` turns it on.
- **The binary is the GitHub release by default**, checked against its `checksums.txt`. There are no releases yet, so
  that fails with the reason. `-FromCheckout` is the dev path and is what every proof so far used.
- **One room per machine.** A machine that is already a room, or runs an atrium this did not install, is refused
  with exit 6.
- **No autostart by default.** The room starts with the new `atrium room --detach` and runs until restart or
  logout. `-Autostart` keeps the service path.

Still open under this item:

- **Autostart as the default.** Decide when it comes back on, and whether a Windows logon task or a detached room is
  the better default there, given the detached room survives the ssh session.
- **systemd PATH.** The user unit does not get the login shell's PATH, so a runner in `~/.local/bin` can pass the
  check and still not be found by a room run by systemd. A detached room is started through a login shell and does
  not have this. Fix in `packaging/atrium.service` with `Environment=PATH` or a login-shell `ExecStart`.
- **The board half of stage 2.** A room with no runner shows on the hub's board as "this room is here but has no
  agents, configure one?", offering the `-Install` above. Not built: it needs the hub to know a room's runners and to
  run this script over ssh, which is the stage 2 dialog itself.

## 47. A resident session's alias defaults from its name (feature)

Raised 2026-09-28 by clint. Item 35 gives a card an alias by default only from a title prefix that holds a digit,
so a worker titled `sa89: ...` wears `@sa89` and a resident session such as saorch wears none until one is set by
hand.

Expected: a resident session's alias defaults from the name it was given, so saorch wears `@saorch`. The same
uniqueness rule as item 35 holds: a default that clashes with a live card's alias is not taken, and the card says
why.

**Widened 2026-09-28 by clint, HIGH:** "i REALLY need to be able to get the alias and set the alias and it needs to
show up on the terminal title bar in place of the current `github/dovholuknf/atrium:claude/main` stuff (far left)
and it needs to be clear that it's an alias / handle."

- Read and set a card's alias from the board (card menu and the terminal title bar), and from an agent (the
  atrium-control tools).
- The terminal title bar's far-left label shows the alias in place of the repo and branch path, marked so it reads
  as a handle (for example `@saorch`). The repo and branch stay reachable, but not in that slot.
- A card with no alias keeps today's label.

## 48. `atrium_launch` takes a model and a thinking effort (feature)

Raised 2026-09-28 by clint. Every launch runs at the runner's default model and effort (medium today). A cheap agent,
an interviewer for example, costs as much per turn as a full worker.

Expected: `atrium_launch` takes an optional model and an optional thinking effort (`low`, `medium` or `high`), and
the runner starts with them. Left out, the runner's defaults hold, as today. The card shows the model and effort it
runs with.

**Status, 2026-09-28, sa48: built on `claude/launch-model-effort`, merged.** Widened by clint to a pass-through:
`args` and `env` go to any runner as given, and `model` and `effort` are mapped per runner row (`effort_args`,
`model_env`, `effort_env` beside `model_args`). Nothing holds a list of models or levels. Migration 0065. See
`docs/launch-options-design.md` and `docs/test-plan.md` section CA. Proven: `claude --model
claude-haiku-4-5-20251001 --effort low` runs on Haiku 4.5 (58 thinking tokens against 222 at the default). Claude
warns about and ignores an effort level it does not know rather than refusing it.

## 49. The orchestrator can appear on every room (design)

Raised 2026-09-28 by clint. A card belongs to one room today, so the orchestrator session is reachable and visible
only from its own room's view. clint wants to be able to put it on every room: seen in each room's view, and
addressable from each. Ideate first: what "on every room" means for a card that runs in one place, and what each
room's view shows of it.

**Status, 2026-09-28: deep backlog, not started.** Moved there by clint.

## 50. Views of agents, beyond groups (design)

Raised 2026-09-28 by clint: "i'm starting to want different 'views' of agents more than just groups". Ideate
first. Examples to explore: saved filters, a view by role, a view by launcher, and workers apart from clint's own
cards.

## 51. Five kept worktrees show 48 commits not matched on `claude/main` (housekeeping)

Raised 2026-09-28. Five kept worktrees, each on the 09-22 base `02fe769`, show 48 commits that `git cherry` does not
match on `claude/main`: sa06 reconcile, sa07 board-fixes-2, sa10 launch-garble, sa11 notif-tray and sa12
terminal-suite. Compare each with `claude/main` and report what, if anything, is missing.

**Status, 2026-09-28: DONE.** sa51 found all five safe, and they were deleted 2026-09-28. Its review of their old
HANDOFF files raised items 52 to 54. Copies of those files are in `D:/tmp/handoffs`.

## 52. A pinned strip with cards from two rooms orders only one room (bug)

Raised 2026-09-28 from sa51's review of the old HANDOFF files. `POST /v1/tasks/pin-order` is one call and names no
room, so a pinned strip that holds cards from two rooms saves the order of only one of them.

Also, unverified and a design question for clint: in any sort other than manual, the board has no way to reorder
pins.

## 53. `setViewport` and `dropViewport` compute under `r.mu` and apply outside it (bug)

Raised 2026-09-28 from sa51's review of the terminal-suite HANDOFF. Both work out the new viewport while holding
`r.mu` and apply it after letting go, so two resizes close together could apply in the wrong order. Never
reproduced. See `D:/tmp/handoffs/terminal-suite-HANDOFF.md`.

## 54. Terminal test suite part 2: `screen.go` against xterm.js (feature)

Raised 2026-09-28 from sa51's review of the terminal-suite HANDOFF. Part 2 was planned and never started: a
differential test that feeds the same trace fixtures to `screen.go` and to xterm.js and compares the screens, run
with Playwright after an `npm install`. The plan is in `D:/tmp/handoffs/terminal-suite-HANDOFF.md`, and the scratch
tools are in `D:/tmp/handoffs/terminal-suite-tw`.

## 55. Launched runners inherit ATRIUM_DEBUG_INPUTLAG from the room (bug)

Raised 2026-09-28. The live scripts `start-atrium-room.ps1`, `start-atrium-hub.ps1` and `deploy-batch.ps1` set
`ATRIUM_DEBUG_INPUTLAG=1` unless `-NoLagLog`, which is meant for atrium's own logging. Every runner the room launches
inherits it, so a worker's `go test` fails `internal/link` TestLagConnTimesNothingWhenOff, and any atrium binary a
worker runs logs lag too. Also measure what the logging costs per keystroke, because clint says input has been slow
lately.

Done by sa55. `inheritedTaint` drops every `ATRIUM_DEBUG_` variable. A launch, a restart, a keep-alive
fork, a source and a recogniser all build their environment through `childEnvFrom`, which applies it, and `shellEnv` drops them from a card's shell. The whole prefix, because
each switch under it is a readout for the process it was set on. A runner's own `environment` field still passes
one on. ROOM-SIDE only (`internal/daemon`). Nothing on the hub changes. See `docs/test-plan.md` section CB.

What the logging costs, from `BenchmarkRoomKeystroke*` and `BenchmarkHubKeystroke*` on the i9-13900H while it was
busy: a keystroke and its echo cost the room about 64ns with the logging off and about 165ns with it on, and the hub
about 15ns off and 53ns on. A logged line is about 0.8us and 248 bytes. Nothing is formatted under the threshold.
On 2026-09-28 from 11:19 to 15:05 the room logged 354 lag lines (about 1.6 a minute, at most 30 in one minute,
72KB). In every slow echo the room logged, atrium's share was at most 1.1ms and the rest was on the runner's side.
The hub logs about 80 lines an hour even when idle. The room pings an idle attach every 45s, the browser's pong
going up starts the hub's `echo` clock, and nothing comes back until the next ping, so each one logs a 45000ms
echo. Any other frame sent up that gets no reply would do the same. That is noise, not lag.

## 56. Every dialog is sleek, one skinned design, starting with card details and the room's edit-agents screen (feature, design first, HIGH)

Raised 2026-09-28 by clint: "i need a backlog to make all dialogs fucking sexy. there is __no__ design given to the
card details screen, nor to the rooms add agent screen (edit agents) etc. the screens are --fucking gross--... they
need an overhaul visually to not suck. to be sleek and fucking sexy".

Scope:

- Inventory every dialog, modal and settings panel on the board: card details, rooms > add and edit agents,
  launch, the gear, permissions, rules, intake, and the rest.
- One design language across all of them, built on the skins and consistent in every skin: spacing, type scale,
  headers, field rows, buttons and empty states.
- The look clint liked is the reference: sa72's restart and blocking modal family (test plan MD, `9d3f676`) and
  sa87's compact details popup (item 45).

Deliverable 1 is a design with before and after mockups, for clint to approve. Build only after that.

## 57. The live room leaks memory (bug, HIGH)

Raised 2026-09-28 by clint, from sa55's measurements while it worked item 55. The live room (up since 11:19)
held about 15.9GB of private memory and a 5.7GB working set, and grew in bursts: 1.2GB in 2 minutes, then flat. The
hub was 76MB. It is also the likelier cause of slow input than the lag log is: the machine sat at 52% CPU with 22
claude processes.

The room has no profiling endpoint, so nothing could say what holds the memory. First add a pprof endpoint to the
room, on the loopback human listener only. Then take heap profiles across a growth burst and find the cause.

After the room-only deploy of `06b87c9` at 15:55:54 on 2026-09-28, the new room (pid 48064) held 7.0GB private
memory at +6 minutes and 13.2GB at +7 minutes. It then stayed flat at 13.6GB private and a 3.3GB working set for a
minute. So most of it is allocated at startup, probably while reopening the saved cards and their scrollback, and
it is not a slow leak. The hub reported rooms=3 and the room answered ok, not halted.

**Status 2026-09-28: DONE by sa57 on `claude/room-memory`, merged, needs a room restart.** The cause was the
scrollback ring. `newRingSized` did `make([]byte, scrollback_mb)` for every runner and shell at spawn, and the live
room has `scrollback_mb` at 512. So 26 cards reopening came to 13GB committed, for about 190MB of scrollback on disk.
Private memory counts the commit and the working set counts only the pages written, which is why the two differed by
10GB. A throwaway room on the old build, eight idle cards at 64MB, held 571MB private, and its heap profile put 512MB
of 517MB in `newRingSized` for rings holding 303 bytes each. The ring now grows as output arrives and wraps at the
setting. The same room reads 58MB. The room also serves `/debug/pprof/` on its loopback `--http` listener only. See
`docs/test-plan.md` CK.

## 58. `atrium_say` reaches cards on other rooms, `name@room` (feature, HIGH)

Raised 2026-09-28 by clint: "atrium say needs to be cross room for sure". Today `atrium_say` refuses `m1mini~<id>`
and the other room's handles. Only the hub's `/v1/tasks/<id>/message` reaches a card on another room.

Addressing: `name@room` reaches a card on another room, and a bare `@name` (or `name`) stays in the sender's own
room. Aliases work the same way, so this lines up with item 35's `@alias`.

The reply path has to work both ways. A card on m1mini must be able to reach `atrium-87300@claude-sg4`. This is
known to be needed: the hub's `/v1/tasks/m1mini~<id>/message` typed the orchestrator's question into an m1mini card,
and the card answered, but the answer could not come back.

**Status, 2026-09-28, sa58: built on `claude/cross-room-say`, merged, not deployed.** Design in
`docs/cross-room-say-design.md`, reviewed by mercurius (`s_H1ILoNvxloBH`, ready_to_build). Test plan CE. Migration 0066. Needs a hub restart and a
restart of every room, and atrium-control on m1mini (the provisioning script now registers it) before an m1mini card
can answer. Not carried across rooms: the ledger's `ended` notice, and a remote launcher's verdict on a worker's work.

## 59. Spike on m1mini: more than one room per machine, and a blocked room that drains (design, spike)

Raised 2026-09-28 by clint. Run it on m1mini. Two ideas:

- More than one room on one machine.
- Marking a room blocked. A blocked room takes no new work and drains, while a fresh room instance on the same
  machine picks up new work.

The goal is to move work between rooms, so that a room restart kills nothing.

**Status, 2026-09-28: deep backlog, not started.** clint: "seems dumb. deep backlog".

## 60. The stdio control MCP has sa48's launch fields but no "room is older" warning (housekeeping)

Raised 2026-09-28. The old stdio control MCP (`internal/cli/control_peers.go`) took sa48's model, effort, args and
env fields for item 48, but not the warning the hub's control MCP gives when the room is older than the change.
Decide whether it needs the warning or should go away.

## 61. A fake 45s hub echo in the lag log from the idle ping and pong (bug)

Raised 2026-09-28, from sa55's review of the live logs. The room pings an idle attach every 45s, and the browser's
pong going up starts the hub's echo clock. So every ping logs a fake echo of about 45000ms, and `hub.err` carries
about 80 lag lines an hour with the board idle. The hub should not start the echo clock on a pong.

## 62. A worker that ends its turn without a report reaches its orchestrator every time (bug, HIGH)

Raised 2026-09-28 by clint: "we can't have missing messages". A worker that ends its turn without an
`atrium_report` must reach its orchestrator every time, at once.

Evidence from 2026-09-28. sa48 ended its turn at 15:03 without a report, and the "ended its turn without reporting"
notice reached the orchestrator. sa42 ended at 19:31:31Z, at needs-input with no report, and no notice reached the
orchestrator. clint saw it first.

sa42's report did not come before its turn ended. Its card shows the turn end at 19:31:31Z with no report, a
prompt (the orchestrator's nudge) at 19:32:33Z, and its report to the merger, saorch, sent about 19:33:27Z. So this is a
missed notice, and the report came only because of the nudge. A separate gap sits beside it: a worker that reports
to the merger instead of its launcher leaves the orchestrator with no sign that it reported.

Find out why one fired and the other did not. Candidates: a delay threshold, the launcher link on the card, or the
card's origin tag (`agentLaunched` reads `origin:agent`, see item 40). Then make the notice certain.

The same happened to sa58 at 17:12:34 on 2026-09-28: it ended its turn with no report, the same pattern as sa42.

The context-size notice (item 45) misses too. It is claimed once per CARD. sa58 got it at 151k, was cleared and
resumed on the same card, and reached 336k with no second notice until about 17:1x. The claim has to re-arm when
the card's session or resume id changes, or when its context drops below the line.

Across rooms (item 58, per sa58): the ledger's `ended` notice, a remote launcher's verdict on a worker's work, and
the ledger's say entry are not carried from one room to another yet. A worker on one room with its launcher on
another can end silently with nothing reaching the launcher.

Related: item 36 (a finished worker stays up), and the work ledger, `docs/work-ledger-design.md` and
`docs/work-ledger-plan.md`.

**Status, 2026-09-28, sa62: fixed on `claude/missed-notices`, merged, not deployed.** Read off the live cards. sa42 had no
`prompted` event before the nudge: the opening prompt goes on the command line and nothing recorded it, so
`prompted_at` was empty, the worker owed no report, and its first turn could never be a silent stop. The launch now
records it, and a session starting no longer writes a turn end, so the worker is not stuck before it begins. sa58's
silent stop did fire (21:12:34, the nudge came 11 seconds later). Its context notice is keyed on the session, but
after the `/clear` the resume id stayed on the old session until the new one's first Stop, 51 minutes later, so the
watcher kept reading the old transcript. The watcher now reads the session the runner last started. The ledger's
`ended` notice to a launcher on another room is held in the relay outbox. Still open: a remote launcher's verdict and
the ledger's say entry across rooms, and a report to the merger that leaves the orchestrator no sign. See
`docs/test-plan.md` section CI.

## 63. Starting onto an existing card goes to the wrong room (bug, HIGH)

Raised 2026-09-28 by clint: "fixed immediately, then fixed in the long run". The board posts `/v1/launch` with
`task_id` in the body. The hub's `roomFor` (`internal/link/proxy.go:218`) picks the room from the header, the
query, or a card id in the PATH, and never from the body's `task_id`. With 3 rooms attached, clint's start of
tlsuv/fix-ci (`01a0e9aa`, on claude-sg4) answered "no card 01a0e9aa... to start onto: sql: no rows".

- Stage 1, now, HUB-SIDE: route a launch that carries a `task_id` to the room that holds that card.
- Stage 2, the long run: a card id carries its room end to end, so no request that names a card can reach another
  room.

## 64. A card cannot stop being lean (bug, HIGH)

Raised 2026-09-28 by clint. tlsuv/fix-ci (`01a0e9aa`) was launched lean by the dotfiles agent, and clint wants it
resumed with his full setup. Nothing gets it there:

- `PATCH /v1/tasks/<id>` with tags that leave out `atrium:lean` changes the tags shown, but a `/v1/launch` with
  `task_id` still starts it lean.
- A `/v1/launch` with no `task_id` and `resume=2c8b8620` has the room adopt the same card by its resume id, and it
  starts lean anyway.

`leanOptions` (`internal/daemon/lean.go:94`) reads the stored tags, not the override.

Two fixes:

- A way to start a card not lean: a launch field that wins over the tags.
- A tag edit that actually clears lean.

Related: item 63, the same start onto an existing card.

**Status, 2026-09-28, sa64: built on `claude/unlean-card`, merged, not deployed.** Test plan CF. `lean` on `/v1/launch` is now
absent, true or false, and false wins over the card and takes `atrium:lean` and `atrium:mcp:*` off it, so a restart
after it is not lean either. The room keeps one tag list per card with no override layer, and a `PATCH` of `tags`
writes that list, so `leanOptions` already read the edited tags. A test proves a tag edit clears lean. The live card's
events show starts after its tag edit came up `"lean": false`. The board notes a lean card on `resume` and on
`restart this session`, and offers `with my full setup` and `restart with my full setup`.

The live fix-ci card was in fact NOT lean after its second resume: no worker prompt, and the user settings loaded.
The earlier "still lean" reading came from testing `--strict-mcp-config`, which every launch passes, lean or not.

## 65. A deploy's revert snapshot is named after the hub's build, not the file it copies (bug)

Raised 2026-09-28. `Save-Revert` in `scripts/live/live-common.ps1` names the snapshot after the build the HUB's
health reports, not after the binary file it copies. When the room was deployed after the hub, the file holds a
newer build than the hub runs, so the name lies. On 2026-09-28 the hub-only deploy of `528f598` wrote
`atrium.revert-f5809905.exe`, which holds `06b87c9`. The real `f5809905` is `atrium.old-20260928155551.exe`.

Fix: label the snapshot with the file's own `atrium version` output, its commit and its board hash.

## 66. New context: capture state, clear, and wake, from one click or one key (feature)

Raised 2026-09-28 by clint. Cycling a long session is done by hand today: tell it to commit and write HANDOFF.md,
wait, POST `/clear` to `/v1/tasks/<card>/message`, wait, POST a resume prompt. He wants it as one action.

- **Trigger.** "new context" on the card's right-click menu, and Ctrl+Alt+N in an attached terminal, caught by the
  board before xterm sees it. AltGr sends Ctrl+Alt on some layouts, so the menu item is the one that always works.
- **Sequence, owned by the room daemon, not the agent.** Type a fixed capture prompt (commit or stash, write all
  relevant state to HANDOFF.md in the cwd, end with the instruction to read back). Wait for that turn to end. Type
  `/clear`. Wait for the new session to start. Type "Read HANDOFF.md and continue from it."
- **Why the daemon holds the resume.** A message the agent queues for itself can land before the clear and be wiped
  with it. `/clear` typed mid-turn can cut the capture short.
- **Visible.** A chip on the card for each step, gone when the wake prompt lands. A step that times out leaves the
  chip in a failed state with the reason, and types nothing further.
- Only for cards whose terminal atrium owns.

## 67. A first-run dialog eats a launched card's first prompt (bug, HIGH)

Found 2026-09-28 by saorch on m1mini. A launch into a directory Claude Code has not seen shows the folder-trust
dialog, and the first typed prompt answers it "No, exit", so the card dies as "failed to start". A fresh Claude Code
also shows a "Try the new fullscreen renderer?" dialog that eats the first say. Two cards died this way.

Blocks sending work to another machine, where every worktree is new. Fix: before a launch, mark the cwd trusted in
the runner's `~/.claude.json` (only for a cwd atrium was asked to launch in), and answer or suppress other first-run
dialogs, or hold typed input until the runner reaches its prompt. The fullscreen renderer must be declined: atrium
renders in xterm.js and an alternate screen loses the board's scrollback.

## 68. atrium_exit and atrium_task do not take a card on another room (bug)

Found 2026-09-28 by saorch. `atrium_say` takes `name@room`, but `atrium_exit` and `atrium_task` take neither
`name@room` nor `room~id`. saorch had to exit a test card on m1mini with the hub's `POST /v1/tasks/m1mini~<id>/exit`.
An orchestrator that launches with `room:` cannot watch or end what it launched.

## 69. The details popover opens in one second, and on the terminals and stack tabs too (feature)

Raised 2026-09-28 by clint. The compact details popover (item 45, `js/peek.js`) opens after a two second hold
(`PEEK_HOVER_MS`). Make it one second. It works on the board tab only: holding the pointer on a card's entry on the
terminals tab or the stack tab must open it the same way.

It must open under the pointer, not anchored to the card, and stay on screen: measured against the viewport,
pushed in from any edge it would cross, and flipped above the pointer when there is no room below. Today it flows
left or right and lands haphazardly.

## 71. "New context" on the terminals tab's right-click menu (feature, end of backlog)

From sa66's open points, 2026-09-28. Item 66 put "new context" on the card menu only, not on the terminal list's
`termMenu`. Ctrl+Alt+N works in an attached terminal. clint: "end of backlog unsure if it's useful".

## 70. Keep-alive is invisible until it has spent something, and one card overspent its budget (feature and bug)

Raised 2026-09-28 by clint: "i still don't see any icons indicating cache is warming or that cachewarming has
STOPPED". Keep-alive is on (default true, 52 refreshes and $4.99 this week), but `keepaliveChip` in
`js/keepalive.js` draws only after a card's first refresh or when it stops. Every idle card that is "not due",
"context under 50k" or "cache already cold" shows nothing, which reads as the feature being off.

- **Visible.** A small chip on every card keep-alive watches, with the `why` and the warm-until time in its tooltip,
  distinct from the warm and cold chips that exist.
- **Overspend.** sa55's card (`01a0e960`) reads `stopped:miss` with `refreshes: 0`, `spent: 1.02` and
  `budget: 0.12`. Spend past eight times the budget with no refresh counted. Find what was charged to it and why the
  budget did not stop it, and whether the refresh count is dropped on a miss.

## 72. One hover on a card, not two (feature)

Built by sa72 (`5f95056` on its branch, old SHA), merged and deployed in `66717c5`. Filed here so the row has a
section. Test plan BZ6.

## 73. A keep-alive fork carries the card's launch args, so lean cards can warm (feature)

From sa70's open points, 2026-09-28. Keep-alive refreshes an idle card's prompt cache with a forked resume, and the
fork does not carry the card's launch args, so lean cards (item 29) cannot be warmed. Make the fork start the way a
restart does, with the card's model, effort, args and lean flag. First confirm with sa70's notes on
`claude/keepalive-visible` exactly which args a lean card's cache depends on.

## 74. A long reply loses lines in the middle on the board's terminal (bug)

Raised 2026-09-28 by clint. A long reply from the orchestrator (a wide table, then several sections) lost 12 to 20
lines from the middle when copied out of the board's scrollback. The full spec is sa74's BRIEF.md in
`D:/worktrees/claude/atrium/lost-lines`.

clint confirmed the loss happened at the first screen update after the long reply, while he was scrolled up a
little. The table's tail was still on the live 50-row screen, and a bare `\e[H` repaint overwrote it. Find what
emitted that repaint (Claude Code, or ConPTY in the room) and why. Also test the reattach seam.

Status: sa74 paused at `ba761d6`. That SHA is from before the 66717c5 re-sign: rebase `claude/lost-lines` onto
`66717c5` before any merge (map in `D:/tmp/resign-map.txt`).

## 75. sg3 as a room, and machine bootstrap reuses the operator's shared folder under `localai` (feature)

sa75 made sg3 a room of this hub. Its `provision-room` fix (`5ced807`, old SHA) is merged. A smoke card on sg3
(2026-09-28) ran Claude and reported system stats, so Claude is signed in there. Found by that card: CIM and WMI are
access denied inside the sg3 room's session, so any worker that reads uptime, services or scheduled tasks through CIM
fails there. Find out which account and session type the room runs in, and whether provisioning should give it more.

clint, 2026-09-28: bootstrapping a machine should reuse a shared folder for the main operator, and the account
should be `localai`, not `claude`. Today sg3 runs as `claude` in `C:\Users\claude`. Fold both into the provisioning
script before the next machine is added.

## 76. A worktree helper that links every CLAUDE.md, so workers get project rules (bug, HIGH, FIRST)

Approved by clint 2026-09-28, first after the restart. `git worktree add` gives a worker none of the 8 CLAUDE.md files:
they are untracked symlinks into dotagents, present in the main checkout only. So every worker launched so far ran
without the project's rules.

Build a helper (pwsh, in `scripts/`) that creates the worktree and branch off `claude/main`, then links every
CLAUDE.md the main checkout has, at the same relative paths, and fails loudly if any link cannot be made. Every
brief uses it from then on. Owned by Release and Quality (@merge). A worker's first check is that `CLAUDE.md`
exists in its worktree.

Status: built on claude/sa76, not merged. `scripts/new-worktree.ps1 -Name <name>`.

## 77. A merge pipeline that does not conflict or rerun (feature, HIGH)

Raised 2026-09-28 by clint: "it always seems super slow". saorch timed its last four merges at about 20 to 25
minutes, mostly `go test` (3 to 5 minutes each), about 1 minute of conflict resolution each, and about 5 minutes of
reruns and calls refused by hooks. Almost every conflict was `CHANGELOG.md` and `docs/test-plan.md`, and several
workers picked the same test-plan letter. Decided with clint:

- **a. One merge-check script.** `go test ./...`, `check-board.sh` with the headless run and `NODE_PATH` preset, and
  `check-skins.sh` when the board changed, in one call. It prints only failures and a pass count per check, so a
  skipped check shows as a missing count instead of silence.
- **b. Each item ships its own changelog and test-plan entry** as its own file, named by item number. The merger folds
  them in. Nobody edits `CHANGELOG.md` or `docs/test-plan.md` on a branch, and nobody picks a letter.
- **c. A worker merges `claude/main` into its branch and passes its targeted checks before it reports done.**
- **d. One full suite per batch,** not per branch. Bisect only on failure. clint evaluates this 2026-09-29 morning.
- **e. A dedicated merge worktree** with Playwright installed, so merges never lock the main checkout.
- **f. `git commit --no-edit --cleanup=strip`** on merges, so no `# Conflicts:` lines land in merge bodies. 33 merges
  in `66717c5` carry them.
- **g. One report per batch** to the orchestrator. clint: "try it and we'll see".

Order: a, c, f, then b, e, g. Owned by @merge.

Status: b, c and g documented on claude/sa77b, not merged. Layout is `docs/changes/<item>.md`, folded by
`scripts/fold-changes.ps1`.

## 78. The details popover's token labels mislead (bug)

Raised 2026-09-28 by clint on @fabric's popover: "2 turns, 48 in, 12k out" looked wrong. Checked against the
transcript: every number is exact. The labels are the problem. "turns" is human prompts (2, across 24 API calls),
and "in" is uncached input only (48), because with caching almost all input is a cache read.

Relabel: prompts, with API calls next to them, and "uncached in". Check the cost estimate against current pricing,
and confirm whether the figures cover the card or only the session since its last `/clear`.

Status 2026-09-28, done on `claude/sa78`. Both popovers (`peek.js` and the details in `usage.js`) now say "prompts" with
"calls" beside it, "uncached in", "cache read" and "cache write", each with a tip saying what it counts. Sonnet 5.5 was
missing from the usage price table and priced at $0, so it is added at $2/$10 with the same cache multipliers, checked
against the pricing page. Rows already stored keep their old cost. The figures cover the whole card, across `/clear`.

## 79. The notification drawer can turn notifications off (feature)

Raised 2026-09-28 by clint: "when i click the notification bell icon to pull the drawer, give me a 'disable
notification' option along with clear and close".

Design (@ui), small on purpose:

- **Where.** A third button in the drawer head (`#toastlog` in `index.html`), beside clear and close: "turn off"
  while on, "turn on" while off.
- **What it mutes.** Toasts, desktop notifications and the sound that goes with them, everything `notify` in
  `js/notify.js` would pop. The drawer keeps logging every entry and the bell's badge keeps counting, so nothing is
  lost and opening the drawer shows what was held back. The marks on cards are untouched.
- **Permission requests still notify.** A permission blocks a session until a human answers, the same exception item
  44 makes. The button's tip says so. Open for clint below.
- **Per browser, in `localStorage`** (`atrium.notify.off`), like the sound mute (`atrium.sound`). A phone and a desk
  want different answers, and the daemon has no notion of which browser is which. Every window of one browser shares
  it, and a popped-out window follows the board.
- **The bell shows it.** Off, the bell is drawn as a struck bell (U+1F515) with the tip "notifications are off. click
  to see what arrived", and the badge still counts. On, it is the bell it is today.
- **Item 44.** 44 is a filter on WHICH cards notify (not agent-launched ones). 79 is a master switch over all of
  them. Off beats everything, including a card's own per-card override, because it is the operator saying stop now.
  On, 44's filter and per-card overrides apply as they do today. The gear's notifications section shows the same
  switch, so the two are found in one place.
- **Not built.** A timed mute ("for an hour") is the obvious next step and is left out until asked for.

Open question for clint: should "off" silence permission requests too? The recommendation is no, since a session
blocks on one until somebody answers, and a muted board is the likeliest place to forget one.

## 80. Real-time token burn and usage charts (feature)

Raised 2026-09-28 by clint, wanted tonight. Item 37 already records every Claude turn's spend, with its cause, in
`session_usage` (`internal/store/usage.go`, migration `0063_session_usage`): one row per turn, keep-alive refresh and
subagent read, with `ended_at`, `cause`, `model`, `replies`, `input`, `output`, both cache writes, `cache_read`,
`context` and `cost`. Today it is served per card only, on `GET /v1/tasks/{id}/usage`, and drawn only in a card's
details. This item charts it. Nothing new is recorded.

Design (@ui):

- **Where.** A `usage` tab beside `history`, its own view. Never on the card face, the terminals list or a toast,
  which is item 37's rule. A card's details gain a small per-card chart above its rows and a link that opens the tab
  filtered to that card.
- **The API.** One new read endpoint on a room, `GET /v1/usage?since=<rfc3339>&bucket=<seconds>`, answering buckets
  of summed rows: per bucket the board total, and per card and per cause. Summed in SQL over the existing
  `(task_id, ended_at)` index, bounded (at most 500 buckets, `since` at most 30 days back), so a month of rows is
  never shipped to a browser. The card titles come from the card list the board already has. Buckets carry raw
  summed tokens per kind (uncached in, out, cache read, cache write 5m and 1h) and the stored `cost` summed, and the
  client divides by bucket width for tokens per minute. For the split chart's per-part dollars the room also
  groups by model and prices each kind with `usagePriceFor`, returned as `cost_by_kind`, labelled "at current
  prices" because a stored row keeps only its total. The headline cost stays the stored sum.
- **Live.** When the tracker writes a row (`AddSessionUsage` in `daemon/usage.go` and `keepalive.go`), the room
  broadcasts a `usage` event on the existing SSE stream carrying that one row's figures and card id. The tab adds
  it to the newest bucket without refetching. No polling. **"Real time" means within about two seconds of a turn
  ending**, because a row is written at the Stop hook. A turn still running shows nothing until it ends. Tailing
  transcripts mid-turn is left out on purpose (Open question below).
- **Charts.**
  1. Burn rate over time: tokens per minute, stacked by kind, for the whole board. Range picker 1h, 6h, 24h, 7d.
  2. The same per card: a small multiple per card that spent in the range, sorted by cost, top 12, the rest summed
     as "others". Click one to filter everything to that card.
  3. Split: cache read versus uncached in versus cache write versus out, as one stacked bar for the range, with
     each part's tokens and cost. This is the chart that says where the money goes.
  4. Cost: cumulative estimated dollars over the range, and a table of cost by cause (you, a say, restart wake,
     keep-alive, resume, subagent) so a restart or a keep-alive round shows as the spend it was.
- **Labels are item 78's.** prompts and calls, uncached in, out, cache read, cache write 5m and 1h, est. The tips in
  `USAGE_TIPS` (`js/usage.js`) are reused, not rewritten.
- **No chart library.** Hand-drawn inline SVG in a new `js/usage-charts.js`, a few hundred lines: a stacked area,
  a bar, a line, axes and a hover readout. No CDN and no vendored library, since the board has to work offline and
  there is no build step. Colours are skin variables (the palette triples), so every skin draws it.
- **Rooms.** The board already talks to more than one room. The tab asks each attached room and merges the buckets,
  with the room as a filter, the way the other cross-room views do. A room that does not answer is named as missing
  rather than silently counted as zero. A room too old to have `/v1/usage` says so the same way. **Every per-card
  bucket, filter, live event and the "others" rollup is keyed by room plus card id**, the identity the board already
  uses for cards from two rooms (`rowOf(list, id, room)` in `js/rooms.js`), never by id or title alone, so two
  rooms' cards are never merged. (Mercurius round 1, concern C1.) A `usage` event that reaches the board by way of
  the hub carries its source room the way the hub's other forwarded card events do, and a room's own stream uses
  the local room key. The headless test includes two rooms with the same card id spending live. (Round 2, C2.)
- **Tests.** Store: bucket sums match row sums, bounds hold. API: shape and bounds. Headless: the tab renders from a
  mocked `/v1/usage`, a mocked `usage` SSE event grows the newest bucket, the labels are 78's, and a skin change
  recolours it.

Open question for clint: is burn at turn end enough, or does "real time" mean watching a turn spend while it runs?
That needs the room to tail every running transcript, which is a bigger and riskier change. Built at turn end first.
Top 12 cards plus "others" is the first cut, easy to change.


------------
