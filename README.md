# Atrium

_The single open hall every agent passes through._

Atrium is a single-pane-of-glass for orchestrating one or many claude-code sessions from one terminal. It has two
distinct modes:

1. **Hub / agent loop** (the working mode): one terminal where you type prompts; many claude sessions where each
   one runs as an MCP-driven agent that submits to the hub, blocks until you reply, acts on the reply, submits
   again. Forever. Built on simple HTTP long-poll.
2. **Read-only state aggregator**: an MCP server that exposes the per-session state that `gwt` already tracks
   (`snapshot`, `wait_for_change`, `focus_session`). Use this when you want any claude tab to be able to ask
   "what are the other agents doing?"

This README covers both. The hub/agent loop is what you most likely want first.

---

## Quick start (the hub/agent loop)

### 1. Build

```powershell
cd D:\git\github\dovholuknf\atrium
go mod tidy
go build -o build.claude\atrium.exe .\cmd\atrium
```

### 2. Start the hub (this is your single pane of glass)

```powershell
.\build.claude\atrium.exe hub
# default listen: :7777, long-poll 60s
# flags: --addr :8000, --long-poll 120
```

You'll see:

```
[atrium hub] listening on :7777 (long-poll 1m0s)
[atrium hub] type prompts and press Enter. '@<agent> <text>' targets a specific agent. '/agents' lists known agents.
>
```

This terminal is where you type prompts to agents and watch their responses.

### 3. Wire `atrium-agent` into a claude session via project-local `.mcp.json`

