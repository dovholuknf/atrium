---
title: Hooks
description: How a session reports to atrium, what the board wires for you, and the permission gate you add yourself.
---

# Hooks

Atrium learns everything it knows about a session from its runner's hooks. Without them, a session is invisible
until it makes a gated tool call.

## What the board wires for you

Open the **rooms** tab and press **hooks** on the claude row, or on the codex row. The button counts what is
missing. Atrium writes the entries into your own settings file, `~/.claude/settings.json` for claude and
`~/.codex/hooks.json` for codex, and keeps a dated copy of the old file first.

| Hook | What it gives you |
| --- | --- |
| `SessionStart` | A card appears when a session opens, before it does anything. |
| `SessionEnd` | The card goes to finished when the session closes. |
| `PreToolUse` | Which tool the session is running right now. |
| `PostToolUse`, `PostToolUseFailure` | When that tool finished, so the card stops claiming it. |
| `UserPromptSubmit` | You answered, so the card leaves ready. |
| `SubagentStart`, `SubagentStop` | The subagent count on the card. |
| `Notification` | The session put a question on screen and is blocked on you. |
| `PreCompact` | The session is compacting, and still working. |
| `Stop` | A queued message reaches a session sitting idle. Offered by name, never installed by default. |

Each entry is a subcommand of atrium itself, such as `atrium hook --event tool-start`, with the absolute path of
the binary the daemon runs from.

The installer keeps any key it does not know, keeps your matchers and timeouts, and replaces an entry that
reports the same event instead of adding a second one. Running it twice adds nothing. It refuses to touch a file
that will not parse. **Sessions already running keep their old settings**, because Claude Code reads the file when
a session starts.

:::warning The Stop hook changes what a session does
A `Stop` hook that answers `block` tells the model to keep going. That is how a queued message reaches an idle
session, and it is also the risk. Atrium's version exits silently when `stop_hook_active` is set and delivers
each message once. That is why "install all" leaves it out and the board asks for it by name.
:::

## The permission gate

The gate is a `PreToolUse` hook that you add. The board does not write it, because it decides what runs on your
machine. It posts each tool call to atrium's agent port and blocks until atrium answers.

### The contract

```http
POST http://localhost:7777/permission
```

```json
{
  "agent": "fix-login",
  "tool": "Bash",
  "command": "go test ./...",
  "cwd": "/home/me/src/api",
  "pid": 4242,
  "tool_use_id": "toolu_01...",
  "details": "the diff for an edit, or the content for a write"
}
```

The reply arrives when you, a rule or auto mode decides:

```json
{ "decision": "approve", "reason": "", "command": "" }
```

- `decision` is `approve` or `block`. On a block, hand `reason` back to the agent. It is your "do this instead".
- `command` is present only when you edited the command before approving. A hook that ignores it runs the
  original, so ignoring it is safe.
- `pid` is the runner's own process. It lets atrium tell a live session from a dead one for free.
- `tool_use_id` is Claude Code's id for this attempt. Send it, so a retried request is recognised as the same
  question and never asked twice.
- `details` is what the tool would do. Send it, and the board draws a real diff.

### Which sessions to gate

`ATRIUM_PERM_GATE` in the session's environment decides first:

| Value | Effect |
| --- | --- |
| `on` | Gate every session. |
| `off` | Every atrium hook exits at once. Nothing is reported or gated. |
| unset | Ask atrium: `GET /gate?agent=<name>` answers `{"gate":true}` after the session ran `atrium join`. |

Asking atrium is what makes `atrium join` and `atrium leave` take effect on the very next tool call, with no
restart.

### A starting script

This PowerShell hook follows the contract. It is a starting point, not a finished tool: adapt the command it sends
for the tools you care about, and add `details` for edits.

```powershell
# atrium-gate.ps1: the permission gate. Every path exits 0, and an unreachable atrium means ungated.
try {
  if ($env:ATRIUM_PERM_GATE -eq 'off') { exit 0 }
  $in = [Console]::In.ReadToEnd() | ConvertFrom-Json
  $hub = if ($env:ATRIUM_HUB_URL) { $env:ATRIUM_HUB_URL } else { 'http://localhost:7777' }
  $agent = if ($env:ATRIUM_AGENT_NAME) { $env:ATRIUM_AGENT_NAME } else { Split-Path -Leaf $in.cwd }

  if ($env:ATRIUM_PERM_GATE -ne 'on') {
    $g = Invoke-RestMethod "$hub/gate?agent=$([uri]::EscapeDataString($agent))" -TimeoutSec 2
    if (-not $g.gate) { exit 0 }
  }

  $command = if ($in.tool_input.command) { $in.tool_input.command } else { $in.tool_input.file_path }
  $body = @{
    agent = $agent; tool = $in.tool_name; command = $command
    cwd = $in.cwd; tool_use_id = $in.tool_use_id
  } | ConvertTo-Json -Compress
  $r = Invoke-RestMethod "$hub/permission" -Method Post -Body $body -ContentType 'application/json'

  $decision = if ($r.decision -eq 'approve') { 'allow' } else { 'deny' }
  @{ hookSpecificOutput = @{
      hookEventName = 'PreToolUse'; permissionDecision = $decision; permissionDecisionReason = $r.reason
  } } | ConvertTo-Json -Compress
} catch { }
exit 0
```

Register it beside atrium's own `PreToolUse` entry, with a long `timeout`, because the request waits for you:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "",
        "hooks": [
          { "type": "command", "command": "pwsh -NoProfile -File /path/to/atrium-gate.ps1", "timeout": 86400 },
          { "type": "command", "command": "/path/to/atrium hook --event tool-start" }
        ]
      }
    ]
  }
}
```

## Rules every atrium hook follows

Keep these in any hook you write. They are why the gate is safe to leave on.

1. **A hook never fails a session.** Exit 0 whatever happens. A session must never fail to start, end a turn or
   make a tool call because atrium was not listening.
2. **Unreachable means ungated.** When atrium is down, approve instead of blocking. A gate that fails closed stops
   all work the moment atrium does.
3. **Nothing is retried on the hot path.** `PreToolUse` runs constantly.
4. **Nothing is logged on success.** A hook that prints on every tool call is a hook you turn off.
