# The hub and the rooms

Status: built. This is the reference for how atrium runs today. The designs that led here are in `docs/archive/`
(`hub-room-plan.md`, `hub-room-requirements.md`, `one-atrium-plan.md`, the federation and forum designs), and the
decisions that shaped it are `docs/fabric/hub-decisions.md`.

Atrium is one binary with two long-running commands. `atrium run` is the hub: it serves the board and holds no work.
`atrium room` is a room: it owns the database, the pseudo terminals and the agents. Every machine that runs agents
runs one room, and every room dials one hub. The split follows lifetimes. Board code changes every few minutes and
agents must not die for it, so the process that serves the board is not the process that holds the terminals.

## What runs, and where it listens

```
  browser                  HUB  atrium run                     ROOM  atrium room          (one per machine)
  ┌──────────┐  :7778   ┌──────────────────────────┐  dials out ┌────────────────────────────┐
  │ the board│ ───────► │ serves the board's files │ ◄───────── │ sqlite store               │
  └──────────┘          │ passes /v1/* to a room   │ :7779 mTLS │ ptys and the agents in them│
                        │ control MCP at /_hub/mcp │            │ :7777 agent listener       │
  claude sessions ────► │ its own store: rooms,    │            │ :7781 its own board        │
   (MCP over HTTP)      │ backlog, reports, git    │            └────────────────────────────┘
                        └──────────────────────────┘
```

| Port | Who listens | Who connects | Bind |
| --- | --- | --- | --- |
| 7778 | the hub (`--addr`) | the browser, the control MCP, `atrium` CLI calls that go through the hub | loopback, always |
| 7779 | the hub (`--link`) | rooms, dialling in | loopback by default, may bind wide |
| 7777 | the room (`--agent`) | hooks, `atrium tell`, `atrium finish`, and the other agent-side commands | loopback |
| 7781 | the room (`--http`) | the browser, when the hub is down | loopback |

The defaults are in `internal/cli/atrium_defaults.go`. State lives under `~/.atrium` (or `$WORKTREE_ROOT/hub` when
that is set): the hub's certificates and store in `hub/`, the room's key, certificate and `room.json` in `room/`, and
the room's database where `atrium daemon` always kept it, so a machine that ran the old single process keeps its cards.

## The hub

The hub serves the board. Its rule is one line, in `internal/link/proxy.go`: if the hub has the file, the hub serves
it, and everything else goes to a room. `index.html`, the CSS and the JS are files the hub has, so restarting the hub
changes the board. `/v1/tasks`, the event stream and the terminal websocket are not, so they go to the room that owns
the database and the terminals, which has been up for a week and is not being restarted.

The hub holds no work: no sessions, no terminals, no agent processes, and no authority over any of them. It does keep a
store of its own (`internal/hubstore`, `hub.db`). That store holds:

- which rooms exist, their names, their join secrets and which are marked for deletion,
- a cache of what each room last reported, so an offline room's cards still show,
- the shared tables every room reaches: the backlog (`atrium backlog`), reports (`atrium reports`), hub documents,
  change requests, pull request claims, the audit log and the push subscriptions,
- git: a bare mirror of each repository it serves, which rooms push finished work to (`docs/fabric/git-sync-design.md`,
  `docs/fabric/hub-forge-design.md`).

A store failure stops the hub. A hub that cannot remember which rooms exist would mint a second room under a name it
forgot, and the work is safe on the rooms anyway, so the halt costs little.

`atrium run` starts the hub, and when no room answers on this machine it starts `atrium room` in the background. The
first time, it also makes the room: it names it after the machine and enrols it over its own link, so one machine
needs no join string. `atrium run --no-room` serves the hub alone, which is how `scripts/live` runs it, with the room
started separately.

## A room

`atrium room` is the daemon (`internal/daemon`): the store, the terminals, the hooks, the permission chain. It runs for
days. It also serves its own board on loopback, `:7781`. That is the escape hatch: when the hub is down or broken, the
room is still a working atrium at its own address, and its agents never notice.

A room writes its agent address to the machine's one location file, which is how every hook, CLI call and the control
MCP find it without being told. A second room on the same machine must not take that file, or the first room's hooks
start arriving at the second. `--isolated` keeps the second room's address in a private file beside its `--dir`.

With the `pty_host` setting on, a room's terminals live in a separate pty host process, so a room restart reattaches to
running agents instead of ending them (`docs/terminal/ptyhost-protocol.md`, `internal/daemon/hostterm.go`). Without it,
a room's terminals end with the room.

## Joining a room to a hub

The hub names every room. `atrium rooms add <name>` writes the room down and prints a join string, `atr1_...`, bound
to that name, good once and for an hour. `atrium rooms token <name>` mints a fresh one and retires the old.

On the room's machine, `atrium room join <string>` takes it. The string carries the hub's link address, a fingerprint
of its certificate authority, and a one-time secret. Joining pins that authority, makes the room's key on the room so
the hub never sees it, and saves the certificate the hub signs. After that every link is mutual TLS, and the name in
the room's certificate is the room's identity. A room the hub has no record of cannot attach. From then on
`atrium room` alone starts it.

