---
title: Rooms and the hub
description: One board over many machines or many accounts. The hub holds nothing, rooms run the agents.
---

# Rooms and the hub

Atrium runs as two processes, split along lifetimes. Board code changes often and agents must not die for it, so the
process that serves the board is not the process that holds the terminals. The same split lets one board show many
machines, or many accounts on one machine.

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

`atrium daemon`, the older single process with the board and the agents together and no hub, still runs. Nothing
on this page applies to it.

## Start the hub and this machine's room {#start-a-hub}

[Build atrium from source](./install.md#build-it), copy it somewhere it can stay, and run it on the machine you
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
atrium room join atr1_eyJhIjoiMTI3LjAuMC4xOjc3Nzki...
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

## Allowed folders

A room launches only in folders it was told about, and claude's folder trust is accepted for them ahead of time, so a
launch never sits at a trust dialog. Provisioning sets them:

```powershell
provision-room.ps1 -AllowedFolders /srv/work,C:/work
```

A new room defaults to its clone, its worktrees folder and `WORKTREE_ROOT`. Running provision again changes nothing
unless `-AllowedFolders` is given, and removing a room leaves the list alone. The provision smoke launches a card
outside the list and expects the room to refuse. `room-check.ps1` has an `allowed-folders` row that warns for a room
with none, and `-Fix -Yes` sets the clone and its worktrees.

## Forge access

A room can say which forge CLI it needs (`gh`, `bb` or `glab`), the host it must be logged in to and the command name.
Set it in the room settings under **forge logins**, or in `atrium.requirements.yaml` under `forges`:

```yaml
forges:
  gh: { host: github.com, scopes: [repo, read:org] }
```

Nothing is checked until something asks, and nothing polls. A preflight runs the CLI's own status command and never
reads a token. Each forge comes back as ok, not installed, logged out or missing scope, with the scope named. A failure
raises a forge-access alert on the board that says what to run on which room, for example "gh is not logged in on sg3:
run `gh auth login --hostname github.com` on sg3". Atrium stores only the host and a bare command name.

## Change requests

A room that has work another room, or you, should take can ask for it on the hub. The **Requests** view of the repos
area lists them, open and closed, with the source and target branch, the hub's own read of whether the branch was
pushed, the reason and the request's history. A request has a state of open, merged, closed or withdrawn, and it ends
once.

- You can open, close, withdraw and mark one merged. Marking merged is yours alone, and the hub checks the commit
  against its own store first.
- A card can open a request for a branch of its own room or one pushed to the hub, and withdraw or close its own. The
  owner of the source branch can close one against it, with a note.
- The owner is told, and the text of a request is quoted to them as data and not as instructions. A request into `main`
  is yours to decide, so it raises a question growler instead.
- Every board is told on every accepted change.

There is no merge button and no queue. Merging stays manual. The routes are under `/_hub/change-requests` and
`/_hub/git/pushed`.

## The deploy queue

The landed commits that still need a deploy are one generated list. `GET /_hub/deploy-queue` shows each commit not yet
live, oldest first, with its subject, its item, whether it needs a `hub` deploy, a `room` deploy or `both`, and the
rooms that are behind. Add `?format=md` for a table. Live is what each process reports as its running commit, and a
room built before this reported none is listed as exactly as far behind as the hub, so the list over-reports and never
hides a commit. Docs, tests and review commits are not listed. The deploy-ready dialog shows the queue under its report,
and `deploy-ready.ps1` and `deploy-batch.ps1` print it before they change anything. Reading it deploys nothing.
