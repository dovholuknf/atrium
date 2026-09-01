# Atrium user guide

Real-world walkthroughs for the patterns this thing supports. If you want a feature reference instead, see the
[README](../README.md).

## Pattern 1: one human, one agent, one terminal

The simplest case. You sit at the hub terminal, fire prompts, one agent does the work in a worktree somewhere.

### Setup

```powershell
# build once
cd D:\git\github\dovholuknf\atrium
go build -o build.claude\atrium.exe .\cmd\atrium

# drop an .mcp.json into the dir where you want the agent to live.
# the agent's name will be the leaf of that dir. for example, an agent in
# D:\worktrees\github\openziti\ziti\fix-login will show up as 'fix-login'.
@'
{
  "mcpServers": {
    "atrium-agent": {
      "command": "D:/git/github/dovholuknf/atrium/build.claude/atrium.exe",
      "args": ["agent", "--url", "http://localhost:7777"]
    }
  }
}
'@ | Out-File -Encoding utf8 .\.mcp.json
```

### Run

```powershell
# terminal A: the hub
.\build.claude\atrium.exe hub

# terminal B: launch claude in the dir with the .mcp.json
gwt cd <branch>      # or just: cd D:\some\worktree
claude               # the .mcp.json is auto-discovered

# in claude:
#   activate the loop by saying "atrium" (or "run atrium" / "start atrium" / "atrium go").
#   the agent loop is opt-in: the model treats atrium-agent as dormant until it sees a
#   recognizable activation phrase. Before activation, the session behaves like a normal
#   claude conversation.
#   to exit the loop within an active session, say "stop atrium" or "leave atrium".
```

You should see the greeting in the hub. Type prompts, watch responses come back.

### Daily routine

- Open hub once at the start of the session, leave it running all day.
- Spawn agents in whatever worktrees you're working in. Drop `.mcp.json` if missing, launch claude, kick it off.
- Type prompts in the hub. Use `@<name>` to target if you have more than one agent running.
- When you're done with an agent, close its claude tab. The hub stays up.

## Pattern 2: one human, many agents, one terminal

Same as pattern 1, multiplied. The hub multiplexes; routing is by agent name.

### Setup

Drop `.mcp.json` into every dir where you want an agent. Agent names default to the leaf of the cwd. If two
worktrees have the same leaf (unlikely but possible), override one with `--name <something>` in the `args`.

### Run

```powershell
# hub in one tab
.\build.claude\atrium.exe hub

# in three other tabs: claude in three different worktrees
gwt cd fix-login        ; claude
gwt cd add-redirects    ; claude
gwt cd update-validation; claude
```

After each one greets, the hub will have three known agents.

### Targeting

```
> /agents
[atrium] known agents:
    fix-login          (last 14:02:11)
  * add-redirects      (last 14:02:33)   <- star = current default target
    update-validation  (last 14:02:55)

> @fix-login summarize the diff against main
[atrium] -> fix-login

> @update-validation any new test failures since last run?
[atrium] -> update-validation
```

Bare prompts (no `@`) go to the most-recently-active agent.

## Pattern 3: hub-down recovery

You closed the hub by mistake (Ctrl-C, accidental window close, machine sleep). Agents are now disconnected.

### What you'll see

Nothing visible in claude. The agent's `submit` MCP tool is blocking inside its silent retry loop. The LLM is
parked. No token burn.

If you want to confirm, grep claude's MCP debug log for lines like:

```
[atrium-agent fix-login] hub http://localhost:7777 unreachable (...) -- retrying silently every 5s
```

(One on first failure, then one every 10 minutes by default. Tune via `ATRIUM_DISCONNECTED_LOG_INTERVAL`.)

### Recovery

Just restart the hub:

```powershell
.\build.claude\atrium.exe hub
```

The agents' next retry succeeds and they pick up where they left off. You'll see a "resumed" line in the MCP
debug log. The next prompt you type from the hub flows normally.

### When you want a longer nag interval

```powershell
# in the SAME shell as the agent's parent claude, before launching claude:
$env:ATRIUM_DISCONNECTED_LOG_INTERVAL = "1h"
claude
```

Or set it persistently via `setx`.

## Pattern 4: navigating the TUI

The Bubble Tea TUI is the default for `atrium hub`. Three tabbed views with keyboard nav.

### Tabs

- `Tab` / `Shift+Tab` cycles `chat` → `perms` → `agents`.
- Or jump directly with `/chat` `/perms` `/agents`.

### Chat view (per-agent)

Each agent has its own scrollback. Tab bar shows `chat: <active-agent>` and `(+N)` for unread in OTHER agents.

- `Ctrl-K` opens the agent picker. ↑/↓ to move, Enter to select, Esc to cancel.
- `1`-`9` (when input is empty) quick-switches to the Nth known agent. UNLESS the active agent has a pending
  `{choices}` picker, in which case the number picks the choice.

### Perms view

Lists every pending permission with command preview.
- `a` approve oldest, `d` deny oldest.
- `/approve <id>` / `/deny <id>` for a specific one.
- `y` / `n` from anywhere = oldest.

### Agents view

