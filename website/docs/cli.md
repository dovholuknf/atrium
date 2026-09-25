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
| `atrium daemon` | The board, the API and the agent listener in one process. |
| `atrium stop` | Ask a running daemon to wind down. Not the same as killing it. |
| `atrium version` | The version this binary was built as, its commit, and the board it carries. `--short` for a script. |
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

`atrium run` flags include `--addr` for the board (loopback, default `127.0.0.1:7778`), `--link` for the room link
(default `127.0.0.1:7779`), and `--link-advertise` for the address written into join strings. The hub's own
directory, store and ziti identity are `--atrium-dir`, `--atrium-db`, `--atrium-identity` and `--atrium-service`,
because `--dir`, `--db`, `--http` and `--agent` on `atrium run` are the room's, the same as `atrium room` takes them.
`atrium room join` and `atrium room` take `--isolated` to keep off the machine's hooks, `--accept-upgrades` to
install builds the hub offers, and `--db`.

The first `atrium run` on a machine with no room makes one: it names it after the machine, mints it a join string,
and enrols it over its own link, so one machine needs no join string at all. Stopping `atrium run` never stops the
room.

`atrium2` is a temporary shim for one machine's deploy scripts, with the old names, and goes at the cutover.

## Environment

| Variable | Default | Meaning |
| --- | --- | --- |
| `ATRIUM_HUB_URL` | `http://localhost:7777` | Where hooks post. |
| `ATRIUM_PERM_GATE` | unset | `on` gates every session. `off` turns every atrium hook off. Unset asks atrium. |
| `ATRIUM_AGENT_NAME` | directory name | What a session calls itself. Set for runners atrium launches. |
| `ATRIUM_TASK_ID` | unset | Binds a launched runner to its card. Set for you. |
| `ATRIUM_PRIORITY` | above normal | `normal` turns off the raised process priority on Windows. |
| `WORKTREE_ROOT` | unset | A worktree tree root. The database lives under it. Unset means `~/.atrium`. |
