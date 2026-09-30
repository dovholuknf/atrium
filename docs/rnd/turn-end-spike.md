# Spike: a launched agent never ends a turn with nobody told

Spike only. Nothing here is built. Written by sa45 for the orchestrator (`atrium-87300`) and clint on 2026-09-24.
It sits on `docs/runtime/a2a-reliability-design.md` and fits `docs/runtime/work-ledger-design.md`, and it asks clint to reverse one
decision in the first of those (see "The decision this needs").

## The short version

- A launched worker's turn ends and Claude Code runs atrium's Stop hook, which asks the room what to say.
- Today the room either hands back queued messages or says nothing, and when the worker owes its launcher a report
  it tells the launcher afterwards that the worker went quiet.
- The Stop hook can refuse the stop and hand the model a reason. Claude Code and codex both honour that, gemini has
  the same thing under another name, and atrium already uses it to deliver messages to idle sessions.
- So the room can answer a silent stop with one short instruction: report to your launcher with `atrium_report`,
  then stop. The worker spends one small turn doing it, the report lands where the launcher and the work ledger read
  it, and the turn ends with somebody told.
- This happens once per prompt at most. The hook already refuses to block the Stop that follows a blocked one, and
  a dedupe row keyed on the prompt stops a second nudge. A worker that still says nothing gets today's notice to its
  launcher.
- Human-launched cards and turns a human prompted are never nudged. A hook failure of any kind lets the turn end,
  as it does now.
- No new blocking tool is needed. An ended turn already costs nothing while it waits, and atrium types the answer
  into a launched worker's terminal when one comes. What went missing was the report, not a way to wait. A worker
  that needs an answer reports `question` with its ask and ends the turn.
- The spike also found a gap that likely explains the card clint saw. The Stop that ends a turn which a Stop hook
  continued is never sent to the room, so after a message is delivered at turn end the card stays `running` with
  the previous turn's unread dot until something else moves it.
- The smallest first step fixes that gap: the hook posts every Stop and never blocks on the second one. It is a
  bug fix under today's rules. The nudge is the second step, and it waits on clint reversing "no forced turns".

## What clint asked for

A subagent card showed an unread-turn dot while it was running again. "Subagents shouldn't ever be in this sort of
state. If atrium determines this it should instruct the agent to report to whomever it needs to report to, or call
some `atrium_needs_user_input` type of tool that basically blocks the agent until the user comes back to it. It
feels like it lets things go missing."

## What exists today

- **The Stop hook** is `atrium turn --event end` (`internal/cli/turn.go`). It posts the turn end to the room's
  `/stop` and prints the room's answer, which Claude Code reads. The room's answer (`handleStop` in
  `internal/daemon/messages.go`) is one of two things: queued messages as `{"decision":"block","reason":...}`, which
  sends the model back to work with the messages as its instruction, or nothing, which lets the turn end.
- **Launched claude sessions get the hook** through `--settings` (`withStopHook` in `internal/daemon/a2a.go`) when
  the operator has not installed it already.
- **A silent stop is reported, never corrected.** On the nothing path, `silentStop` checks that the card was
  launched by another session (`origin:agent`), is in `needs-input`, and owes a report (`prompted_at` later than
  `reported_at`). It then queues one notice to the launcher, keyed on the prompt. The watchdog does the same after
  two minutes for a card with no Stop hook, and rings the board on clint's backoff.
- **Decision 3 of the a2a design** (clint, 2026-09-23) is "No forced turns. The Stop hook never blocks for the
  guard." An earlier draft did block, and clint rejected it because a forced turn spends tokens every time it fires.
- **What counts as a report** today (`MarkReported`): `atrium_report` or `atrium finish` with any status, and any
  peer message to the launcher (`atrium_say`, `atrium tell`, `atrium ask --peer`, `atrium answer`). `atrium ask` to
  a human does not count.

## Findings

### 1. The Stop that ends a continued turn never reaches the room

`turnEnded` in `internal/cli/turn.go` returns before it posts anything when `stop_hook_active` is set. Claude Code
sets that field on every Stop of a turn that only runs because a Stop hook blocked the one before. The guard is
right about not blocking twice. It is wrong to also not report, and the result is:

