# Agent-to-agent reliability: no silent stall, no silent loss

Design for how atrium guarantees that work handed from one session to another never stalls without somebody
hearing about it, and that a message between sessions is never lost without somebody hearing about that either.
Design only. Nothing here is built until clint approves it.

Read `docs/agent-messaging.md` first. It is the reference for the queue and the two delivery paths this design
builds on. `docs/agent-lineage-design.md` is the design for recording who launched whom, which this design needs
and which has not shipped.

## What went wrong on 2026-09-23

The orchestrator, `atrium-87300`, launched `sa20` through `atrium_launch`. The launch prompt listed three steps.
`sa20` did them, ended its turn and sat in `needs-input` with nothing blocking it. It never reported. The
orchestrator found out because clint noticed.

The event log on `sa20`'s card (`01a0ce42-89f8-71a8-a6ad-324e8546fa19`) shows it plainly:

```
12:34:22  created, launched (pty, prompted)
12:35:41  prompted   from atrium-87300   (output location changed)
12:38:34  status-changed running -> needs-input      <- the turn ended. nothing else happened
12:40:02  prompted   from atrium-87300   ("You have been idle since 12:38. Reply with atrium_say...")
12:40:16  sa20 -> atrium-87300, queued:true (the orchestrator's gate was shut), delivered 12:40:36
```

There is no `submitted` event with `kind: finished` on the card, so `sa20` never ran `atrium finish`. Between
12:38:34 and 12:40:02 the orchestrator's card has no `prompted` event from `sa20` and its queue holds nothing.

### Why dispatch-notify did not tell the orchestrator

