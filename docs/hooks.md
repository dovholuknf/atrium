# Wiring the hooks

Atrium learns everything it knows about a session from Claude Code hooks. Without them a session is invisible
until it makes a gated tool call, and several features are built but inert.

**The board can wire these for you.** Runners tab, the `hooks` button on the claude row. It lists what is
missing and writes it into your own `settings.json`, keeping a copy of the old file first. The rest of this page
is what it writes and what each entry buys, for anyone who would rather do it by hand.

The button sits on that row because these are claude's hooks, not because they belong to that row. They cover
every claude on the machine, including ones atrium never started, and disabling or deleting the row does not
unwire them.

## What atrium writes

The activity hooks are a subcommand of atrium itself, not a script:

```
<path to atrium.exe> hook --event tool-start
```

The path is absolute and is whichever binary the daemon is running from, because `settings.json` is read by a
session whose PATH atrium has no say over. Rebuild somewhere else and the board reports the entry as pointing
elsewhere, with the same button to correct it.

| Hook | Event | What it gives you |
| --- | --- | --- |
| `PreToolUse` | `tool-start` | which tool a session is running right now |
| `PostToolUse` | `tool-end` | when that tool finished, so the card stops claiming it |
| `UserPromptSubmit` | `prompt` | you answered, so the card leaves `needs-input` |
| `SubagentStart` | `subagent-start` | the subagent count going up |
| `SubagentStop` | `subagent-end` | and coming back down |

Together they turn a board of identical `running` cards into one that says `thinking`, `running Bash`,
`3 subagents`, and how long it has been at it.

A gated session already reports activity through the permission hook, at no extra cost, because a permission
request IS a tool starting. These five are what reach the sessions that are not gated, which are the ones that
look dead on the board.

`PreToolUse` already runs the permission hook. Claude Code allows more than one command per hook, so the activity
entry joins it rather than replacing it, and installing does not touch what is already there.

**`tool-start` runs before every tool call in every session**, so it is written to be free: a one second timeout,
one attempt, every failure ignored, nothing downstream reading its result. A session that never lands a single
successful post behaves exactly as it did before. See `docs/activity-design.md`.

## What the board will not write for you

| Hook | Script | Why it stays manual |
| --- | --- | --- |
| `PreToolUse` | `atrium-perm-hook.ps1` | The permission gate. Yours, in your dotfiles, and it decides what runs. |
| `SessionStart` / `SessionEnd` | `atrium-session-hook.ps1` | Same file, same reason. |
| `Stop` | `atrium-stop-hook.ps1` | See below. |

### Stop: reaching a session that is sitting idle

This delivers a message you queued for a session that is not making tool calls. The permission hook can carry a
message back, but only when the session calls a tool, and an idle session calls none. Idle is when you most want
to reach it.

**Understand this one before wiring it**, which is why the board does not offer it. A `Stop` hook that returns
`block` tells the model to keep going with the reason it was given. That is the mechanism, and it is also the
risk: get it wrong and sessions will not stop. Two things keep it safe, and both must stay:

- The hook exits silently when `stop_hook_active` is set, so a session already continuing because of atrium is
  never told to continue again.
- A message is delivered once. The queue is emptied as it is read, so the same text cannot fire twice.

With nothing queued it returns `{"continue":true}` and the turn ends normally.

## Shape of an entry

Claude Code hooks take a matcher and a list of commands. An atrium entry looks like:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "",
        "hooks": [
          { "type": "command", "command": "pwsh -NoProfile -File <path>/atrium-perm-hook.ps1" },
          { "type": "command", "command": "D:/tools/atrium.exe hook --event tool-start" }
        ]
      }
    ]
  }
}
```

## What installing from the board does and does not do

- It reads `~/.claude/settings.json`, never a project file. A hook belongs to you, not to a repository, and a
  per-project hook would report only the sessions that happened to start there.
- It copies the old file aside before writing, to `settings.json.atrium-<timestamp>.bak`.
- It decodes the whole file generically, so a key atrium has never heard of survives the round trip. A `timeout`
  or a matcher you set on an entry is kept.
- It refuses outright when the file will not parse, and changes nothing. Merging into something atrium cannot
  read would lose whatever is in there.
- It replaces an entry that reports the same event, including the old PowerShell activity script, rather than
  adding a second one. Two commands reporting one event would double every count.
- Running it twice adds nothing the second time.
- **Sessions already running keep their old settings.** Claude Code reads `settings.json` when a session starts.

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
