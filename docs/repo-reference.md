# Repo reference

What runs, the stack, the repo layout, the subcommands and what is out of scope. Moved out of the root `AGENTS.md` so
that every session does not load it. The architecture narrative is `docs/how-atrium-works.md` and
`docs/fabric/hub-and-rooms.md`.

## What runs

Atrium is one binary with two long-running commands, and a third process a room may start.

| Command | What it is | Listens on |
| --- | --- | --- |
| `atrium run` | the hub: serves the board, holds no work, keeps rooms, the backlog, reports and a git mirror | board 127.0.0.1:7778, room link 127.0.0.1:7779 |
| `atrium room` | a room, one per machine: the database, the terminals, the agents | agents 127.0.0.1:7777, its own board 127.0.0.1:7781 |
| `atrium ptyhost` | the pty host: hidden, started by the room when the `pty_host` setting is on, holds the terminals so they outlive a room restart | no port: a named pipe on Windows, a 0600 unix socket elsewhere |

```
  browser                  HUB  atrium run                     ROOM  atrium room          (one per machine)
  ┌──────────┐  :7778   ┌──────────────────────────┐  dials out ┌────────────────────────────┐
  │ the board│ ───────► │ serves the board's files │ ◄───────── │ sqlite store               │
  └──────────┘          │ passes /v1/* to a room   │ :7779 mTLS │ ptys and the agents in them│
                        │ control MCP at /_hub/mcp │            │ :7777 agent listener       │
  claude sessions ────► │ its own store: rooms,    │            │ :7781 its own board        │
   (MCP over HTTP)      │ backlog, reports, git    │            └─────────────┬──────────────┘
                        └──────────────────────────┘          pipe or socket  │  only with pty_host
                                                              ┌──────────────┴─────────────┐
                                                              │ PTY HOST  atrium ptyhost   │
                                                              │ the ptys, outlives the room│
                                                              └────────────────────────────┘
```

Rooms dial the hub over mutual TLS, never the reverse. `atrium run` starts this machine's room when none answers. The
defaults are in `internal/cli/atrium_defaults.go`. `atrium daemon` still runs the older single process (board :7778,
agents :7777, no hub). `docs/fabric/hub-and-rooms.md` has the details.

The pty host is the same binary again, `atrium ptyhost`, hidden from `--help`. With the `pty_host` setting on, the room
starts it detached (from a copy, `atrium-pty`, beside the binary) and asks it for every terminal, so a runner lives
in the host and not in the room. It listens on no port. `docs/terminal/ptyhost-protocol.md` has the channel, the
frames and the lifetime.

A room keeps two listeners on purpose. When the store halts the agent listener closes and stays closed, so runners park
on connection-refused and burn nothing, while the board stays up to say what broke.

Key ideas, each documented at length in `docs/`:

- **Cards, not agents.** A card outlives the process it describes. `wire_name` is an attribute; a pid is only a
  reconnect hint.
- **Observed versus overrides.** What a machine reports never overwrites what a human typed.
- **The halt.** Storage failure is not a degraded mode. See "Resilience" below.
- **Status is a column, activity is a badge.** A column is a bucket of human attention. What a runner is doing
  right now is a fact about the runner, never stored. `docs/runtime/activity-design.md`.
- **Auto mode trades interruption for review.** `docs/runtime/auto-mode.md`. There is a per-session switch and a
  board-wide one. Both sit last in the permission chain, and both are recorded under their own name.
- **Atrium drives an overlay, it does not become one.** Loopback and no login stays true for the board.
  `docs/fabric/overlays.md`.
- **Supervision means atrium owns the terminal.** `docs/terminal/supervision-design.md`.

## Stack

- Go 1.26.x
- Cobra for CLI subcommands
- `modernc.org/sqlite`, pure Go so there is no cgo and no cross-compile pain
- `github.com/coder/websocket` for terminal attach, `github.com/aymanbagabas/go-pty` for ConPTY
- `github.com/modelcontextprotocol/go-sdk` (v1.5.0) for the MCP surfaces
- `github.com/openziti/sdk-golang` and `github.com/openziti/zrok/v2` for the overlays
- `net/http` stdlib for every HTTP server here
- xterm.js, vendored rather than from a CDN, because the board has to work offline and over an overlay

