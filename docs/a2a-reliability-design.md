# Agent-to-agent reliability: no silent stall, no silent loss

Design for how atrium guarantees that work handed from one session to another never stalls without somebody
hearing about it, and that a message between sessions is never lost without somebody hearing about that either.
Clint approved stage 1 with changes on 2026-09-23 (see "Decisions"), and stage 1 is built. Stages 2 and 3 are
design only.

Read `docs/agent-messaging.md` first. It is the reference for the queue and the two delivery paths this design
builds on. `docs/agent-lineage-design.md` is the design for recording who launched whom. Stage 1 ships its two
columns and the write-once setter.

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
| F6 | A guard loops or burns tokens. | No guard exists. Resolved by never forcing a turn (decision 3). |
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

**Every agent-launched session ends each turn in a report, or atrium says it did not.** A card is agent-launched
when it carries the `origin:agent` tag, which `launchHandler` already adds. Human-started cards are untouched by
all of this.

### The report

One verb, two doors:

- **`atrium_report`**, a new tool on the control MCP. Every launched claude session already has the control MCP
  (`--mcp-config ~/.atrium/mcp.json`), and the CLI is not on PATH on every machine, so the tool is the primary
  door.
- **`atrium finish`**, extended with `--status` and `--sha`. The CLI door for any runner without MCP.

Both land on one room endpoint and one function, so they cannot drift.

```
status    done | blocked | question | progress      required
summary   what happened, in the worker's words      reaches the launcher verbatim, bounded to 8000 chars
sha       the commit the work landed as             required for done, unless no_commit says why
no_commit why a done has no commit                   e.g. "research only, answer is in the summary"
ask       what the worker needs                      required for blocked and question
```

What each status does to the card:

| Status | Card moves to | Launcher is told | Counts as the turn's report |
| --- | --- | --- | --- |
| `done` | `done` | yes | yes |
| `blocked` | `needs-input`, reason `blocked` | yes | yes |
| `question` | `needs-input`, reason `question` | yes | yes |
| `progress` | unchanged | yes | yes |

`progress` exists for a worker that ends a turn on purpose while something runs in the background. Without it
every such stop would be reported as silent.

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
2. No launcher (empty, `@human`, unresolvable): nothing is queued. The board still hears, because the watchdog's
   escalation is computed from the worker alone.
3. Launcher `done` or `dead` (F10): queued anyway, because a dead card can be resumed and its queue survives.
4. Claim the notice in `a2a_notice`. A key already there means it was sent, and nothing more happens.
5. Deliver through `deliverPeer`, the path `atrium tell` uses: typed when atrium owns the launcher's terminal
   and the gate is open, queued with the on-screen retry otherwise, so F3b stays covered. Sent from the worker's
   handle, so the launcher can reply straight to it.

It never fails its caller. It runs beside a report, a Stop hook and a tick, and every failure is logged.

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
prompt. A worker whose one silent stop is seen by the Stop hook and again by every watchdog tick produces one.

**Automatic notices only travel upward, from a worker to its launcher or to the board.** Atrium never writes an
automatic message to a worker. So no cycle of automatic messages can exist. Any loop needs a model to choose to
send, and model sends stay bounded by the 20 per minute peer limit.

## Silent stops (F1)

**Atrium never forces a turn.** An earlier draft had the Stop hook block an agent-launched session that ended a
turn without reporting, sending it back to work with the contract as the reason. Clint rejected it: atrium is
token-conservative, and a forced turn spends tokens every time it fires. So a silent stop is DETECTED and
REPORTED, never corrected, and the Stop hook's answer is exactly what it was before this design: queued messages
when there are any, nothing otherwise.

`handleStop` already runs on every turn end and already knows the card. On the path where it has no messages to
deliver, which is a turn that really is over, it asks one question:

```
card has origin:agent
AND it is in needs-input (the turn just ended)
AND prompted_at is later than reported_at      (nothing said to the launcher since the last prompt)
  -> notifyLauncher(worker, "silent-stop", key = the prompt's time)
```

