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
| 8 | Input lag follow-ups | bug | narrowed to one prefix: hop split `1f49694` (runner side), unsent fix `6bb14e4` |
| 9 | Eliminate unstyled tooltips | bug | DONE, `069c16b`, deployed, check-titles guards it |
| 10 | `atrium_say` types immediately by default | feature | DONE, `04c2095`, not deployed. Also covers item 7's reason and count |
| 11 | Clicking `? N` or a question clears it | bug | not started, 2026-09-25: the click selects the row instead |
| 12 | Keep codex up to date | feature | not started |
| 13 | Housekeeping asked, not answered | housekeeping | waiting on clint |
| 14 | Per-card notification log | design | tentative |
| 15 | Pluggable event sink, what is left | design | stages 1-2 done |
| 16 | Reviews that remember: a resident reviewer per repo, and a panel that reads once | design, HIGH PRIORITY | designed, `docs/review-memory-design.md`. Stage 1 held for clint's yes (dotfiles), stage 2 waits on 3 questions |
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
| 46 | Provision a machine as a room over ssh, from one command and later from the board | feature | stage 1 DONE, `35fa4a1` `5145bad`, fb03 toolchain waits on a fetch. Stage 2 not started, 4 questions |
| 47 | A resident session's alias defaults from its name, can be read and set, and heads the terminal title bar | feature, HIGH | DONE by sa47, merged `5b3d9e5`, deployed `66717c5` |
| 48 | `atrium_launch` takes a model and a thinking effort | feature | DONE by sa48, merged, needs room and hub restarts |
| 49 | The orchestrator can appear on every room | design | @fabric, design reviewed `1ccecf2`, ready to build, 3 questions |
| 50 | Views of agents, beyond groups | design | not started |
| 51 | Five kept worktrees show 48 commits not matched on `claude/main` | housekeeping | DONE, all five safe, deleted 2026-09-28 |
| 52 | A pinned strip with cards from two rooms orders only one room | bug | DONE by fb04, merged into claude/fabric, needs hub and room restarts. `prune` is item 92 |
| 53 | `setViewport` and `dropViewport` compute under `r.mu` and apply outside it | bug | by sa53, DONE, reproduced and fixed |
| 54 | Terminal test suite part 2: `screen.go` against xterm.js | feature | DONE by sa54, `1938e74`, on claude/main `e45fc2c`. Its two skips became 81 and 82, both done. No part 3 |
| 55 | Launched runners inherit ATRIUM_DEBUG_INPUTLAG from the room | bug | DONE by sa55, merged, needs a room restart |
| 56 | Every dialog is sleek, one skinned design, starting with card details and the room's edit-agents screen | feature, design first, HIGH | DONE by sa56, merged `a1ab1c2`, deployed `66717c5` |
| 57 | The live room leaks memory | bug, HIGH | DONE by sa57, merged `aa74dd5`, deployed `66717c5` |
| 58 | `atrium_say` reaches cards on other rooms, `name@room` | feature, HIGH | DONE by sa58, merged, needs hub and room restarts |
| 59 | Spike on m1mini: more than one room per machine, and a blocked room that drains | design, spike | deep backlog, not started |
| 60 | The stdio control MCP has sa48's launch fields but no "room is older" warning | housekeeping | not started |
| 61 | A fake 45s hub echo in the lag log from the idle ping and pong | bug | DONE `88fc53d`, on claude/main |
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
| 74 | A long reply loses lines in the middle on the board's terminal | bug | sa74: inbox ConPTY on a row change. Height hold built, not merged. OpenConsole choice with clint |
| 75 | sg3 as a room, and machine bootstrap reuses the operator's shared folder under `localai` | feature | sg3 room works (`35fa4a1` `5145bad`), provision script merged `c3dc597`. CIM for workers and the `localai` account not started, 4 questions |
| 76 | A worktree helper that links every CLAUDE.md, so workers get project rules | bug, HIGH, FIRST | not started, @merge |
| 77 | A merge pipeline that does not conflict or rerun | feature, HIGH | not started, @merge, after 76 |
| 78 | The details popover's token labels mislead | bug | not started |
| 79 | The notification drawer can turn notifications off | feature | design filed, queued behind 78, 44 and 43 |
| 80 | Real-time token burn and usage charts | feature, TONIGHT | design filed, ahead of 43 and 79 |
| 92 | `/v1/tasks/prune` reaches one room, like pin-order did | bug | not started, @fabric, with the next `internal/link` worker |

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

**Status 2026-09-29, narrowed to the prefix.** On claude/main:

- The hop split is `1f49694`. The room's echo line now splits `runner` from `atrium` time. It says the stall is
  the runner side: the runner's own redraw, or Windows not scheduling it, and not atrium. The hub and the room
  already raise themselves to above normal for the second. The hiccup probe in `docs/input-lag-logging.md` tells
  the two apart on a given night, and no run of it is on record here. A live sample agrees on the side: on
  2026-09-28 at 09:55:19 a 176.6ms echo to card `01a0e80e` was `runner 176.1ms, atrium 0.5ms, ws write 0.5ms`.
  Nothing in atrium's hops is left to chase. The hub's fake 45s echo that muddied the hub's side was item 61
  (`88fc53d`).
- The unsent-bytes line reading the key's own frame is `6bb14e4`: the board reads the socket backlog before the
  send.
- Left: the prefix. `internal/inputlag/inputlag.go` and `js/inputlag.js` still log `[inputlag]`. It is a small
  naming choice (`[atrium inputlag]` keeps a `[atrium` filter working, at the cost of anyone grepping for the old
  one), not a bug, so it waits for someone to want it.

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

**Design, 2026-09-29 (@ui).**

Two different things wear a count, and they need different answers.

- **`? N` is a turn's Open Questions** (`seenChips` in `js/seen.js`, drawn by `board.js`, `stack.js` and
  `termRowChips` in `terminal-list.js`). It clears only when `turn_seen.answered_at` is set, and today only a
  prompt, a message or `atrium finish` sets that. Typing the answer in the terminal is invisible to atrium, which is
  the way clint actually answers, so the chip stays up.
- **`! N` is a peer message held behind the operator's line or a turn** (`termHeldChip`). It is not a question.
  "Clearing" it could mean dropping a message or forcing it in past the gate, and both lose something.

What gets built:

1. **Clicking `? N` dismisses the questions.** Dismiss, not "seen": seen is the dot's business and does not take
   `?` off. The chip's markup gets `onclick="event.stopPropagation();dismissQuestions(id)"` inside `seenChips`,
   so all three places it is drawn change at once and none of them selects its row or opens its card. It also gets
   `role="button"`, `tabindex="0"` and an Enter/Space handler, the same way the other clickable chips work.
2. **One new route: `POST /v1/tasks/{id}/questions/dismiss`, body `{"questions_at": "<the value the chip was
   drawn from>"}`.** Shaped like `POST /v1/tasks/{id}/seen` and for the same reason: the click names the set it
   was shown, so a stale render cannot dismiss a newer set. It calls a new store function,
   `DismissQuestions(taskID, shownQuestionsAt)`, which sets `answered_at` and `answered_via = "dismissed"` only when
   the stored `questions_at` is not later than the shown one (compared as times, as `MarkSeen` compares its turn), and leaves `seen_at` alone. `MarkAnswered` is not reused because
   it also marks the turn seen, and a dismissed question is not a read turn. It answers
   `{"dismissed": true|false, "stale": true|false}` and publishes the card. `stale` means newer questions arrived
   since the chip was drawn: nothing changes, and the board toasts "newer questions arrived, nothing was dismissed"
   and repaints so the new chip shows. A missing or unparseable `questions_at` is a 400. An unknown card is a 404.
   A card with nothing open answers `dismissed: false, stale: false` rather than an error. A `room~id` goes through
   the hub's generic task proxy like `/asks` does (`internal/link/fanout_test.go`), so it needs no hub change.
3. **What an agent sees.** `atrium_task`'s `seen.answered` becomes true and `answered_via` reads `dismissed`, so a
   launcher can tell "clint dismissed it" from "clint replied". `internal/link/control_mcp.go` already passes
   `answered_via` through. The tool description needs one clause about `dismissed`.
4. **Feedback.** The chip comes off when the card is repainted, which `refreshSoon()` asks for after the call, the
   same helper `reportSeen` uses. A
   toast says "questions dismissed, nothing was sent to the session", matching `dismissAsks`. A failed call leaves
   the chip and toasts the error. No confirm, because clint asked for one click. The tooltip, which already lists
   the questions, ends "click to dismiss them without replying" rather than only "clears when you reply to it".
5. **`! N` and `✉ N` take their own click too, and do nothing else yet.** `event.stopPropagation()` so the row is
   not selected. What a click should DO to a held message is an open question for clint (below).

Not in scope: the unseen dot (it already clears on dwell), the `asked you` state chip on stack rows (it is the ask
field, which the card menu's "dismiss questions" already clears), and item 20's repaint.

Headless section `questionsClick`: a terminal row, a stack row and a board card each with `seen.answered: false`
and two questions. Clicking the chip must not change `termTask` or open the card dialog, must send exactly one
`POST /v1/tasks/<id>/questions/dismiss` carrying the card's `questions_at`, and the chip must be gone after the
next poll. A mocked `stale: true` answer must toast and leave the chip. A click on `!` must not change `termTask`.
Go: store tests that `DismissQuestions` sets answered and leaves seen, and that an older `questions_at` changes
nothing. An api test for the route, including the 400.

Review: Mercurius round 1 (s_ajiZDcEq7DfD) found that dismissing without naming the set shown lets a stale render
dismiss newer questions (C1, folded as the `questions_at` precondition and `stale`). Its advisory, use
`refreshSoon`, is folded too.

Open question for clint: what should clicking `!` on a held message do? Recommendation: nothing yet beyond not
selecting the row, because the tooltip already says what clears it and both obvious actions (send it now past
the gate, or drop it) lose something.

**Status 2026-09-29 (branch claude/sa11).** Built as designed. `DismissQuestions` in `internal/store/seen.go`, the
route in `internal/api/api.go`, the chip and `dismissQuestions` in `js/seen.js`, `stopPropagation` on the held chips,
the `dismissed` clause in `atrium_task`, and headless section `questionsClick`. Changelog and test plan are in
`docs/changes/11.md`. The `!` click question above is still open.

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
Not started. Reconciled and designed 2026-09-29 by @ui, owner @ui, waiting on clint's Open Questions below.

### The idea

A card accumulates notifications over its life: a peer message held/deferred and re-warned on backoff (see the
peer-message injection work), a permission asked, a going-down, an audit event. Today a notification fires once as
a transient toast (and toasts have been vanishing too fast to read), so a human who was not looking never learns it
happened. The board has a global notification history (the bell). This item is a PER-CARD view of that: open a card
and see the notifications it has raised, newest first, so "what has this session been trying to tell me" is
answerable after the fact rather than only in the moment.

### Reconciled 2026-09-29, against what is built

Most of this exists. What is left is one view and one gap.

What the board already keeps:

- **The toast log** (`js/toast-log.js`, the bell). Every toast, every desktop notification (`logNotification`), every
  alert item 44 mutes for an agent-launched card, and every alert item 79 holds back while notifications are off,
  lands there. Each entry carries `taskFor`, the card a click lands on. It is per browser, in localStorage, capped at
  200 entries across the whole board, and clearing it clears everything. That is on purpose: its header says what
  you were told is a fact about a screen, not about the work.
- **The card's timeline** (the details dialog, `#d-events`, `timelineHTML`). The daemon's own event log for the card:
  permissions asked and answered, status changes, and `notified` events for uploads, file and terminal opens, the
  restart wake, the unexpected-exit wake and a dropped cross-room message. It is durable and the same in every
  browser.
- **The held-message chips** (`!` and `✉` on a terminal row), for a peer message waiting at the gate.

So the idea's examples split. A permission asked is already in the timeline. A going-down and a held peer message
are in the toast log when they toasted, and a held one also wears a chip while it waits. "The daemon already records
`notified` events per card" is true but those are not the notifications: none of them is what the board said to you.

What is missing:

1. **A per-card cut of the toast log.** The data is there, keyed by `taskFor`, and nothing shows one card's entries.
2. **A pile belongs to no card.** "3 agents need permission" and "2 agents are ready" are logged with `taskFor`
   empty, because a click on them lands on a tab, not a card. So a card that only ever alerted as part of a pile has
   nothing in its cut, and those are the busy moments this item is about.

### Design (board only, no daemon or store change)

- **A section in the details dialog**, "what the board told you", under the timeline and collapsed when empty. It
  lists this browser's toast log entries for the card, newest first, with the same row the tray draws (time, title,
  repeat count, body, copy). A row does what the tray's row does, through `landOnAlert`. Matched with `sameCard`, so
  `room~id` and a bare id are one card.
- **It says whose record it is**: "in this browser. the card's timeline above is the room's record". Two tabs on two
  machines see different lists, and that is the toast log's rule, not a bug in this view.