The schema is written to stay Postgres portable: text ULID-ish keys, RFC3339 text timestamps, `CHECK` instead of
enums, TEXT instead of JSONB, `?` placeholders. Nothing runs it against Postgres yet.

**State lives on disk.** The room keeps cards in its store and the hub keeps rooms, the backlog and reports in its
own. "How long has this been sitting" and "what was I even doing" cannot be answered from memory that dies with the
process.

## Repo layout

```
cmd/atrium/main.go          entry point, wires the cobra root
internal/cli/               every subcommand: run, room, rooms, daemon, launch, hook, finish, backlog and the rest
internal/daemon/            the room: wires store + api, owns hooks, launching, supervision, messaging
  daemon.go                   both listeners, the permission chain, the halt
  activity.go                 what a runner is doing right now. in memory on purpose
  supervisor.go               pseudo terminals atrium owns
  attach.go                   the websocket carrying one of those terminals
  messages.go                 saying something to a running session
  peers.go                    one session messaging another, QUEUED, never typed
  session.go                  join, leave, start, end, and the gate question
  launch.go                   spawning a runner from a harness row
  shutdown.go                 stopping without killing
  reaper.go                   free liveness: ask the OS, not the agent
  orphans.go                  a question asked for a session that has since gone
  drain.go                    turning auto mode on empties the queue it was turned on because of
  overlay*.go                 driving zrok or OpenZiti so the board is reachable from elsewhere
  fixtures.go                 terminals that come up with the daemon
  whereami.go                 where the daemon is listening, written down for the CLI to find
  sources.go                  running the commands that find work, bounded and reported on the row
  finish.go                   a session declaring its work over, which nothing could say before
  actions.go                  saying a stored prompt to a card, and asking the runner to leave
  sweep.go                    two timers that are easy to confuse: archiving, and deleting
internal/base/rules/        pure values the room and the hub share: the recogniser, worker policy, context limits, PRKey
internal/store/             the room's sqlite. every table, every query, the halt
  schema.go                   migrations, applied in slice order. read the header before editing
  tasks.go / permissions.go / rules.go / harness.go / messages.go / review.go
  fixtures.go                 what starts with the daemon, and adopting a card already there
  settings.go                 daemon-wide settings, keyed by name
  intake.go                   the inbox, its deduplication key, and what makes a link safe to render
  sources.go                  a command on a timer, shaped like harness because it is the same idea
  actions.go                  named prompts the operator wrote, and which card each belongs on
  recap.go                    what a session says it did, bounded
  history.go                  everything ever run here, cut by whether it was written up
internal/api/               the room's HTTP surface: JSON API, SSE, files, settings
  web/                        the board: index.html, css/, js/, m/ (the phone), read.html, sw.js
internal/link/              the hub: the proxy to rooms, the room link, the control MCP, git, notifications
internal/hubstore/          the hub's own store
internal/ptyhost/           the pty host that keeps terminals alive across a room restart
internal/gitsync/           git between hub and rooms
internal/base/forge/        forges
internal/base/prreview/     pull request review
internal/claudeconf/        reading and writing a runner's hook config
  whichexe.go                 WHICH BINARY goes in there. not `os.Executable()`. read the header
internal/base/safepath/     is this path inside that directory. one function, three rules
internal/base/ edge/ itemgate/ requirements/ resources/ mcprule/ cardurl/ cardcolors/ shellpick/ inputlag/ webasset/
  detach/ testguard/
internal/ runnerprofile/ runnersetup/ roomstats/ deployready/
                            one concern each
```


## Subcommands

The main ones. `atrium --help` lists the rest.

