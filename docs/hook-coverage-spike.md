# Hook coverage

Atrium learns everything about a session from Claude Code hooks. It consumes five events today and there are
twelve. This works out what the other seven would buy, whether the permission gate is sitting on the wrong event,
and whether the board's columns should change shape depending on what is wired.

## What is wired today

| Event | Endpoint | What it does to a card |
| --- | --- | --- |
| `PreToolUse` | `/permission` | The gate. Moves the card to `needs-permission` and blocks the session. |
| `PreToolUse` | `/activity` | Sets the badge to `tool`, with the tool name. Written, not wired. |
| `SessionStart` | `/session` | Creates the card, records pid, cwd and resume id. |
| `SessionEnd` | `/session` | Moves the card to `dead` and forgets its activity. |
| `Stop` | `/stop` | Moves the card to `needs-input`, and delivers a queued message by blocking. |
| `PostToolUse`, `UserPromptSubmit`, `SubagentStop` | `/activity` | Written, not wired. |

## Sources and what is verified

Everything below about payload fields comes from the official hooks reference at
`https://code.claude.com/docs/en/hooks.md`, read for this spike. Two claims are flagged as unverified where two
readings of that page disagreed, and both are called out in place rather than resolved by guessing.

The reference also documents events not in the set of twelve: `PostToolBatch`, `PermissionDenied`,
`MessageDisplay` and `StopFailure`. They are covered at the end.

## The twelve, one at a time

Common to nearly every payload: `session_id`, `hook_event_name`, `transcript_path`, `cwd`. Tool and turn events
also carry `prompt_id` and `permission_mode`. `Notification` and `PreCompact` carry neither `cwd` nor
`transcript_path`, which matters because atrium's hooks derive the agent name from `cwd` when
`ATRIUM_AGENT_NAME` is unset.

### SessionStart

Wired. Payload carries `session_id`, `cwd`, `model`, and `source` with values `startup`, `resume`, `clear`,
`compact` and `fork`. Atrium already records `source` without interpreting it.

Worth having, and already the reason a session is visible before it does anything.

One thing it could start doing: `source` is `compact` when a session restarts itself after compaction, and
`fork` when it was forked. Both are currently filed as an ordinary start, so a card that survived a compaction
looks the same as one that was resumed by hand. See `PreCompact` below.

Failure posture: three second timeout, exit 0 on anything. A session must never fail to start.

### UserPromptSubmit

Not wired. Payload carries `user_input`, the prompt text.

What atrium learns: the operator typed something, so the session has work again. `/activity` with
`event=prompt`, which sets the badge to `thinking` and calls `turnResumed`, moving the card out of
`needs-input`.

Worth having, and it is the other half of `Stop`. Without it a card that landed in `needs-input` stays there for
the rest of the session unless a tool call happens to move it. A session answered in its own terminal would sit
on the board claiming to want you.

`user_input` is deliberately not sent. Atrium is not a transcript, and the prompt text is the operator's, not
the runner's, so there is nothing on a card it would answer.

Failure posture: fire and forget, one second, every failure ignored. It is not on the tool hot path, but it is
on the path between the operator pressing enter and the model starting, which is worse to add latency to.

### PreToolUse

Wired twice: the gate and the activity badge.

Payload carries `tool_name`, `tool_input`, `tool_use_id`, `permission_mode`, `agent_id` and `agent_type`. The
last two are present only for subagents, which atrium does not use today and could.

The gate belongs on `PermissionRequest` instead. See the next section. The activity half stays here whatever
happens to the gate, because it has to fire for calls the harness would never ask about.

Failure posture: the activity hook is fire and forget with a one second timeout. The gate blocks for as long as
the human takes and fails open on anything, because a gate that fails closed stops all work the moment atrium
does.

### PermissionRequest

Not wired to atrium. Wired in the dotfiles today to play a sound, so the event is confirmed to fire on this
machine.

This is the recommendation, and it has its own section below.

### PostToolUse

Not wired. Payload carries `tool_name`, `tool_input`, `tool_use_id` and `tool_result`.

What atrium learns: the tool finished, so the model has the floor again. `/activity` with `event=tool-end`,
which sets the badge to `thinking`.

Worth having, narrowly. It is the only thing that clears a `tool` badge. Without it a badge reads "running Bash"
from the moment the call starts until the next event of any kind, which for a long quiet turn is the whole turn.
The fifteen minute staleness cutoff is the backstop, and a backstop is not a reading.