1. A worker's turn ends. The room files the card `needs-input` and notes an unseen turn (the dot).
2. A message is queued for it, and the next Stop carries it. `handleStop` calls `turnResumed`, so the card is
   `running`.
3. The worker acts on the message and its turn ends. This Stop has `stop_hook_active` set, so the hook prints
   nothing and posts nothing.
4. The room never hears that the turn ended. The card stays `running`, the dot from step 1 stays because a peer's
   message does not count as the human reading the turn, no silent-stop check runs, and the watchdog does not look
   at it because it only looks at `needs-input` for silent stops.

That is the card clint describes: an unread dot on a card that reads as running. It is the likely cause, not a
proven one: this spike did not trace the particular card. `TestTurnRefusesToBlockInsideABlockedTurn` pins the
behaviour, since it asserts the hook returns before it would reach a daemon.

Any nudge built on the Stop hook makes this worse, because every nudge is a blocked Stop followed by an unreported
one. So it is fixed first.

### 2. What each runner's turn-end hook can do

| Runner | Turn-end hook | Can refuse the stop | Loop guard | Default timeout | In atrium today |
| --- | --- | --- | --- | --- | --- |
| claude code | `Stop` | yes: `{"decision":"block","reason":...}`, or exit code 2 with the reason on stderr | `stop_hook_active` on the next Stop | 600 s, per-hook `timeout` | wired, used to deliver messages, added to launched sessions by `--settings` |
| codex | `Stop` | yes, same JSON, measured in `docs/runtime/other-runners.md` (the BANANA probe) | `stop_hook_active`, measured | not measured | wired when the operator installs the codex Stop hook. Install all skips it, and launched codex sessions get no `--settings` equivalent |
| gemini | `AfterAgent` | yes: `decision: "deny"` sends the reason to the agent as a new prompt. Exit code 2 does the same with stderr | `stop_hook_active` | 60 s | no gemini hooks target yet, so the watchdog is all gemini has |

From the Claude Code hooks reference: a Stop hook that times out, errors, or exits with any code but 2 lets the stop
proceed. Prompt and agent hook types are not offered for `Stop`, so the hook stays a command. Nothing in the
reference limits how often a Stop hook may block, which is why atrium keeps its own guard.

So one answer from the room serves claude and codex unchanged, and gemini needs only a hooks target that maps
`deny` onto the same answer when it lands.

### 3. What the nudge costs, against what it replaces

A nudge is one extra model turn for the worker: the whole context is sent again (mostly a prompt-cache read), the
reason is about eighty tokens, and the model's output is one `atrium_report` call and a stop. It fires at most once
per prompt, and only on a turn that is already a silent stop.

What that turn replaces today is a notice typed into the launcher. The launcher then spends a turn of its own,
usually on a larger context than the worker's (the orchestrator's is the largest on the board), reading that the
worker went quiet, and often a second one sending the worker "report please", which costs the worker a turn anyway.
So on the case it covers, the nudge is likely cheaper than what happens now. It is still a forced turn, and whether
to spend it is clint's call.

### 4. A blocking "needs input" tool buys little and brings hazards

The question was whether an MCP tool should park a worker until the user or launcher answers, the way Mode A's
`submit` long-polls.

**It is possible.** The model waits on a tool result and spends no tokens while it waits. The control MCP runs in
the hub, so the hub can hold the call and long-poll the room in a loop the way the Mode A agent re-polls on a
keepalive, and a room restart then does not break it.

**It is not needed for launched workers.** A card `atrium_launch` started is supervised: atrium owns its terminal.
A worker that ends its turn after `atrium_report status=question` is already parked. It costs nothing while it
waits, its card is in `needs-input` with reason `question`, the launcher has the ask verbatim, and the answer is
typed into its terminal when it comes, or carried in by a hook. The queue is durable, so an answer sent while the
room is restarting arrives after it.

**The hazards, all of which the ended turn does not have:**

- The human's natural answer does not unpark it. Text typed into the terminal while a tool is running waits in
  Claude Code's input until the tool returns, so a human who types the answer sees nothing happen until they press
  Esc, which cancels the tool.