- **A pile records its members, on every path that writes the log** (Mercurius round 1 C1, C2). An entry gains an
  optional `tasks`, the bare ids of the cards a pile covers. The card cut matches `taskFor` or `tasks`. The tray is
  unchanged. `tasks` is threaded through every function between a pile and the log, because which one writes it
  depends on where the focus was:
  - `notify` takes it (in `opts`, beside `pending`) and hands it on in all four of its cases: `logNotification` when
    notifications are held (item 79) and when the desktop takes it, `toast` when this window is focused or nothing can
    reach you, and the `win-toast` message when another atrium window is focused. The `win-toast` receiver passes it
    to `toast`.
  - `toast`'s wrapper in `toast-log.js` and `logNotification` pass it to `recordToLog`. The real toast ignores it.
  - Every call that raises a pile passes it: `announce` for a pile of fresh items, AND the first-pass permission branch
    in `check`, which raises "N agents need permission" on a page load without going through `announce`. The rule is
    that any alert whose title counts several cards names them.
  - The per-item lines `announce` already writes for an agent-launched card (item 44's `quietDoer`) carry `taskFor`
    and need nothing.
- **A repeat needs the same members** (C3). The repeat rule bumps the last entry when title and body match. For an
  entry with `tasks`, the sorted `tasks` is part of the signature, so "2 agents are ready" for two different pairs
  is two lines and each card's cut shows only its own.
- **No filter in the tray.** The tray is "what did I miss", across the board. A card filter there is the same list
  as the dialog section, reached from the wrong end.
- **The cap is Open Question 3's answer, 200 until then.** A card's cut is thin for a card that is old, and the dialog
  says so when the oldest kept entry is newer than the card: "older entries have rolled out of this browser's log".

Headless: a section `cardToastLog`. Seed the log with entries for two cards, a pile naming both and one naming a
third, open each card's details and check what each lists, that a `room~id` entry matches its bare card, that the
rolled-out line appears only when it should, and that a row click calls `landOnAlert` with the entry's fields. Then
drive real piles through `notify` in each focus case (focused, held by item 79, desktop, another window by a stubbed
`win-toast`) and through a page load with two permissions already pending, and check each entry names both cards.
Two same-text piles of different pairs are two entries.

Mercurius round 1 (session s_5rBGJK0xqSTF, needs_changes) is folded above: C1 and C2 are the propagation rule and
the first-pass branch, C3 is the repeat signature, A1 was the cap stated two ways. Q1 is Open Question 1.

### Open Questions for clint

1. **Build it, or close it?** It is about 80 lines of board code and one headless section, with nothing on the daemon.
   Recommendation: build it, since the pile gap means the busiest moments are the ones nobody can look up per card.
2. **Per browser is the right home?** A daemon-side record of what was said would follow you to another machine, but
   it would be a second event log that records who was looking, which the toast log was written to avoid.
   Recommendation: per browser.
3. **Is 200 across the board enough?** At a busy hour that is less than a day. Options: keep 200, raise it (the
   entries are small, 1000 is well under 1 MB of localStorage), or keep the last N per card. Recommendation: raise
   it to 1000 and say what rolled out.

### Why it might not be worth it

The global bell history plus the new per-card held-message indicator may already cover the need. The event log
(and the pluggable event sink above) already records `notified` events per card, so this could be a thin read view
over data that exists rather than new storage. Decide whether a dedicated per-card log earns its place or whether
filtering the existing history by card is enough. clint has not committed to building it.

------------

## 15. Pluggable event sink: get the audit trail out of the primary database

**Raised 2026-09-18.** Stages 1 and 2 done. Stage 3 (the permission table) is designed in
`docs/event-sink-stage3-design.md`, not built. The offsite sink and a live swap are left after that.

### Status

Done (reconciled against `claude/main` 2026-09-29. Every sha below is an ancestor of it except `02fe769`):

- **Stage 1** (2026-09-18): the `EventSink` seam (`8bf4083`), the rolling-JSONL `file` cold sink and
  `event_sink=db,file` (`1959d8a`), the opt-in per-card hot window `event_window_bytes` (`ede2f4b`), plus
  `rolled_off` on a card's event feed and incremental auto_vacuum for fresh databases with the `vacuumLoop` timer.
  The whole of it also went out in the squashed `02fe769` (2026-09-21), which is on a separate line of history.
- **Stage 2, routing** (`1f075a3`): `event_cold_kinds` names kinds that go to the cold sinks only. Off by default.
  The card's detail dialog says which kinds and whether older events rolled off.
- **Stage 2, compact** (`1c26346`): `atrium2 db compact --in <db> --out <db> [--window-bytes N] [--drop-kinds
  k,...]`, an offline `VACUUM INTO` copy switched to incremental auto_vacuum.
- **Stage 2, docs** (`950f518`): the measurement, the decisions, the changelog and the test plan.

This section used to give `1e03180` and `1070cbf` for the two stage 2 commits. Those are pre-rebase copies with the
same subjects, and neither is on `claude/main`.

Left:

- **Stage 3, the permission table** (designed, see above). It is now the largest thing in the file (see the
  measurement) and nothing trims it.
- The `offsite` sink.
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

**Status 2026-09-29:** designed in `docs/review-memory-design.md`, reviewed by Mercurius over two rounds. Stage 1 (read
once, panel sized to the change, the #4480 replay) is buildable, and HELD for clint, because it changes his
dotfiles skill and personas. The design's "Stage 1, file by file" section lists the eight edits and the one-line
yes. The replay (about 17M tokens) is a separate yes. Stage 2 (reviewer files) waits on his open questions 1, 2 and 6.

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

### Findings (sa21, 2026-09-28)

**Why the existing check did not fire.** `stuckNow` (`internal/daemon/a2a.go`) is a hook-timing check, never a pty
check, and it has three gaps that each cover this card. It only walks AGENT-LAUNCHED cards (`agentLaunched`), and this
card was started from the board. Its silent-stop case needs the card already in `needs-input` and owing a report,
and this card read `running`. Its stuck-tool case needs `toolSince`, a tool call running 20 minutes, and this card
read `thinking`. So a `running` card with a lost Stop is invisible to it by construction, and the board's "stuck"
list (`settings-spine.js`, `isStuck`) only renders what `stuckNow` serves.

**Why the Stop was lost: not provable from the log.** `room.err.20260926-083926` (the file spanning 2026-09-25 after
09:12) has no line at all between 13:00 and 17:00: the room logs nothing for a hook that succeeds, and nothing for
one that never arrived. The only mention of the card is its restart at 18:50. What the code does allow, most likely
first: (1) an out-of-order `/activity` post. `handleActivity` answers and then runs `go d.onActivity(in)`, so two
posts a few milliseconds apart race. A `tool-start` (PreToolUse) processed AFTER the `turn-end` calls `turnResumed`,
which moves `needs-input` back to `running` and sets the activity, and the following `tool-end` leaves `thinking`.
That is exactly `running` plus `thinking` with a finished terminal, and needs no lost hook. (2) The Stop hook process
never ran or timed out. (3) A late `prompt` event. Only (1) can be pinned by logging, so the firing log below
records the last activity event's kind and age, and a later pass can order `/activity` posts by a sequence stamp
if the log shows (1).

### Design

**Signal, all of it required.** The card is `running`. The runner is Claude (`t.Runner`, scoped to claude: codex has
no idle signature I could confirm, so it is never flagged). It is supervised, so atrium owns the pty. The activity is
mid-turn (`midTurn`, read past the staleness cutoff, since a lost Stop is exactly the case that outlives it). It has
no subagents from its last Stop (`onSubagents`) and no dialog. The pty has produced no output for `LooksIdleAfter`
(25s, `ATRIUM_LOOKS_IDLE`). And the last frame reads idle.

**Silence threshold.** 25s. Claude Code redraws its spinner about once a second while it works, so 25s is more than
20 missed redraws. The check rides the reaper's 20s tick, so a firing lands 25 to 45s after the turn ended. A
dedicated faster timer was rejected: the badge is a hint, and a second ticker is a second thing to stop at shutdown.

**Idle signature from the last frame (claude only).** Read `buf.Tail(8KB)`, strip escapes with the existing `ansi`
regexp, and take the text from the LAST input-box top border (`╭`). Idle when that box has its bottom border (`╰`)
after it and neither the box's footer nor the non-empty line just above the box contains `to interrupt` (the working
spinner line reads `... esc to interrupt` and sits directly above the box). No box at all, or an interrupt hint in
the last frame, means not idle, so a long silent Bash under a live spinner, a dialog, and a half drawn screen all
stay unflagged. It fails toward not flagging: a missed badge costs what the board costs today. The strings are
Claude Code's and can change, so they are constants in one file (`idleframe.go`) with tests that pin them.

**How the pty is read.** Two additions to `supervisor.go`, nothing restructured. `runner.lastOut` is an atomic unix
nano stamped in `deliverOutput` (one atomic store, outside `r.mu`), read by `lastOutputAt()`. The frame is
`r.buf.Tail`, an existing bounded read. For clearing, `runner.wake` is an `atomic.Pointer[func()]`, nil unless the
card is flagged, so the byte path and the keystroke path each pay one atomic load, the same shape as `onKey`. It is
called after `r.mu` is released in `deliverOutput` and after a real keystroke in `noteOperatorTyped`.

**The badge is activity, never status.** `Activity` gains `looks_idle` and `idle_seconds`, held in the activity
tracker's own map, in memory. `get` attaches them (including past the staleness cutoff). `set` (any hook event: tool,
prompt, idle) clears them, so a late Stop settles the card exactly as it does today. `forget` clears them. The stored
status is never touched, so nothing here can move a card between columns.

**Board.** `activityChip` draws, for `looks_idle`, a warn coloured chip with a hollow ring with a gap and no
animation, in place of the live chip, with the tooltip "no turn-end from the agent. its screen has been idle for
Ns." `workingNow` returns false for it, which stops the terminal strip's runner mark and the activity sort from
claiming work. Any activity push replaces `t.activity`, so clearing needs no board state.