`tool_result` is not sent. It is unbounded, it is the output of the tool, and no card shows it.

Failure posture: same as the activity hook. This runs as often as `PreToolUse`, so the same one second and the
same silence.

### PostToolUseFailure

Not wired. Payload carries `tool_name`, `tool_input`, `tool_use_id` and `error`.

Whether `PostToolUse` also fires when a tool fails is **unverified**. The reference lists the two as separate
events, which reads as mutually exclusive, and if that is right then without this hook a failed tool pins the
`tool` badge until something else happens.

Worth having, but not as a new idea. It should call the same script with `event=tool-end` and change nothing
about the endpoint or the card. A failed tool and a finished tool mean the same thing to a badge that only says
what is running now. Building a distinct `tool-failed` state, or storing `error`, would put the tool's problems
on a board that answers "what needs me", and a failing tool does not need you until the model gives up and
stops, at which point `Stop` says so.

So: one more line in `settings.json`, no code.

Failure posture: identical to `PostToolUse`.

### Notification

Not wired to atrium. Wired in the dotfiles for two matchers today. Payload carries `notification_type` and
`message`, and carries neither `cwd` nor `transcript_path`.

`notification_type` values are `permission_prompt`, `idle_prompt`, `auth_success`, `elicitation_dialog`,
`elicitation_url_dialog`, `elicitation_complete`, `elicitation_response`, `agent_needs_input`,
`agent_completed`, `quota_auto_resume_fired`, `quota_auto_resume_stale` and `quota_auto_resume_disabled`.

The daemon already handles this. `/activity` accepts `event=waiting` and the comment in `activity.go` names the
Notification hook as its source. Nothing sends it.

Worth having, matched, and **not worth having unmatched**. Three of those twelve types are a card wanting a
human: `idle_prompt`, `agent_needs_input` and `elicitation_dialog`. The rest are noise on a board, and
`permission_prompt` is actively circular when the gate is on, because atrium is the thing that caused the prompt
and would be told about a state it already set.

`agent_completed` is the interesting one and is **unverified** as to when it fires. If it means a subagent
finished it duplicates `SubagentStop`. If it means the top level session finished a long task it is a better
`needs-input` signal than `Stop`, because it fires when the harness thinks you should look.

The missing `cwd` is a real problem. A session with no `ATRIUM_AGENT_NAME` falls back to the leaf of the working
directory, and the hook process inherits the runner's directory, so `(Get-Location).Path` still answers. That
already works in the existing hooks and needs no change, but it is worth knowing it is the fallback carrying it
rather than the payload.

Failure posture: fire and forget, one second. Notifications fire rarely, but the hook still must not delay a
dialog appearing.

### SubagentStart

Not wired, and currently believed not to exist. `activity.go` says "Claude Code has no hook for a subagent
starting" and infers the count from a `Task` call going up.

It exists. Payload carries `agent_id`, `agent_type` and `user_input`.

This is the largest correctness win available. See the section below.

Failure posture: fire and forget, one second.

### SubagentStop

Not wired to atrium. Payload carries `agent_id`, `agent_type`, `last_assistant_message` and `stop_reason`.

`/activity` with `event=subagent-end`, decrementing the count. Already implemented and already tested.

Worth having, and worth having with `agent_id` added to the wire, so the pairing with `SubagentStart` is exact
rather than a tally. `last_assistant_message` is not sent, for the same reason `tool_result` is not.

Failure posture: fire and forget, one second.

### Stop

Wired. Payload carries `last_assistant_message` and `stop_reason`. Blocking it with a `reason` sends the model
back to work, which is the whole mechanism behind delivering a message to an idle session.

Worth having and already the most consequential of the wired hooks, because it is what makes `needs-input` mean
anything.

`stop_reason` is not currently read and could be. A turn that ended because the model was done is a different
card to one that ended on an error, and today both land in `needs-input` identically. The values of
`stop_reason` are **unverified**, so this is a note rather than a proposal.

Failure posture: three second timeout, exit 0 on anything, and it exits immediately when `stop_hook_active` is
set so a session already continuing because of atrium is never told to continue again.

### PreCompact

Not wired. Payload carries `triggered_by`, with values `manual` and `auto`. It carries neither `cwd` nor
`transcript_path`.

