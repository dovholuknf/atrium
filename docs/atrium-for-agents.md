# What atrium is, for an agent about to be wired into it

Hand this to an agent running a CLI that atrium does not yet wire up, together with a prompt such as "read
docs/atrium-for-agents.md and produce the report it describes for your CLI".

Atrium is a local supervisor for coding-agent sessions on this machine. You are running inside it right now. It
owns the pseudo terminal your CLI draws in, it shows your session as a **card** on a web board a human watches,
and it can gate your tool calls. It is loopback only, with no login and no cloud. The human is clint.

Your job, once you have read this, is to work out how your CLI can report to atrium the way Claude Code and codex
already do. Report what you MEASURE by running things, not what docs claim.

## The pieces

- **The hub and the room** (`C:\Users\claude\.atrium2\bin\atrium2.exe`). The hub serves the board and holds
  nothing. The room holds the database and the running agents, and it dials the hub. The room's **agent
  listener** is what hooks post to.
- **The hook binary** is a different executable: `C:\Users\claude\.atrium\bin\atrium.exe`. Every hook command
  below runs it, for example `C:/Users/claude/.atrium/bin/atrium.exe hook --event tool-start`. Read the real
  entries in `C:\Users\claude\.claude\settings.json` and copy their exact form.
  A hook finds the daemon's port through a location file (`daemon.json` under `%LOCALAPPDATA%\atrium`, or the
  path in `ATRIUM_LOCATION` when that is set). Never hardcode a port.
- **A card** is the durable record of one piece of work. It outlives the process. A session id and a pid are
  attributes of it, not its identity.
- **A column** (running, needs-input, needs-permission, done...) is a bucket of human attention. **Activity**
  (which tool is running now) is a badge, held in memory and never stored.
- **A runner** is a row in the harness table: a command atrium launches, such as claude, codex, gemini, ollama or
  a shell. Each runner either reports through hooks or reports nothing.

## How a runner talks to atrium: hooks

Everything atrium knows about a session comes from hooks the runner fires. Each hook runs an atrium subcommand,
which reads the runner's JSON from stdin, posts one fact to the agent listener and exits. Claude Code's set:

| Runner event | Command atrium registers | What the board gets |
| --- | --- | --- |
| SessionStart | `atrium session --event start` | a card appears before the session does anything |
| SessionEnd | `atrium session --event end` | the card goes to finished |
| PreToolUse | `atrium hook --event tool-start` | the badge shows the tool running now |
| PostToolUse | `atrium hook --event tool-end` | the badge clears |
| PostToolUseFailure | `atrium hook --event tool-failed` | the badge clears |
| UserPromptSubmit | `atrium hook --event prompt` | the card leaves needs-input |
| SubagentStart / Stop | `atrium hook --event subagent-start` / `subagent-end` | the subagent count |
| Notification | `atrium hook --event notification` | the session is waiting on a question |
| PreCompact | `atrium session --event compact` | the moment the session forgot something |
| Stop (optional) | `atrium turn --event end` | reaches an idle session, and the card goes to needs-input |

Fields atrium reads from the stdin JSON: `session_id`, `cwd`, `transcript_path`, `hook_event_name`, `source`,
`reason`, `tool_name`, `tool_input`, `tool_use_id`, `stop_hook_active`, `agent_id`, `agent_type`, `trigger`.
Codex spells these the same as Claude, which is why codex needed almost no work. See `docs/other-runners.md` in
the atrium repo (`D:\git\github\dovholuknf\atrium`) for how codex was measured, and copy that method. The table
of hooks atrium wants is `WantedHooks` in `internal/claudeconf/hooks.go`.

### The two hooks that change what a session does

Every other hook only reports, and a broken one only leaves the board stale. These two answer back:

- **Stop.** Printing `{"decision":"block","reason":"..."}` makes the model keep working with that reason as its
  next instruction. That is the only way atrium reaches an idle session, and it is how a queued message from a
  human or another agent is delivered. The second Stop arrives with `stop_hook_active: true`, and atrium never
  blocks twice in a row, which is the loop guard.
- **PreToolUse as a permission gate.** Printing
  `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"..."}}`
  refuses the tool call with that reason. A queued message can also arrive this way, as a refusal whose reason is
  the human speaking. The gate script is clint's own `atrium-perm-hook.ps1`, and atrium never writes it.

A runner without these two cannot receive a queued message at all. The sender is told "queued" and nothing ever
delivers it.

### Rules every hook follows

- **A hook never fails a session.** When atrium is unreachable a hook exits cleanly and the session carries on.
  The permission gate fails open.
- **Activity hooks are fire and forget:** a one second timeout, no retry, output ignored.
- Hook commands are written by atrium into the runner's own config, replacing any older atrium entry and leaving
  the operator's other hooks alone. Watch the quoting: codex takes the first word of `command` as the program with
  no quote handling, and that broke every codex hook on a path containing a space.
- **Trust is the human's.** Atrium never approves a trust prompt or stores a credential on a runner's behalf. It
  may write config and tell the human the exact command to run.

## Other ways in

- `atrium finish [recap]`: a session declaring its work over, with a short account of what it did.
- `atrium peers`: the other sessions this one can address. `atrium tell <handle> <msg>`: say something to
  one. Queued, never typed into its terminal.
- An MCP server (`atrium control`, registered at user scope for claude) exposes `atrium_status`, `atrium_peers`,
  `atrium_say`, `atrium_launch` and similar. A runner that can register an MCP server could use these directly.

## What to produce

Write `<runner>-hooks.md` (for example `gemini-hooks.md`) in your working directory, measured against the CLI
installed here:

1. Your CLI's exact version.
2. Every hook or lifecycle event it fires: its name, when it fires, the stdin JSON (capture a real one with a
   probe hook that writes stdin to a file), and what its output or exit code can do.
3. Where hooks are configured: file paths, user, project and system scope, and the exact schema.
4. A mapping table from the atrium table above to your CLI's events, with each row marked works, partial or none, and
   the evidence for each.
5. Whether a Stop-equivalent can block and feed a reason back, and whether a PreToolUse-equivalent can deny with
   a reason. Prove each with a probe, the way `docs/other-runners.md` did for codex.
6. How folder trust works and how a folder or a parent such as `D:\worktrees` is trusted without the interactive
   prompt. Also how to register an MCP server.
7. Field names that differ from Claude's, and anything about quoting or paths that would break a written hook.

Use a scratch config home for probes. Do not edit clint's real config for your CLI (`~/.gemini`, `~/.codex` and so
on). Do not restart atrium.

When the report is in, it becomes a section of `docs/other-runners.md`, and the runner gets a hooks target like
codex's.