In any dir you want to host an agent (e.g. `D:\git\github\dovholuknf\atrium\` itself, or any worktree), drop a
`.mcp.json`:

```json
{
  "mcpServers": {
    "atrium-agent": {
      "command": "D:/git/github/dovholuknf/atrium/build.claude/atrium.exe",
      "args": ["agent", "--url", "http://localhost:7777"]
    }
  }
}
```

The agent's name defaults to the leaf of the current working directory. If you launch claude in
`D:\worktrees\github\openziti\ziti\fix-login`, the agent shows up as `fix-login` in the hub. Override with
`--name <whatever>` in args if you want.

Launch a fresh claude in that dir. The MCP loads but the loop does NOT auto-start; the model treats the
atrium tool as opt-in. Activate it by saying:

```
atrium
```

(or `run atrium`, `start atrium`, `atrium go`, etc.) Once it sees that activation phrase, the model fires its
first `submit(kind="greeting", ...)` and the hub terminal lights up.

Until you activate, the session behaves like a normal claude conversation. To exit the loop within an active
session, say `stop atrium` or `leave atrium`.

### 4. The loop

In the hub terminal:

```
> what cwd are you in
[atrium] -> atrium
>
[14:02:33] <atrium/response>
D:\git\github\dovholuknf\atrium
>
```

That's the whole loop. You type, agent acts, agent responds, you type again. Forever, until you Ctrl-C the hub.

### Hub TUI

The Bubble Tea TUI is the default. Use `atrium hub --simple` if you need the line-mode fallback.

| Key / Input | Effect |
| --- | --- |
| `Tab` / `Shift+Tab` | Cycle the chat / perms / all-agents tabs. |
| `Ctrl-K` | Open the agent switcher overlay (↑/↓ + Enter, Esc cancels). |
| `↑` / `↓` (or `k` / `j`, input empty) | Move the cursor in the perms and all-agents lists. |
| `Enter` (all-agents tab, input empty) | View the highlighted agent's chat. |
| `x` / `Delete` (all-agents tab, input empty) | Forget the highlighted agent: drops it from the hub and the UI. If its claude process is still alive it re-registers on its next submit. |
| `1`-`9` (input empty) | If the active agent is presenting a `{choices}` picker, picks the Nth option. Otherwise quick-switches to the Nth known agent. |
| `y` / `n` | Approve / deny the OLDEST pending permission (no fall-through to typed prompt). |
| `a` / `d` / `Enter` (in perms tab) | Approve (`a` / `Enter`) or deny (`d`) the highlighted request. |
| Typed text + Enter | Send to the currently-active agent. `@<name>` prefix targets a specific agent. |
| `/agents` `/perms` `/chat` | Jump to that view. |
| `/pick` (or `/k`) | Open the agent switcher overlay (same as `Ctrl-K`). |
| `/approve [id]` `/deny [id]` | Resolve a specific permission (or the oldest, with no id). |
| `/rename <agent> <new>` | UI-only alias for an agent. `/rename <agent>` with no second arg clears it. |
| `/help` | Brief command list. |
| `PgUp` / `PgDn` | Scroll the chat viewport. |
| Ctrl-C | Quit. Agents silently park, retrying every 5s -> capped at 60s, until the hub returns. |

The TUI is **inline** (not full-screen): chat messages live in your terminal's native scrollback; the TUI
itself is a small floating panel at the bottom. Mouse-wheel scroll, copy/paste, and Ctrl+F all work because
the terminal owns the buffer.

The floating bottom panel is:
- (optional, top of panel) The flashing perm banner when any permission request is pending.
- (only in perms / agents tabs) An inline list with cursor selection.
- The fixed-bottom strip: `atrium │ <active-agent>  ← waiting  (+N elsewhere)  · K agents (ctrl-k)`,
  then tabs `chat │ perms (N!) │ all agents`, then the input box, then the status line.

In the chat tab the floating panel is small (no middle content) because the conversation IS the scrollback.
Your typed prompts echo into the scrollback as `you → <agent>` lines so you can read top-down.

Permission requests are loud:
- A bordered, flashing banner appears at the top of every view when any perm is pending. Shows the first
  pending perm in detail plus a total count.
- The terminal beeps (BEL byte) once per new arrival. Disable in your terminal settings if it annoys you.
- Status bar nudge on arrival: `⚠ NEW permission #N -- press y/n`.

### Multi-agent

Just drop the same `.mcp.json` into another dir and launch another claude there. With distinct cwds you get
distinct auto-names. Hub routes by name; `/agents` shows everyone.

---

## Agent-side formatting conventions

The agent's `submit` tool description teaches two formatting affordances that the TUI honors:

### ANSI colors via sentinels

The agent writes `{green}foo{reset}` and the hub prints real green. Vocabulary:

- `{reset}` `{bold}` `{dim}` `{underline}`
- Foreground: `{black}` `{red}` `{green}` `{yellow}` `{blue}` `{magenta}` `{cyan}` `{white}` `{gray}`
- Background: `{bgblack}` ... `{bgwhite}`

The hub auto-appends `{reset}` at end of message so the model doesn't have to remember a trailing reset.

### Interactive picker via `{choices}`

When the agent has a small finite set of options for the human, it wraps them in `{choices}...{/choices}`:

```
How would you like to proceed?
{choices}
walk through the codebase
add a new feature
run the test suite
something else (describe it)
{/choices}
```

The TUI strips the markers, renders the options in a colored numbered box, and `1`-`9` quick-pick sends the
chosen line back to the agent. Typing a freeform reply also works and clears the picker.

## What happens when the hub goes down

The agent's `submit` tool catches all transport failures (connection refused, timeout, 5xx, network blackhole)
and retries silently with exponential backoff (5s, 10s, 20s, 40s, capped at 60s). The LLM **never** sees a
connectivity error -- the tool call just stays parked until the hub is back. No token burn while disconnected.

You'll see one log line on stderr when the hub first becomes unreachable, then one more every 10 minutes (set
`ATRIUM_DISCONNECTED_LOG_INTERVAL=1h` or any Go duration to change). When the hub returns, one "resumed" line.
These end up in claude's MCP debug log -- grep there if you want to confirm the agent is parked vs dead.

---

## Permissions-only mode (no submit loop)

You do not need the agent submit loop to get value out of the hub. If you have a fleet of claude sessions that
constantly stop to ask for permission, you can funnel every one of those approvals into a single hub pane and
answer them all from one place. No `.mcp.json`, no `atrium` activation phrase, no per-session loop.

How it works: the `PreToolUse` hook (`atrium-perm-hook.ps1`) fires before every side-effecting tool call, POSTs
it to the hub's `/permission`, and blocks until you decide. The hub returns your decision and the hook hands it
straight back to claude-code:

- **Approve** ("yep, go for it"): the tool runs.
- **Deny with guidance** ("no, do X instead"): the tool is blocked and your message is surfaced to the model as
  the reason, so the agent course-corrects instead of just hitting a wall.

### Turn it on

1. Run the hub: `.\build.claude\atrium.exe hub`.
2. Force the gate fleet-wide by setting `ATRIUM_PERM_GATE=on`. The durable way is the `env` block of your
   `settings.json`:

   ```json
   {
     "env": { "ATRIUM_PERM_GATE": "on" }
   }
   ```

   Per-shell instead of global: `$env:ATRIUM_PERM_GATE = 'on'` before launching a claude session.
3. Make sure the hook's `timeout` in `settings.json` is large enough to cover how long you might take to answer
   (the hook blocks the tool call until you do). Set it high (e.g. `86400`) for "wait until I get to it".