See its own section below.

### SessionEnd

Wired. Payload carries `reason`, with values `clear`, `resume`, `logout`, `prompt_input_exit` and `other`.

Worth having and already the only reliable signal that a session is over.

It could read `reason`. A card whose session ended as `clear` or `resume` is not over, it restarted, and the
board currently marks both `dead` and then revives the card when the new `SessionStart` arrives. That flicker is
harmless but visible, and `reason` is enough to suppress it: `clear` and `resume` mean a start is coming, so the
card can stay `running` and wait for it rather than dying and being resurrected a second later.

Failure posture: three second timeout, exit 0 on anything.

## PermissionRequest versus PreToolUse for the gate

### The problem with where the gate sits

`PreToolUse` fires for every tool call. The harness's own allow list has not been consulted yet, so atrium is
asked about calls Claude Code would never have surfaced. The hook works around this with a hardcoded skip list
of read-only tools and an `mcp__*` prefix rule, and the daemon works around the rest with standing rules. One
hundred and thirty four of them had to be imported from `settings.json` to get back to the silence the harness
already provided. That import is a feature now, `internal/claudeconf`, but it exists to undo a consequence of
listening on the wrong event.

`PermissionRequest` fires when a tool call needs a permission decision, which is to say when Claude Code is
about to ask. Everything the allow list already covers never arrives. The skip list becomes unnecessary. The
rule import stops being a prerequisite for a usable board and becomes what it should be, a way to carry
decisions across machines.

### What the payload carries

Verified from the reference:

```json
{
  "session_id": "abc123",
  "prompt_id": "550e8400-e29b-41d4-a716-446655440000",
  "transcript_path": "...",
  "cwd": "/home/user/my-project",
  "permission_mode": "default",
  "hook_event_name": "PermissionRequest",
  "tool_name": "Bash",
  "tool_input": { "command": "rm -rf /tmp/build", "description": "...", "timeout": 30000 },
  "tool_use_id": "toolu_01ABC123...",
  "agent_id": "subagent_xyz",
  "agent_type": "code-reviewer"
}
```

`tool_input` is the full tool input, the same object `PreToolUse` receives. For an `Edit` that is `file_path`,
`old_string` and `new_string`.

**The diff survives.** That was the question that mattered, because the board renders a real diff for an edit
and that diff is built in the hook from `old_string` and `new_string`. The block of the permission hook that
builds `$details` needs no change at all.

`tool_use_id` is new and is better than the dedup key the hook computes by hashing session, agent, tool and
command. It identifies the exact call rather than a call that looks the same. The known gap in `docs/backlog.md`
that says the hook sends no dedup key closes here, with a field the harness supplies.

`agent_id` and `agent_type` are new and mean a permission request from a subagent can be labelled as one. Today
a subagent's Bash call and its parent's are indistinguishable on the board.

### What does not survive

**Rewriting the command before approving it.** The hook currently returns `updatedInput` when the human edits a
request on the board, which is how "approve this but with `--dry-run`" works. `PermissionRequest` does not
support `updatedInput`. That is stated directly in the reference and both readings of it agree.

This is the whole cost of the migration and it is not small. The board's edit-then-approve field would have to
either disappear or change meaning to "deny, and tell the model to run this instead", which is a different
interaction: the model gets guidance and decides, rather than the human deciding and the model obeying.

**The exact output shape is unverified.** Two readings of the same page produced two shapes:

```json
{ "hookSpecificOutput": { "hookEventName": "PermissionRequest",
    "decision": "allow" | "deny", "decisionReason": "string" } }
```

and

```json
{ "hookSpecificOutput": { "hookEventName": "PermissionRequest",
    "decision": { "behavior": "allow" | "deny", "denyReason": "string",
      "updatedPermissions": [ { "type": "setMode", "mode": "...", "destination": "session" } ] } } }
```

They agree that the decision is allow or deny, that a reason rides along, and that there is no `updatedInput`.
They disagree on nesting and field names. Do not write the hook from this document. Emit a probe hook that dumps
its stdin and try both shapes against a live session before committing to one.

If the second shape is right, `updatedPermissions` with `type: setMode` is worth a look on its own. It would let
an approval also change the session's permission mode, which is close to what auto mode does today by
approving every request one at a time.

### The recommendation