A report is `atrium_report`, `atrium finish --status`, or any peer message to the launcher (`atrium_say`,
`atrium tell`, `atrium ask --peer`, `atrium answer`). Clint's decision: a message to the launcher counts, and the
structured report is the form a worker is asked to prefer. `prompted_at` is stamped by the store wherever a
`prompted` event is written, so a long turn cannot push the last prompt out of reach.

The launcher hears about the stop within seconds, which is what `sa20` needed at 12:38:34. The board hears next,
on the backoff below.

### Why this cannot loop (F6)

- **The Stop hook never blocks for the guard.** No turn is ever started by atrium, so there is nothing to loop.
- **`stop_hook_active` handling is unchanged.** `turn.go` still refuses to block on the Stop that follows a
  blocked Stop, which bounds the existing message delivery exactly as before.
- **One notice per prompt.** The dedupe key is the prompt, so however many Stops or watchdog ticks see the same
  silent stop, the launcher is told once.
- **Upward-only notices.** Atrium never writes to a worker. A launcher's reaction to a notice is a model choice,
  bounded by the 20 per minute peer limit.

### The Stop hook on agent-launched sessions

Approved by clint for agent-launched claude sessions. `launchLocked` adds `--settings <json>` in front of the
claude arguments for an `origin:agent` launch, holding one Stop hook: `<atrium> turn --event end`. Given the
decision above, **this hook only reports that the turn ended.** It is the same hook the operator can install, with
the same three safety properties in `turn.go`, and on these sessions it has nothing to block with except queued
messages, the same as everywhere else.

- **Not added when the operator already has it.** `claudeconf.Inspect` reads the operator's settings, and when a
  Stop hook reporting `turn-end` is installed, Claude Code already runs it for every session.
- **The program is taken from a hook the operator already has**, since that binary is known to run from a claude
  session on this machine. The room's own binary is the wrong answer: `atrium2` has no `turn` subcommand. With no
  atrium hook installed at all, nothing is added, and the watchdog catches the stop at 2 minutes.
- **Human sessions keep the opt-in.** The `CLAUDE.md` rule governs installing the hook into the operator's
  settings, and none of this touches install all, the hooks pane, or `settings.json`.

## The watchdog (F2, F7)

A pass in the room daemon on the reaper's tick, every 20 seconds (`ReapEvery`), after liveness is settled so a
card just marked dead is not reported as stuck. It reads the store and the in-memory activity. It never calls a
runner, never types, never wakes a model. It only calls `notifyLauncher` and sets the board escalation.

| Condition on a live `origin:agent` card | Launcher | Board |
| --- | --- | --- |
| Silent stop: `needs-input`, owing a report | at the Stop hook, or at 2 min by the watchdog when no Stop hook caught it | on the backoff, from the moment it stopped |
| Stuck tool: one tool call running past 20 min | once, at 20 min | on the backoff, from 20 min |
| Permission wait (F2) | never. A launcher never answers its workers' permissions | the board's permission nag, on the same backoff, "X is STUCK on a permission, n minutes" |

**The backoff is clint's:** 1m, 2m, 5m, 10m, 30m, 1h, 2h, 4h, 8h, 24h, then every 24 hours. One schedule for all
three, `EscalationBackoff` in `internal/daemon/a2a.go` and `nagSlot` in `notify.js`. **It resets when the card
moves**: an escalation is dropped the moment the card leaves the stuck state, and a card that gets stuck again
starts at 1 minute. A permission nag resets per request.

**Auto mode means no prompt at all.** With approve everything on, the permission is decided before it pends, so
nothing is waiting and nothing rings. That was already true and is unchanged.