The room always dials the hub, never the reverse. A hub restart is a room noticing a closed socket and dialling again.

| Transport | `atrium room join` flag | When |
| --- | --- | --- |
| direct mTLS | `--mtls <host:port>`, or none | the room can reach the hub's link port. The default |
| a private zrok share | `--zrok-private <token>` | the machines share no network and use zrok |
| OpenZiti | `--openziti <identity>` | the machines are on an OpenZiti network |

A ziti or zrok room from before certificates were minted for overlays can still attach without one until
`atrium rooms legacy refuse` turns that off (decision 18, `docs/rnd/overlay-room-identity.md`).

## Managing rooms

All of these run on the hub's machine, because the board cannot mint a join string. Atrium has no login, and a board
that could enrol a machine would let anyone who reaches the page do it.

| Command | What it does |
| --- | --- |
| `atrium rooms add <name>` | write a room down and print its join string |
| `atrium rooms ls` | every room the hub knows, connected or not |
| `atrium rooms token <name>` | a fresh join string, the old one retired |
| `atrium rooms mark <name>` | mark a room for deletion: it takes no new work. `--undo` takes the mark off |
| `atrium rooms rm <name>` | remove a marked room, and refuse one that is not marked. `--force` drops only the hub's record, for a machine that is never coming back |
| `atrium rooms log [name]` | what happened to the rooms, newest first |
| `atrium rooms legacy allow\|refuse` | whether an overlay room with no certificate may attach |
| `atrium rooms git ...` | sync a room's clone, collect its branches, and choose which repositories the hub mirrors |
| `atrium backups`, `atrium backups restore` | the hub store's snapshots, every ten minutes, kept in tiers for a month |

## One board over every room

A request that names a room goes to that room. A request names it by a `room~id` card id in its path, the
`X-Atrium-Room` header, or the `atrium_room` query parameter on a websocket, which cannot set a header. A card id beats
the header, because a card lives on one room. A request that names none gets the merged view: lists, the event stream
and history merge across rooms, and each card's id comes back tagged with its room. With one room attached and no
other remembered, the board is that room and nothing is tagged.

When a room stops answering, its cards are drawn from its last report, dimmed, and nothing on it can be opened or
started until it returns.

## Sessions across rooms

The hub serves the control MCP at `/_hub/mcp`, on its board port and loopback only, since its tools drive sessions and
restart processes. Every session gets the `atrium_*` tools from it: `atrium_peers`, `atrium_say`, `atrium_launch`,
`atrium_report`, `atrium_task`, `atrium_backlog`, `atrium_reports`, `atrium_resources`, `atrium_git_url`,
`atrium_git_push` and the rest. `website/docs/control-mcp.md` lists them.

A bare name in `atrium_say` is a card on the caller's own room. `name@room` names a card on another room, and the hub
delivers it there with the sender shown as `you@yourroom`. A card tagged `atrium:everywhere` is reachable from every
room. `docs/runtime/agent-messaging.md` has the mechanics.

## Reaching the board from elsewhere

The board binds loopback and has no login. `atrium run --board-transport zrok|ziti` also serves it over an overlay:
`--board-share private` (the default) needs zrok on the other end, and `public` gives a URL and is refused without a
share login set. `docs/fabric/overlays.md` has the reasoning.

## Upgrades

A hub started with `--builds <dir>` offers rooms a binary per platform, named `atrium_<os>_<arch>`. A room started with
`--accept-upgrades` fetches, hashes and checks the build it is offered, then restarts on it. Without the flag, a room
keeps what it has. A hub can never make a room upgrade.

## The old single process

`atrium daemon` still runs: one process, the board on :7778 and agents on :7777, no hub, no rooms. It is the same
daemon code as a room with the board served in-process. The service installers still default to it
(`docs/release/packaging.md`). Mode A (`atrium hub`, `atrium agent`) and Mode B (`atrium serve`, `status`, `watch`) are
gone.

## Room spec and lock

What a room is lives as data. The hub keeps, for each room, a desired `room.yaml` (verbatim, with its sha256 and who set
it when) and the latest observed `room.lock` the room posted (verbatim, with when it arrived). They are separate tables,
so posting a lock never touches the spec. The hub checks only that `version` is 1, that the spec's `name` is the room's,
that the size is within bounds (64 KiB for a spec, 256 KiB for a lock) and that no key is named like a token, password,
secret or key.

- On the hub: `atrium rooms add <name> --spec <file>`, `atrium rooms spec get|set <name>`,
  `atrium rooms lock get <name>`.
- On the board, read only: `GET /_hub/rooms/<name>/spec` and `GET /_hub/rooms/<name>/lock`.
- On the room: `atrium room spec pull [--out file]`. Phase A's `atrium room setup` posts its lock with
  `cli.PostRoomLock(ctx, dir, lock)`, where `dir` is the room's key directory (empty for the default) and `lock` is the
  JSON bytes. Underneath are `link.FetchRoomSpec` and `link.PostRoomLock`, which take the room's `link.Dialer`.

The room link carries these on the `roomspec` connection kind: one HTTP request on a connection the room dialled, as the
room its certificate names. The path names no room, so a room can only read and write its own.
