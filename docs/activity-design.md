# Live activity on a card

What a runner is doing *right now*, shown on its card, alongside the column that says what it needs from you.

## The problem

The board answers "what needs me". It does not answer "is this one moving". A card in `running` looks identical
whether its session is grinding through a build, thinking, waiting on four subagents, or hung, and the only way
to tell is to attach.

## A column versus a badge

Columns are buckets of human attention. `needs-input` and `needs-permission` mean you have to act. `backlog`,
`done`, `shelved` and `dead` mean you decided something. A column costs a glance across the whole board.

"Thinking" and "running Bash" never need a human. They are facts about the runner, not requests, and a column of
them becomes a column you learn to ignore.

So status stays a column and activity becomes a badge on the card.

## The states

| activity | meaning | where it comes from |
| --- | --- | --- |
| `thinking` | the model has the turn and is not in a tool | a prompt was submitted, or a tool just finished |
| `tool` | a named tool is running | `PreToolUse` |
| `waiting` | blocked on a human | already known: status is `needs-input` or `needs-permission` |
| `idle` | the turn ended, nothing is running | `Stop` |
| unknown | atrium has never heard from this session | no events yet, or a runner with no hooks |

Alongside it, a count of live subagents. Claude Code has no "subagent started" hook, so the count comes from
`PreToolUse` for the `Task` tool going up and `SubagentStop` coming down.

## Never stored

Activity lives in a map in the daemon and dies with the process.

After a restart, a stored "running Bash" describes a process that no longer exists, and the board would show it
without hedging. Atrium does not know what that session is doing, so it says nothing.

Cards, history and rules go the other way and are durable, because "how long has this been sitting" cannot be
answered from memory that dies with the process. Activity is only ever about now.

## Staleness

A hook that never fires leaves an activity pinned forever. A session killed mid-tool would show `tool` until the
daemon restarts.

So an activity has an age, and past a fifteen minute cutoff it reads as unknown rather than as its last value.
The card shows how long the current activity has been running, so "running Bash for 40 minutes" is visible
instead of looking the same as any other running card.

## The wire

`POST /activity` on the agent-facing listener, from the hooks:

```json
{ "agent": "string", "task_id": "string", "event": "tool-start|tool-end|prompt|subagent-end|idle", "tool": "Bash" }
```

Answered immediately with `{"ok":true}`. The daemon writes the response before it does any bookkeeping, so a
caller never waits on a database.

Unknown agents are accepted and dropped rather than refused. A session atrium has no card for has no activity to
record.

### The caller's side

This rides `PreToolUse`, the hot path for every tool call every session makes, and the failure that hurts is not
a slow answer but no answer: a daemon reachable but stalled, a halted listener still accepting connections, a
machine under load. Waiting on any of those adds latency to every tool call in every session, before the
permission path and its fail-open rule are reached.

So the rule lives on the hook. An activity post is fire and forget:

- A one second timeout. Short enough that failing costs nothing.
- Every failure ignored: connection refused, timeout, 5xx, unparseable body, no daemon at all. None reported,
  retried or logged.
- Nothing depends on it. The permission decision does not read its result, and a session that never lands a
  single successful post behaves as it did before this existed.

A card with no activity falls back to saying nothing, and a stale one expires on its own.

### Gated sessions get it for free

A permission request is a tool starting, so the daemon records activity from `/permission` as well. That call is
already being made, so it costs nothing extra.

The separate post therefore only matters for sessions making no other traffic: ungated ones, and the events that
are not tool calls (`tool-end`, `idle`, `subagent-end`). Those are the sessions that look dead on the board.

## Interaction with the permission hook

`PreToolUse` posts to `/permission` for gated sessions, and that call blocks until a human answers. The activity
post is separate and happens first, so:

- An ungated session reports activity despite never being gated. Most sessions are not gated, and they are the
  ones that look dead.
- A gated session reports `tool-start` and then blocks in `/permission`. The card shows `needs-permission`, which
  outranks the activity badge, so nothing contradicts on screen.

## What the board shows

On a card, under the title: the activity, its tool name when there is one, and its age. Subagent count when
non-zero. `waiting` is not drawn, since the column says it and the wait chip times it.
