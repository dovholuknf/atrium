# Atrium

_The single open hall every agent passes through._

Atrium organizes coding agents. It is a daemon plus a web board that answers the questions you actually have when
several agents are running at once: which one needs me, how long has it been sitting there, what was I doing in
that one, and is it even still alive.

It runs on one machine, for one person. There is no multi-tenancy, no accounts, and no cloud.

```powershell
go build -o build.claude\ .\...
.\build.claude\atrium.exe daemon
# agents -> http://localhost:7777
# board  -> http://localhost:7778
```

## What it gives you

**A board.** Every agent is a card in a kanban column: backlog, running, needs input, needs permission, done,
shelved, dead. Each card shows how long it has been idle and how long it has been waiting on you. A column can be
folded away when it gets tall.

**A permission gate.** A PreToolUse hook sends every tool call an agent wants to make to atrium, which blocks
until you answer. Approving is one click. Blocking hands your reason back to the agent, so a refusal is "no, do
this instead" rather than a wall.

**Standing rules, so you stop clicking.** Answer once with **always** or **never** and every matching request
after that is answered instantly and never shown. Patterns are a prefix by default, or a glob when they contain
`*` or `?`, and the most specific match wins. Atrium can import the allow and deny lists Claude Code already has,
which on a working setup means starting with a hundred or more rules rather than none.

**The change, not just the target.** A pending edit shows a real diff, with unchanged context dimmed and the
changed words picked out. "Approve this edit" is not a question you can answer from a file path.

**Notifications that reach you.** A desktop notification carries approve and block buttons, and works with no
atrium tab open. An in page toast covers the case where the tab is already in front. Sounds are configurable per
event.

**A history.** Every card carries an append only event log, and every permission decision is recorded with who
answered it: you, or the rule that matched. Filterable, exportable as JSON or CSV.

**Runners you can start.** claude, codex, ollama, a bare shell, or anything else you add. A runner is a command,
arguments, a working directory, an environment, and a way to resume. Adding one is configuration, not a code
change.

**Liveness for free.** A card carries its runner's process id, and whether that process still exists is a question
the operating system answers. No turn, no token, no contact with the agent.

## Quick start

### 1. Build

```powershell
go build -o build.claude\ .\...
```

### 2. Run the daemon

```powershell
.\build.claude\atrium.exe daemon
```

Two listeners, on purpose. Agents talk to `:7777`. You talk to `:7778`. If storage ever fails the agent listener
closes and the board stays up to tell you why.

Open <http://localhost:7778>.

### 3. Gate a session through it

Atrium sees an agent when that agent's hook reports in. The hook lives outside this repo, in your Claude Code
configuration, and posts to `/permission` before every gated tool call and to `/session` when a session starts or
ends.

A minimal hook posts JSON like this and blocks on the response:

```json
{ "agent": "my-session", "tool": "Bash", "command": "go build ./...", "pid": 4242, "cwd": "/path/to/repo" }
```

```json
{ "decision": "approve", "reason": "", "command": "optional rewrite" }
```

See `docs/user-guide.md` for a working PowerShell hook, and "Permissions-only mode" for gating every session on a
machine without wiring anything into the agent itself.

### 4. Import the rules you already trust

On the board, **perms** then **import rules from claude**. It previews what it would add before adding anything,
translating `Bash(go build:*)` into a prefix, `//c/temp/**` into a real path, and reporting anything it cannot map
rather than dropping it silently.

## Two other modes

**Hub and agent loop.** The original mode, still here. `atrium hub` is a terminal you type into, and a claude
session with the `atrium-agent` MCP server wired in calls one tool, `submit`, in a loop: it posts, blocks until
you reply, acts on the reply, posts again. The agent absorbs disconnects and long poll timeouts silently, so the
model never wakes up while you are away and idle costs nothing. See `docs/user-guide.md`.

**State aggregator.** `atrium serve`, `status` and `watch` read a session ledger written by an external worktree
tool and expose it over MCP. Optional and inert unless `WORKTREE_ROOT` points at a ledger it understands.

## Configuration

| Var | Default | Meaning |
| --- | --- | --- |
| `WORKTREE_ROOT` | unset | Root of a worktree tree. The daemon keeps its database in `hub/` under it. Unset means `~/.atrium`. |
| `ATRIUM_HUB_URL` | `http://localhost:7777` | Where the hooks post. |
| `ATRIUM_PERM_GATE` | unset | `on` gates every session. `off` disables. Unset gates only sessions with the atrium-agent MCP wired in. |
| `ATRIUM_AGENT_NAME` | directory name | What a session calls itself. Set automatically for runners atrium launches. |
| `ATRIUM_TASK_ID` | unset | Binds a launched runner to the card that launched it. Set automatically. |
| `ATRIUM_DISCONNECTED_LOG_INTERVAL` | `10m` | How often the agent mentions that the hub is unreachable. |

Flags: `--addr` for the agent listener, `--http` for the board, `--db` for the database, `--long-poll` for the
agent long poll ceiling, `--tui` to also attach the terminal UI.

## Scope, and what is not built

This is a personal tool, and the parts that are missing are missing on purpose or are simply next.

- **Single machine, no auth.** Loopback only. Reaching it from elsewhere is a job for an overlay such as
  OpenZiti, not for an auth layer invented here.
- **PTY supervision is not built.** A runner launched from atrium opens a real terminal and owns itself, which
  means atrium cannot attach to it in the browser and cannot reliably stop it. That is the next significant piece.
- **The board is a plain page**, not the React app the design calls for. It speaks the same JSON and SSE API, so
  replacing it is a client side job.
- **Windows first.** It builds and the tests pass elsewhere, but the hooks shipped alongside it are PowerShell.

## Documentation

- `docs/architecture-v2.md` -- the design, the decisions, what is built and what was abandoned.
- `docs/user-guide.md` -- walkthroughs, including the hooks.
- `docs/test-plan.md` -- manual scenarios that should pass before tagging a build.
- `CHANGELOG.md` -- what landed, and when.

## License

Apache 2.0. See `LICENSE`.