**Move the gate to `PermissionRequest`. Keep the activity hook on `PreToolUse`.** They are separate concerns and
always were: one asks a question, the other reports a fact, and the fact has to be reported for calls nobody
would ever be asked about.

Do it in this order, because the risky part is not the new event, it is the silence:

1. Wire `PermissionRequest` alongside the existing `PreToolUse` gate, posting to a new path so nothing is
   ambiguous. Log both. Run for a day and compare: every request the new event raises should be one the old one
   also raised, and the old one should raise many the new one does not.
2. Confirm the diff renders from the new payload, on an `Edit`, a `Write` and a `MultiEdit`.
3. Decide what happens to edit-then-approve. Removing it is honest. Turning it into a deny with guidance is also
   honest. Leaving a field on the board that silently no longer rewrites anything is not.
4. Only then remove the `PreToolUse` gate, and with it the skip list.

### The migration risk, stated

- **Everything hangs on Claude Code's allow list being right.** With the gate on `PreToolUse`, atrium sees every
  call and a bad rule in `settings.json` costs you a question. With the gate on `PermissionRequest`, a permissive
  entry in `settings.json` means atrium is never asked, and a tool runs that the board would have caught. The
  gate stops being "atrium decides" and becomes "atrium decides what the harness could not decide alone". For
  permissions-only mode across a fleet, that is a real reduction in reach and it must be documented as one.
- **`ATRIUM_PERM_GATE=on` stops meaning what it says.** Today it gates every session unconditionally. After the
  move it gates every session that the harness would have asked about, which is a much smaller set. The
  documentation in `README.md` and `docs/hooks.md` says "every tool call is offered to you before it runs" and
  that becomes false.
- **`bypassPermissions` mode.** Unverified, but a mode that skips permission prompts entirely would presumably
  skip `PermissionRequest` too, and therefore skip atrium. `PreToolUse` has no such hole.
- **The 134 imported rules become dead weight.** Harmless, since a rule that never matches costs a lookup, but
  the board will show a rules list dominated by entries that can no longer fire.

Given all of that, the honest framing is a trade: `PreToolUse` gives complete coverage at the price of asking
about everything, and `PermissionRequest` gives the harness's own judgement about what is worth asking at the
price of trusting it. Atrium's users already trust `settings.json` for the sessions that are not gated, so the
trade is worth making, but not silently.

## SubagentStart

`activity.go` says Claude Code has no hook for a subagent starting, and the count is inferred from a `Task` tool
call going up and `SubagentStop` coming down. The comment names the consequence and clamps the count at zero so
the drift renders as something rather than nonsense.

`SubagentStart` exists. It carries `agent_id`, `agent_type` and `user_input`.

What changes:

- `/activity` grows `subagent-start`, and the increment moves off the `Task` special case in `onActivity` and
  `onPermRequest`. Two places currently do `if strings.EqualFold(tool, "Task")`, and both can stop.
- The count stops being inferred. A `Task` call that is denied, that errors before the subagent starts, or that
  the model abandons no longer leaves a phantom subagent on the card. That is the drift the comment describes and
  it goes away entirely.
- The clamp at zero stays. It is cheap and it defends against a `SubagentStop` arriving for a subagent whose
  start was lost to a one second timeout, which is a failure the fire-and-forget rule guarantees will happen.
- `agent_id` on both events turns a tally into a set. The card can then say which subagents are running, not just
  how many, and `agent_type` names them: "3 subagents" becomes "code-reviewer, functional-tester, Explore".
  Whether that is worth the board space is a separate question, but the data is free once the hook is wired.
- The skip list in the permission hook still contains `Task`, and after this it should. Gating the spawn of a
  subagent is a question about the subagent's work, which its own tool calls will ask.

The doc line in `docs/activity-design.md` that says "Claude Code has no subagent started hook" becomes wrong and
should be corrected in the same change.

## Lanes and whether they should follow the hooks

### The idea, and the problem with it

The proposal is that the board's columns depend on which hooks are enabled. A hook wired means a lane appears.

The instinct behind it is right: a lane that can never receive a card is a lie, and the board already agrees
with this. `backlog` is a real status in `store.go` and the board does not draw a column for it, with the comment
"nothing ever creates a card in it, and an empty column on every screen is a column you learn to skip past".