| Command | Purpose |
| --- | --- |
| `atrium run` | The hub. Serves the board, and starts this machine's room when none is running. |
| `atrium room` | A room: the agents on this machine, attached to the hub it joined. `atrium room join <string>` joins one. |
| `atrium rooms` | The hub's side of rooms: add one and print its join string, mint a fresh token, the git mirror. |
| `atrium daemon` | The older single process: board, JSON API, SSE stream and agent listener, no hub. |
| `atrium backlog` | The hub's backlog: list, show, file, status, priority, import, from any room. |
| `atrium ci` | A repository's CI, read through the hub as `atrium_ci` is. A log is written to a file and its path printed. |
| `atrium stop` | Wind a daemon down. NOT the same as killing it: see "Resilience". |
| `atrium join` / `leave` | Put the current session on the board, or take it off, without restarting it. |
| `atrium hook --event <e>` | One activity event, posted and forgotten. Registered in `settings.json` by the board. |
| `atrium session --event <e>` | A session opening or closing. Finds the runner's pid without WMI. |
| `atrium turn --event end` | Claude Code's Stop hook. The only way to reach a session sitting idle. Optional, and never installed by "install all": it is the one hook whose answer changes what a session does. |
| `atrium finish [recap]` | An agent saying its work is over, and what it did. A command rather than a tool, because a command is the channel every runner already has. |
| `atrium peers` | The other sessions this one can address, with what each is doing and how much is already queued for it. |
| `atrium tell <handle> <msg>` | Say something to another session. QUEUED, never typed: atrium owns a terminal a human may be mid-command in, and a peer is not that human. An unknown handle answers with the list of ones that would have worked. |
| `atrium name [<name>]` | Name this atrium, once. Prefixes every wire name so two machines cannot collide. |
| `atrium version` | What this binary is: the tag, the commit, the board hash, the platform. Stamped by the linker, `dev` when it is not. |
| `atrium launch` | Start a runner in a directory you have already prepared. Takes `--tags`, `--prompt` and `--external` so a script can produce a card that is already filed, and `--item` (with `--item-owner`) to start it as an item (`docs/changes/items-stage1.md`, `docs/changes/items-stage2a.md`). |
| `atrium control` | Stdio MCP server. Registered at USER scope against an INSTALLED copy of the binary, never `build.claude/`, which an MCP server holds open and would block every build. |


## Out of scope (intentional)

- Injecting prompts into running claude sessions from outside MCP. There is no IPC channel into a claude
  process. This holds for the peer bus too: one session messaging another is QUEUED and delivered by a hook,
  never typed into the target's terminal, even where atrium owns one and could. See `internal/daemon/peers.go`,
  which says so at length because the tempting simplification is to reuse `handleMessage`, which does type.
- Replacing `gwt sessions`. It remains the per-repo workflow tool.
- Authentication on the board. The board binds loopback and has no login. Reaching it from elsewhere is an
  overlay's job, not an auth layer invented here. `--shutdown-token` is the one exception, and only because a kill
  switch reachable from anywhere is worth ruling out by accident. Atrium SERVES THE BOARD ON an overlay
  (`atrium run --board-transport zrok|ziti`, `docs/fabric/overlays.md`), which is not the same thing and must not
  become it: both SDKs hand back a `net.Listener` and the board is one handler, so nothing is proxied and no
  traffic passes through atrium that was not already destined for it. It never holds an identity or decides who
  may connect.

  The room link is the one place atrium does decide who connects, and it does so with the hub-signed
  certificate above, over direct TCP or inside a zrok or OpenZiti overlay. `overlay_reserve.go` calls the zrok
  REST API to reserve a name, using the token that machine's own `zrok enable` put on disk. The rule that holds:
  atrium may hold the NAME of a command that has a credential, and never somebody else's credential.
- Storing what a runner is doing right now. It would be a lie the moment the room restarted.

## Out of scope that we MIGHT do later (named, not promised)

- WebSocket transport for the agent listener (push semantics, one connection per agent, less reconnect chatter).
  The terminal attach already uses one.
- Per-agent transcript on disk.
- A "broadcast" command: send the same prompt to N agents at once.
- Postgres. The schema is written for it and nothing has ever run against it.
- A model reading the auto-mode review and summarising what a session actually changed.