- A hub restart drops the open request. The model gets a tool error, spends a turn on it, and may decide to carry
  on without the answer.
- Each runner's MCP client has its own tool timeout (`MCP_TOOL_TIMEOUT` for claude, a per-server setting for codex
  and gemini). A park past it is an error the model sees. Parking for hours means changing the operator's settings.
- The card reads `running` with a tool in flight. The long-tool watchdog calls it stuck at twenty minutes and needs
  an exemption, and the board needs a new state to show it as waiting.

A park is worth building only for a session atrium cannot type into and that has no Stop hook, and such a session
also cannot be nudged, so it is left for later.

### 5. What counts as reported

Unchanged from the a2a design, with one addition:

- `atrium_report` and `atrium finish` with any status.
- Any peer message to the launcher, or to the arbiter once the ledger exists: `atrium_say`, `atrium tell`, `atrium
  ask --peer`, `atrium answer`.
- **New: `atrium ask` to a human.** A worker asking the human something has said exactly why it stopped, and
  nudging it to also tell its launcher would spend a turn on nothing. It counts as reported, and the launcher gets
  the ask as a notice so it is not left out.

Not a report: a message to any other session, an Open Questions block in the turn's last message (it is addressed
to the human and the launcher never sees it), and anything atrium writes itself.

## Recommended design

### The rule

In `handleStop`, on the path with no queued messages, before `silentStop`:

```
card is launched by another session (origin:agent, lineage not @human)
AND it owes a report (prompted_at later than reported_at)
AND the prompt that opened the turn did not come from the human
AND no report-nudge notice exists for (card, prompt key)
  -> record the report-nudge notice
  -> answer {"decision":"block","reason": <the nudge>}
  -> turnResumed, and do not note the turn for seen
otherwise
  -> today's path: silentStop, noteTurnForSeen, nothing
```

The nudge, worded as atrium and not as a person, short, and naming the one way out:

```
atrium: this turn is ending without a report, and atrium-87300 launched you. Call atrium_report now (done,
blocked, question or progress, with a summary), or run `atrium finish --status ...` if you have no atrium tools.
If you are waiting on an answer, report question with your ask. Then end your turn. atrium will not ask again this
turn.
```

It names `atrium_report`, not `atrium_say`, because a report is recorded on the card and in the ledger whether or
not the launcher is alive. `atrium_say` to a dead launcher is refused.

### Why it cannot loop

- **The hook never blocks twice in a row.** The Stop after a block carries `stop_hook_active`, and the hook
  refuses to block on it whatever the room says. After step 1 it posts that Stop with the flag, and the room never
  answers it with a block either, so there are two independent guards.
- **Once per prompt.** The dedupe row is `a2a_notice` with source `report-nudge` and the prompt's key, the table
  that already dedupes silent-stop notices. A nudge is not a prompt: it writes no `prompted` event, so it cannot
  make the worker owe a report for the nudge itself.
- **Upward only, still.** The nudge goes to the worker that just stopped, in its own turn, from its own hook. It
  never writes to the launcher and never starts a turn in an idle session. The a2a design's rule that automatic
  messages only travel upward holds for everything queued. The nudge is a hook's answer, not a queued message.

### The hazards

- **A worker that cannot report.** No control MCP and no shell, or the report is refused. It ends its turn again,
  the Stop carries `stop_hook_active`, and the room runs today's silent-stop path: the launcher is told and the
  board rings. Cost: one turn.
- **A launcher that is gone.** The nudge still fires, since the report is recorded without it. The silent-stop
  notice has nowhere to go, as today. Once the ledger exists the report is a `report` row, and the board flags
  "arbiter gone".
- **A human-launched card.** `Launched()` is false for `@human` lineage and for a session that joined by itself.
  Never nudged. The same holds for a card with `origin:agent` but no lineage.