The hook **fails open**: if the hub is not running, the session falls back to claude-code's normal permission
flow rather than getting stuck. Set `ATRIUM_PERM_GATE=off` (or remove it) to disable gating entirely.

### Answering from the hub

In the perms tab:

| Input | Effect |
| --- | --- |
| `a` / `Enter` (empty input) | Approve the highlighted request. |
| `d` | Deny the highlighted request (bare refusal, no guidance). |
| Type a message + `Enter` | Deny the highlighted request and hand your text to the agent as the reason. |
| `y` / `n` | Approve / deny the OLDEST pending, from any tab. |
| `/approve [id] [why]` | Approve a specific request (or oldest), optional note. |
| `/deny [id] [why]` | Deny a specific request (or oldest), optional guidance. E.g. `/deny 3 use staging`. |

The agent name shown is `ATRIUM_AGENT_NAME` if set, otherwise the leaf of that session's working directory.
Two agents launched from the same directory will share a display name; set `ATRIUM_AGENT_NAME` per session if
you need them distinct in the pane.

---

## The other mode: state aggregator (MCP tools)

Separate from the hub/agent loop. The `atrium serve` subcommand runs an MCP server (stdio) that exposes
read-only views of the `gwt` session ledger.

| Tool | Shape | Purpose |
| --- | --- | --- |
| `snapshot` | request/response | Returns every session's current state from `$env:WORKTREE_ROOT\sessions\*.json`. |
| `wait_for_change` | long-poll (up to N seconds) | Blocks until any session transitions, returns the change. Pass `since` (RFC3339) to skip already-seen events. |
| `focus_session` | request/response | Brings the wt window hosting `<session_id>` to the foreground via `wt -w <window> focus-tab`. |

Wire it via `.mcp.json` or global settings (`mcpServers.atrium`) with `command: atrium`, `args: ["serve"]`.

CLI subcommands related to the same data:

```sh
atrium status [--needs-input] [--alive]   # tabular snapshot to stdout
atrium watch  [--tail N]                  # Go-native equivalent of `gwt watch`
```

---

## Install

If you want `atrium` on PATH bare:

```powershell
go install ./cmd/atrium
# OR symlink the built exe
New-Item -ItemType SymbolicLink -Path "$env:ON_PATH\atrium.exe" `
  -Target "$PWD\build.claude\atrium.exe"
```

---

## Config (env-var driven, no YAML for the MVP)

| Var | Default | Used by | Meaning |
| --- | --- | --- | --- |
| `WORKTREE_ROOT` | `D:\worktrees` | `serve`, `status`, `watch` | Where to read session JSONs + state.log. |
| `ATRIUM_LONG_POLL_TIMEOUT` | `30s` | `serve` (`wait_for_change`) | Default upper bound for long-poll if input omits `timeout_seconds`. |
| `ATRIUM_DISCONNECTED_LOG_INTERVAL` | `10m` | `agent` | How often to nag stderr when the hub is unreachable. |
| `ATRIUM_PERM_GATE` | unset (auto) | perm hook | `on`/`force` gates every session through the hub (permissions-only mode, no MCP). `off` disables. Unset auto-detects the atrium-agent MCP. |
| `ATRIUM_HUB_URL` | `http://localhost:7777` | perm hook | Hub base URL the PreToolUse hook POSTs `/permission` to. |
| `ATRIUM_PERM_TIMEOUT` | none (blocks) | perm hook | Client-side deadline (e.g. `00:05:00`) for a permission decision. Unset = block until answered. |

---

## Troubleshooting

- **"atrium: command not found"** -- the binary isn't on PATH. Use the full path to `build.claude\atrium.exe` or
  `go install ./cmd/atrium` (and put `$env:GOPATH\bin` on PATH).
- **Agent connects but no greeting appears** -- check the claude tab for an MCP error. Common cause: the
  `command` in `.mcp.json` points at an old build location.
- **Hub shows `warn: no agent named 'X' has submitted yet`** -- you typo'd the `@<name>` or the agent registered
  under a different name. Run `/agents` to see the live list.
- **Agent seems frozen, hub is up** -- the model probably hit some other error and isn't calling submit. Send a
  one-word reset prompt; the loop instructions are in the MCP system prompt.
- **Tokens burning while you're away from the hub** -- shouldn't happen anymore (silent retry, no narration).
  If it does, look at the MCP debug log for stderr lines from `atrium-agent` -- they reveal whether the agent
  is parked or trying something else.

---

## License

Apache 2.0.
