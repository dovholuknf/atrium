# Wiring the hooks

Atrium learns everything it knows about a session from Claude Code hooks. Without them a session is invisible
until it makes a gated tool call, and several features are built but inert.

The hook scripts live in your dotfiles, not in this repo, because they hold machine-specific paths. This page is
what goes in `settings.json` and what each entry buys.

## What is wired now

| Hook | Script | What it gives you |
| --- | --- | --- |
| `PreToolUse` | `atrium-perm-hook.ps1` | The permission gate. Every tool call is offered to you before it runs. |
| `SessionStart` | `atrium-session-hook.ps1 -Event start` | A card appears the moment a session opens, before it does anything. |
| `SessionEnd` | `atrium-session-hook.ps1 -Event end` | The card goes to `finished` when the session closes. |

## What is built but not wired

### Activity: what each session is doing right now

Four entries. Together they turn a board of identical `running` cards into one that says `thinking`,
`running Bash`, `3 subagents`, and how long it has been at it.

| Hook | Argument |
| --- | --- |
| `PreToolUse` | `atrium-activity-hook.ps1 -Event tool-start` |
| `PostToolUse` | `atrium-activity-hook.ps1 -Event tool-end` |
| `UserPromptSubmit` | `atrium-activity-hook.ps1 -Event prompt` |
| `SubagentStop` | `atrium-activity-hook.ps1 -Event subagent-end` |

A gated session already reports activity through the permission hook, at no extra cost, because a permission
request IS a tool starting. These four are what reach the sessions that are not gated, which are the ones that
look dead on the board.

`PreToolUse` already runs the permission hook. Claude Code allows more than one hook per event, so the activity
hook goes alongside it rather than replacing it.

**This one runs before every tool call in every session**, so it is written to be free: a one second timeout,
every failure ignored, nothing downstream reading its result. A session that never lands a single successful
post behaves exactly as it did before. See `docs/activity-design.md`.

### Stop: reaching a session that is sitting idle

| Hook | Script |
| --- | --- |
| `Stop` | `atrium-stop-hook.ps1` |

This delivers a message you queued for a session that is not making tool calls. The permission hook can carry a
message back, but only when the session calls a tool, and an idle session calls none. Idle is when you most want
to reach it.

**Understand this one before wiring it.** A `Stop` hook that returns `block` tells the model to keep going with
the reason it was given. That is the mechanism, and it is also the risk: get it wrong and sessions will not
stop. Two things keep it safe, and both must stay:

- The hook exits silently when `stop_hook_active` is set, so a session already continuing because of atrium is
  never told to continue again.
- A message is delivered once. The queue is emptied as it is read, so the same text cannot fire twice.

With nothing queued it returns `{"continue":true}` and the turn ends normally.

## Shape of an entry

Claude Code hooks take a matcher and a list of commands. An atrium entry looks like:

```json
{
  "PreToolUse": [
    {
      "matcher": "",
      "hooks": [
        { "type": "command", "command": "pwsh -NoProfile -File <path>/atrium-perm-hook.ps1" },
        { "type": "command", "command": "pwsh -NoProfile -File <path>/atrium-activity-hook.ps1 -Event tool-start" }
      ]
    }
  ]
}
```

## Turning it all off

Every atrium hook checks `ATRIUM_PERM_GATE` first and exits immediately when it is `off`. One environment
variable disables the lot without editing `settings.json`.

| Value | Effect |
| --- | --- |
| `on` | Every session is gated, with no MCP server and no agent loop. |
| `off` | Every atrium hook exits at once. Nothing is reported and nothing is gated. |
| unset | Gated only when the `atrium-agent` MCP server is present. |

Gating can also be turned on and off per session while it runs, with `atrium join` and `atrium leave`. Those ask
the daemon rather than reading the environment, so they take effect on the very next tool call.

## Rules every atrium hook follows

Restate these in any hook you add. They are why the gate is safe to leave on.

1. **A hook never fails a session.** Every one of them exits 0 whatever happens. A session must never fail to
   start, finish a turn, or make a tool call because atrium was not listening.
2. **Unreachable means ungated.** When the daemon is down the permission hook approves rather than blocking. A
   tool that fails open is an inconvenience; one that fails closed stops all work the moment atrium does.
3. **Nothing is retried on the hot path.** `PreToolUse` runs constantly. The activity hook gets one second and
   one attempt.
4. **Nothing is logged on success.** A hook that prints on every tool call is a hook you turn off.