**The long-tool trigger cannot tell a hung process from a long build**, so it only says what it sees ("sa20 has
been in one Bash call for 25 minutes"). It never kills anything. It reads the activity table past its 15 minute
staleness cutoff, since a tool that ran that long is the one it is looking for. A session that died mid-tool is
marked dead by the reaper first and leaves the watched set.

| Constant | Default | Override |
| --- | --- | --- |
| `SilentStopNotifyAfter` | 2 min | `ATRIUM_A2A_SILENT_STOP` |
| `LongToolAfter` | 20 min | `ATRIUM_A2A_LONG_TOOL` |

Each override takes a Go duration (`90s`, `5m`). A value that does not parse is ignored, the same rule `launchCap`
uses. The backoff has no override: it is the operator's schedule, and one place to change it.

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

- **The launcher** hears through its queue, typed when its terminal is free, carried by a hook otherwise, from
  the worker's handle so a reply reaches the worker.
- **The board** hears through a new field on the card, `escalation` (`source`, `since`, `count`, `minutes`,
  `text`), computed by the watchdog and held in memory like activity. The board rings each time `count` steps up,
  through the existing notify path (`notify.js`, the toast log), titled from `text`, for example
  `sa20 is STUCK: it stopped without reporting, 5 minutes`. The body names the launcher, which is the identity
  half of dispatch-notify. The card also shows a `stuck` chip.
- A worker whose launcher cannot be found (a crossed room, a pruned card) still escalates to the board, because
  the board's escalation is computed from the worker alone.

## How each part keeps the resilience rules

From `CLAUDE.md`, "Resilience guarantees (daemon)".

1. **Storage failure halts.** The report endpoint writes through the store and returns its error, the way
   `handleFinish` always has. `prompted_at` is written inside `appendEvent`, under the same guard, so a store that
   cannot record it cannot record the event either. A store that fails closes the agent listener as today, and
   the watchdog's list fails with it, so it acts on nothing rather than on a guess.
2. **A hook never fails a session.**
   - The Stop hook is unchanged in `turn.go`. In `handleStop` the silent-stop check runs only on the path that
     already answers `{}`, and every failure inside it (a store read, a notice that cannot be queued) is logged and
     the answer is still `{}`.
   - The hook-seen stamp in the permission hook is one write per card, not per call, and returns its error like
     every other write on that path. The one in the Stop hook is logged and ignored.
   - `--settings` is added only when a working atrium hook command is known. Otherwise the session launches as
     before and the watchdog covers the stop.
3. **`/activity` stays fire and forget.** Nothing here reads `/activity`'s answer or adds work to its path. The
   watchdog reads the in-memory activity table the endpoint already fills.
4. **Shutdown is narrated and bounded.** The watchdog is not a new goroutine. It rides the reaper's tick and
   stops with it.

## Stages

### Stage 1: the silent stop is caught, within seconds (BUILT)

1. Lineage: migration `0055` cherry-picked from `claude/dispatch-notify`, `SetLineage` write-once, `spawned_by`
   forwarded by `atrium_launch`, `@human` for the board's dialog.
2. `atrium_report` on the control MCP, and `atrium finish --status --sha --no-commit --ask`, validated. A `done`
   on an agent-launched card needs a sha or `no_commit`. A sha the worktree does not contain is accepted and the
   card flags it (`sha unverified`). `blocked` and `question` need `ask`.
3. `notifyLauncher`, deduped per `(worker, source, key)` in the `a2a_notice` table, not rate limited.
4. Silent-stop detection in `handleStop`. No block, ever.
5. The Stop hook via `--settings` on agent-launched claude sessions, reporting only.
6. The watchdog on the reaper's tick: silent stop and stuck tool, launcher then board on the backoff. The board's
   permission nag moved to the same backoff and wording.
7. F15, the send-time warning. The daemon stamps `tool_hook_seen_at` and `stop_hook_seen_at`, and
   `handleMessage` and `/tell` answer `queued`, `queued-unconfirmed` or `undeliverable`, with the alternatives.
   `atrium_say` passes the warning through. Which runners have hooks is a stand-in (`runnerDelivers`: claude and
   codex) until sa22's `Adapter.Delivery` lands.
8. `atrium_launch` appends one line to the launch prompt asking for a report before the turn ends.

### Stage 2: nothing waits unseen

1. The watchdog's `died` row (F9).
2. Dead letters, the sender told, the board chip (F3).
3. `Adapter.Delivery` from `internal/runnersetup` in place of `runnerDelivers`, and `reachable` in `atrium_peers`.

### Stage 3: nothing is lost across a restart

1. `acked_at` and redelivery of unacked messages, with the "may be a repeat" banner.
2. The SessionStart digest for launchers and workers.
3. `atrium_peers` `mine`.

## Test plan

Stage 1's scenarios are in `docs/test-plan.md`, section AB, and every stage 1 failure mode has a Go test in
`internal/daemon/a2a_test.go`, `internal/store/a2a_test.go` and `internal/link/a2a_test.go`. The scenarios below
are for the later stages and move into the test plan when their stage ships. Each runs in a throwaway room (see
the throwaway hub and room recipe) and never against the live board.

### Later: a worker that dies is reported (F9, stage 2)

1. Launch a worker, then kill its process from Task Manager.

**Expected:** when the reaper marks it dead, the launcher gets a `died` notice.

### Later: a dead letter tells its sender (F3, stage 2)

1. Send a message to an unsupervised idle card with no Stop hook. Set the dead-letter threshold to 1 minute.

**Expected:** the sender's queue gets the dead-letter notice with the reason. The recipient's card shows a
dead-letter chip. The message is still queued.

### Later: a report to a launcher that is down arrives when it comes back (F4, stage 3)

1. Launch a worker. Exit the launcher's session. Have the worker report.
2. Resume the launcher's card.

**Expected:** the report is in the launcher's queue while it is down, and the SessionStart digest lists the worker
as done. The first tool call or turn end delivers the report.

### Later: a message delivered in a lost turn is redelivered as a possible repeat (F12, stage 3)

1. Queue a message to a session. Let the permission hook deliver it. Kill the room before the turn ends.
2. Restart the room and resume the card.

**Expected:** the message is delivered again with the "may be a repeat" banner. `acked_at` is set only after the
resumed turn ends.

### Later: a compacted worker is pointed back at its brief (F11, stage 3)

1. Launch a worker, then run `/compact` in it.

**Expected:** the new context carries the digest line naming its launcher, its `BRIEF.md` and `atrium_report`.

## Decisions (clint, 2026-09-23)

1. **Permissions.** A launcher never answers its workers' permissions. The human does, unless auto mode is on, in
   which case the prompt never appears. A worker waiting on one escalates to the operator's board as "agent X is
   STUCK on a permission, n minutes", re-notified on the backoff 1m, 2m, 5m, 10m, 30m, 1h, 2h, 4h, 8h, 24h. The
   same schedule serves silent stops and stuck tools. It resets when the card moves.
2. **An `atrium_say` to the launcher counts as a report.** `atrium_report` and `finish --status --sha` stay as the
   structured form.
3. **No forced turns.** The Stop hook never blocks for the guard. A silent stop is detected, the launcher is
   notified, then the board on the backoff. The forced-turn budget and the loop rules that existed only for it are
   gone. `stop_hook_active` handling for message delivery is unchanged.
4. **The Stop hook via `--settings` for agent-launched sessions: approved.** It only reports turn end.
5. **A done report with an unverified sha is accepted, and the card flags it.**
6. **The F15 send-time warning ships in stage 1.**

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

### After approval

Clint approved stage 1 with six changes (see "Decisions"). The largest removed the forced turn and everything
that existed only to bound it (the per-card budget, the "when the guard gives up" path). The watchdog's board
escalation moved from fixed thresholds to his backoff, and the permission-wait row moved from a launcher notice to
the board's permission nag. The F15 warning moved into stage 1. The `auto` flag on queued notices was dropped: the
dedupe table and the worker's handle as sender carry what it was for.
