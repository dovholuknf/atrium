---
title: Install
description: Install atrium from a package, a per-user archive, or source, and have it start at login.
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

# Install

Atrium is one Go binary. Each release carries packages for Linux, macOS and Windows, plus a plain archive for each
platform that installs without `sudo` or administrator rights.

:::info Atrium runs as you
On every platform atrium runs **as you, in your session**. It starts claude and other agents, and those need your
PATH, your shell configuration, your ssh agent and your Claude Code settings. A system service has none of those,
so every agent it started would be useless. This is why atrium uses a user unit, a LaunchAgent and a logon task,
never a system-wide service.
:::

## From a package

<Tabs groupId="os">
<TabItem value="linux" label="Linux">

```bash
sudo dpkg -i atrium_0.0.1_amd64.deb
# or
sudo dnf install ./atrium-0.0.1-1.x86_64.rpm

systemctl --user status atrium
```

The package installs a systemd **user** unit and enables it for the person who ran the install. It also turns on
lingering, so atrium keeps running after you log out.

- `ATRIUM_NO_ENABLE=1` installs the files and enables nothing.
- `ATRIUM_NO_LINGER=1` enables atrium but lets it stop at logout.

An upgrade keeps atrium enabled. Removing the package leaves lingering as it found it.

</TabItem>
<TabItem value="mac" label="macOS">

```bash
sudo installer -pkg atrium_v0.0.1_darwin_arm64.pkg -target /
```

The package installs a LaunchAgent, never a LaunchDaemon, so atrium starts when you log in to the desktop.

</TabItem>
<TabItem value="win" label="Windows">

```powershell
Start-Process msiexec "/i atrium_0.0.1_windows_amd64.msi /qn /norestart" -Wait
& "$env:ProgramFiles\atrium\scripts\atrium-service.ps1" install
& "$env:ProgramFiles\atrium\scripts\atrium-service.ps1" status
```

The MSI puts the binary in Program Files, adds it to the PATH, and ships the service scripts beside it. It does
not register autostart, because it runs elevated and may not run as the person who will use atrium. You run the
service script yourself, as yourself.

Atrium starts from a **logon task**. It runs as you, in your session, with no stored password and no elevation. It
restarts on failure and survives a reboot. It stops when you log out, because Windows has no equivalent of
lingering.

`atrium-service.ps1` takes `install`, `uninstall`, `start`, `stop`, `restart`, `status` and `selftest`. Its `stop`
calls `atrium stop` first, so your agents wind down instead of dying at once.

</TabItem>
</Tabs>

## Without sudo or admin

Unpack the archive, put the binary in your home, and run the service script. It registers autostart as you and
touches no system location.

<Tabs groupId="os">
<TabItem value="linux" label="Linux">

```bash
tar -xzf atrium_v0.0.1_linux_amd64.tar.gz
cp atrium_v0.0.1_linux_amd64/atrium ~/.local/bin/atrium
cd atrium_v0.0.1_linux_amd64
ATRIUM_EXE=$HOME/.local/bin/atrium scripts/atrium-service.sh install
```

</TabItem>
<TabItem value="mac" label="macOS">

```bash
tar -xzf atrium_v0.0.1_darwin_arm64.tar.gz
cp atrium_v0.0.1_darwin_arm64/atrium ~/.local/bin/atrium
cd atrium_v0.0.1_darwin_arm64
ATRIUM_EXE=$HOME/.local/bin/atrium scripts/atrium-service.sh install
```

The LaunchAgent loads at your next desktop login. Over a headless ssh session it cannot load yet, and the script
says so and exits cleanly.

</TabItem>
<TabItem value="win" label="Windows">

```powershell
Expand-Archive atrium_v0.0.1_windows_amd64.zip -DestinationPath $HOME\atrium
$dir = "$HOME\atrium\atrium_v0.0.1_windows_amd64"
& "$dir\scripts\atrium-service.ps1" install -Exe "$dir\atrium.exe"
```

The task runs at `RunLevel Limited`, a standard-user task. Nothing is written to Program Files or the system PATH.

</TabItem>
</Tabs>

## From source

```bash
go build -o build.claude/ ./...
./build.claude/atrium daemon
```

This builds `atrium`, the single-process daemon, and `atrium2`, which runs the [hub and rooms](./rooms.md).

:::warning Run the installed copy
Hooks, the logon task and the self-restart all name a path to the binary. A path inside your build folder is
rewritten by every build, so anything that points there goes stale the next time you build. Install the binary
somewhere it can stay, and run it from there.
:::

## Check it

```bash
atrium version
```

Then open `http://localhost:7778`. The next step is the [Quick start](./quick-start.md).

## One database

Pass `--db` if you ever start the daemon by hand from more than one shell. Without it, which database atrium opens
depends on `WORKTREE_ROOT` in the shell it started from, and opening a different one looks exactly like your board
losing everything. Atrium warns loudly when the database is not the one it opened last time.
