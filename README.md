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
shelved, dead. The columns are buckets of your attention, so a card sits in one because you have to act or
because you decided something. A column can be folded away when it gets tall, and the finished ones can be
cleared out in one press.

**What each one is doing right now.** Each card shows a live badge: thinking, running `Bash`, three subagents,
and how long it has been at it. This is the difference between "leave it alone" and "go look at it", and
"running Bash for 40 minutes" is information a status column cannot give you. Never stored, because a stored
activity is a lie the moment the daemon restarts.

**A permission gate.** A PreToolUse hook sends every tool call an agent wants to make to atrium, which blocks
until you answer. Approving is one click. Blocking hands your reason back to the agent, so a refusal is "no, do
this instead" rather than a wall. Every request says which agent is asking, because with several running the
same command means different things from different sessions.

**Standing rules, so you stop clicking.** Answer once with **always** or **never** and every matching request
after that is answered instantly and never shown. A rule covers either a command shape or a folder:

- A **command shape** is a prefix by default, or a glob when it contains `*` or `?`. `go build` covers every
  later build and leaves `go install` to ask on its own.
- A **folder** covers work inside a directory. Two ways in: the command names an absolute path inside it, or
  the session is working inside it and the command does not reach out. The second is what makes it useful,
  since commands are written relative to where the session is and `go test ./...` names no path at all. A
  command mentioning any absolute path outside the folder, or climbing out with `..`, still asks. It does not
  follow a `cd`: a rule answers a request, it does not simulate a shell.

  Folders exist because writing the same thing as a glob means accounting for the quoting yourself, and
  `rm -f "C:/x/*"` fails against `rm -f "C:/x/y.db"` over the closing quote alone. Silently.

The most specific match wins, so a narrow rule overrides a broad one. Atrium can import the allow and deny lists
Claude Code already has, which on a working setup means starting with a hundred or more rules rather than none.

**Auto mode, when you do not want to be asked at all.** Turn it on for one session and its requests are approved
without stopping, while everything is still recorded. It does not override a **never** rule or a shelved card:
auto mode means stop asking me new questions, not forget the answers I already gave. Afterwards, **what did it
do?** reads the record back, grouped by tool, with identical calls folded into one line and the decisions nobody
saw put first. That last part is the whole trade: interruption for review.

**The change, not just the target.** A pending edit shows a real diff, with unchanged context dimmed and the
changed words picked out. "Approve this edit" is not a question you can answer from a file path.

**Notifications that reach you.** A desktop notification has approve and block buttons, and works with no
atrium tab open. An in page toast covers the case where the tab is already in front. Sounds are configurable per
event, and so is how long a notification stays before it takes itself down.

**A history.** Every card has an append only event log, and every permission decision is recorded with which
agent asked and who answered: you, the rule that matched, or auto mode. Filterable, exportable as JSON or CSV.

**Runners you can start, and terminals you can watch.** claude, codex, ollama, a bare shell, or anything else you
add. A runner is a command, arguments, a working directory, an environment, and a way to resume. Adding one is
configuration, not a code change. Anything atrium launches runs under a pseudo terminal it owns, so you can
attach to it in the browser, type into it, and stop it. Pick the working directory by browsing the daemon's
filesystem, which is the right filesystem even when the board is open on your phone.

**Something to say to a running session.** Queue a message and it is delivered the next time that session can
hear one: typed straight into its terminal when atrium owns it, or carried back through a hook when it does not.
It arrives framed as a message from you rather than as a policy refusal.

**Liveness for free.** A card stores its runner's process id, and whether that process still exists is a question
the operating system answers. No turn, no token, no contact with the agent.

**A way to stop that is not a kill.** `atrium stop`, or a request to `POST /v1/shutdown`, winds the daemon down
the way ctrl-c does: event streams released, supervised runners given ten seconds to finish, listeners closed in
order. Killing the process closes every pseudo terminal at once and takes the runners with it.

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
agent long poll ceiling, `--tui` to also attach the terminal UI, `--shutdown-token` to allow a remote shutdown
carrying that token instead of refusing everything but loopback.

## Subcommands

| Command | Purpose |
| --- | --- |
| `atrium daemon` | The board, the API, and the agent listener. This is the one you run. |
| `atrium stop` | Ask a running daemon to wind down. Not the same as killing it. |
| `atrium join` | Put the session you are in on the board and start gating it, without a restart. |
| `atrium leave` | Stop gating it and mark its card done. |
| `atrium launch` | Put a directory on the board and start a runner in it, for scripts that make worktrees. |
| `atrium hook` | Report one activity event. Run by Claude Code, wired for you from the runners tab. |
| `atrium hub` | The v1 terminal broker. Still works, untouched. |
| `atrium agent` | The v1 MCP client side of that loop. |
| `atrium serve` / `status` / `watch` | Read-only views over an external worktree ledger. |

## Scope, and what is not built

This is a personal tool, and the parts that are missing are missing on purpose or are simply next.

- **Single machine, no auth.** Loopback only. Reaching it from elsewhere is a job for an overlay such as
  OpenZiti, not for an auth layer invented here. Shutdown is the one endpoint with a guard, and only because a
  reachable-from-anywhere kill switch is the kind of accident worth ruling out.
- **A supervised runner dies with the daemon.** The daemon owns each pseudo terminal, and on Windows closing one
  takes the attached process with it. There is no reattach. So stopping the daemon ends every runner it started,
  and the answer is resume ids rather than orphan survival, which ConPTY does not offer.
- **Shelving does not yet stop the runner.** It moves the card and answers anything the session was waiting on,
  but the process keeps sitting there. Stopping it and resuming later is next.
- **The board is a plain page**, not the React app the design calls for. It speaks the same JSON and SSE API, so
  replacing it is a client side job.
- **Windows first.** It builds and the tests pass elsewhere, but the hooks shipped alongside it are PowerShell.

## Documentation

- `docs/architecture-v2.md` -- the design, the decisions, what is built and what was abandoned.
- `docs/user-guide.md` -- walkthroughs, including the hooks.
- `docs/backlog.md` -- what is outstanding, why it matters, and what is out of scope.
- `docs/activity-design.md` -- the live badge on a card, and why it is never written down.
- `docs/auto-mode.md` -- approving without being asked, and reading the record afterwards.
- `docs/supervision-design.md` -- pseudo terminals, attaching from the browser, and how a runner is stopped.
- `docs/test-plan.md` -- manual scenarios that should pass before tagging a build.
- `CHANGELOG.md` -- what landed, and when.

## License

Apache 2.0. See `LICENSE`.
