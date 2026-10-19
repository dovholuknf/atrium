# AGENTS.md (atrium)

Atrium organizes coding agents: durable state, a permission gate, a web board, and terminals it owns. Start with the
in-repo bring-up, `docs/agents.md`. This file holds the rules that sit beside the code.

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
internal/forge/             forges
internal/prreview/          pull request review
internal/claudeconf/        reading and writing a runner's hook config
  whichexe.go                 WHICH BINARY goes in there. not `os.Executable()`. read the header
internal/safepath/          is this path inside that directory. one function, three rules
internal/edge/ itemgate/ requirements/ resources/ runnerprofile/ runnersetup/ roomstats/ mcprule/ cardurl/
  cardcolors/ deployready/ detach/ shellpick/ inputlag/ testguard/ webasset/
                            one concern each
```

## Migrations -- read this before touching schema.go

Migrations are applied in slice order and recorded by name. **A migration already recorded will never run again**,
so editing one is the same as not shipping it. Two rules follow:

- **Add at the END of the slice.** Adding in the middle means an existing database silently skips it.
- **Write every statement to tolerate already being there.** `CREATE TABLE IF NOT EXISTS`, and the runner
  swallows "duplicate column name" for `ADD COLUMN`, which SQLite has no `IF NOT EXISTS` for.

Both rules exist because they were broken. Adding `perm_rule` to `0001` meant every existing database had `0001`
recorded, never created the table, and halted on the first rule lookup.

Changing a `UNIQUE` constraint means rebuilding the table: SQLite cannot alter one in place. See `0010` and
`0014` for the pattern.

## Documentation

Follow the reading order in `docs/README.md`: `docs/agents.md`, `docs/how-atrium-works.md`,
`docs/fabric/hub-and-rooms.md`, `docs/runtime/agent-messaging.md`, then the `AGENTS.md` of the package being changed.
`docs/README.md` maps the rest of the folder, and `docs/agents.md` says which docs to trust.

- **The backlog is on the hub.** Use `atrium_backlog` or `atrium backlog`. `docs/backlog.md` is frozen.
- **A change ships its own notes.** Never edit `CHANGELOG.md` or `docs/test-plan.md`. Write
  `changelog/<dept>/<yyyy-mm-dd>-<item>.md` and `docs/changes/<item>.md` (its test plan section).
  `docs/changes/README.md` has the format.

## Subcommands

The main ones. `atrium --help` lists the rest.

| Command | Purpose |
| --- | --- |
| `atrium run` | The hub. Serves the board, and starts this machine's room when none is running. |
| `atrium room` | A room: the agents on this machine, attached to the hub it joined. `atrium room join <string>` joins one. |
| `atrium rooms` | The hub's side of rooms: add one and print its join string, mint a fresh token, the git mirror. |
| `atrium daemon` | The older single process: board, JSON API, SSE stream and agent listener, no hub. |
| `atrium backlog` | The hub's backlog: list, show, file, status, import. |
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

## The permission chain -- order is critical

`onPermRequest` in `internal/daemon/daemon.go`. Each step can answer and stop. Reordering changes what the tool
means, so do not do it without reading why each one sits where it does.

1. **A replayed decision.** A request with a dedup key already answered returns that answer.
2. **A queued message.** Delivered as a block carrying the text, framed so the model reads it as you talking
   rather than as a policy refusal. Ahead of everything else because a message is you reaching out.
3. **A shelved card.** A standing no. Putting work down has to answer for that work, or the agent freezes behind
   a card you stopped looking at.
4. **A standing rule.** The most specific match wins. Prefix, glob, or folder.
5. **Auto mode.** Approve without asking, record everything. LAST on purpose: it means "stop asking me new
   questions", not "forget the answers I already gave", so 2, 3 and 4 all still win.
6. **Ask the human.** The card moves to `needs-permission` and the agent blocks.

## Resilience guarantees (the room)

CRITICAL. Do not weaken them without an explicit reason.

1. **Storage failure halts, it does not degrade.** Open or migration failure means the room refuses to start.
   A later failure closes the agent listener and leaves it closed, so runners see connection-refused and park on
   the backoff they already have, burning nothing. The process stays alive and the board reports the cause.
   Running without durable state is worse than not running.
2. **A hook must never fail a session.** Everything posted by a hook is best effort. `/activity` answers before
   it does any work and swallows unknown agents; `/session` logs and moves on; the permission hook fails open
   when atrium is unreachable. A session must never fail to start, finish a turn, or make a tool call because
   atrium was not listening.
3. **`/activity` is fire and forget from the caller's side too.** It rides `PreToolUse`, the hot path for every
   tool call in every session. One second timeout, every failure ignored, nothing downstream reads the result.
   Do not add a retry. Do not raise the timeout. See `docs/runtime/activity-design.md`.
4. **Shutdown is narrated and bounded.** Event streams released first, since Shutdown waits on in-flight
   requests and one open tab would otherwise hold the whole grace period. Supervised runners get ten seconds.
   Every step logs, because several seconds of silence reads as a hang.
5. **A kill is not a stop.** Without the `pty_host` setting, the room owns a pseudo terminal per supervised runner,
   and closing one takes the attached process with it. `taskkill` on the room ends every runner at once. Use
   `atrium stop`. With `pty_host` on, the terminals live in a separate pty host process and survive a room restart.
6. **A source is a suggestion, never durable state.** Intake cannot halt anything. A source that fails is
   reported on its own row and retried on its next tick, its output is bounded WHILE being read rather than
   after, and three consecutive failures switch it off with the reason attached. A run that could not land
   everything is recorded as a failure, because items already offered are deduplicated on the next tick and the
   thing that must not happen is a row claiming success while the inbox is short.
7. **`/finish` follows the hook posture.** A session declaring its work over is best effort like everything else
   on the agent listener: an agent atrium has never heard of gets `ok` and nothing recorded, because failing
   there would mean a session could fail at the moment it tried to end tidily.
8. **Files never leave a card.** Upload computes its own destination and accepts no path at all. Download takes
   one and resolves it through `internal/safepath`, which follows symlinks on both sides. Everything outside a
   card answers `403` whether or not it exists, so the endpoint is not an oracle for what is on the machine.

## Testing

Before handing back, run `bash scripts/test-touched.sh` (or `scripts/test-touched.ps1`) and nothing else. It diffs
against `claude/main` (`--base REF` for another) and runs the changed Go packages, the headless board units mapped
from the changed board files plus `bootClean`, and the node tests whose subject changed. It prints what it picked and
why, and exits non-zero on any failure. `--plan` prints the plan and stops.

The whole suite runs after landing, by whoever lands. A red there is a follow-up, not a reason to run it all first.
`check-board.sh`, `test-board-headless.js` and `test-board-sharded.js` refuse a full run without `FULL_SUITE=1`,
which CI (`scripts/ci.sh`), `make check` and `merge-check.ps1` set.

To run a few headless units by hand, name them: `HEADLESS_UNITS=bootClean,pulls node scripts/test-board-headless.js`,
or `node scripts/test-board-sharded.js --local --units bootClean,pulls` for parallel shards. `HEADLESS_LIST=1` lists
the units.

## Branching and landing work

A feature is not done until it lands cleanly on `dovholuknf/atrium:main`. That is the bar, not "it works on my
machine" and not "it is deployed to the live hub".

- `claude/main` is the integration branch, branched from `origin/main`. Every feature branches from it and merges
  back to it. Recreate it from `origin/main` when it is missing.
- A feature gets its own branch off `claude/main`, and merges back to `claude/main` when it is done.
- A worker is launched with `repo` and `branch`, not with a worktree made by hand: the room makes the card's home under its
  `card_root` and adds the worktree there. `scratch: true` is for throwaway work. With the room setting
  `card_root_required` on, the room refuses an agent launch whose `cwd` is outside the card root.
- `claude/main` has to fast-forward onto `origin/main`. If it does not, rebase it on `origin/main` and fix the
  conflict before calling anything done. Landing on the remote `main` is the finish line.
- A merge worker merges the branch, runs `bash scripts/test-touched.sh`, and lands with
  `git push hub HEAD:claude/main`. The hub takes that only as a fast-forward of its `claude/main`, and the orchestrator's
  checkout follows it (`docs/fabric/git-sync-design.md` 4.3). A refusal means `claude/main` moved: merge again and
  push again. Never push `main` or a tag, and never ask the orchestrator to move `claude/main` by hand.
## Conventions

- Comments, especially in tests: delete every comment unless it is a one-line why for a non-obvious invariant. Trim
  a longer one to that line or remove it. Apply this to every comment in each function or test you touch, and never
  add or reword one while fixing a failing test. Never write one about what changed or how the cause was found: that
  goes in the commit, `REPORT.md` or `docs/troubleshooting-tests.md`.
- All paths in code use forward slashes (Go convention). Reading from gwt JSONs with backslashes is fine; pass
  through unchanged.
- Wrap markdown at 120 characters.
- No em-dashes (U+2014), no double-hyphen as a dash, no semicolons in prose.
- Build with `make build` (writes to `build.claude/`). Never to the repo root.
- The room link is mutual TLS. `atrium rooms add <name>` prints a one-time join string, and joining saves a
  certificate the hub signs. The name in that certificate is the room's identity, and a room the hub has no record
  of cannot attach.

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