- **A turn the human prompted.** When clint types into a launched worker, the turn is his and the worker owes him
  an answer in the terminal, not a report upward. Never nudged. This needs one fact atrium does not keep on the
  card today: who sent the prompt that opened the turn. The `prompted` event already carries `from_peer` for a
  peer's message. It needs keeping beside `prompted_at`, and the launch prompt needs checking to see that it reads
  as the launcher's. Today's silent-stop notice has the same blind spot and gets the same fix.
- **A Stop hook that fails.** Nothing changes in the hook's posture. Any failure, timeout (two seconds) or
  unparseable answer lets the turn end. On the room side, a failure to record the dedupe row means no block,
  because a block that cannot be deduplicated is the one that could repeat.
- **Codex.** The same answer works. A launched codex session has the Stop hook only when the operator installed
  it, since there is no `--settings` for codex. Without it the watchdog covers the stop, as today.
- **The kill switch.** `ATRIUM_A2A_NUDGE=off` restores today's behaviour, read the way the other a2a overrides are.

### How it plugs into the work ledger

The ledger's states and rules do not change. The nudge acts before the ledger sees anything, and its effect is
that more turns end with a report, so more items are moved by reports and fewer are rebuilt by hand.

- A nudged report is an ordinary `report` row. A `done` moves the item to `reported`, and anything else is logged.
- The nudge itself is not a log row. The `a2a_notice` row is its record, and a work log with one row per prompt
  would trim real history to keep it.
- A silent stop that survives the nudge is not a state. It is a sub-label on an `open` or `reopened` item: "quiet
  since 14:02, asked to report, did not". `ended-without-report` stays reserved for a session that is gone.
- The nudge names the arbiter once the ledger exists, since the arbiter is who hears.
- The ledger design says "the Stop hook is untouched" and "no hook gains work". Step 1 changes what the hook
  posts, and step 2 changes what the room answers, with no new writes except the dedupe row. The ledger's own
  writes stay off the hook path.

### The dot

With the nudge, a launched worker's turn normally ends with a report its launcher has. The unread dot on a
launched card then says the human has not read a turn whose audience was the launcher, which is noise. A later
step can note a launched card's turn as unseen only when it ended without a report after the nudge, so the dot
on a worker means "this went quiet and nobody was told". That is a change to `docs/runtime/seen-design.md` and is left
for after the nudge ships.

## The decision this needs

**Reverse a2a decision 3 for one case.** Allow one forced turn per prompt, only for a launched worker whose turn
is ending without a report, only when a peer or the launch opened the turn. Recommended, because it spends one
small turn where today the launcher spends one or two larger ones, and because it is the direct form of what clint
asked for. If the answer is no, step 1 still ships and step 2 does not.

## Build steps

1. **The hook posts every Stop (ROOM-SIDE, the hook binary).** `turnEnded` posts when `stop_hook_active` is set,
   with the flag in the body, and still returns nothing whatever the room answers. `handleStop` treats a flagged
   Stop as a turn that really is over: it runs the silent-stop check and notes the turn for seen, and never answers
   with a block or takes messages. Tests: the CLI posts and still prints nothing on a flagged Stop, and a block in
   the answer is ignored. In the room, a flagged Stop moves a `running` card to `needs-input`, leaves queued
   messages queued, and sends a silent-stop notice when one is owed. This is a fix under today's rules and needs no
   decision.
2. **The nudge (ROOM-SIDE).** Record who opened the turn beside `prompted_at`, count `atrium ask` to a human as a
   report and forward it to the launcher, and add the rule above with its kill switch. Tests: one nudge per prompt,
   none for a human-launched card, none for a human-prompted turn, none after a report, none when the dedupe write
   fails, no `prompted` event written by a nudge, and the silent-stop notice on the Stop after an ignored nudge.
3. **Later, each on its own.** A gemini hooks target mapping `deny` onto the same answer. The dot rule above. A
   parking tool, only if a runner turns up that atrium can neither type into nor hook.

## Not done in this spike

No prototype was built. The one question a prototype could answer, whether the runners honour a block with a
reason at turn end, is already answered: claude by atrium's message delivery, which runs on it every day, codex
by the measured probe in `docs/runtime/other-runners.md`, and gemini by its hooks reference. The gemini and Claude Code
hook facts above come from their published references, not from a probe on this machine.
