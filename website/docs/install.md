---
title: Install
description: Build atrium from source, start it, and have this machine's room start at login.
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

# Install

Atrium is one binary, `atrium`. There is no published release yet, so you build it from source. It takes a minute.

:::tip Run the agents as their own user
Atrium recommends that the agents never run as you. Make a standard account called `localai` for the room and its
agents, and drive them from your own account through the board. [Run the agents as their own user](./accounts.md)
says why and how. Running everything as yourself works, and it is not the recommendation.
:::

Whichever account runs the room, it runs as that user, in that user's session, never as a system service. The
agents it starts need that account's PATH, shell configuration and runner settings, and a system service has none
of them. This is why atrium starts from a user unit, a LaunchAgent or a logon task.

## Build it

You need [Go](https://go.dev/dl/) at the version `go.mod` names, and git.

```bash
git clone https://github.com/dovholuknf/atrium
cd atrium
go build -o build.claude/ ./cmd/atrium
```

Copy `build.claude/atrium` (`atrium.exe` on Windows) somewhere it can stay, such as `~/.local/bin`, and put that
folder on your PATH.

:::warning Run the installed copy
Hooks, the logon task and the self-restart all name a path to the binary. A path inside your build folder is
rewritten by every build, so anything that points there goes stale the next time you build. Install the binary
somewhere it can stay, and run it from there.
:::

## Start it

```bash
atrium run
```

That starts the hub, which serves the board on `http://localhost:7778`, and a room under your account, which runs
the agents as you. The first time, it makes the room and names it after the machine. [Rooms and the
hub](./rooms.md) explains the two, and how to add another machine.

For the recommended setup, start the hub alone with `atrium run --no-room` and run the room as `localai`.
[Set it up on one machine](./accounts.md#set-it-up-on-one-machine) has the steps.

`atrium daemon` still starts the older single process, with the board and the agents together and no hub.

## Start at login

The service scripts in `scripts/` register this machine's **room** to start when its account logs in. Run them as
the account that runs the room, `localai` in the recommended setup, never elevated. Pass the installed path to the
binary.

<Tabs groupId="os">
<TabItem value="linux" label="Linux">

```bash
ATRIUM_SERVICE_VERB=room ATRIUM_EXE=$HOME/.local/bin/atrium scripts/atrium-service.sh install
systemctl --user status atrium
```

This writes a systemd **user** unit. `ATRIUM_LINGER=1` keeps it running after the account logs out, so a
`localai` room needs nobody logged in as `localai`.

</TabItem>
<TabItem value="mac" label="macOS">

```bash
ATRIUM_SERVICE_VERB=room ATRIUM_EXE=$HOME/.local/bin/atrium scripts/atrium-service.sh install
```

This writes a LaunchAgent, never a LaunchDaemon, so the room starts when its account logs in to the desktop. Over a
headless ssh session it cannot load yet, and the script says so and exits cleanly.

</TabItem>
<TabItem value="win" label="Windows">

```powershell
.\scripts\atrium-service.ps1 install -Verb room -Exe "$HOME\.local\bin\atrium.exe"
.\scripts\atrium-service.ps1 status
```

Atrium starts from a **logon task**. It runs as the account that installed it, in that account's session, with no
stored password and no elevation. It restarts on failure and survives a reboot. It stops when that account logs
out, because Windows has no equivalent of lingering. A Windows service would run in session 0, where it cannot
open a terminal you can attach to.

</TabItem>
</Tabs>

The scripts take `install`, `uninstall`, `start`, `stop`, `restart` and `status`, and the Windows one `selftest`
as well. Their `stop` calls
`atrium stop` first, so your agents wind down instead of dying at once. Without the room verb they register
`atrium daemon`, the single process.

The scripts start the room, not the hub. Start the hub as yourself when you want the board, with
`atrium run --no-room` when the room runs as `localai`. A plain `atrium run` finds a room of yours already running
and leaves it alone. The hub holds no work, so starting and stopping it costs your agents nothing.

## Packages

The repository can build a deb, an rpm, a macOS pkg, a Windows MSI and a plain archive per platform, and a release
will carry them. None is published yet. `docs/release/packaging.md` covers what each one installs.

## Check it

```bash
atrium version
```

Then open `http://localhost:7778`. The next step is the [Quick start](./quick-start.md).

## One database

Pass `--db` if you ever start a room by hand from more than one shell. Without it, which database atrium opens
depends on `WORKTREE_ROOT` in the shell it started from, and opening a different one looks exactly like your board
losing everything. Atrium warns loudly when the database is not the one it opened last time.