But making the shape of the board depend on configuration fails for a reason the board cannot escape: **atrium
cannot see which hooks are enabled.** Hooks live in `settings.json` on the session's machine, at any of user,
project or local scope, and the daemon is never told. It can guess by reading `~/.claude/settings.json`, the way
`internal/claudeconf` reads permissions, but that guess is per-machine and the board is per-daemon while sessions
are per-directory. A board that redraws itself from a guess about a file it does not own is worse than a board
with a dead column, because the operator has no way to see why the column went away.

There is a second problem. Lanes are shared. One session with `Stop` wired and one without do not get their own
boards. So "the lanes depend on the hooks" resolves to "the lanes depend on the union of the hooks any session
has", which is a lane that appears the first time anyone wires something and never leaves.

### The alternative, which is what to build

**Fixed lanes. The board says which are inert, and why, from what it has actually observed.**

The daemon already keeps in-memory per-process state that dies on restart, and this is the same kind of fact. It
counts, since start, how many events of each kind have arrived: permission requests, `Stop` posts, `prompt`
activity posts, `session` posts. A lane whose feeding event has never arrived is drawn with its header dimmed
and its `why` text extended:

> **needs input** (nothing has fed this)
> No session has reported a turn ending since this daemon started. That signal comes from the `Stop` hook. See
> `docs/hooks.md`.

This is honest in a way a config guess cannot be. It reports what happened, not what a file says should happen.
It also catches the failure the config read would miss: a hook that is wired and broken, or wired in a scope the
session does not load. The `why` popovers already exist on every column, so the surface is there.

The cost is a false positive on a fresh daemon: every lane reads inert for the first few minutes because nothing
has happened yet. Fix that by keying off whether any session has reported anything at all. With zero sessions the
board says nothing about lanes.

### The logical groupings

The twelve events fall into six groups, and only three of them make a lane.

| Group | Events | What it feeds |
| --- | --- | --- |
| Existence | `SessionStart`, `SessionEnd` | The card, and `running` and `finished` |
| Attention | `Stop`, `UserPromptSubmit`, `Notification` | `needs-input`, and moving out of it |
| Permission | `PermissionRequest`, or `PreToolUse` today | `needs-permission` |
| Motion | `PreToolUse`, `PostToolUse`, `PostToolUseFailure` | The activity badge. No lane. |
| Fan-out | `SubagentStart`, `SubagentStop` | The subagent count on the card. No lane. |
| Context | `PreCompact` | The card's timeline. No lane. |

That table is the answer to whether lanes should multiply with hooks. They should not, because half the events
do not produce a bucket of human attention. Motion and fan-out are facts about a runner, and
`docs/activity-design.md` already settled where those go: a badge, because a column of "thinking" and "running
Bash" is a column you learn to ignore. Context is a fact about the past, which belongs on a timeline.

Wiring seven more hooks therefore adds zero lanes. It makes three existing ones trustworthy:

- `needs-input` currently fills from `Stop` and empties only when a tool call happens to fire. `UserPromptSubmit`
  empties it correctly, and `Notification` fills it for the cases `Stop` misses.
- `needs-permission` gets the tool calls the harness thinks are worth asking about, instead of all of them.
- `finished` stops flickering, once `SessionEnd` reads its `reason`.

### The one lane that could be added, and should not be

`backlog` exists in the store and is not drawn. There is a version of this where `SubagentStart` justifies a
`subagents` lane. It does not. A subagent is not a card. It has no independent life the operator can act on, it
cannot be shelved, messaged or attached to, and it belongs to a session that already has a card. Giving it a lane
would fill the board with rows nobody can do anything about, which is exactly what adopting the worktree ledger
did before it was removed. See the abandoned section in `docs/architecture-v2.md`.

## PreCompact

`PreCompact` fires before the session's context is compacted. Payload carries `session_id`, `hook_event_name`
and `triggered_by`, with values `manual` and `auto`. It carries no `cwd`, so the agent name comes from the hook
process's own working directory, which is inherited from the runner and already the fallback every atrium hook
uses.

### Why it is worth recording

Compaction is the moment a session forgets. Everything before it survives only as whatever the summary kept, and
an auto compaction happens without anyone deciding it should. A card that has compacted three times is a
different card to one that has not, and nothing on the board says so today.

The failure this addresses is the specific one that made the daemon store state in the first place: "what was I
even doing". After a compaction the session cannot fully answer that and the operator often cannot either. An
event on the card's timeline saying "context compacted, automatically, at 14:32" is the marker that separates
what the session still knows from what it does not.

