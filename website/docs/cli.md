---
title: The CLI
description: Every atrium and atrium2 subcommand, and what it is for.
---

# The CLI

Atrium ships two binaries. `atrium` is the single-process daemon and the commands sessions use. `atrium2` runs the
[hub and rooms](./rooms.md).

## atrium: running it

| Command | Purpose |
| --- | --- |
| `atrium daemon` | The board, the API and the agent listener in one process. |
| `atrium stop` | Ask a running daemon to wind down. Not the same as killing it. |
| `atrium version` | The version this binary was built as. |
| `atrium preview` | A throwaway second board on a copy of your cards, for looking at a change before installing it. |
| `atrium name [<name>]` | Name this atrium once, so two machines cannot claim each other's cards. |

`atrium daemon` flags: `--addr` for the agent listener, `--http` for the board, `--db` for the database,
`--shutdown-token` to allow a remote shutdown that carries the token.

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

## atrium2: hub and rooms

| Command | Purpose |
| --- | --- |
| `atrium2 hub` | Serve the board and the room link. Holds no card state. |
| `atrium2 hub room add <name>` | Write a room down and print its one-time join string. |
| `atrium2 hub room token <name>` | Mint a fresh join string, retiring the old one. |
| `atrium2 hub room ls` | Every room the hub knows, connected or not. |
| `atrium2 hub room log [name]` | What happened to the hub's rooms, newest first. |
| `atrium2 hub room mark <name>` | Mark a room for deletion, or take the mark off. |
| `atrium2 hub room rm <name>` | Remove a room once it holds nothing. |
| `atrium2 hub backups` / `atrium2 hub restore <snapshot>` | List the hub's snapshots, or restore one. |
| `atrium2 join <join string>` | Join a hub as a room and start it. |
| `atrium2 room` | Start a room that has already joined. |
| `atrium2 db compact` | Write a compacted copy of a database, offline. |
| `atrium2 version` | The version this binary was built as. |

`atrium2 hub` flags include `--addr` for the board (loopback), `--link` for the room link, and `--link-advertise`
for the address written into join strings. `atrium2 join` and `atrium2 room` take `--isolated` to keep off the
machine's hooks, `--accept-upgrades` to install builds the hub offers, and `--db`.

## Environment

| Variable | Default | Meaning |
| --- | --- | --- |
| `ATRIUM_HUB_URL` | `http://localhost:7777` | Where hooks post. |
| `ATRIUM_PERM_GATE` | unset | `on` gates every session. `off` turns every atrium hook off. Unset asks atrium. |
| `ATRIUM_AGENT_NAME` | directory name | What a session calls itself. Set for runners atrium launches. |
| `ATRIUM_TASK_ID` | unset | Binds a launched runner to its card. Set for you. |
| `ATRIUM_PRIORITY` | above normal | `normal` turns off the raised process priority on Windows. |
| `WORKTREE_ROOT` | unset | A worktree tree root. The database lives under it. Unset means `~/.atrium`. |

## The older modes

Two modes from atrium's first version still build and still work, and share no state with the daemon.

- **`atrium hub` and `atrium agent`**: a terminal you type prompts into, and the MCP server a claude session calls
  `submit` on in a loop. This `hub` is not `atrium2 hub`.
- **`atrium serve`, `atrium status` and `atrium watch`**: read-only views over an external worktree ledger,
  inert unless `WORKTREE_ROOT` points at one.