Every known agent, last-contact time, current default-routing target marked `>`, flashing `← waiting` on
agents that have submitted and haven't been answered.

### Status bar

The bottom strip shows transient feedback (`approve perm #5 (atrium)`) on the left and the current key-hint
cheatsheet on the right.

## Pattern 4b: targeting an agent by name with auto-naming

Question that comes up a lot: "the model said its name was X, why doesn't `@X` work?"

The agent's *registered* name is `--name` from `.mcp.json` (or the cwd leaf if `--name` is omitted). The
**model's prose** ("I'll call myself atrium-client-1") has zero authority over the routing key. The hub binds
to the value in the POST payload, which the Go agent code sets from `--name`.

### Renaming (UI-only, from the hub)

In the hub TUI, type `/rename <wire-name> <new display name>`. This sets a per-agent UI alias. Wire routing
still uses the underlying name (you didn't have to restart anything), but everywhere in the TUI you see the
friendly name, and `@<friendly-name>` works for targeting.

```
> /rename atrium-19432 scout
[atrium] renamed atrium-19432 -> scout
> @scout pull latest and re-run tests
[atrium] -> atrium-19432
```

Clear an alias by calling `/rename <name>` with no second argument.

### Renaming the WIRE name (persistent across launches)

If you want the wire name itself to be different (so it's stable in logs and across restarts), edit `.mcp.json`:

```json
"args": ["agent", "--url", "http://localhost:7777", "--name", "scout"]
```

Or set the env var before launching that claude:

```powershell
$env:ATRIUM_AGENT_NAME = 'scout'
claude
```

Otherwise the default `<cwd-leaf>-<pid>` is fine; two agents in the same dir get distinct names automatically.

### Forgetting a stale agent

When a claude process dies, its wire name lingers in `/agents` and the switcher because the hub keeps every
name it has ever seen. To clear one, go to the all-agents tab, highlight the row with `↑`/`↓`, and press `x`
(or `Delete`). That drops it from both the TUI and the hub's in-memory maps. If that agent is actually still
alive and POSTs again, it re-registers fresh on its next submit, so forgetting a live agent is harmless.

### Why we don't let the LLM choose its own name

Because the LLM is the user-content layer and the routing key is a system identifier. Cleanly separating those
saves you from "I asked claude to call itself X and now nothing routes to it" debugging sessions. The /rename
alias gives you the friendly-name UX without coupling it to model output.

## Pattern 5: choices pickers

When an agent asks you "which of these do you want me to do?" it should emit a `{choices}...{/choices}` block.
The TUI shows it as a numbered box; `1`-`9` picks.

### What it looks like

Hub chat view:

```
[14:32:11] atrium/response
  How would you like to proceed?

  ┌─ choices ─────────
  │ 1) walk through the codebase
  │ 2) add a new feature
  │ 3) run the test suite
  │ 4) something else (describe it)
  └─ press 1-4 to pick (or type your own reply)
```

Press `1`. Status bar shows `picked: walk through the codebase`. The agent receives that text as its next
prompt and continues.

### When this doesn't fire

If the agent invents ad-hoc `1) ... 2) ...` prose instead of using the sentinel block, that means it's running
an old binary OR its MCP tool description regressed. Restart the agent claude. The tool description tells the
model to use `{choices}` whenever the natural reply is a small finite set; pasting it as prose is explicitly
called out as worse.

### Typing your own reply

Always works. Just type and Enter. The picker disappears as soon as you send any prompt to that agent.

## Pattern 6: combining the two modes

You can run `atrium hub` (Mode A: interactive) AND register `atrium serve` (Mode B: read-only state) in your
global claude settings. Then any claude tab -- including agents driving the hub -- can call `snapshot` to ask
"what is every other gwt session doing right now?" or `wait_for_change` to be notified of state transitions.

Example use: an agent assigned to "be the watchman" can sit on `wait_for_change` and report back to the hub
when something needs input elsewhere. The hub is your interactive surface; Mode B is the observational
substrate.

## Limits / what this won't do

- **No persistence.** Stopping the hub loses the conversation log. We may add per-agent JSONL transcripts in
  `$env:WORKTREE_ROOT\hub\` later.
- **No auth.** Single machine, localhost. If you bind to a non-loopback address, anything on your LAN can talk
  to the hub. Don't do that without thinking about it first.
- **No real TUI yet.** If a message arrives while you're mid-type, it'll print over your input. Workaround:
  finish your line and re-read.
- **No multi-host.** If you need cross-machine, the right substrate is Agora (OpenZiti A2A), not extending
  Atrium with network identity / policy code.

## Reference -- where things live

| Thing | Path |
| --- | --- |
| Built binary | `D:\git\github\dovholuknf\atrium\build.claude\atrium.exe` |
| Project MCP config | `<repo or worktree>\.mcp.json` |
| Atrium repo docs | `D:\git\github\dovholuknf\atrium\docs\` |
| gwt session JSONs | `$env:WORKTREE_ROOT\sessions\*.json` |
| gwt state log | `$env:WORKTREE_ROOT\watch\state.log` |
| claude MCP debug logs | check claude-code's per-session log location (varies by version) |