There is a second use, which is diagnosis. A session that starts behaving oddly after a long run is very often a
session that compacted. Right now working that out means reading the transcript. On the timeline it is one line.

### What it should do

An event, and nothing else.

- `POST /activity` with `event=compacting`, or a new event kind on `/session`. `/session` is the better home,
  because this is a lifecycle fact that should be durable, and `/activity` is explicitly never stored.
- It appends a store event. `EventNotified` is the closest existing kind, but this deserves its own,
  `EventCompacted`, carrying `triggered_by`.
- **No status change.** The session is not blocked, is not waiting, and is not finished. It is busy doing
  something that takes a while and then it carries on. Moving the card would claim the operator has something to
  do.
- **A badge is defensible and a lane is not.** Compaction takes long enough to look like a hang, so an activity
  of `compacting` for the duration is the honest reading of a card that would otherwise sit on a stale `tool`
  badge. But there is no `PostCompact` event, so the badge would only clear on the next tool call or the fifteen
  minute cutoff. That argues for the timeline entry first and the badge only if the stale reading turns out to
  matter in practice.

### The one thing it cannot tell you

`PreCompact` says compaction happened. It does not say what was lost. `transcript_path` is on the payload, so a
hook could read the transcript and record its size, or the last few messages, before the compaction. That is a
per-agent transcript on disk, which `CLAUDE.md` already names as out of scope that we might do later. This does
not change that.

Failure posture: three second timeout, exit 0 on anything. Not a hot path, fires rarely, and a compaction must
never fail because atrium was not listening.

## Events outside the twelve

The reference documents four more. None of them are urgent and one is worth a note.

- **`PostToolBatch`** fires after a batch of parallel tool calls, carrying `tool_calls[]` with each call's
  `tool_name`, `tool_use_id`, `succeeded`, `tool_input` and result. If atrium wired `PostToolUse` and
  `PostToolUseFailure` per call it would be doing N process spawns where this does one. Worth investigating as a
  cheaper `tool-end`, since the badge only ever wants "the last one finished". **Unverified** whether it fires
  in addition to the per-call events or instead of them, which decides everything.
- **`PermissionDenied`** carries `denial_reason` and returns a `retry` boolean. Not worth having. Atrium already
  knows every denial it issued, because it issued it, and a denial issued by the harness is one atrium was never
  asked about. The `retry` control is interesting and belongs to a different tool than a kanban board.
- **`MessageDisplay`** fires per displayed message with `message_text`. Not worth having. It is a transcript
  feed, it fires constantly, and no card shows message text.
- **`StopFailure`** carries `error_type` with values including `rate_limit`, `overloaded`, `billing_error` and
  `max_output_tokens`. This is the interesting one. A session that stopped because of a rate limit is not
  waiting for the operator, it is waiting for a clock, and today it lands in `needs-input` looking identical to
  a session that finished its turn. A distinct badge, or a chip on the card naming the error, would separate
  "this wants you" from "this wants time". Worth a follow-up on its own.

## Summary of verdicts

| Event | Verdict | Endpoint | Card effect |
| --- | --- | --- | --- |
| `SessionStart` | Keep. Read `source`. | `/session` | Creates or revives the card |
| `UserPromptSubmit` | Wire. | `/activity` | Badge `thinking`, leaves `needs-input` |
| `PreToolUse` | Keep for activity. Drop the gate. | `/activity` | Badge `tool` |
| `PermissionRequest` | Wire, and move the gate here. | `/permission` | `needs-permission` |
| `PostToolUse` | Wire. | `/activity` | Badge `thinking` |
| `PostToolUseFailure` | Wire, as an alias of `tool-end`. No new code. | `/activity` | Badge `thinking` |
| `Notification` | Wire, matched. Not worth it unmatched. | `/activity` | `needs-input` |
| `SubagentStart` | Wire. Replaces the `Task` inference. | `/activity` | Subagent count |
| `SubagentStop` | Wire, and send `agent_id`. | `/activity` | Subagent count |
| `Stop` | Keep. Consider reading `stop_reason`. | `/stop` | `needs-input`, delivers messages |
| `PreCompact` | Wire. Timeline event only. | `/session` | A durable event, no status change |
| `SessionEnd` | Keep. Read `reason` to stop the flicker. | `/session` | `dead` |
