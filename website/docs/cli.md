---
title: The CLI
description: Every atrium subcommand, and what it is for.
---

# The CLI

Atrium is one binary. `atrium run` starts the [hub and this machine's room](./rooms.md), and the same binary holds
the commands sessions and hooks use.

## atrium: running it

| Command | Purpose |
| --- | --- |
| `atrium run` | The hub: the board on `127.0.0.1:7778` and the room link on `127.0.0.1:7779`. Starts this machine's room when none answers. |
| `atrium room` | A room: the database, the terminals and the agents, on `127.0.0.1:7777`, with its own board on `127.0.0.1:7781`. |
| `atrium stop` | Ask a running room or daemon to wind down. Not the same as killing it. The hub refuses it, so name a room by its board: `--url http://127.0.0.1:7781`. |
| `atrium version` | The version this binary was built as, its commit, and the board it carries. `--short` for a script. |
| `atrium preview` | A throwaway second board on a copy of your cards, for looking at a change before installing it. |
| `atrium name [<name>]` | Name this atrium once, so two machines cannot claim each other's cards. |
| `atrium daemon` | The older single process: board on `:7778`, agents on `:7777`, no hub. Still runs. |

[Rooms and the hub](./rooms.md) covers `atrium run` and `atrium room`. `atrium daemon` takes `--addr` for the agent
listener, `--http` for the board, `--db` for the database, and `--shutdown-token` to allow a remote shutdown that
carries the token.

`atrium preview --from live` copies the cards the running daemon has. A preview takes no hooks and starts
passive: it does not start fixtures or open shares, so it is a place to look, never a second place to work.

## atrium: sessions and hooks

| Command | Purpose |
| --- | --- |
| `atrium join` / `atrium leave` | Put the session you are in on the board and gate it, or take it off, with no restart. |
| `atrium launch` | Put a directory on the board and start a runner in it. For scripts that make worktrees. |
| `atrium open <url>` | Fill the launch dialog from an issue or pull request URL. |
| `atrium hook install` / `atrium hook status` | Write atrium's hooks into your Claude Code settings, or say which are there. |
| `atrium hook`, `atrium session`, `atrium turn` | The hook entry points. Wired for you. |
| `atrium replay [file]` | Render a captured terminal stream the way an attach would. |
| `atrium task <who>` | One card, named by handle, alias, `name@room` or id. |
| `atrium exit <who>` | Ask a card's runner to leave, as `atrium_exit` does. `--force` for a card that is not yours. |
| `atrium new-context <who>` | Cycle a card onto a fresh context, as the board's new-context does. |
| `atrium archive-workers` | Archive done worker cards that piled up. Never a director. `--dry-run` lists them. |
| `atrium merged` | Tell the room a merge happened, so the covered workers are marked finished. Run by git's post-merge hook. |
| `atrium control` | A stdio MCP server for sessions that do not reach the hub: `atrium_status`, `restart_atrium`, the peer tools (`atrium_peers`, `atrium_say`, `atrium_report`, `atrium_launch`, `atrium_task`, `atrium_exit`), `atrium_git_clone`, `atrium_git_push`, `atrium_git_url` and `atrium_open`. |

`atrium launch` takes `--cwd`, `--title`, `--tags`, `--prompt`, `--model`, and where the work came from: `--source`,
`--external` and `--item-url`.

## atrium: an agent talking to atrium

| Command | Purpose |
| --- | --- |
| `atrium finish [recap]` | The work is over, and here is what was done. `--hand-back` means over to you, not over. |
| `atrium ask [question]` | Stuck, and what would unstick it. `--continue` means still working. `--peer` asks a session. |
| `atrium answer <handle> <answer>` | Reply to a peer's question, which also takes it off that card. |
| `atrium peers` | The other sessions this one can address. |
| `atrium tell <handle> <message>` | Say something to another session. Queued, never typed into a busy line. |
| `atrium ready` | The handoff is written, so atrium can clear this session's context. Asked for by the limit prompt. |

## atrium: hub and rooms

| Command | Purpose |
| --- | --- |
| `atrium run` | Serve the board and the room link, and start this machine's room when none is running. |
| `atrium run --no-room` | Serve the board and the room link alone. Holds no card state. |
| `atrium rooms add <name>` | Write a room down and print its one-time join string. |
| `atrium rooms token <name>` | Mint a fresh join string, retiring the old one. |
| `atrium rooms ls` | Every room the hub knows, connected or not. |
| `atrium rooms log [name]` | What happened to the hub's rooms, newest first. |
| `atrium rooms mark <name>` | Mark a room for deletion, or take the mark off. |
| `atrium rooms rm <name>` | Remove a room once it holds nothing. |
| `atrium backups` / `atrium backups restore <snapshot>` | List the hub's snapshots, or restore one. |
| `atrium room join <join string>` | Join a hub as a room and start it. |
| `atrium room` | Start a room that has already joined. |
| `atrium db compact` | Write a compacted copy of a database, offline. |
| `atrium ledger` | The work ledger, read straight from a room's database. |
| `atrium dispatch to <room>` / `list` / `cancel` | Queue a launch for a room, see what is queued, or withdraw an item no room has taken. |
| `atrium usage backfill` | Write usage rows for turns the room did not record, from transcripts. |
| `atrium requirements` | Parse an `atrium.requirements.yaml` and print it normalized, or refuse it with the key and line. |
| `atrium rooms git` | Move code between the hub and its rooms: sync, collect, and which repositories. |
| `atrium rooms legacy` | Whether a ziti or zrok room without a certificate may still attach. |
| `atrium room get` / `set` | Read or set a room setting, such as `git_root`, in a stopped room's database. |
| `atrium backlog` | The hub's backlog: `list`, `show`, `file`, `status`. |
| `atrium reports` | Reports directors leave for each other on the hub: `list`, `read`, `add`. |
| `atrium resources init` | Write a starter `resources.md`, the inventory of machines agents may use. |

`atrium run` flags include `--addr` for the board (loopback, default `127.0.0.1:7778`), `--link` for the room link
(default `127.0.0.1:7779`), and `--link-advertise` for the address written into join strings. The hub's own
directory, store and ziti identity are `--atrium-dir`, `--atrium-db`, `--atrium-identity` and `--atrium-service`,
because `--dir`, `--db`, `--http` and `--agent` on `atrium run` are the room's, the same as `atrium room` takes them.
`atrium room join` and `atrium room` take `--isolated` to keep off the machine's hooks, `--accept-upgrades` to
install builds the hub offers, and `--db`.

The first `atrium run` on a machine with no room makes one: it names it after the machine, mints it a join string,
and enrols it over its own link, so one machine needs no join string at all. Stopping `atrium run` never stops the
room.


## Environment

| Variable | Default | Meaning |
| --- | --- | --- |
| `ATRIUM_HUB_URL` | `http://localhost:7777` | Where hooks post. |
| `ATRIUM_PERM_GATE` | unset | `on` gates every session. `off` turns every atrium hook off. Unset asks atrium. |
| `ATRIUM_AGENT_NAME` | directory name | What a session calls itself. Set for runners atrium launches. |
| `ATRIUM_TASK_ID` | unset | Binds a launched runner to its card. Set for you. |
| `ATRIUM_PRIORITY` | above normal | `normal` turns off the raised process priority on Windows. |
| `WORKTREE_ROOT` | unset | A worktree tree root. The database lives under it. Unset means `~/.atrium`. |
