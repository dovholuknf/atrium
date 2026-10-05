---
title: Rooms and the hub
description: One board over many machines or many accounts. The hub holds nothing, rooms run the agents.
---

# Rooms and the hub

`atrium daemon` is one process: the board, the database and the agents together. `atrium run` splits that along
lifetimes, so one board can show many machines, or many accounts on one machine.

- **The hub** (`atrium run`) serves the board and passes every other request to the right room. It holds no card
  state, so it restarts freely, and restarting it costs no session anything.
- **A room** (`atrium room`) owns the database, the pseudo terminals and the agents. It runs for days.

```text
  browser                  HUB  atrium run                  ROOM  atrium room
  ┌──────────┐  :7778   ┌───────────────────┐  dials out  ┌───────────────────────┐
  │ the board│ ───────► │ serves the board  │ ◄────────── │ sqlite store          │
  └──────────┘          │ passes /v1/* on   │  :7779 mTLS │ ptys and agents       │
                        │ holds no cards    │             │ :7777 agent listener  │
                        └───────────────────┘             └───────────────────────┘
```

**The room dials the hub, never the reverse.** A hub restart is a room noticing a closed socket and dialing again.
A room also serves its own board on loopback, `:7781` by default, so it stays usable while the hub is down.

## Start the hub and this machine's room {#start-a-hub}

[Build atrium from source](./install.md#from-source), copy it somewhere it can stay, and run it on the machine you
will open the board from:

```bash
atrium run
```

It prints where the board is, `http://localhost:7778` by default, and where rooms dial in, `127.0.0.1:7779`. When
this machine has no room answering, it starts `atrium room` in the background. The first time, it also makes the
room: it names it after the machine and enrols it over its own link, so one machine needs no join string.

The two are separate processes on purpose. Stopping or restarting `atrium run` never stops the room or its agents.
`atrium run --no-room` serves the hub alone, for a machine whose room is started some other way.

## Adding a room on another machine

The hub names every room. Adding one writes it down and prints a join string for that name:

```bash
atrium rooms add laptop       # prints a join string, good once and for an hour
```

If the string goes missing, `atrium rooms token laptop` mints another and retires the old one.

On the machine that runs the agents, paste it:

```bash
atrium room join atr1_eyJhIjoiMTI3LjAuMC4xOjc4MDEi...
```

The join string carries the hub's address, a fingerprint of its certificate authority, and a one-time secret.
Joining pins that authority, makes the room's key on the room so the hub never sees it, and saves the signed
certificate. From then on every link is mutual TLS, and **the name in the room's certificate is the room's
identity**. A room the hub has no record of cannot attach. After the first time, `atrium room` starts it from what
was saved.

Then open `http://localhost:7778`.

## Three ways to join

`atrium room join` takes a transport:

| Transport | When |
| --- | --- |
| direct mTLS | The room can reach the hub's link port. The default. |
| a private zrok share | The two machines share no network, and you use zrok. |
| OpenZiti | The two machines are on an OpenZiti network. |

The board binds loopback only (`--addr`). The room link (`--link`) may bind wide, and `--link-advertise` names the
address that goes into join strings.

## One board, every room

Lists, the event stream and history merge across rooms. The **room picker** scopes the board to one room, with a
state dot per room, and lists disconnected rooms dimmed. Each room has its own settings cog: editor, paste
directory, browse roots, shell, scrollback and the like.

The **rooms** tab lists every room the hub knows, grouped as here now, not answering, and never connected, each
with a transport badge. The board cannot mint a join string. Only `atrium rooms add` can.

A room tells the hub what it holds whenever that changes. When a room stops answering, its cards are drawn from
that last report, and nothing on it can be opened or started until it returns.

## Agents under their own account

Running an agent as its own operating system user bounds what it can reach. The cost used to be that it
disappeared: its terminal, its `~/.claude` and its transcripts are its own, and nothing on your desktop says it is
there.

A room per account answers that. Each account runs its own room, with its own `~/.atrium` and database, dialing one
hub. Every session on every account shows on one board, and a permission request from another account is answered
in your own **perms** tab. Atrium launches runners under the room's own account and never under another's, so the
isolation is the account boundary itself.

A second room on one machine should keep off that machine's hooks, or it takes them from the first:

```bash
atrium room join <join string> --isolated
```

## Removing a room

Deleting a room is deliberate, in steps: **mark** it so it starts no new work, let the room itself confirm it holds
nothing, stop it, then remove it.

```bash
atrium rooms mark laptop
atrium rooms rm laptop
```

`--force` removes only the hub's record. A stale room can also be forgotten from the picker.

## The hub's own store

The hub snapshots its store every ten minutes and keeps snapshots in tiers over a month.

```bash
atrium backups
atrium backups restore <snapshot>
```

A restore moves the current store aside rather than deleting it.

## Upgrades

A hub offers a build per platform, named `atrium_<os>_<arch>` in its `--builds` directory. A room started with
`--accept-upgrades` installs the build it is offered. Without it, the room keeps what it has.

## Launching into a room

The launch dialog asks which room, and offers only the runners that room has. Each room holds its own path to each
runner's binary, so a runner installed off one machine's PATH still works there.

A pull request under review is placed on the least busy online room, and the hub keeps one claim per pull request
across rooms, so two rooms never review the same one. A claim whose room is offline is not re-placed: the board warns
instead, so you choose.