**Because dispatch-notify was never built.** The card `dispatch-notify-12600` ("notify the launcher when a worker
returns + name it") is in `done`, but the branch `claude/dispatch-notify` holds four commits and none of them is
the feature:

- `HANDOFF.md`, a design that clint approved and a list of next steps for a fresh session.
- Migration `0055_task_lineage`, which adds the `spawned_by` and `spawned_by_id` columns. Nothing reads or writes
  them. `Task`, `taskColumns`, `LaunchRequest`, `launchHandler` and `handleFinish` are all on their old shape.
- Two unrelated fixes (resume onto a bare card id, routing card actions by their own card).

The branch is merged into neither `main` nor `claude/main`. On `claude/main` there is no `notifyLauncher`, no
`SetLineage`, and no reference to `spawned_by` in any Go file. `launchHandler` in `internal/link/control_mcp.go`
reads the caller's handle into `agentOf(req)` for other tools and does not forward it on the launch. The room
never learns who launched a card. So there is no record a notify could route by.

**Even the approved design would have missed sa20.** Its only trigger was `handleFinish`, and `sa20` never ran
`atrium finish`. The design named a stall trigger as "next" and left it undesigned (HANDOFF open question Q1). A
worker that ends its turn without a word is exactly the case it did not cover.

**The card went to `done` without the feature landing.** That is its own failure mode, and this design lists it
below: a worker claimed done with no sha, and nothing checked.

**The Stop hook was not the gap.** It is installed on this machine (`atrium turn --event end` in
`~/.claude/settings.json`), and it fired at 12:38:34, which is what moved `sa20` to `needs-input`. `handleStop`
found nothing queued and let the turn end. It has no idea that an agent-launched card owes somebody a report.

So three things were missing, in this order: the launcher's identity on the card, a report the worker is held to,
and a check at turn end that the report was made.

## Failure modes

The brief's seven, and eight more found while tracing the incident or raised during review.

| # | Failure | Today |
| --- | --- | --- |
| F1 | A worker ends a turn without reporting (done, blocked or a question). | Silent. Card sits in `needs-input`. |
| F2 | A worker waits on a permission prompt nobody is looking at. | Board shows it. The launcher is not told. |
| F3 | A message is queued to a session that never takes another tool call or turn. | Stuck in the queue. The board shows a count. The sender is not told. |
| F3b | A message is typed into a terminal mid-command. | Guarded already by the typing gate (`runner.injectPeer`). |
| F4 | The launcher is down, restarting, compacted or `/clear`ed when a report arrives. | The queue is durable, so it waits. A turn lost in a restart can take a delivered message with it. |
| F5 | A report is ambiguous: done with no sha, blocked with no ask. | Accepted as written. |
| F6 | A guard that forces a turn loops or burns tokens. | No guard exists. |
| F7 | A worker is stuck in a long tool call or a hung process. | Activity shows the tool and its age. Nobody is told. |
| F8 | The launcher's identity is not recorded, so nothing can route back to it. | The root cause today. |
| F9 | A worker process dies (crash, kill, room restart) without reporting. | The reaper marks it dead. The launcher is not told. |
| F10 | The launcher is a card that is itself `done` or `dead`. | A message to it answers 409 and goes nowhere else. |
| F11 | A worker compacts or `/clear`s and loses its brief and its report duty. | `BRIEF.md` is on disk. Nothing points the new context at it. |
| F12 | A delivered message is lost because the turn that received it never completed. | Marked delivered. Never redelivered. This is how the deploy ran twice. |
| F13 | A system notice to a launcher is dropped by the per-sender rate limit. | Would be dropped if it existed (the HANDOFF design logged and dropped). |
| F14 | A worker claims done and the claim is false (card moved, no work landed). | Accepted as written. dispatch-notify is an example. |
| F15 | A message is sent to a session whose runner has no atrium hooks: a gemini card, or a claude session with hooks not installed. | `atrium_say` answers `queued` and nothing will ever deliver it. The sender is not warned. Live case: the gemini card `atrium-throwaway-2453665367`. |

## The worker contract

**Every agent-launched session ends each turn in a report, or atrium makes it.** A card is agent-launched when it
carries the `origin:agent` tag, which `launchHandler` already adds. Human-started cards are untouched by all of
this.

### The report

One verb, two doors:

- **`atrium_report`**, a new tool on the control MCP. Every launched claude session already has the control MCP
  (`--mcp-config ~/.atrium/mcp.json`), and the CLI is not on PATH on every machine, so the tool is the primary
  door.
- **`atrium finish`**, extended with `--status` and `--sha`. The CLI door for any runner without MCP.

Both land on one room endpoint and one function, so they cannot drift.

```
status    done | blocked | question | progress      required
summary   what happened, in the worker's words      required, bounded to 8000 chars
sha       the commit the work landed as             required for done, unless no_commit says why
no_commit why a done has no commit                   e.g. "research only, answer is in the summary"
ask       what the worker needs                      required for blocked and question
```

What each status does to the card:

| Status | Card moves to | Launcher is told | Satisfies the turn-end guard |
| --- | --- | --- | --- |
| `done` | `done` | yes | yes |
| `blocked` | `needs-input`, reason `blocked` | yes | yes |
| `question` | `needs-input`, reason `asked` | yes | yes |
| `progress` | unchanged | yes | yes, once per prompt |

`progress` exists for a worker that ends a turn on purpose while something runs in the background. Without it the
guard would force a turn on every such stop.

### Validation (F5, F14)

The endpoint refuses an incomplete report with a 400 that says exactly what is missing. The tool and the CLI hand
that text back to the model in the same turn, so it fixes the report rather than giving up. This is a tool call,
not a hook, so an error here is the right answer and fails nothing.

- `done` with no `sha` and no `no_commit`: refused.
- `done` with a `sha` the worktree does not contain (`git cat-file -e <sha>^{commit}` in the card's worktree):
  accepted, marked `unverified`, and the launcher's notice says so. Not refused, because a worker that committed
  in another worktree is doing its job. This answers F14 as far as atrium can: it cannot judge the work, but it
  can say whether the commit exists.
- `blocked` or `question` with no `ask`: refused.

### What the launcher receives

A peer message on the launcher's queue, from the worker's handle, under the usual `[atrium] <handle> says:`
banner:

```
report from sa20-sdk-matrix-c-sdk-vs-the-c-sdk-it-wr: done at 3f2a91c
card 01a0ce42-89f8-71a8-a6ad-324e8546fa19

<summary, verbatim, bounded>
```

The card id and the handle are in the body, so the launcher can act without a lookup (HANDOFF Q2, answered yes).

## Lineage (F8)

Stage 1 ships the lineage plumbing exactly as `claude/dispatch-notify`'s `HANDOFF.md` lays it out: migration
`0055`, `Task.SpawnedBy` and `SpawnedByID`, write-once `SetLineage`, `LaunchRequest.SpawnedBy`, and
`launchHandler` forwarding `agentOf(req)` as `spawned_by`. Cherry-pick the migration commit rather than rewrite
it. The board's own launch dialog records `@human`, and a card whose `spawned_by` is `@human` or empty never
notifies anybody (HANDOFF Q5).

`atrium_launch` launches into the caller's own room (`roomOf(req)`), so a launcher and its workers share a room
and every lookup below is local to one daemon. A launch that crosses rooms through the dispatch queue is out of
scope and falls through to the board (see Escalation).

## The notice path: `notifyLauncher`

One function in the room daemon, `notifyLauncher(worker, source, body)`, and every trigger feeds it. `source` is
one of `report`, `silent-stop`, `permission-wait`, `long-tool`, `died`, `dead-letter`.

1. Resolve the launcher from `worker.SpawnedByID`, falling back to `GetByWireName(SpawnedBy)`.
2. No launcher (empty, `@human`, unresolvable): escalate to the board instead. Never silent.
3. Launcher `done` or `dead` (F10): queue it anyway, because a dead card can be resumed and its queue survives,
   and escalate to the board as well.
4. Queue with `QueueFromPeer(launcher, body, worker.WireName)`, with a new `auto` flag on the row that marks it as
   written by atrium rather than typed by a model.
5. Try to type it through `typeThroughGate` when atrium owns the launcher's terminal. The existing gate and the
   pending injector apply unchanged, so F3b stays covered.
6. `publishTask(launcher)` so its waiting count moves.

**System notices are not subject to the per-sender rate limit (F13).** That limit exists to stop a looping model,
and a notice is not a model. They have their own bound instead: at most one notice per `(worker, source, key)`,
where the key names the one event that created the obligation. A second notice with the same key is dropped.

| Source | Key | So |
| --- | --- | --- |
| `report` | the report's own id | every report is sent, once |
| `silent-stop` | the id of the prompt that opened the unreported turn | one notice per prompt, however many Stops follow it |
| `permission-wait` | the permission request id | one notice per request, one board escalation per request |
| `long-tool` | the tool call id (`tool_use_id`), else the activity start time | one notice per call |
| `died` | the event id of the `dead` status change | one notice per death |
| `dead-letter` | the message id | one notice per message |

A worker that is prompted three times and stops silently each time produces three `silent-stop` notices, one per
prompt. A worker that stops twice on one prompt (the natural Stop and the Stop after the forced turn) produces
one.

**Automatic notices only travel upward, from a worker to its launcher or to the board.** Atrium never writes an
automatic message to a worker. So no cycle of automatic messages can exist. Any loop needs a model to choose to
send, and model sends stay bounded by the 20 per minute peer limit.

## The turn-end guard (F1)

This is the Stop-hook guard, and it is stage 1.

`handleStop` already runs on every turn end and already knows the card. Before it drains the queue it asks one
more question:

```
card has origin:agent
AND the Stop is a turn end, not a subagent end                  (turn.go already filters these)
AND stop_hook_active is false                                   (turn.go already refuses to block when set)
AND no report and no peer message to its launcher since the last prompt
AND the card's guard budget is not spent
  -> block, with the contract as the reason
```

The reason is short and carries the exact call:

```
You are an agent-launched session and you ended your turn without reporting to <launcher>.
Call atrium_report now with status done (and the sha), blocked or question (and what you need), or progress
(if you are waiting on something on purpose). Then end your turn.
```

A peer message to the launcher (`atrium_say`, `atrium tell`) since the last prompt also satisfies the guard. A
worker that has already told its launcher something should not be made to repeat it. The launcher sees what it
said.

If messages are also queued, one block carries both: the queued messages first, then the contract line.

### When the guard gives up

When a Stop arrives with `stop_hook_active` set and there is still no report, the forced turn did not produce one.
The guard does not block again. It calls `notifyLauncher(worker, "silent-stop", ...)` with the last activity it
saw, and lets the turn end. The launcher hears about the silent stop within seconds of it happening, which is
what `sa20` needed.

### Loop bounds (F6)

Four independent bounds, any one of which stops a loop:

1. **`stop_hook_active`.** Claude Code sets it on the Stop that follows a blocked Stop. `turn.go` already refuses
   to block when it is set, and that code is not touched. One forced turn per natural stop, at most.
2. **Once per prompt.** The guard fires only when there has been no report since the last prompt. A forced turn
   that reports resets nothing, because there is no new prompt.
3. **A per-card budget.** At most 3 forced turns per card per rolling hour (`ATRIUM_GUARD_BUDGET`). Past that the
   guard stops forcing and notifies instead. A worker that keeps being prompted and keeps not reporting costs at
   most three extra turns an hour.
4. **Upward-only notices.** See above. The launcher's reaction to a notice is a model choice, bounded by the peer
   limit.

### How it squares with "Stop is optional"

`CLAUDE.md` says the Stop hook is optional and never installed by "install all", because it is the one hook
whose answer changes what a session does. That rule stays exactly as it is for human sessions.

- **The rule governs installing the hook into the operator's settings.** This design does not change install
  all, the hooks pane, or `settings.json`.
- **Agent-launched claude sessions get the Stop hook for themselves, at launch.** `launchLocked` adds
  `--settings <file>` to the claude command line for `origin:agent` launches, pointing at an atrium-written file
  that holds only the Stop hook. Claude Code merges settings from `--settings` with the user's own, and it
  deduplicates identical hook commands, so a machine that already has the Stop hook runs it once. The daemon also
  treats two `/stop` posts for one turn as one (same session id within the same second), in case the commands
  differ in spelling.
- **The policy lives in the daemon, not the hook.** The hook is the same `atrium turn --event end` everywhere. The
  daemon's answer depends on the card's `origin:agent` tag. A human card's Stop behaves exactly as today, even on
  a machine where the hook is installed.
- **An agent-launched card can be opted out.** A launch with `guard: false`, or clearing the tag on the card,
  turns the guard off for that card.

Why this is acceptable: the concern behind the rule is a human's session that will not stop. An agent-launched
session has no human at its keyboard by default, the launcher asked for the work, and the contract is the thing
it was launched under. The three safety properties in `turn.go` (every failure prints nothing, `stop_hook_active`
is honored, only a well-formed block passes through) are unchanged.

For runners other than claude, `docs/other-runners.md` says codex sends the same Stop payload, so the same
`--settings` equivalent (its `hooks.json`) applies. A runner with no Stop hook falls back to the watchdog below,
which catches the same stop minutes later instead of seconds.

## The watchdog (F2, F3, F7, F9)

A new ticker in the room daemon, beside the reaper, every 20 seconds (`ReapEvery`). It reads the store and the
in-memory activity. It never calls a runner, never types, never wakes a model. It only calls `notifyLauncher` and
the board escalation.

| Trigger | Condition on an `origin:agent` card | Launcher at | Board at |
| --- | --- | --- | --- |
| Silent stop, no Stop hook | `needs-input`, no report and no peer message to its launcher since last prompt | 2 min | +10 min |
| Permission wait (F2) | `needs-permission` with a pending request | 2 min | 5 min |
| Question or blocked | a report with status `blocked` or `question` | immediately (it is the report) | +15 min unanswered |
| Long tool (F7) | activity `tool` with no hook heard | 20 min | 45 min |
| Died (F9) | the reaper moves it to `dead` with no report | immediately | immediately if no launcher |
| Undeliverable (F3) | a message pending 30 min to a card that is idle, dead or unhookable | the SENDER is told | 30 min |

"Launcher at" means `notifyLauncher` fires. "Board at" means the escalation fires if the card is still in that
state and the launcher has not acted. The launcher has acted when it sent the worker a message, answered the
permission, or moved the card. Every threshold is a constant with an environment override, the way `QuietAfter`
and `OrphanGrace` are:

| Constant | Default | Override |
| --- | --- | --- |
| `SilentStopNotifyAfter` | 2 min | `ATRIUM_A2A_SILENT_STOP` |
| `SilentStopBoardAfter` | 10 min after the notice | `ATRIUM_A2A_SILENT_STOP_BOARD` |
| `PermWaitNotifyAfter` | 2 min | `ATRIUM_A2A_PERM_WAIT` |
| `PermWaitBoardAfter` | 5 min | `ATRIUM_A2A_PERM_WAIT_BOARD` |
| `AskBoardAfter` | 15 min unanswered | `ATRIUM_A2A_ASK_BOARD` |
| `LongToolNotifyAfter` | 20 min | `ATRIUM_A2A_LONG_TOOL` |
| `LongToolBoardAfter` | 45 min | `ATRIUM_A2A_LONG_TOOL_BOARD` |
| `DeadLetterAfter` | 30 min | `ATRIUM_A2A_DEAD_LETTER` |
| `GuardBudget` | 3 forced turns per card per hour | `ATRIUM_GUARD_BUDGET` |

Each override takes a Go duration (`90s`, `5m`), or an integer for the budget. A value that does not parse is
ignored, the same rule `launchCap` uses.

The long-tool trigger cannot tell a hung process from a long build, so it only says what it sees ("sa20 has been
in Bash for 20 minutes, no hook since 12:41"). It never kills anything.

The permission-wait notice tells the launcher what is being asked. The launcher cannot answer the permission. The
answer stays with the human, and the notice says so. Whether a launcher may answer its own workers' permissions
is an open question below.

## Delivery guarantees

The queue is already durable and already at-least-once from the store's side. What is missing is knowing that a
delivered message was actually acted on.

### Ack (F12)

Today `delivered_at` means "a hook handed this over". A new `acked_at` means "the turn that received it went on to
end normally". It is stamped by the next Stop from the same card after delivery, or by the next permission request
after a typed or Stop delivery. Both prove the model kept running after reading it.

A message that is delivered and then followed by a SessionStart (the session restarted, resumed or crashed)
instead of a Stop is **unacked**. It goes back on the queue, marked `redelivered`, and the banner says:

```
This message may be a repeat. It was delivered in a turn that did not finish.
```

This is exactly the deploy that ran twice. The orchestrator's resumed session had lost the turn that received its
instruction. With ack it would have been told, and told it might be a repeat, which is the fact it needed to check
before acting.

### Delivery capability, and telling the sender at send time (F15)

A queued message is only as good as the hook that drains it. Today `handleMessage` queues whenever it cannot type,
and answers `queued` whether or not the target has any hook that will ever take it. The fix has three parts.

**A runner declares how it can be reached.** A new field on the runner's adapter, `Adapter.Delivery []string` in
`internal/runnersetup` (sa22's work on `claude/runner-setup`), lists the paths that runner kind supports. It is a
fact about the runner kind, so it lives on the adapter and not in a `store.Harness` column the operator could set
wrong. Adapters match a runner row by its command's leaf name, as they already do.

```
delivery   ["typed", "tool-hook", "stop-hook"]     claude, codex
           ["typed"]                               gemini today, until a gemini hooks target exists
```

- `typed` means atrium can put the text into a terminal it owns, through the existing gate.
- `tool-hook` means the runner has a hook before each tool call whose answer can carry text back to the model
  (claude's PreToolUse and PermissionRequest).
- `stop-hook` means the runner has a turn-end hook that can block with a reason.

**Effective delivery for a runner row** is the declared list, minus `tool-hook` and `stop-hook` when the adapter's
existing `hooks` check for that row is not `ok`. That check already ships as `harnesses[].setup.checks` on
`GET /v1/harnesses`. gemini's `hooks` check is `n/a` today, which gives `["typed"]`. Shape agreed with sa22.

**A card says which of those it has actually shown.** The runner row says what should work. The card says what
did. The room already sees every hook post, so it stamps two times on the card: `tool_hook_seen_at` and
`stop_hook_seen_at`. A claude session started in a shell whose settings lack the hooks has a runner row whose
check is `ok` and a card that has shown neither. That is the second live case, and only the card can tell it.

**`handleMessage` answers with the truth.** Before queueing, it works out whether the target can drain its queue:

| Target | Answer |
| --- | --- |
| atrium owns the terminal and peer typing is allowed | `typed`, or `held` while the gate is shut. As today. |
| a hook that carries text has been seen on this card | `queued`, as today |
| the runner declares a hook, the card has never shown one | `queued-unconfirmed`, with a warning |
| the runner declares no hook and atrium does not own the terminal | `undeliverable`, with the alternatives |

The message is still written to the queue in every case, because a card can gain hooks or be resumed under a
supervised terminal, and the queue is the durable record. The table is for live cards. A `done` or `dead` target
follows the rule under "Dead letter". What changes is what the sender is told. The
`undeliverable` answer names the alternatives in the text `atrium_say` returns:

```
queued, but <handle> has no way to receive it: its runner (gemini) has no atrium hook, and atrium does not own
its terminal. Relaunch it under atrium so the text can be typed, or ask the human to relay it.
```

`atrium_peers` shows the same fact per peer (`reachable: typed | hook | unconfirmed | no`), so a sender can see it
before it sends. A launch from `atrium_launch` is always supervised, so every worker it starts is reachable by
typing whatever its runner declares.

**It ages into a dead letter like any other undelivered message.** See "Dead letter" below for the rule, which
covers `undeliverable` sends.

### Dead letter (F3)

A message pending for 30 minutes (`DeadLetterAfter`) to a card that cannot drain it is a dead letter. "Cannot
drain it" is concrete: the card is `dead` or `done`, or its effective delivery has no path that works for it now
(no hook seen, no terminal atrium owns). The message stays in the queue, because the card may come back. This is
the one rule for `done` and `dead` recipients:

- **A model's send to a `done` or `dead` card** is refused with 409, as `resolvePeer` does today. Nothing is
  queued, and the refusal is the sender's notice.
- **An automatic notice to a `done` or `dead` launcher** is queued (F10), escalated to the board at once, and
  becomes a dead letter after the normal 30 minutes like any other.
- **An `undeliverable` send (F15)** is queued and shown as a dead letter at once, since the sender was already told
  at send time.

Otherwise, at 30 minutes:

- The sender is told, on its own queue: `your message to <handle> has not been delivered in 30 minutes: <why>`.
  For an automatic notice the sender is atrium, so this goes to the board.
- The board shows it as a dead letter on the recipient's card, distinct from the ordinary waiting count.

### What a launcher sees when it comes back (F4, F11)

The SessionStart hook fires on startup, resume, `/clear` and compaction (`source` in the payload). Today it
writes nothing to stdout. For a card that has workers or that is itself agent-launched, the daemon answers with a
short digest and the hook prints it as `additionalContext`, which Claude Code adds to the new context:

```
atrium: you launched 3 sessions that are still open.
  sa20-...  done at 3f2a91c, reported 12:52        card 01a0ce42...
  sa19-...  needs-permission for 4m (Bash: go test ./...)
  sa22-...  running, last report: progress 12:47
2 messages were delivered in a turn that did not finish and are queued again.
```

And for an agent-launched worker, one more line: `you were launched by <launcher> with the brief in
<worktree>/BRIEF.md. end every turn with atrium_report.` That answers F11.

`atrium_peers` gains a `mine` filter that lists only the caller's own workers, for a launcher that wants the
digest again mid-session.

## Escalation

The launcher first, clint's board second.

- **The launcher** hears through its queue, typed when its terminal is free, carried by a hook otherwise.
- **The board** hears through a new card reason, `unattended`, set on the WORKER's card with the source attached
  (for example `unattended: silent-stop, launcher did not act in 10 min`). It rides the existing notify path
  (`notify.js`, the toast log), so it rings the way `needs-input` already does when clint is away. The notice
  names the launcher, which is the identity half of dispatch-notify.
- A worker with no launcher (F8 residue, a crossed room, `@human` as launcher of an `origin:agent` card) skips
  straight to the board.

## How each part keeps the resilience rules

From `CLAUDE.md`, "Resilience guarantees (daemon)".

1. **Storage failure halts.** The report endpoint, `notifyLauncher`, ack and dead-letter all write through the
   store and return its error, the way `handleFinish` already does. A store that fails closes the agent listener
   as today. The watchdog stops ticking when the store halts, since there is nothing durable to act on, and never
   holds notices in memory as a substitute.
2. **A hook never fails a session.**
   - The Stop hook's three guards in `turn.go` are untouched. The guard decision is made in the daemon. Any error
     computing it (store read, tag lookup) answers `{}` and the turn ends normally.
   - The SessionStart digest is best effort. Any error, or anything past the three second budget, prints nothing.
     The digest is capped at 2000 characters.
   - `--settings` is only added when the atrium-written file exists and parses. A missing file launches without
     it, the watchdog covers the stop.
3. **`/activity` stays fire and forget.** Nothing here reads `/activity`'s answer or adds work to its path. The
   watchdog reads the in-memory activity table the endpoint already fills.
4. **Shutdown is narrated and bounded.** The watchdog is one more ticker, stopped with the reaper.

## Stages

### Stage 1: the silent stop is caught, within seconds

Ships the most value, and would have caught `sa20` at 12:38:34.

1. Lineage plumbing from `claude/dispatch-notify` (migration `0055` cherry-picked, the rest per `HANDOFF.md`).
2. `atrium_report` and `atrium finish --status --sha`, with validation.
3. `notifyLauncher` with sources `report` and `silent-stop`, the `auto` flag, and the per-state dedupe.
4. The turn-end guard in `handleStop`, with the per-card budget.
5. `--settings` with the Stop hook on `origin:agent` claude launches. This waits on open question 4. If clint
   says no, stage 1 still ships items 1 to 4 and 6. The guard then runs only where the operator already installed
   the Stop hook (as on this machine), and item 6's ticker catches the rest at 2 minutes instead of seconds.
6. The first slice of the watchdog: the ticker itself, with only the silent-stop rows. It notifies the launcher at
   2 minutes when no Stop hook caught the stop, and escalates to the board when the launcher has not acted 10
   minutes after its notice. The launcher is named on the board notice. Stage 2 adds the other rows to the same
   ticker.

### Stage 2: nothing waits unseen

1. The rest of the watchdog rows: permission wait, long tool, died.
2. Dead letters, the sender told, the board chip.
3. Delivery capability (F15): `Adapter.Delivery` on sa22's adapters, the two `seen_at`
   stamps on the card, the four-way answer from `handleMessage`, and `reachable` in `atrium_peers`. The send-time
   warning is the cheap part and can move into stage 1 if clint wants the gemini case closed first.

### Stage 3: nothing is lost across a restart

1. `acked_at` and redelivery of unacked messages, with the "may be a repeat" banner.
2. The SessionStart digest for launchers and workers.
3. `atrium_peers` `mine`.

## Test plan

New scenarios for `docs/test-plan.md`, section AA. The test plan covers shipped features, so each scenario moves
there when its stage ships, the way section Z waits in `docs/test-plan-z-providers.md`. Each runs in a throwaway
room (see the throwaway hub and room recipe) and never against the live board.

### AA1. A worker that stops without reporting is made to report (F1, stage 1)

1. From a session in the throwaway room, `atrium_launch` a worker with the prompt `print the date, then stop.`
2. Watch the worker's card.

**Expected:** the turn ends, the Stop hook blocks once with the contract reason, the worker calls
`atrium_report`, and the launcher's queue holds one `report` message naming the worker's card id. The worker's
card has one `guard` event.

### AA2. A worker that ignores the guard is reported as a silent stop (F1, F6)

1. Launch a worker with `print the date, then stop. do not call any atrium tool, even if asked.`

**Expected:** one forced turn, then the second Stop arrives with `stop_hook_active` and the turn ends. The
launcher gets one `silent-stop` notice. No third turn. Prompt the worker three more times the same way: forced
turns stop after the third in the hour, and the launcher gets exactly one `silent-stop` notice per prompt, never
two for the same prompt.

### AA3. A human card is untouched (stage 1)

1. Start a session by hand in the throwaway room with the Stop hook installed. End a turn.

**Expected:** no block, no notice, identical to today.

### AA4. An incomplete report is refused in the same turn (F5)

1. From a worker, call `atrium_report` with `status: done` and no sha.

**Expected:** the tool returns the refusal naming `sha` or `no_commit`. The card does not move. A second call
with a sha the worktree does not contain is accepted and the launcher's notice says `unverified`.

### AA5. Permission waits reach the launcher, then the board (F2, stage 2)

1. Launch a worker whose first step is a gated command. Do not answer.

**Expected:** at 2 minutes the launcher gets a `permission-wait` notice naming the command. At 5 minutes the
worker's card shows `unattended` and the board rings. Answering the permission clears both.

### AA6. A long tool call is reported, not killed (F7)

1. Launch a worker that runs `Start-Sleep 1500`. Set the long-tool threshold to 1 minute for the test.

**Expected:** one `long-tool` notice to the launcher naming the tool and its age. The process keeps running.

### AA7. A worker that dies is reported (F9)

1. Launch a worker, then kill its process from Task Manager.

**Expected:** when the reaper marks it dead, the launcher gets a `died` notice.

### AA8. A report to a launcher that is down arrives when it comes back (F4)

1. Launch a worker. Exit the launcher's session. Have the worker report.
2. Resume the launcher's card.

**Expected:** the report is in the launcher's queue while it is down, and the SessionStart digest lists the worker
as done. The first tool call or turn end delivers the report.

### AA9. A message delivered in a lost turn is redelivered as a possible repeat (F12, stage 3)

1. Queue a message to a session. Let the permission hook deliver it. Kill the room before the turn ends.
2. Restart the room and resume the card.

**Expected:** the message is delivered again with the "may be a repeat" banner. `acked_at` is set only after the
resumed turn ends.

### AA10. A dead letter tells its sender (F3)

1. Send a message to an unsupervised idle card with no Stop hook. Set the dead-letter threshold to 1 minute.

**Expected:** the sender's queue gets the dead-letter notice with the reason. The recipient's card shows a
dead-letter chip. The message is still queued.

### AA11. A compacted worker is pointed back at its brief (F11, stage 3)

1. Launch a worker, then run `/compact` in it.

**Expected:** the new context carries the digest line naming its launcher, its `BRIEF.md` and `atrium_report`.

### AA12. A message to a runner that cannot receive it says so at send time (F15)

1. In the throwaway room, start a gemini session by hand, not through atrium, so atrium does not own its terminal.
2. From another session, `atrium_say` to it.
3. Repeat with a claude session started from a shell whose settings have no atrium hooks.

**Expected:** the gemini send answers `undeliverable`, naming the runner and the two alternatives (relaunch under
atrium, or the human relays it). The claude send answers `queued-unconfirmed`, because the runner declares hooks
and the card has shown none. `atrium_peers` shows `reachable: no` and `reachable: unconfirmed`. Both messages are
still in the queue, and the gemini one shows on the board as a dead letter at once.

### AA13. The guard survives an unreachable daemon (resilience)

1. Stop the throwaway room daemon. End a turn in a launched worker.

**Expected:** the turn ends normally. No hang, no error shown to the model.

## Open questions for clint

1. **May a launcher answer its own workers' permissions?** Today only a human can. Allowing it would close F2
   without waking clint, at the cost of an agent approving another agent's commands. Recommend no for now: the
   notice tells the launcher, and the launcher tells clint.
2. **Does an `atrium_say` to the launcher count as a report for the guard?** Recommend yes, so a worker that
   already spoke is not made to repeat itself. The alternative is to demand the structured report every turn.
3. **The timer defaults.** 2 minutes to the launcher, then 5 to 15 more to the board, 20 minutes for a long tool,
   3 forced turns an hour. These are guesses sized to today's waves. Confirm or give numbers.
4. **Adding `--settings` to launched claude sessions.** It is a change to what a launched session runs with, even
   if only the Stop hook. Confirm that is acceptable under the "Stop is optional" rule as framed above.
5. **A card that went to `done` without its work landing (dispatch-notify).** Should `done` on an
   `origin:agent` card require a verified sha before the card moves, rather than accepting it as `unverified`?
6. **When does the no-hook send warning (F15) ship?** It is in stage 2 because it needs sa22's adapters. The
   send-time `undeliverable` answer for a runner with no hooks is small and could ride stage 1. Recommend stage 1,
   so the gemini case stops answering `queued` first.

## Appendix: review rounds

Reviewed with Mercurius (reviewer codex, gpt-5.5). The repo has no `mercurius.yaml`, so the session ran on the
server's own config and generic calibration.

### Round 1: needs changes

| Ref | Finding | Taken | What changed |
| --- | --- | --- | --- |
| C1 | Stage 1 promised a board escalation at +10 min, but the only timer was in stage 2. | yes | Stage 1 item 6 is now the first slice of the watchdog: the ticker, with only the silent-stop rows. Stage 2 adds the other rows to it. |
| C2 | The notice dedupe key `(worker, source, state)` was undefined and disagreed with AA2. | yes | A table of keys per source. `silent-stop` keys on the prompt that opened the turn. AA2 rewritten to one notice per prompt. |
| Q1 | Stage 1 depends on `--settings`, which is still an open question. | yes, as a fallback | Stage 1 item 5 now says what ships if clint says no: the guard where the Stop hook is already installed, and the stage 1 ticker at 2 minutes elsewhere. The question stays open for clint. |
| A1 | Open questions with recommendations should become decisions. | deferred | They become decisions when clint answers them. Converting them now would decide for him. |
| A2 | The environment overrides were only partly named. | yes | A constants table with defaults and override names under the watchdog. |

Added between rounds, from the orchestrator and sa22: failure mode F15 (a message to a runner with no atrium
hooks), and the delivery capability section. The capability lives on sa22's `Adapter` as `Delivery []string`, and
the effective list drops the hook paths when the adapter's `hooks` check is not `ok`.

### Round 2: needs changes, two consistency fixes

| Ref | Finding | Taken | What changed |
| --- | --- | --- | --- |
| C1 | The watchdog's silent-stop row did not share the guard's exemption for a peer message to the launcher. | yes | The row now reads "no report and no peer message to its launcher since last prompt", the same test as the guard. |
| C2 | Messages to `done` cards were queued in one section and dead letters in another. | yes | One rule under "Dead letter": a model's send to a `done` or `dead` card is refused with 409 as today, an automatic notice is queued and escalated at once, an `undeliverable` send is a dead letter at once. |
| A1 | Convert open questions to decisions after clint answers. | deferred | Same as round 1. |

No structural findings in round 2, so no third round was run.