**Alert.** Wording "looks idle (no turn-end received)", a guess and worded as one. It rides the existing `waiting`
alert kind (so the gear's waiting setting governs it), keyed by card id plus the time it was flagged, so it rings once
per firing and a card that wakes and stalls again rings again.

**Logging.** Every firing logs one line: `[atrium] looks idle: <wire> <card> silent 31s, activity thinking for 12m,
last event tool-end 12m ago`. Every clearing logs `looks idle cleared: <wire> by output|keystroke|hook after 40s`.
A firing cleared within the same minute by output says the frame check is too eager.

**Review.** Mercurius session `s_jGsDlA3ObWch`, round 1, verdict `ready_to_build`, no concerns. Two advisories, both
taken: tests pin a working frame and an idle frame, and the firing log names the classifier reason (`frame=`).

**Built** on `claude/sa21`: `idleframe.go` (signature), `looksidle.go` (watch, flag, clear, log), a `lastOut` stamp and a
`wake` hook in `supervisor.go`, `Activity.looks_idle`, the board chip, `workingNow`, and the `looksidle` alert.
Not verified: the signature strings against a real Claude Code screen (the fixtures are hand-written frames), and the
headless board section `looksIdle`, because playwright is not installed on this machine.

**Not done.** No migration. No change to the permission chain, `/activity`, or the hooks. No status change. No
codex. `stuckNow` is untouched because sa31 is in a2a.go: the new watch is its own function in a new file, called
from the reaper next to `watchWorkers` in one line.

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

**Status: built on `claude/sa31`, not merged.** Mercurius review of the design (`docs/background-hold-design.md`): round 1
on this whole file was off target (the reviewer judged it against another design), round 2 on the standalone design
returned ready to build with one advisory, the post-cap transition, now stated. Changelog and test plan in
`docs/changes/31.md`.

### Design (sa31)

**The signal is the Stop payload's `background_tasks`.** `atrium turn` already reads it (`internal/cli/turn.go`) and
counts only `type == subagent`, on purpose, so that a subagent panel does not ring the board. Each entry has
`type` and `status`. It is the runner's own account of what it is waiting on, it costs nothing, and it is the same
on Windows and Linux. Nothing new is asked of the OS.

**The process tree is rejected.** Descendants of the runner pid cannot tell a Bash-tool shell running `go test` from
the MCP servers (atrium control, mercurius) that every claude owns, and the two look alike on Windows, where the
tool shell is a `bash.exe` or `pwsh.exe` child among other children. A baseline taken at session start would need
the reaper to walk trees on every tick and would still misread a shell the MCP server itself spawns. The payload
is an answer, the tree is a guess. If `background_tasks` turns out absent from a Claude Code version, the fallback
is the existing behaviour, not a tree walk.

**Distinguishing MCP children** is therefore not needed: they never appear in `background_tasks`.

**What counts.** Any task with `status == running` whose type is not `subagent`. Type names belong to Claude Code and
are not enumerated. This is a NEW count (`background_running` on `/stop`), kept apart from `subagents_running`,
because subagents keep the card in running and shells must not: a session that left a dev server up and stopped is
still waiting on the operator, so the card still moves to needs-input.

**Hold.** `stoppedSilently` returns "not silent" while the card's last Stop named running background work. That one
function feeds both the board escalation and the launcher notice (`stuckNow`, `silentStop`), so both hold.

**When the clock starts.** Claude Code wakes the session when a background task completes, and that wake ends in
another Stop. That Stop names nothing running, records a new `turn_ended` and replaces the held count, so the clock
is the LATER turn end with no new signal. If the work ends without a wake, nothing tells atrium, so the hold is
bounded by `BackgroundHoldMax` (default 2h, `ATRIUM_A2A_BACKGROUND_HOLD`), after which the original turn end is the
clock and the alert fires. That also bounds a dev server left running on purpose.

**Storage.** In memory in the activity tracker (`bgWork`), replaced by every Stop and dropped on `forget`, like
`background`. No migration, no event.

**The board.** Unchanged for now: the card shows needs-input as before, with no STUCK. A "waiting on background
work" badge would need the count to travel on the card, and is left out until asked for.

**Hooks stay safe.** No new failure path: an old hook omits the field and reads as zero, and the Stop answer is untouched.

## 32. A queued say from http-support never produced a backlog entry, and nothing can say why (bug)

**Status: built on `claude/sa32`, migration `0069_say`.** Lifecycle row, candidates on a miss, and `reply: true` owed
replies are done. Cross-room delivery receipts need a say id on the relay request (link and hub, not changed).
Design: `docs/say-lifecycle-design.md`.

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

Status: built and on claude/main. The line's text and keystroke-only counting are `118d6e5` (`typedline.go`), the
Esc Esc port onto it is `d713e5c`, and the readout behind a setting is `2f15ace` (`js/typing.js`, polling
`GET /v1/tasks/{id}/typing`). `TestASayWhenDoneWaitsForTheTurnToEnd` covers a `when: done` message typed at the turn
end. All three are in the room binary deployed at 22:53 on 2026-09-28 (`66717c5`).

The sa85 incident is confirmed from a copy of the room database and `room.err.20260928-111944`. Three messages from
the orchestrator: a `when: done` one at 09:03, and immediate ones at 09:45:50 and 09:50:59. sa85 reported done at
09:45:32. The room log has clint's keystrokes to that card at 09:55:16 to 09:55:19, and all three messages were
typed at 09:55:21 to 09:55:22. The turn end at 09:45 released nothing, so the gate was reading a part-typed line,
which is the old counter. That build predates `118d6e5`.

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

**Status, 2026-09-28, sa41: built on `claude/sa41`.** Design in `docs/owed-report-design.md`. The sender was already in
every `prompted` event as `from_peer`, so `appendEventOn` now decides in one place whether a prompt counts (the launch
prompt, or a `from_peer` that is the card's launcher) and stamps a new `task.owed_at`. `OwesReport` and `PromptKey`
read it, so the silent-stop notice and the STUCK mark share one rule. Migration `0068_owed_at`, backfilled from
`prompted_at`. A monitor-woken turn needs no detection: no door records a prompt for it, so it creates no debt, and a
debt already open is noticed once per key.

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

**Status, 2026-09-28, sa43: built on `claude/sa43`, not merged.** As designed. `noteTurnForSeen` takes an auto-seen
via and `launcherSeen` picks `SeenLauncher` for an agent-launched card that owes no report. The board shows no
`seen_via`, so it is unchanged, and `atrium_task`'s description of `unseen` now names the launcher. A Stop still
publishes twice in all: the status move to needs-input, then the one seen publish. See `docs/changes/43.md`.

## 44. A gear checkbox: no notifications from agent-launched cards, on by default (feature)

Raised by clint 2026-09-28: "I don't need notifications from them." A worker an agent launched reports to its
launcher, so its turn ends, waits and stuck alerts reach clint as noise. The workers' own launcher already hears
through `notifyLauncher`.

Expected: a checkbox under `notifications` in the gear, "don't notify me about cards an agent launched", ticked by
default. Ticked, a card with the `origin:agent` tag raises no toast, no desktop notification and no sound. Its
marks on the card stay, and so does the toast log entry, so nothing is lost. A permission request from such a card
still notifies, because it blocks until a human answers. A card's own notification override beats the checkbox.

**Status, 2026-09-28, sa44: built on `claude/sa44`, not merged.** The gear box `quietDoers` (per browser, default
on) logs and does not say arrivals, waiting and stuck alerts for `origin:agent` cards. Permissions still notify. No
per-card notification override exists in the board, so a card with its own tone stands in for one. See
`docs/changes/44.md`.

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

**Status, 2026-09-29, @fabric (fb01, fb02, fb03): stage 1 is one command, and a room's work comes back by git.**
Done:

- **No flags needed.** With no release and no `-Version`, the script builds from the checkout with a `fetch warn`
  (`35fa4a1`). The release path is unchanged and still fails with its reason, since there are no releases.
- **Signed-in check and smoke test.** An `auth` step names `ssh -t <target> claude auth login` when claude is not
  signed in, and a `smoke` step launches a worker that reports a nonce back, with exit 8 when it does not, and
  `-SmokeOnly` for a room already in use. Proven against sg3, reported in 9 seconds (`35fa4a1`).
- **No CIM.** Scheduled task actions go through `schtasks.exe` and autostart registers by XML, so a Windows machine
  that denies CIM over ssh works (`35fa4a1`).
- **systemd PATH, the second bullet above: built, not proven.** A login-shell `ExecStart` in the packaged unit, and
  the login shell's PATH written by `atrium-service.sh` (`35fa4a1`). No Linux machine we may test on has run it.
- **Git both ways.** `scripts/room-git.ps1` `init`, `push-base`, `fetch` and `worktree`: the remote clone is made
  by push, so the remote needs no GitHub credential, and `provision-room.ps1` runs `init` last (`5145bad`). `push-base`
  and `fetch` reach a remote, so under the hooks they are clint's to run.
- **Toolchain: built, not merged.** fb03 installs a room's toolchain under `~/.atrium/toolchain` with a checked hash,
  and a `room-env.ps1` the room is started through. Proven on claudevm, head `9bce8ad` on `claude/fb03-toolchain` in
  sg3's clone. It comes here when clint runs `room-git.ps1 fetch sg3`.

Left:

- Autostart as the default (the first bullet above), unchanged.
- Proving the systemd PATH ("Linux autostart", step 5 of `docs/changes/fabric-1-provision.md`) on a Linux machine
  that is not a live room.
- Proving `-Autostart` starting on a Windows room, and the binary swap's `schtasks /End` on a live task. Both only on
  claudevm, never on a real room.
- The provision start hook for fb03's `room-env.ps1`, on top of the XML registration, once fb03 is here.
- Stage 2, the board half. Not started.

Open questions for clint:

1. Should `-Autostart` become the default, and on Windows is it a logon task or the detached room?
2. Which Linux machine may the Linux autostart test run on? sg4-wsl is a live room and is ruled out.
3. Should provision write `permissions.allow` for `mcp__atrium-control__*`? Likely no: the smoke worker passes with
   `--allowedTools=` alone.
4. When does stage 2 start, and does it wait on item 75's account question?

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

**Status, 2026-09-29: design reviewed, ready to build.** `docs/everywhere-card-design.md`: a tag
`atrium:everywhere`, a hub index of tagged cards, bare names that miss locally fall through to it for say, tell and
task, and a scoped view that shows those cards with a room chip. Mercurius round 1 was `ready_to_build`, its one
advisory folded in as FE10. Building waits on clint's three open questions in the doc. The board's part is @ui's.

## 50. Views of agents, beyond groups (design)

Raised 2026-09-28 by clint: "i'm starting to want different 'views' of agents more than just groups". Ideate
first. Examples to explore: saved filters, a view by role, a view by launcher, and workers apart from clint's own
cards.

Spec (@ui, 2026-09-29), not built, for clint to answer before anything is:

What exists today. The board groups by project, recency, window, tag or hand-made groups, or by a free expression
kept in this browser (`grouper` in `js/board.js`). The terminal list can hide doers (`origin:agent`) apart from
agents. The room picker narrows to one room. Each of those is its own switch in its own place, and none of them is
remembered as a set.

The gap is that a "view" is three answers given together: which cards, grouped how, sorted how. Today you rebuild
the set by hand each time you change your mind about what you are looking at.

- **A view is a named preset.** It holds a filter, a grouping mode and a sort, and picking one sets all three. A
  picker sits beside the grouping control. Changing any of the three by hand leaves the view marked "edited" rather
  than silently rewriting it, and "save" writes the change back.
- **Filters come from a fixed menu, not from code.** Started by me or by an agent (`origin:agent`), has tag, in room,
  in status, launched by. A free expression stays where it is, in grouping only, because of the rule `compiled` in
  `js/board.js` writes down: an expression may be stored where it was typed. A fixed menu is what keeps the door open
  to views that follow you to another browser.
- **Two new grouping modes, for the examples clint named.** *By role* groups by a tag prefix, `dept:` by default, so
  `dept:ui` and `dept:runtime` are the groups and a card with no such tag falls back to its project, the way window
  mode does. *By launcher* groups a worker under the card that launched it. That needs the launcher on the card as
  the board reads it. It is in the ledger today (`LauncherID` on `WorkItem` in `internal/store/ledger.go`) and not
  on the task JSON, so this one mode costs a small API change owned with @runtime.
- **Built-in views, so it is useful on day one.** "Mine" (not `origin:agent`, by project), "workers" (only
  `origin:agent`, by launcher), "by role" (all, by `dept:` prefix), "needs me" (needs-input or needs-permission,
  by recency). They can be edited but not deleted, and a "reset" puts one back.
- **One active view for the board, stack and terminals tabs,** since the question "what am I looking at" does not
  change when the tab does. Per browser, in `localStorage`, like grouping.
- **Not in the first build.** A launcher tree (orchestrator, then director, then worker, nested), views shared
  between browsers, and a view per tab.

Open questions for clint:

1. Are the four built-ins the right four? Recommendation: yes, with "workers" and "mine" as the pair that answers
   "workers apart from my own cards".
2. By launcher: one level (a worker under its launcher), or the whole tree? Recommendation: one level first. The
   tree is the same data drawn nested, and it is worth seeing one level in use before deciding.
3. One view across the board, stack and terminals tabs, or one each? Recommendation: one.
4. Per browser, or following you to other browsers? Recommendation: per browser now. Since filters are a fixed menu,
   moving them to the daemon later is a settings key and not a security question.

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

**Status, 2026-09-29: diagnosed, design written, moved from @ui to @fabric.** `docs/pin-order-rooms-design.md`. The
order carries no card in its path, so the hub routes it by the board's stale `writeRoom` header to one room. Fix:
the hub posts the whole list to every attached room, so each writes its own cards' ranks at their position in the
whole strip. Hub-side only, no room, board or migration change. The second note stays a question for clint and
@ui.

**Status, 2026-09-29: DONE by fb04, merged into claude/fabric.** Hub fan-out `63ed4e8` (design tests FF4, FF5), plus the
store now orders pinned rows only, `8b7d11c`, read and passed by @runtime. `docs/changes/fabric-52-pin-order.md`.
Needs a hub and room restart. Not fixed and the same shape: `/v1/tasks/prune`, now item 92.

@ui wrote a board-only design first, one post per room with an explicit room header. atrium-87300 chose the hub-side
one in `docs/pin-order-rooms-design.md` instead, and @ui agreed. Its other finding stands: `nudgeItems` already
offers "move it up or down" for a pinned card under every sort, from the card menu.

## 53. `setViewport` and `dropViewport` compute under `r.mu` and apply outside it (bug)

Raised 2026-09-28 from sa51's review of the terminal-suite HANDOFF. Both work out the new viewport while holding
`r.mu` and apply it after letting go, so two resizes close together could apply in the wrong order. Never
reproduced. See `D:/tmp/handoffs/terminal-suite-HANDOFF.md`.

Done by sa53. It did reproduce, with a fake pty that yields in `Resize`: without the fix the pty ended a column off
the ring's mark in most runs. A new `runner.resizeMu` is held across compute, guard, mark and resize in both
functions. The shell shares the `runner` type, so it is covered. Nothing else resizes a live runner.

## 54. Terminal test suite part 2: `screen.go` against xterm.js (feature)

Raised 2026-09-28 from sa51's review of the terminal-suite HANDOFF. Part 2 was planned and never started: a
differential test that feeds the same trace fixtures to `screen.go` and to xterm.js and compares the screens, run
with Playwright after an `npm install`. The plan is in `D:/tmp/handoffs/terminal-suite-HANDOFF.md`, and the scratch
tools are in `D:/tmp/handoffs/terminal-suite-tw`.

Done by sa54, as a Go test that shells out to node rather than Playwright (`screen_diff_test.go`,
`screen_diff_cases_test.go`, `testdata/xterm_dump.js`) plus four socket size tests (`attach_size_test.go`). Two real
differences remain as skipped cases: `screen.go` ignores DECSTBM scroll regions, and it gives wide (CJK) characters one
cell. Accepted on purpose: cleared rows go to history, `CSI S` files rows into history, no reflow on a width change.
The bare `CSI H` repaint agrees with xterm.js in both fixtures, so sa74 still owns what to do about it. See
`docs/changes/54.md`.

**Status 2026-09-29: DONE, `1938e74`, landed on claude/main in terminal batch 1 (`e45fc2c`).** The two skipped
cases became item 81 (scroll regions) and item 82 (wide characters), and both are done. `TestScreenAgainstXterm`
now runs with no skips. The three accepted differences are on purpose, so there is no part 3.

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

**Status, 2026-09-29: designed, not built.** `docs/multi-room-design.md`, by @fabric. Recommends sibling rooms and a
room that stops accepting new work before a restart, and NOT the drain to a sibling, because resident sessions never
drain. The goal (a restart kills nothing) goes to a new item: a holder process per runner that outlives the room.
Mercurius review: ready_to_build on round 1, one advisory folded in. Waiting on clint's four open questions.

## 60. The stdio control MCP has sa48's launch fields but no "room is older" warning (housekeeping)

Raised 2026-09-28. The old stdio control MCP (`internal/cli/control_peers.go`) took sa48's model, effort, args and
env fields for item 48, but not the warning the hub's control MCP gives when the room is older than the change.
Decide whether it needs the warning or should go away.

Status, 2026-09-28, sa60: fixed on `claude/sa60`. The stdio MCP is live: `atrium control` registers `atrium_launch`
through `addPeerTools`, so it needed the warning rather than removal. The check now lives in `link.LaunchOptionsDropped`
and `link.LaunchDroppedWarning`, used by both MCPs, and the stdio result reports the model and effort. See
`docs/changes/60.md`.

## 61. A fake 45s hub echo in the lag log from the idle ping and pong (bug)

Raised 2026-09-28, from sa55's review of the live logs. The room pings an idle attach every 45s, and the browser's
pong going up starts the hub's echo clock. So every ping logs a fake echo of about 45000ms, and `hub.err` carries
about 80 lag lines an hour with the board idle. The hub should not start the echo clock on a pong.

Status: fixed on `claude/sa61`. `lagConn.Write` starts the clock only when the Write holds a data frame, and a
control frame read back no longer closes it. The room's own timing starts only on an `in` message, so it was never
affected. See `docs/changes/61.md`.

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

**Status, 2026-09-28: DONE, both stages in claude/main.**

- Stage 1 is 0cbbaa2. The hub routes any request that names a card, path or body, plain or tagged.
- Stage 2 is 8deea51, with CHANGELOG and test plan CD5 and CD6 in 479c9d7. The board sends tagged ids and never
  routes a card write by header, the launch answer is retagged, and the room's not-found names the card and the
  room.
- Design in `docs/card-room-routing.md`.
- Follow-up nobody has asked for: tagged ids in scoped views, and ids minted with their room.

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

Status: fixed on claude/sa65, not merged.

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

Spec (@ui, 2026-09-29), not built. The build is small: `termMenu` in `js/terminal-list.js` gets the same entry the card
menu has (`card-menu.js`, "new context", note "commit, hand off, clear"), calling the same `newContext(id)`, under the
same guard (supervised, and not already mid-cycle). No daemon change. The progress chip it drives is already on the
card and the tab.

The question is only whether it is worth a line on that menu. For: the terminals tab is where you watch a session's
context fill, and Ctrl+Alt+N there is not always reachable (some layouts send Ctrl+Alt for AltGr, which is why the
card menu has it). Against: the card menu already has it, one right-click away, and every entry on `termMenu` makes
the others slower to find.

Open question for clint: add it, or close 71? Recommendation: add it, since it is the one place the operator is
already looking when a context is full, and it costs one entry and no new code path.

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

Status: diagnosed by sa74. Recommendation 1, the height hold, is on claude/main (`387ccd5`, batch 2). Option 2
(OpenConsole ConPTY) and option 3 (replay-only repair) are both designed below and wait on clint's pick. See "2 or
3, for clint" at the end of this item.

### What dropped the lines

**Not atrium's ring, replay or board.** The orchestrator's uncollapsed ring (`/scrollback/raw?collapse=0`, 2,723,702
bytes, pty 206x50) holds the whole reply. At byte 2460305 the pseudo console emits
`\e[46;3H\e[?25h\e[?2026h\e[?2026l\e[?25l\e[H` and then all 50 rows, each ending `\e[K\r\n`, with no line feed
ahead of them. Row 1 of that repaint had been row 11 of the screen just before, so rows 1 to 10 (the table's tail,
Merging, Waiting, Running) are overwritten in place and never reach history. The board's own xterm.js fed those
bytes at a fixed 206x50 loses exactly those rows. The same ring has about 70 bare-home repaints, and 9 of them shifted
the screen: 2, 2, 25, 26, 3, 3, 2, 3 and 10 rows lost.

**The inbox ConPTY's resize path, set off by a change in the pty's row count.** Tested through a throwaway pseudo
console on `conhost.exe` 10.0.26100 (the one the room uses), always with a control that feeds the child's own bytes
straight to xterm.js, which never lost a line:

| What the child and the host did | Lines lost through inbox ConPTY |
| --- | --- |
| Plain and Claude-shaped scrolling (parked cursor, erase and insert, full-width rows, sync output, DECSTBM) | 0 |
| A child process on the console (git bash, cmd, pwsh), focus reports `\e[O` and `\e[I` | 0, and no repaint |
| A resize to the same size | 0, one bare `\e[H` repaint each |
| Columns 120 and 121 alternating | 0 |
| Rows 50 and 49 alternating, under Claude-shaped frames | 18 of 300 |
| Rows 50 to 40 and back, under Claude-shaped frames | 49 to 72 of 300 |
| Rows 50 to 40 and back, under a steady stream | 100 of 300 |

A single row change gives one bare `\e[H` repaint at the new height. conhost files the top rows into its own history,
which is never sent, and the repaint overwrites them downstream. Two changes close together (50 to 40 to 50) are
often painted ONCE: a single 50-row repaint, 50 rows before and after, whose row 1 had been row 11. That is the live
2460305 byte for byte, and it explains why the new reply fits the freed rows exactly.

**Claude Code is not implicated.** Claude 2.1.284 (classic renderer) in a bare 206x50 pty with no resizes produced no
full repaint at all over 4 runs, including a turn taller than the screen and messages typed mid-turn.

What changed the rows at 21:19:59 is not recorded. atrium logs no resizes, and a mark laid and undone with nothing
written between merges away. But the live ring's own repaints show the pty at 47, 48, 50 and 51 rows at different
points of the session. The agreed height is the SHORTEST attached viewer's (`agreedViewport`).

Checked in the code, what can and cannot move the rows for a moment:
- **One viewer's refit that ends at the same size cannot.** `onTermResize` skips when the pane's pixels did not move
  (`terminal-links.js:2096`), sends only when the fitted size changed (`:2120`), and waits for it to hold 250 ms
  before sending what it is by then (`:1914`). The daemon drops a frame at the size the pty already has
  (`supervisor.go:1286`). A wobble held longer than 250 ms does send both sizes.
- **A reattach cannot shrink the rows through its own viewer.** Both sockets carry the same pane's `termFitRows`, and
  a new terminal fits before it connects (`terminal.js:926` then `:930`), so its first frame is never a default size.
- **A reattach of the SHORTEST viewer, with another viewer attached, grows the rows and shrinks them back.** Viewers
  are keyed by socket (`attach.go:285`, `:380`). The board closes the old socket first (`terminal-links.js:833`), the
  daemon drops it as soon as its reader sees the close (the deferred `dropViewport`), and the new socket's size lands
  only after `onopen` (`terminal-links.js:908`). In that gap the agreed height is the next shortest viewer's. With a
  single viewer nothing moves, because the last viewer leaving never resizes (`supervisor.go:1350`).
- **A shorter viewer that attaches for a while (a popped-out window, a phone, a second pane) shrinks the rows**, and
  grows them back when it leaves.

### OpenConsole ConPTY is the fix at the source

`Microsoft.Windows.Console.ConPTY` 1.24.260710001 (conpty.dll plus OpenConsole.exe, MIT) through the same harness:
**0 lines lost in every row above, and no `\e[H` repaint at all**, over 30 to 62 resizes a run. It passes the child's
VT through, so a line feed arrives as a line feed.

What adopting it costs:
- Two binaries per architecture shipped beside atrium (x64 about 1.2 MB together).
- go-pty calls kernel32's `CreatePseudoConsole`, so atrium needs its own create, resize and close through
  conpty.dll. `internal/daemon/conpty_harness_repro_test.go` does it in about 100 lines, with the inbox call kept
  as the fallback.
- At start OpenConsole queries the terminal (`\e[c` and `\e[1t`). A runner with no viewer attached needs atrium to
  answer, or the host waits for a timeout.
- Passthrough changes the byte shapes that `screen.go`, `collapseRedraws`, the cursor settle, the typing gate and the
  replay tests were tuned on, all of which were measured against conhost's re-rendered output. So it needs the whole
  terminal test plan run again.
- Item 81 becomes a prerequisite. The inbox conhost turns a runner's scroll region into plain line feeds and a
  repaint (seen in the DECSTBM repro), so `screen.go` rarely meets one today. Passthrough hands DECSTBM straight to
  it.
- Where the binaries are looked for: the directory the setting names, else the one beside `atrium.exe`. conpty.dll is
  loaded by full path only, never by bare name, and OpenConsole.exe must exist beside it, checked before use, because
  conpty.dll quietly starts the System32 conhost when it is missing (to be confirmed against its source when built).
  Anything else goes to the inbox kernel32 `CreatePseudoConsole`, logged once with the reason: the setting off, either
  file missing, a load or export failure, or a create through conpty.dll that fails.

### Keeping the rows a no-scroll repaint is about to overwrite

Both options detect the same thing and differ only in where they act.

**The strict-match rule.** A candidate is a bare `\e[H` (or `\e[1;1H`) that starts a repaint of the full current
height, meaning as many rows written, each ending in an erase and a line feed, before the next cursor move. There
must be no height change at that byte in the ring's marks, since a resize repaint to a new height is the height
model's business. Buffer the repaint until it is complete. Find the smallest `k` from 1 to rows-1 such that repaint
rows 1 to M equal screen rows k+1 to k+M exactly, where M is at least max(6, rows/4), and at least 4 of those rows are
non-blank and pairwise distinct. On a match, rows 1 to k go to history before the repaint is applied. No match, or
more than one `k` passing, means do nothing.

**False positives.** The rule can only ever ADD rows to history, never remove one. So a wrong call puts a duplicate or
stale line in the scrollback, and a missed call leaves the loss as it is today. The ways it goes wrong:
- Content that legitimately moved up: Claude collapsing a block, a tool's output shrinking, a reprint after `/clear`
  or a compaction. Those files rows the runner deliberately removed.
- Repeated rows (blanks, separators, box borders) aligning at the wrong `k`. The distinct non-blank rows are there
  to stop it.
- A repaint split across reads, which has to be held until it is whole, so the buffer needs a byte and time cap.

**Replay only, in `screen.go`.** The grid model already exists there. Detection costs O(rows squared) per candidate,
and candidates are rare (about 70 in 2.7 MB), so the cost is negligible. It repairs `/scrollback/text`, every attach
replay, and the carryover after a restart, and viewers see nothing new live. It does NOT repair a pane that was
watching when it happened, which is clint's case, until that pane reattaches.

**Live, on the fan-out path.** One screen model per runner fed every output byte (an O(bytes) VT parse on the hot path,
about 50x206 cells a runner), and the matching repaint held back so that `\e[<rows>;1H` plus `k` line feeds can go
ahead of it to every viewer. It repairs the pane clint was copying from. It costs parse CPU on all output of all
runners, latency on every repaint that is held, and a wrong call is shown to every viewer at once. It also becomes
dead code the day the ConPTY is swapped.

### The height hold, as a state rule

Recommendation 1 below, stated exactly. The hold is `heightHold`, 500 ms, twice the board's own 250 ms settle.

**State.** On the runner, beside `views`:
- `views` is updated at once on every frame and every detach, as it is today.
- The applied size is what the pty is at. It is `buf.CurrentSize()`, which only the apply steps below move, so it
  needs no new field. `appliedRows` below means its rows.
- `pendingRows` is a height waiting to be applied, 0 for none. `pendingGen` counts every change to it, and
  `pendingTimer` is the one timer.

**`setViewport` and `dropViewport`**, under `resizeMu` as item 53 made them, after updating `views` and computing
`agreed`. `dropViewport` keeps its two early returns first: after `r.done`, and when no viewer is left.
1. Width is immediate. If `agreed.cols` differs from the applied width, `SetSize(agreed.cols, appliedRows)`, then
   `Resize(agreed.cols, appliedRows)`, then `noteResized`. The CURRENT APPLIED rows, never `agreed.rows`, or a width
   change would carry the new height past the hold.
2. Height is held. If `agreed.rows` equals `appliedRows`, stop the timer and clear `pendingRows`, which cancels a
   flip that came back. If it equals `pendingRows`, do nothing, so the hold keeps counting from when that value was
   first seen. Otherwise set `pendingRows` to it, bump `pendingGen`, and restart the timer for the full hold. A new
   value always restarts it.
3. The re-tell. Whenever a held height is cancelled or superseded, here or when the timer fires, call `noteResized`
   with no `SetSize` and no `Resize`, so every attach re-reads `CurrentSize` and tells its viewer the applied size.

**The timer firing.** Take `resizeMu`, then in order:
1. A `pendingGen` that is not the one it was started with means a later change superseded it. Return.
2. After `r.done`, clear `pendingRows` and return. A dead terminal is never resized, as in `dropViewport`.
3. Re-read `views` under `r.mu`. No viewers left means cancel: clear `pendingRows` and keep the applied size, which
   is what the last viewer leaving already means.
4. Apply only if `agreedViewport(views).rows` still equals `pendingRows` AND differs from `appliedRows`. Then
   `SetSize(agreed.cols, pendingRows)`, then `Resize`, then `noteResized`, keeping mark before resize. A `Resize`
   error is logged, as `attach.go` does now. Anything else clears `pendingRows` without resizing, and re-tells.

**What viewers see during the hold.** `CurrentSize` reports the APPLIED size, never the agreed one, so `tellSize`, the
ring's marks and the replay all agree with the pty. A viewer is told the new height only when it is applied.

**The re-tell cannot start a refit loop.** Each attach sends a `size` frame only when `CurrentSize` differs from
what it last told that socket (`attach.go:479`), so a re-tell with nothing changed sends no frame. A frame at the same
size would be harmless anyway. `takeTermSize` (`terminal.js:65`) only stores the size and calls `applyPtySize`, which
resizes xterm only when the grid differs (`terminal-links.js:1875`) and never sends a `resize` back. Only a fit
(`onTermResize`) sends one.

**Growing waits too.** The transient in the live room was a reattach of the shortest viewer, and that GROWS first
and shrinks back (the list above), so an immediate grow would let the very flip this exists to stop through. A
taller pane held at the old height only shows empty space under the grid for half a second, which costs nothing.
One rule for both directions is also the simpler one to test.

**The cost, named.** A viewer that really is shorter waits half a second while the pty paints more rows than its
grid has. That viewer can garble its bottom rows, and scroll a few into its own scrollback, until the repaint at the
new height. It is one viewer and one resize, against a flip that costs every viewer's history.

**Tests.** Rows flipped 50, 40, 50 inside the hold: zero `Resize` calls. Held past it: one, at the new height. A width
change during a pending shrink: one `Resize` at the new width and the OLD rows, then the shrink when the hold ends.
Every viewer gone before the timer fires, and `r.done` closed before it fires: zero. `CurrentSize` read during the
hold: the applied size. A hold cancelled by the height coming back, and one superseded by a new height: `sizeChanged`
wakes with no `Resize`. Then the harness `flip` runs through the real code path.

### Recommendation

1. **Now, small: stop transient row changes reaching the pty,** by the state rule above. That removes the coalesced
   flips, which are the big losses (10, 25, 26 rows). A deliberate height change still costs about a row per row
   changed, and the width stays immediate. Testable with the harness above.
2. **The real fix: OpenConsole ConPTY**, behind a setting with inbox as the fallback, gated on the whole terminal test
   plan.
3. **Replay-only repair only if 2 is refused.** The live version is not worth its cost and risk next to 2.

Repro, captures and scripts: `HANDOFF.md` on `claude/lost-lines`, `build.claude/lost-lines/` and
`build.claude/conpty/` in that worktree (not committed). That worktree has since been removed, so the captures are
gone. The harness itself is committed: `conpty_harness_repro_test.go`, `conpty_scroll_repro_test.go` and
`conpty_claude_repro_test.go` in `internal/daemon`.

### Option 3, the replay-only repair, designed

Written 2026-09-29 by @terminal so clint can weigh it against option 2. Nothing here is built.

**What it repairs, exactly.** The grid that replays a ring (`screen.applyCuts`, reached through `replayCut`) gets the
rows a no-scroll repaint overwrote back into its history. That grid is behind three things:
- every attach in the default `screen` replay mode, which is a new pane, a reload, a pop-out and a reattach
  (`attach.go`, the `default:` arm),
- `GET /v1/tasks/{id}/scrollback/text` in `screen` mode, which is how clint copies a reply out,
- the pre-restart bytes joined onto an attach by `runner.withCarried`, because they go through the same grid.

**What it does not repair.** The pane that was watching when the repaint arrived. Its xterm.js already overwrote the
rows, and nothing on the replay side reaches it. The operator has to reattach, and a reload does that. It also does
not repair `raw` replay mode, where xterm.js is the only emulator. The "older scrollback" tab needs nothing: it is
`flatten`, which keeps every line ever written, so those rows were never lost there.

**Where it hooks in.** Only `replayCut` turns it on, through a field on `screen` (`keepShifted`). `idleframe.go` and
every other `newScreenSized` caller leave it off and pay nothing.

**The candidate.** A `CSI H` or `CSI f` whose target is row 1, column 1 (bare, `1;1`, or `;`), on the normal buffer
and not the alternate one, with no DECSTBM region set, and with `fixedRows` true. A guessed height is skipped,
because the rule compares whole screens and a guessed screen is not one. Skipped too: a candidate while one is
already open, and the grid's first screenful (nothing is in history and the grid has never been full).

**A candidate at a height change is NOT skipped.** This changes the strict-match rule above, which skipped any
candidate with a height change at its byte (Mercurius `s_ijoTH04DNGvl` round 1, C1). With the height hold in, a real
height change held past half a second is the loss that is left, so skipping those would make the report read clean
exactly when lines are still going. A candidate is AT A CUT when a row cut from `applyCuts` falls between the last
printable character or line feed before the `\e[H` and the `\e[H` itself. conhost's own
`\e[46;3H\e[?25h\e[?2026h\e[?2026l\e[?25l` ahead of the repaint is cursor moves and modes, so it does not separate
them. For those:
- `applyCuts` snapshots the grid and notes the history length just BEFORE `resizeRows`, and keeps the number of rows
  `fitRows` filed off the top (`gone`). The candidate compares against that pre-cut snapshot, not the fitted grid.
- `k` is found the same way, with M from the NEW height, since that is the repaint's height.
- Rows 1 to `gone` of the snapshot are already in history, put there by `fitRows`. Only snapshot rows gone+1 to `k`
  are spliced in, after them. A `k` of `gone` or less adds nothing, and the report says the resize already filed
  them.
- A grow has `gone` 0. A taller repaint whose row 1 is older than anything on the snapshot finds no `k` and adds
  nothing, which is the safe side.
- A second cut before the repaint completes still cancels it, reported as such.

**Opening one.** Copy the grid's rows into a snapshot buffer held on the screen and reused, and note the history
length. The copy is rows times cols cells, about 10,000 for 206x50, and candidates are about 70 in 2.7 MB of ring.

**Following it.** Each line feed while the candidate is open records the row just finished as text, SGR stripped
and trailing blanks trimmed. That is the repaint's row `n`. Recording at the line feed rather than at the end is
what keeps it right when the last row's `\r\n` scrolls the grid, as a 50-row repaint of `\e[K\r\n` rows does.
Printable text, `\r`, `CSI K` and SGR are the only things allowed while it is open. One exception: once exactly
`rows - 1` rows are recorded, the next cursor move CLOSES the candidate instead of cancelling it, and the row the
cursor was on is recorded as the last one first (see "Closing one"). Anything else cancels it without a word:
another cursor move, `CSI J`, insert or delete lines, a scroll, DECSTBM, the alternate screen, a size cut
from `applyCuts` after the `\e[H` (a cut just before it is the case below), or more than 64 KB consumed since it
opened.

**Closing one.** Complete at `rows` line feeds, or at `rows - 1` line feeds followed by a cursor move, which is how a
repaint that does not end in `\r\n` finishes. Then the strict-match rule above, with the recorded rows against the
snapshot: the smallest `k` from 1 to rows-1 where repaint rows 1 to M equal snapshot rows k+1 to k+M, M at least
max(6, rows/4), and at least 4 of the matched rows non-blank and pairwise distinct. Exactly one passing `k` or
nothing. On a match, what is spliced depends on the candidate:
- Not at a cut: snapshot rows 1 to `k`, at the history length noted on open.
- At a cut: only snapshot rows gone+1 to `k`, at the pre-cut history length plus `gone`, which is right after the
  rows `fitRows` filed. A `k` of `gone` or less adds nothing and reports `resize-filed`.
Either way they sit ahead of anything the repaint's own last line feed scrolled off.

**Text, not SGR, is compared.** conhost re-renders its buffer, so a row's colours can come back as different bytes
for the same look. Comparing text is what makes a real shift match, and the distinct-rows rule is what stops blank
rows and box borders matching at the wrong `k`.

**A diagnostic first, in the same change.** `GET /v1/tasks/{id}/scrollback/text?repair=report` returns the
replay with one line per candidate instead of the text. It is the only way to know how often this still happens now
that the height hold is in. Without it option 3 is built blind. Tab-separated, one header line, these columns:
- `offset`, the byte of the `\e[H` in the replayed bytes,
- `rows` and `cols`, the grid's size at the candidate, and `cut`, the rows before a cut it sits at or `-`,
- `outcome`: `repaired`, `resize-filed` (k was `gone` or less), `no-match`, `ambiguous` (more than one k),
  `cancelled`, or `incomplete` (the ring ended inside it),
- `k`, and `added`, the rows spliced into history, both 0 when nothing was,
- `why`, what cancelled it or which condition failed, empty otherwise.
The last line totals each outcome and the rows added, so two readings a day apart compare at a glance.
`repair=report` always renders in `screen` mode whatever `mode` says, and ignores `ansi` and `collapse`. It takes
its bytes and its cuts from ONE `ReplayCuts` call, so every cut's offset is in the stream it is compared against.
`collapse=0` swaps in `Snapshot()`, which is the same stream today (both are `from(retainedStart())`, and nothing
collapses any more), but it is taken under a second lock, and a ring that wraps between the two moves the offsets.
The repair itself only ever runs inside `replayCut`, which is handed bytes and cuts together, so this is a rule for
the report alone. It replaces the `[atrium] ... mode` banner as well: the body is the report and nothing else. The
totals line starts with `totals` and uses the same tabs, `outcome=count` pairs then `added=N`. `?kind=shell` works
as it does today, so a card's shell can be reported on too.

Reviewed by Mercurius `s_ijoTH04DNGvl`: five rounds, one major finding in each of the first four (a repaint at a
height cut was skipped, a cursor move both cancelled and closed a repaint, two splice rules for a cut, and cut
offsets across two locks), each fixed above. Round 5 is ready_to_build, and its one advisory is the `kind` line.

**Tests.**
- Unit, in `screen_test.go`: a 10-row grid, 10 numbered lines, then `\e[H` and 10 rows `\e[K\r\n` starting from
  old row 4. History gains rows 1 to 3 in order. The same with the last row not ending in `\r\n`.
- The false positives, each must add nothing: a repaint identical to the screen (`k` 0), a repaint of mostly blank
  rows and one border, a real collapse where rows moved up because content was removed (asserted as a named, known
  duplicate, since the rule cannot tell it apart when the rows match), two `k` values passing, a second cut inside a
  repaint, a repaint under DECSTBM, and one on the alternate screen.
- At a cut: rows 50 to 40 with conhost's repaint shifted by 12 adds 2 rows (the grid filed 10), shifted by 10 adds 0
  and reports `resize-filed`, and a grow from 40 to 50 adds 0.
- A fixture from the committed harness. `TestConPTYScrollRepro` drives rows 50, 40, 50 through the inbox ConPTY with
  Claude-shaped frames. Capture its host-side bytes once into `testdata/`, and assert the replay keeps every
  numbered line the child wrote, where today it loses 49 to 72 of 300. This is the test that says it works.
- `HEADLESS_ONLY` needs nothing new: the board is not touched.

**Cost.** One worker. About 200 lines in `screen.go` with the cut case, about 50 for the report, about 350 of
tests. No new binary, no setting, nothing shipped, nothing packaging has to learn. It runs only on replay, so it
adds nothing to the output path. A wrong call puts a duplicate line into history, never removes one. It becomes
dead code on the day option 2 lands, and removing it is deleting one field and its code.

### 2 or 3, for clint

What the height hold (`387ccd5`) already stopped: the coalesced flips, which were the big losses (10, 25 and 26 rows).
What is left: a height change held past half a second, such as a shorter pop-out or phone that stays attached, costs
about a row for each row changed. How often that happens now is not known, which is why option 3 starts with the
report.

| | Option 2, OpenConsole ConPTY | Option 3, replay-only repair |
| --- | --- | --- |
| Fixes the pane that was watching | yes | no, a reload repairs it |
| Fixes attach replay and `/scrollback/text` | yes | yes, in `screen` mode |
| Fixes `raw` replay mode | yes | no |
| Where it acts | at the source, conhost is gone | after the fact, on replay only |
| Wrong answers | none known: 0 lost, no `\e[H` repaint, over 30 to 62 resizes a run | a duplicate line in history, never a lost one |
| New things shipped | conpty.dll and OpenConsole.exe per arch, about 1.2 MB, MIT notice | nothing |
| New code | own create, resize and close through conpty.dll (about 100 lines, in the harness), answering `\e[c` and `\e[1t` for a runner with no viewer, the setting, the fallback | about 250 lines in `screen.go` and the report |
| Retesting | the whole terminal test plan: passthrough changes the byte shapes `screen.go`, the cursor settle, the typing gate and the replay tests were tuned on | its own tests only |
| Packaging | every install path in `docs/packaging.md` has to carry two more files | none |
| Prerequisites | item 81 (DECSTBM in `screen.go`), now on claude/main | none |
| Size | two or three workers, one of them in packaging, plus a full plan run | one worker |
| Afterwards | the root cause is gone | dead code once 2 lands |

**@terminal's recommendation.** Build option 3's report on its own first, which is a day's work at most, and read it
on the live room for a few days. If it shows almost nothing, the height hold was enough and neither option is worth
its cost yet. If it shows real losses, option 2 is the fix. Option 3's repair is worth building only if option 2 is
refused, or as a stopgap while option 2 is built, because it cannot fix the pane clint was copying from.

## 75. sg3 as a room, and machine bootstrap reuses the operator's shared folder under `localai` (feature)

sa75 made sg3 a room of this hub. Its `provision-room` fix (`5ced807`, old SHA) is merged. A smoke card on sg3
(2026-09-28) ran Claude and reported system stats, so Claude is signed in there. Found by that card: CIM and WMI are
access denied inside the sg3 room's session, so any worker that reads uptime, services or scheduled tasks through CIM
fails there. Find out which account and session type the room runs in, and whether provisioning should give it more.

clint, 2026-09-28: bootstrapping a machine should reuse a shared folder for the main operator, and the account
should be `localai`, not `claude`. Today sg3 runs as `claude` in `C:\Users\claude`. Fold both into the provisioning
script before the next machine is added.

**Status, 2026-09-29, @fabric: sg3 is a working room, and the account half has not started.** Done:

- sg3 is attached, and its `provision-room` fix is merged. The smoke step now proves a room end to end, and did on
  sg3 (`35fa4a1`, see item 46).
- The CIM denial no longer breaks provisioning: every scheduled task action goes through `schtasks.exe` (`35fa4a1`).
- Work on sg3 comes back by git: `room-git.ps1` (`5145bad`). fb03 was built and committed there, so the path is used.

Left:

- **CIM inside the room.** The scripts avoid it, but a worker in the sg3 room that reads uptime, services or
  scheduled tasks through CIM still gets access denied. Which account and session type the room runs in has not
  been checked.
- **The `localai` account and the shared operator folder.** Not built. Creating an account needs admin, which
  provisioning avoids today on purpose.

Open questions for clint:

1. What is the shared folder? A path on each machine (for example under `C:\Users\Public` or `/Users/Shared`), an
   SMB share, or a folder synced from here, and what goes in it: the repo clones, the toolchain, runner config?
2. Does `localai` replace `claude` on sg3, which means moving a live room, or only apply to machines added from now
   on?
3. May provisioning create the account and so need admin, or does the operator make `localai` by hand first and
   provisioning start from there?
4. Should the room run so that CIM works for its workers, or is a worker that needs CIM told to go without?

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

Status: a and e built on claude/sa77a, not merged. `scripts/merge-check.ps1` and `scripts/setup-merge-worktree.ps1`.

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
- **Held back means recorded, not dropped.** Today the drawer is filled by `toast` (through the toast-log wrapper)
  and by the one explicit `logNotification` call on the desktop branch. Off skips both `toast` and
  `showNotification`, so the off path calls `logNotification` itself, once per alert, and plays no sound. Without that
  the switch would mute everything and silently empty the drawer it promises to fill.
- **Permission requests still notify. Built that way, waiting on clint.** A permission blocks a session until a
  human answers, the same exception item 44 makes, so off lets them through the normal `notify` path. The choice is
  ONE named constant in `js/notify.js` (off silences permissions: false), so flipping it is one line.
  `docs/changes/79.md` names it as waiting on clint. The button's tip says permissions still come through.
- **Per browser, in `localStorage`** (`atrium.notify.off`), like the sound mute (`atrium.sound`). A phone and a desk
  want different answers, and the daemon has no notion of which browser is which. Every window of one browser shares
  it, and a popped-out window follows the board.
- **The bell shows it.** Off, the bell is drawn as a struck bell (U+1F515) with the tip "notifications are off. click
  to see what arrived", and the badge still counts. On, it is the bell it is today. The toggle button and the bell
  carry the same text as their `aria-label`, repainted with the state.
- **Item 44.** 44 is a filter on WHICH cards notify (not agent-launched ones). 79 is a master switch over all of
  them. Off beats everything, including a card's own per-card override, because it is the operator saying stop now.
  On, 44's filter and per-card overrides apply as they do today. The gear's notifications section shows the same
  switch, so the two are found in one place.
- **Not built.** A timed mute ("for an hour") is the obvious next step and is left out until asked for.

Open question for clint: should "off" silence permission requests too? The build says no, since a session blocks on
one until somebody answers, and a muted board is the likeliest place to forget one. Flipping it is the one constant
above.

Review: Mercurius round 1 (s_12NjmPjbUY9e) found the permission question left formally open (C1, closed above as
built-no, flippable) and no record-only path for held-back alerts (C2, folded as the `logNotification` rule). Its
advisory, an `aria-label` that follows the state, is folded too.

Status 2026-09-29 (branch `claude/sa79`): built as written. `notifyHeld` in `js/notify.js` gates both `play` and
`notify`, and the off path calls `logNotification` once per alert. The permission question is still waiting on clint,
held in `NOTIFY_OFF_SILENCES_PERMISSIONS`. The bell glyph moved into its own `.glyph` span so it can be repainted
without touching the badge. Headless section `notifyOff`. No timed mute.

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
  client divides by bucket width for tokens per minute. No per-kind dollars in the first build: a row sums
  replies that may come from more than one model and keeps only the last model's name, so repricing its kinds
  would misattribute money silently. The split chart shows tokens per kind and the stored total cost. Exact
  per-kind dollars would need item 37 to store more first. (Round 3, C1.)
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
     each part's tokens and the range's total cost beside it.
  4. Cost: cumulative estimated dollars over the range, and a table of cost by cause (you, a say, restart wake,
     keep-alive, resume, subagent) so a restart or a keep-alive round shows as the spend it was.
- **Labels are item 78's.** prompts and calls, uncached in, out, cache read, cache write 5m and 1h, est. The tips in
  `USAGE_TIPS` (`js/usage.js`) are reused, not rewritten.
- **No chart library.** Hand-drawn inline SVG in a new `js/usage-charts.js`, a few hundred lines: a stacked area,
  a bar, a line, axes and a hover readout. No CDN and no vendored library, since the board has to work offline and
  there is no build step. Colours are skin variables (the palette triples), so every skin draws it.
- **Rooms.** The board already talks to more than one room. The tab asks each attached room with an explicit
  room-scoped read (the `X-Atrium-Room` pattern `loadRoomCfg` uses, not the aggregate fetch) and merges the buckets,
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

Status (branch `claude/sa80`): built as written, at turn end. `GET /v1/usage` takes one addition the design did not
have, an optional `card` id, because a bucket carries causes only for the whole board and a card-filtered tab could not
show that card's cause table otherwise. A card filter reads that one card from its own room. The tab, the small chart
in a card's details, and the `usage` event are in. Not done: watching a turn spend while it runs, and any per-kind
dollars. Charts were drawn in a headless run against mocks only, not yet looked at against a live room.

## 85. The headless board run flakes under load (bug)

Raised 2026-09-29 by the orchestrator. On m1mini the terminated-terminal dismiss check in
`scripts/test-board-headless.js` failed while `go test` ran beside it, and passed on an idle rerun. Its waits are
fixed at 15000ms, and a wait that runs out there either throws out of the run as "the headless run threw" or fails with
a message that does not say which wait it was.

Fix: every Playwright timeout in the script goes through one scale, read from an environment variable (a factor, 1 by
default), so a loaded machine can be given more time without editing the file. Each wait in the dismiss block, and
any other wait whose failure names only the symptom, says which wait ran out and how long it had. Owner @ui, queued
behind item 80.

Status 2026-09-29, done on `claude/ui`. `HEADLESS_SLOW` scales all 191 timeouts, not the sleeps. The dismiss block
names its three waits, and its menu wait opens the menu again if a render closed it, which is the likelier flake
than a slow browser. A full run with `HEADLESS_SLOW=3` beside `go test` passed. Other sections still fail on the
symptom only when a wait runs out, and get the same treatment when one is caught flaking.

## 81. `screen.go` ignores DECSTBM scroll regions (bug)

Found 2026-09-28 by sa54's differential test (item 54). `screen.go` never reads `CSI top;bottom r`, so a runner
scrolling inside a region scrolls the whole grid and files rows into history that stayed put on a real terminal. In
the fixture `screen.go` scrolled 3 rows off where xterm.js scrolled 5, with the cursor on row 5 against 3. This
shows in the attach replay and the text scrollback view whenever a runner uses a scroll region. The case is in
`internal/daemon/screen_diff_cases_test.go`, skipped as `backlog-2 81` until it agrees, and it fails once it does
so the skip gets removed. Owned by @terminal.

**Done by sa81 on `claude/sa81`.** `screen.go` now keeps a scroll region per buffer and follows xterm.js: line feed,
RI, `CSI S/T/L/M` act inside it, rows leave to history only when the region starts at the top, an invalid one is
ignored, and RIS, a resize and the alt screen reset it. The skip is gone and thirteen differential cases cover each
rule. The text replay (`textAtRows`) now writes the region back before the cursor. `collapseRedraws` needed no change.
DECOM is not implemented, since nothing here uses it.

## 82. `screen.go` gives a wide character one cell (bug)

Found 2026-09-28 by sa54's differential test (item 54). `screen.go` gives every rune one cell, and xterm.js gives
CJK and other wide characters two. A cursor move back over a wide character lands on the wrong column: `あ.う`
against `あい.`. A fix needs a continuation cell handled in `render`, `writeRow`, and the erase and insert ops. The
case is skipped as `backlog-2 82` in `internal/daemon/screen_diff_cases_test.go`. Owned by @terminal.

Status: built by sa82, merged into claude/terminal. `screen.go` gives a wide character a head and a continuation cell, widths
come from a table generated from the vendored xterm.js, and the skip is gone. Differential cases cover each op,
all agreeing with xterm.js apart from one accepted reflow difference. See `docs/changes/82.md`.

### Design

Written against `screen.go` before sa81 (DECSTBM) merges. Sa81 lands first and this lands second, so the cell ops
below are named by what they touch, not by line.

**How width is decided.** The board loads `@xterm/xterm` 5.5.0 (`internal/api/web/vendor/VERSIONS.md`) and
`index.html` loads no unicode addon, so xterm runs its DEFAULT provider, `UnicodeV6`, and that is the table to match.
It is not what `golang.org/x/text/width` (in `go.mod`, indirect only, Unicode 15 based) or `go-runewidth` say. The
visible difference is that V6 gives every astral emoji (U+1F300 and up) width 1, and only U+20000..U+2FFFD and
U+30000..U+3FFFD width 2 above the BMP, so a grinning face is ONE cell on the board today. Matching a newer table
would leave the replay one column off per emoji, which is this bug in the other direction. No new dependency: a
generated table, `screen_width.go`, holding the BMP wide ranges, the zero width (combining) ranges and the two astral
wide ranges, produced by `testdata/gen_widths.js` from the vendored `xterm.js` (`UnicodeService.wcwidth`). A parity
test runs the same node script over every code point 0..0x10FFFF and compares with the Go function, so an xterm
upgrade, or a unicode11 addon, fails a test instead of drifting. Lookup is a `< 0x7f` fast path, then a binary search
over a few hundred ranges.

- Ambiguous width is 1. V6 has no ambiguous class.
- Combining marks, ZWJ (U+200D), variation selectors (FE00..FE0F) and other zero width code points are width 0. They
  attach to the cell before them and take no cell. VS16 does not widen its base, again as V6.
- A ZWJ emoji sequence is therefore several width 1 emoji with joiners attached, which is what xterm shows. No
  grapheme segmentation, so no `rivo/uniseg`.
- A zero width code point with nothing to attach to (column 0, or after another loose mark) is NOT dropped: xterm
  gives it a cell of its own that moves the cursor one column. The differential settled this, and `put` does the
  same. It attaches to a blank cell like any other.
- C0 and DEL stay in `step`, unchanged.

**The cell model.** `cell` is `{ch rune, sgr string}`, 24 bytes. It becomes `{ch rune, ext uint32, sgr string}`, still
24 bytes, since the rune leaves 4 bytes of padding.

- `ch == contCh` (a negative sentinel) marks the second cell of a wide character. It carries the head's `sgr`, so
  per cell colour ops stay per cell.
- `ext` is an index into `screen.combs []string` (0 means none) holding the marks appended to that cell. Marks are
  rare, so the table stays tiny and rows stay flat and copyable. It lives as long as the screen, which is as long as
  any row that refers to it, history included.
- Every wide head is followed by exactly one `contCh`, and every `contCh` follows a wide head. The ops below keep
  that true, and the differential asserts it after every case.

**Per op.** Where I say "as xterm" from memory of its `InputHandler` and `eraseInBufferLine`, phase 2 confirms it in
the differential and takes xterm's answer where they differ.

- `put`: width 0 appends to the previous cell (the head, when the previous is a continuation) via `combs`, moves
  nothing and leaves `wrapNext` alone. Width 1 writes one cell. Width 2 writes head and continuation and advances
  two. Before writing, `clearHalf` repairs what is overwritten: writing on a continuation blanks the head before it,
  writing on a head blanks its continuation, and a width 2 write whose second cell lands on the head of another wide
  character blanks that one's continuation. Blanked cells are the plain `blank` value.
- A wide character at the last column wraps early. `put` sees `col == cols-1` with width 2, wraps first and writes on
  the next row. The differential settled the abandoned last-column cell: xterm BLANKS it, wearing the colour being
  written, so a character already there is gone. xterm clamps a terminal to two columns, so the one column guard is
  only a guard.
- A cursor landing on a continuation (`CUB`, `CHA`, `CUP`, backspace, restore, tab) stays there. The next write
  repairs through `clearHalf`. Nothing snaps the cursor to the head.
- Erase (`EL`, `ED`, `ECH`): the blanked span widens to whole characters. A span starting on a continuation blanks
  the head before it, and one ending on a head blanks the continuation after it. `ED` goes through `EL` plus
  whole-row blanks, so it inherits this.
- `ICH` and `DCH`: after the shift, a continuation left first in the moved run, or a head left last with its
  continuation shifted off, is blanked. That is `clearHalf` at the two seams.
- `IL`, `DL`, `CSI S` and `CSI T`: move whole rows, so a wide character and its continuation always move together.
  No change. Under sa81's regions that still holds, since a region chooses WHICH rows move and never cuts a row.
  Nothing there reads columns, and there is no left and right margin mode (DECLRMM) to cut one. Sa81's row copies
  must stay row copies, and if they ever copy a column span this design needs another pass.
- `resize` narrower: a cut between head and continuation blanks the orphan head. Widening pads blanks. History rows
  keep the width and cells they had. `fitRows` and `applyCuts` move rows, never cells, so they are unchanged. The alt
  screen is `[]cell` too and gets the same cut.
- `rowIsBlank` counts a continuation as not blank, which is safe because one only follows a non-blank head.

**Output.** `render` and `writeRow` skip `contCh` cells and write the head rune once, followed by its `ext` marks. The
end-trim loop trims only blanks, so a wide character in the last two cells is kept. `textWithCursor` needs no change:
`s.col` is already a cell column, `CUF` in cells is what the terminal executes after painting the rune in two of
them, and the relative move stays right. `textAtRows` ends in an absolute `CSI row;col H` in cells, correct for the
same reason. The scrollback view and the replay therefore emit each wide rune once and let the attaching xterm give
it two columns.

**Cost.** Memory: nothing per cell, 24 bytes as now. Time: `put` gains one comparison on the ASCII path, a binary
search on other runes, and two neighbour reads for `clearHalf`. Replay parse cost is dominated by `decodeRune`, which
allocates through `[]rune(string(...))` for every non ASCII rune, so a CJK heavy ring pays it per character. Worth
replacing with `utf8.DecodeRune` while here. It is a small change outside the cell model, and I will measure both
with a benchmark added in phase 2.

**Tests.**

- `TestWidthMatchesXterm` (node, every code point, skips without node) and `TestWidthTable` (no node: 2 for a
  hiragana letter, 1 for an astral emoji, 0 for a combining acute and for U+200D, 2 for U+20000).
- No node: wide put and overwrite of each half, wide at the last column, wide in a one column grid, a mark on a wide
  head, each erase and `ICH`/`DCH` seam, `resize` across a wide character, and the head and continuation invariant
  after each. `render` and `writeRow` emit the rune once, and `textWithCursor` lands on the right column after wide
  text.
- Differential, the `backlog-2 82` skip removed: the existing case, overwrite the first half, overwrite the second
  half, wide at the last column, `EL` 0, 1 and 2 through a wide character, `ECH`, `DCH` and `ICH` at a seam, a
  combining mark after ASCII and after a wide character, a ZWJ sequence, an astral emoji (width 1, the V6 result) and
  an ambiguous character. Each agrees with xterm.js or carries an `accept` with the reason. `dumpScreen` learns to
  skip continuations and carry marks, and asserts the invariant.

## 83. `atrium_say` refuses a card in `done` while its terminal is still alive (bug)

Found 2026-09-28 by @runtime reviewing sa21. `atrium_report` with status `done` moves the worker's card to `done`,
and the session keeps running at its prompt, as the brief asked, so the director can send review changes. The review
`atrium_say` then answered `undeliverable`: "has no running session (it is done). resume it first". `atrium_task` on
the same card said `atrium_owns_terminal: true` and `doing: idle`. The only way through was a board PATCH moving the
card back to `needs-input` by hand.

The refusal reads the card's status and not the runner. Expected: a say to a card whose terminal atrium owns is
delivered whatever column the card is in. A `done` card with no runner still refuses, and says so. Item 41 is about
which prompts make a card owe a report, not the card's state after one, so this is separate. Owned by @runtime.

Status: fixed on `claude/sa83`. `sessionGone` is now a daemon method that also asks the supervisor, and `atrium tell`
uses it. See `docs/changes/83.md`.

## 84. Two `nosession` tests fail on macOS and Linux (bug)

Found 2026-09-28 on m1mini (macOS arm64, `hub-main` 52ca01a). `TestASayToAGoneSessionIsUndeliverable` and
`TestAHeldMessageOnAnEndedSessionIsDropped` (`internal/daemon/nosession_test.go`) fail every time there.

The cause is the fixture. `cardFor` (`internal/daemon/help_test.go:34`) registers every test card with `PID: 1`, and
`sessionGone` (`nosession.go`) calls a card gone only when `!processAlive(pid)`. On Windows nothing is PID 1, so the
card reads gone. On macOS and Linux PID 1 is launchd or init. `Signal(0)` from a non-root user returns `EPERM`, which
`alive_other.go` counts as alive on purpose, so the card never goes. Linux CI would fail the same way.

Fix, test only: give `cardFor` a PID that is guaranteed dead. Prefer a helper that starts and reaps a short child and
returns its pid, over a large constant. Then grep the `_test.go` files for other `PID: 1` style assumptions.
`processAlive` is right and does not change. Verified on m1mini with pid 2147483000: both pass, and so does the
whole package. Owned by @runtime.

## 87. Two keep-alive refreshes rewrote the whole context (bug)

Found 2026-09-29 by @runtime writing the item 39 spec (`docs/keepalive-marked-spec.md`), from `keepalive_refresh` and
`session_usage` on a COPY of the live database. Of 60 refreshes, two wrote most of the context and read almost none of
it, which is the rewrite keep-alive exists to avoid, and cost $1.78 of the $5.34 keep-alive spent in all:

| Card | At (UTC) | Context | Read | Written | Cost | ttl_left_s |
| --- | --- | --- | --- | --- | --- | --- |
| `01a0e960-fc85` inputlag-env-leak | 2026-09-28 20:13:00 | 123,752 | 0 | 127,952 | $1.02 | 266 |
| `01a0e8f0-ecda` tlsuv sch-credentials | 2026-09-28 21:03:28 | 99,886 | 10,259 | 94,346 | $0.76 | 222 |

Both are `atrium:lean` cards, launched and resumed with `--mcp-config ~/.atrium/mcp.json --strict-mcp-config`. Every
refresh that hit was on a card that is not lean. A lean card runs with its own tool list, MCP config and system
prompt, and the fork carries none of them, so its prefix differs from the first token (read 0) or right after the
system prompt (read 10k). The TTL was not the cause: both had about four minutes left.

**Diagnosis: already fixed by item 70**, fa2b2cc (2026-09-29 01:19Z). `decide` in `internal/daemon/keepalive.go` skips
a card tagged `atrium:lean` ("lean card: a refresh cannot rebuild its prompt"), and its comment cites the $1.02 row
above. Lean is decided by the same tag at launch (`lean.go`), so a lean card cannot lack it. On the copy there is no
refresh on a lean-tagged card after the fix. Item 73 (sa73, on m1mini) is the step after this: a fork that carries a
lean card's prompt, so those cards can be warmed rather than skipped.

The other four refreshes recorded as `miss` read the whole context and wrote 3k to 8k for $0.04 to $0.09 each. They
were effectively warm, and are only labelled `miss` by the outcome rule item 70 also changed. No worker needed. Close
once a room running fa2b2cc or later shows no full-write refresh for a day.

## 86. `screen.go`'s combining-mark table only grows (bug, low)

Found 2026-09-29 reviewing item 82. A cell carrying combining marks points into `screen.combs`, and every mark
attached appends a new string there, even one already held. Nothing ever removes an entry: not a clear, not RIS,
not a row leaving history. So the table fills to `combsMaxKept` (65536), and past that `combine` drops every new
mark. Memory is bounded (65536 entries of at most 32 bytes), so this is not a leak, and it did not block item 82.
Owned by @terminal. Low priority.

**Status: done on `claude/sa86`.** Built as designed: `combIdx` interns sequences, `combsMaxKept` and `combMax` are
unchanged, and the ASCII apply benchmark still allocates 211 per 200 lines. Tests are in `screen_comb_test.go`.

### Design

**How long a screen lives.** Not as long as a session, which the first filing assumed. `screen` is built fresh
from the ring on every render and thrown away after: `replayCut` (attach replay), `renderHistory` (the text
scrollback view) and `classifyFrame` (the looks-idle badge, a 64KB tail) each call `newScreenSized`. So the table
is bounded by what one ring holds, and the ring is `scrollback_mb`, 16MB by default and up to 512MB. That is
still far past the cap, and the cap bites where it hurts most: the marks dropped are the LAST ones, which is the
newest text, the part on screen.

**Who reaches it.** A combining mark is at least two bytes of UTF-8, so the cap is about 128KB of marks in a 16MB
ring. Precomposed Latin (NFC Vietnamese, French) never gets here, since those are single code points of width 1.
What does get here is any script whose vowel signs are combining: Devanagari, Bengali, Tamil, Thai, where most
syllables carry one. Also VS16 (U+FE0F) after an emoji, which Claude Code and many CLIs print in status lines, one
per redraw. A spinner line redrawn every second with a VS16 in it spends one entry per redraw, and a long session
spends the cap on a spinner before any real text needs it.

**The fix is interning, and only interning.** `screen` gains `combIdx map[string]uint32`. `combine` builds the
new string (`prev+string(ch)`), looks it up, and only appends when it is new. `ext` is unchanged, `emit` is
unchanged, and the "entries are never changed once added" rule still holds, so a cell copied into history or the
alternate screen still means the same thing. What the cap then counts is DISTINCT mark sequences. Real text has a
few hundred at most (the string is the marks alone, never the base, so every `ka` with a vowel sign `i` shares
`ि`). The cap becomes something only a hostile or corrupt stream reaches, which is what it is for.

- Keep `combsMaxKept` and `combMax` as they are. With interning, 65536 distinct sequences of up to 32 bytes is
  about 2MB plus the map, per render, only under hostile input. Past the cap a NEW sequence is still dropped.
  One that was already interned is still attached, which the current code cannot do.
- The map is made lazily, on the first mark, so an all-ASCII render allocates nothing new. The ASCII apply
  benchmark from item 82 (211 allocs per 200 lines) must not move.
- A render is one goroutine, so there is no lock. Nothing else shares a `screen`.

**Why not compaction.** Renumbering `ext` on a history trim or a reset means walking every live and history cell
and rebuilding the table. Nothing trims history within one render (`history` only grows, and RIS clears the grid
but keeps it), so there is no trigger to hang it on, and a render ends before it would matter. It would be a pass
over up to 16MB of cells to save memory the interned table no longer uses. Rejected.

**Tests.**

- A spinner fixture: one row redrawn 100,000 times with `\r` and a VS16, then a Devanagari line. The table holds
  2 entries (VS16, and the vowel sign), and the Devanagari line renders with its marks.
- Past the cap: 65536 distinct sequences (a loop over pairs of combining code points, each pair its own
  sequence), then an ASCII letter with a mark that was already interned. It keeps the mark, and a NEW sequence
  after that is dropped. This is the cap still working and interning still answering.
- The differential cases from item 82 still agree, and the ASCII and CJK apply benchmarks are re-run and
  reported next to the numbers in `docs/changes/82.md`.

## 88. The looks-idle classifier writes U+FFFD for the second half of a wide character (bug, low)

Found 2026-09-29 merging claude/main (item 21) into claude/terminal (item 82). `classifyFrame` in
`internal/daemon/idleframe.go` walks `sc.cells` itself and writes every `c.ch`, turning only `0` into a space.
Item 82 gave a wide character a continuation cell holding `contCh` (-1), and `WriteRune(-1)` writes U+FFFD. So
every CJK character on screen reaches `classifyScreen` followed by a replacement character. The two branches
could not see each other, so neither test caught it.

It is low because nothing `classifyScreen` looks for sits in a wide character's second half: the rules are box
drawing (width 1), the prompt is `>`, the veto strings are ASCII plus `…`. A wrong badge would need a veto string
split by one. It is still wrong, and the comment at `idleframe.go:88` names items 81 and 82 as open gaps, which
they no longer are.

Fix: skip `contCh` cells in the loop, the way `writeRow` does, and update that comment. A test puts a CJK
prompt inside a real idle frame and asserts the text handed to `classifyScreen` directly, not only the verdict,
since the heuristics mask the bug: each wide character appears once, there is no U+FFFD, and the frame still
reads idle. Owned by @terminal, built with item 86 by the same worker.

**Status: done on `claude/sa86`.** The loop is now `frameText` in `idleframe.go`, which skips `contCh` cells, and the
stale comment is rewritten. `idleframe_wide_test.go` asserts the text handed to `classifyScreen` directly.

## 93. The screen model's real-session tests read the live scrollback, so every full suite fails on machine data (bug)

Filed 2026-09-29 by the orchestrator, from @fabric's note. `internal/daemon/screen_real_test.go` has four tests
(`TestEveryRealSessionReplays`, `TestRealSessionsKeepTheirText`, `TestReplayIsNotQuadratic`,
`TestRealSpinnersCollapse`). All four replay every `.scrollback` over 200KB in the hardcoded directory
`C:/Users/claude/.atrium/scrollback`, which is the live room's own ring. So the corpus is whatever this machine did
this week. It is about 215MB today and the largest file is 35MB. It grows with every session, and a new card can
fail the suite without a line of code changing. sa74's card `01a0eac2` is full of lost-lines repros and keeps 26%
of its sampled words, under the 40% floor. Every merger has to rerun it alone and write it off, and
`scripts/merge-check.ps1` carries it in `$Flaky` as load noise, which it is not. On any other machine all four skip,
so CI checks nothing.

Owned by @terminal.

**The fix.** Split the one corpus into two, and let each answer a different question.

1. **A frozen corpus in `internal/daemon/testdata/scrollback/`, run every time.** A few captures, fixed bytes, and
   numbers that never move. This is the regression suite.
2. **The live corpus, run on purpose.** Keep the same four tests, but only when `ATRIUM_REAL_SCROLLBACK` is set.
   Its value is a directory, or `1` for `<home>/.atrium/scrollback` from `os.UserHomeDir`, which also removes the
   hardcoded user. Use it to ask "does the renderer survive what this machine has emitted lately", before a
   screen.go change or when chasing item 74. Unset, the tests skip with a message that names the variable.

**An env var, not a build tag.** A build tag keeps the file out of `go vet` and out of the compile on every normal
run, so it rots unseen. With an env var the code compiles every time and costs one `Getenv`. It is also how the
other live-data tests already gate: `ATRIUM_LIVE_COPY` in `internal/store` and `ATRIUM_CARRY_LIVE` in
`carry_join_test.go`.

**What goes in the fixtures. This is the part that needs care.** The repository is public on GitHub, and a live
scrollback is a transcript. It holds the operator's prompts, file contents, paths, and anything a tool printed.
Nothing is copied from `~/.atrium/scrollback` into `testdata`, so no scrubbing pass is needed and nobody has to
review a megabyte of escapes. Instead:

- **Capture on a throwaway room** (own `ATRIUM_LOCATION`, ports and dirs, per the brief) from a runner doing
  scripted work on atrium's own public source. Read a few files, run `go vet`, write a long reply, and sit through
  a spinner. That produces what the tests need, which is claude-code's real repaint behaviour: synchronized
  output, cursor-home redraws, `\r` spinner frames, a long reply past one screen, and a row change. The content is
  public by construction.
- **Three files, each under 1MB and about 2MB together**, gzipped in the repo if the total is over 1MB. The
  loader reads `.scrollback` and `.scrollback.gz`. The existing precedent is `testdata/frame-*.bin`, real frames of
  64KB each.
- **One file is a lost-lines repro** (item 74's shape: a long reply, then a repaint while the pane is scrolled).
  The long reply is scripted as numbered lines, `L0001` to `L0300`, each followed by a fixed tail, so what was
  lost is counted directly rather than sampled. It is not held to a floor measured from today's renderer, which
  would bless the bug. See the assertions below.
- **The throwaway room, and each rule here has bitten before.** `ATRIUM_LOCATION` is a private dir and
  `ATRIUM_SHARED=-`. Without that it takes 7777 and hijacks every live session's hooks. It starts from a fresh
  empty database and never a copy of the live one, which would resume live conversations through fixtures. Stop
  only the throwaway process, by its own pid.
- **Clint signs off on the files before they are committed**, because committing them publishes them. The
  fixture files stay uncommitted in the worker's tree until he does. The test split is committed on its own. The
  worker's report lists each file, its size, and how it was produced.
- A `testdata/scrollback/README.md` says how each file was made, so it can be recaptured when claude-code's
  output changes.

**The assertions get stronger, not just quieter.** With fixed bytes the numbers are deterministic.

- **The two ordinary fixtures** each pin their own floor: the measured survival percentage, less 5 points to
  allow for a legitimate renderer change, and never below the old 40%. A capture that measures under 80% is not
  pinned. It is read by hand first, the way `01a080db` was, because an ordinary scripted session should sit at 80
  to 100. The floors live in a small table in `screen_real_test.go` beside the fixture cases, keyed by file
  name, so recapturing a fixture and re-pinning it is one diff.
- **The lost-lines fixture gets two tests, and neither is derived from today's number.**
  `TestLostLinesFixtureKeepsEveryLine` asserts all 300 numbered lines are in the transcript, in order. It is the
  target, and until item 74 lands it opens with `t.Skip("item 74 open: ...")` naming the backlog section. The fix
  for 74 removes the skip in the same commit, so the suite shows the fix landing. `TestLostLinesFixtureRenders`
  runs today. It asserts that the lines before the long reply and after the repaint are there, which the bug does
  not touch, and it logs how many of the 300 survived. So the fixture is exercised and its number is visible
  without being a bar anyone has to clear.
- The global 40% floor and the minimum of ten sampled words stay only in the live mode, where they belong.
- `TestRealSpinnersCollapse` has at least one fixture with over 50 spinner frames, and it fails if no fixture
  reaches 50, so it can no longer pass by finding nothing to check.

**Growth, and what the old quadratic test really checked.** `TestReplayIsNotQuadratic` compares the size of the
output for half the input against the whole. That catches output blowing up. It cannot catch a renderer that is
quadratic in time and still produces linear output, which is the hazard its comment names. So the design is
honest about both:

- The size check is renamed `TestReplayOutputGrowsLinearly` and says what it checks. It builds its input by
  concatenating the largest fixture four times (selected by size explicitly, while every other test walks the
  fixtures sorted by name, so logs do not depend on directory order), then compares 2x against 4x, so it always runs.
- Time goes in `BenchmarkReplayGrowth`, with sub-benchmarks at 1x, 2x and 4x of that same input, for manual
  comparison. It is not a test, because a timing ratio under a loaded full suite is the kind of flake this item
  exists to remove. The live twin logs the wall time per file next to its byte counts, so a hang shows up there.
- A counted-work assertion (cells written per input byte) would be the deterministic answer, but `renderHistory`
  has no counter today and adding one to a hot path for a test is out of scope. It is named, not built.

**Shape of the change.** `realScrollbacks(t)` becomes two helpers: `fixtureScrollbacks(t)` reads `testdata`, never
skips, and fails if the directory is empty, and `liveScrollbacks(t)` reads the env var and skips when it is unset.
Each test body becomes a function over `(name, raw, floor)`. It is called by a fixture test and by a `Live`
twin. The final set:

| Frozen, always run | Live, behind `ATRIUM_REAL_SCROLLBACK` |
| --- | --- |
| `TestEveryRealSessionReplays` | `TestLiveEverySessionReplays` |
| `TestRealSessionsKeepTheirText` | `TestLiveSessionsKeepTheirText` |
| `TestReplayOutputGrowsLinearly` | `TestLiveReplayOutputGrowsLinearly` |
| `TestRealSpinnersCollapse` | `TestLiveSpinnersCollapse` |
| `TestLostLinesFixtureRenders` | |
| `TestLostLinesFixtureKeepsEveryLine` (skipped until 74) | |

Plus `BenchmarkReplayGrowth`. `TestRealSessionsKeepTheirText` keeps its name because merge tooling matches it.
`TestReplayIsNotQuadratic` is renamed, because the old name claims something it never checked. Then `TestRealSessionsKeepTheirText` comes out of `$Flaky` in
`scripts/merge-check.ps1`, and out of the "known noise" lines in `DIRECTOR.md` and `docs/cold-start.md`. The
first is @merge's file, so @merge is told and does that edit.

**Tests.** The frozen tests pass (the 74 target skips, by name) on this machine and on a machine with no `~/.atrium`, and the suite no
longer reads outside the repo. Check that with `ATRIUM_REAL_SCROLLBACK` unset and the live directory renamed
aside on a copy, or with `HOME`/`USERPROFILE` pointed at an empty dir. With the variable set, the live twins run
and report the same per-file lines as today. A fixture test that fails prints the fixture name and its pinned
floor.

**Cost.** Small, half a day. Most of it is the capture and clint's look at the files, not the code.

**Status: built by sa93 (`642675a`), merged into `claude/terminal`, NOT landed.** The three fixtures are
uncommitted, so a clean checkout fails the frozen tests. It waits on clint's sign-off (below) before it can go to
`claude/main`. Mercurius `s_5q0QKZVeAa30`: round 1 needs_changes (two majors, fixed), round 2 ready_to_build.

What changed from the design in the build:

- **The fixtures are 67 to 112KB**, so they sample every 20th eligible word (`fixtureStride`). Live keeps 500.
  `session-reply` measured 87% and is pinned at 82. `session-tools` measured 70% and is pinned at 65. That is under
  80, so its six misses were read by hand: fused pseudo-words, an OSC title, a spinner fragment, and a hook line a
  repaint replaced. It carries 123 `Kneading` frames for the spinner test.
- **The lost-lines capture did not reproduce item 74.** 300 of 300 lines survived through 18 height resizes during
  the stream, on a build with the height hold (`387ccd5`). That is consistent with the hold working, but it is not
  proof. So the "target" test that would be unskipped by the 74 fix passed already. It now runs unskipped as
  `TestLongReplyThroughResizesKeepsEveryLine` (a test fix by @terminal after the merge), a guard that the renderer keeps every line of a long reply. A capture
  that really loses lines needs a build without the hold. That is the job of whoever builds item 74's option 3
  report or repair, and it is not in this item.
- **The live growth test replays one specimen**, the first file over 400KB by name, as the old test did. Over every
  live file, seven of about forty exceed 4x on the half-file comparison because of where the ring cut them.

**For clint, before the fixtures are committed.** The machine's home path in the OSC window title was replaced with
`claude.exe`. The statusline's account usage (the `5h` and `wk` percentages and the reset time) was zeroed by
@terminal at the orchestrator's call, and every rendered frame of all three files was checked to show `00%`. The
floors did not move. What is left: the header line's `Claude Code v2.1.284 / Sonnet 5.5 / Claude Team`, the capture
directory `D:\cap\atrium`, the session's own `ctx` and `tx` counts, the reset countdown's minute digit where it was
repainted alone, and a throwaway card id. No credential, email or user path.

## 89. A finished worker's runner outlives its worktree and locks the directory (bug)

Reported 2026-09-29 by the orchestrator: every ended worker left its worktree directory "used by another process"
after git had unregistered it (sa21, sa80, fb01, lost-lines, sa82). The suspect was a leftover child (a shell, node,
or the conpty host) whose cwd was that directory.

**Diagnosis, read-only, 2026-09-29 ~01:15 local.** It is not a leftover child. It is the worker's own runner, which
never exited. Each directory's holder was found by reading every process's current directory out of its PEB:

| Worktree | Holder | Parent | Started (UTC) | Card | Last event |
| --- | --- | --- | --- | --- | --- |
| lost-lines | `claude.exe` 56032 | room `atrium.exe` 43988 | 02:54 (resume) | `01a0eac2` | `done` report 04:23 |
| sa21 | `claude.exe` 16812 | room `atrium.exe` 43988 | 03:14 | `01a0eb28` | `done` report 04:06 |
| sa80 | `claude.exe` 47888 | room `atrium.exe` 43988 | 03:33 | `01a0eb39` | `done` report 04:00 |
| fb01-provision | `claude.exe` 47516 | room `atrium.exe` 43988 | 03:39 | `01a0eb29` | `done` report 04:06 |
| sa82 | `claude.exe` 57404 | room `atrium.exe` 43988 | 04:06 | `01a0eb57` | `done` report 04:23 |

Every holder is a full session (about 350MB each, 1.8GB in all, no child processes, responding) and a direct child
of the room daemon. On every card `supervised` is true, which is `d.sup.get(id) != nil`, so the supervisor still
owns each runner and could stop it. None of the five has an `exited` event after its `done` report, and none was
culled. The worktrees went by hand: `git worktree remove --force` unregisters the worktree and deletes its files,
then fails on the directory the runner is sitting in. That is the "Permission denied" and the empty directory.

Why nothing ended them:

- **`done` keeps the runner on purpose.** A done report moves the card to `done` and the session sits at its prompt,
  so a director can send it back (sa21 went `done` to `needs-input` twice for review) and item 83 lets a say reach it.
- **The reaper never looks at a done card.** `reapOnce` checks running and the needs-* columns, and
  `reviveOwnedDead` only `dead` ones. A `done` card with a live runner is in neither list. Its stored `pid` is 0
  (a supervised card's pid is not the observed one), so a pid check would not have answered either.
- **`atrium_cull` does the right thing and was not used.** It calls `StopRunner` and `waitRunnerGone` before
  `git worktree remove`, because "on Windows a directory in use cannot be removed". Removing the worktree before
  `atrium_exit` is the path that locks it. DIRECTOR.md says "exit the worker and remove its worktree", and the
  order in that sentence is the whole fix for the manual path.

**What the supervisor or reaper should do.** Not end a runner for being `done`, which would break the review loop
and item 83. Two candidates:

1. **A supervised runner whose worktree is gone is ended.** On the reaper tick, for each `d.sup.all()` runner whose
   card has a worktree recorded: if that directory no longer exists, or exists with no `.git` entry (git has
   unregistered it), `windDown` the runner with its harness's exit keys, and record `exited` with `by: reaper`,
   `detected: its worktree was removed`. The resume id stays, so nothing is lost. This is exactly the case in the
   table and has no false positive worth worrying about: a session in a directory with no repository has no work
   left to do. Only `atrium:subagent` cards, so a human's own terminal in a scratch directory is never touched.
   Small, owned by @runtime, one targeted test with a temp worktree.
2. **A `done` card's runner idle past a limit is parked.** The memory case: five idle sessions held 1.8GB. This is
   the item 38 question (Open Question 1 in `docs/restart-idle-spec.md`: parking saves processes and memory, and no
   tokens), so it waits on clint's answer there rather than being decided here.

Until then, the five runners above can be asked to leave by their owners with `atrium_exit` (sa21 @runtime, sa80
@ui, fb01 @fabric, lost-lines and sa82 @terminal), after which each empty directory removes normally. Nothing was
killed or exited during the diagnosis. The orchestrator sent `atrium_exit` to all five afterwards.

**A second finding, the same family as item 83.** `atrium_say` and `atrium_exit` refuse a `done` card named by its
alias ("no session called sa21"), and only the `room~id` form works. Reproduced by @runtime on sa32 the same night.
The cause is `GetByAlias` in `internal/store/alias.go`, which only matches `liveClause` (not `done`, not `dead`,
not archived). So an alias stops resolving the moment a worker reports done, while its runner is still at the
prompt and item 83 says a say should reach it. Every resolver built on it (`localTarget`, `resolvePeer`, the MCP
`resolvePeer` in `internal/link`) inherits that. sa32 found the mirror of it: `GetByWireName` matches ended cards,
so an exact handle of a dead card is refused as ended even when a live card holds that name as an alias.

The fix belongs in resolution, not in the alias query: an alias resolves to the newest card holding it that is
live, or else to the newest `done` card whose session is not gone by `sessionGone` (item 83's rule). A dead card's
alias stays unresolved, so a reused alias still means the live card. Separate from the reaper fix above, and it
touches the same resolver sa32 (item 32) changed, so it goes after that merge.

## 91. Two cards in one worktree share one HANDOFF.md, and new-context overwrites the other's (bug, design only)

Reported 2026-09-29 by the orchestrator. @merge and @orchestrator both run in the main checkout
(`D:/git/github/dovholuknf/atrium`), and at about 01:15 local one card's new-context capture overwrote the
other's HANDOFF.md. The file is a fixed name in the card's directory at every step of `newcontext.go`: the capture
prompt says "HANDOFF.md in the current directory", the wake prompt says "Read HANDOFF.md", and `handoffWritten`
checks `filepath.Join(task.Worktree, "HANDOFF.md")`.

Two harms, and the second is why nothing noticed the first:

- **The overwrite.** A handoff not yet read back, or one a human is keeping, is replaced by another card's.
- **The check passes for the wrong card.** `handoffWritten` only asks whether the file was modified since the
  capture began. A capture on card A that wrote nothing still passes when card B wrote the file in that window,
  and card A then wakes into card B's state and carries on as B. A card in two places is the worst outcome here.

Options:

1. **A per-card file name.** `HANDOFF.<alias or first 8 of the card id>.md`, in the capture prompt, the wake prompt
   and `handoffWritten`, the same three places. Two cards in one directory can then never touch each other's
   file, and the check is about the right file by construction. The cost: every habit and script that says
   `HANDOFF.md` (DIRECTOR.md's "git rm HANDOFF.md", the orchestrator's touch-after-POST workaround, briefs) has to
   learn the pattern. Using the per-card name always, rather than only when a directory is shared, keeps one rule.
   A `.gitignore` line for `HANDOFF.*.md` would also stop a handoff reaching a merge by accident, which is the
   thing every director currently removes by hand.
2. **Refuse new-context when another live card shares the directory.** Small, and it would have prevented this
   one. But the two cards that share a directory are the orchestrator and the merger, which are the long-lived
   sessions that most need cycling, so the refusal lands on exactly the cards it should serve. It could refuse
   only while the OTHER card's own new-context is in flight, which closes the concurrent case but not an
   overwrite of a handoff written earlier and not yet read.
3. **The handoff outside the worktree,** in the room's state directory keyed by card (`~/.atrium/handoff/<id>.md`),
   with the absolute path in both prompts. Per-card by construction and never in git. But a human can no longer
   find it next to the work, and a runner on another room writes to that room's disk, which the board then has to
   serve.

**Recommendation: option 1, with option 2's narrow form as a guard.** The name removes the collision, and refusing
only while another card sharing the directory is mid-sequence costs nothing and covers a card whose runner ignores
the name it was given. A migration is not needed: the name is derived, not stored. Owned by @runtime
(`internal/daemon/newcontext.go`). The open question for clint is option 1's cost to existing habits: whether the
fixed name `HANDOFF.md` is worth keeping for the single-card case humans are used to.
## 90. busyGuard's refusal-line check sleeps a fixed 50ms (bug)

Raised 2026-09-29 by the orchestrator. @merge's full headless run on `db2b33a` failed once under load in
`busyGuard`: "the refusal line outlived its dialog". The check closed the launch dialog, slept 50ms and then looked,
so a loaded browser that had not yet run the close handler failed it. It passes alone. Owner @ui.

Status 2026-09-29, done on `claude/ui`. The sleep is now a wait for the line to be gone, with a `slow(2000)` budget
and the same failure message, so `HEADLESS_SLOW` scales it like every other wait since item 85.

## 92. `/v1/tasks/prune` reaches one room, like pin-order did (bug)

Found 2026-09-29 by fb04 while building item 52. `prune` sits under `/v1/tasks/` and names no card in its path, so
the hub routes it by the board's `X-Atrium-Room` header (the stale `writeRoom`) to one room at most, the way
`pin-order` was routed before `internal/link/pinorder.go`. On the all-rooms view a prune reaches whichever room the
header names, and the others keep what should have gone.

Fix, probably the same shape as item 52: fan it out to every attached room, bounded per room, 200 with `unreached`
while one room took it. Read what `prune` does to each room first, since a prune that should only reach one room
would make the fan-out wrong. Owned by @fabric, and folded into the next worker that touches `internal/link`.


------------
