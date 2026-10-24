# Install, update and run atrium as services, for a human on a fresh machine

Status: proposed, not built. A design for review, revised 2026-10-23 with clint's answers to the first draft, and
again the same day with his answers to the second (backlog item `o-install-round3`).

Origin: backlog item `o-install-update-lifecycle` (ops, high), filed 2026-10-23 by @orchestrator for clint. It folds in
`r-rooms-as-services`, `f-install-wires-atrium-control` and `bug-install-live-scripts-skips-room-ssh-json`, and it
builds on `docs/rnd/room-autostart-design.md` (shelved 2026-09-29), `docs/release/packaging.md`,
`docs/rnd/rolling-restart-design.md` (the pty host) and `docs/rnd/room-deploy-hold-design.md`.

## 0. The answer, in one paragraph

Atrium has three long-running parts. The **hub** (`atrium run`) serves the board and links rooms together. A
**room** (`atrium room`) is the one per machine that runs agents, holds their cards and does their git work. The
**pty host** (`atrium ptyhost`) holds the terminals the agents type into, so a room can restart without ending them.
This design makes each one an operating system **service**: a program the OS itself starts, stops and restarts,
registered once with the OS's **service manager** (systemd on Linux, the Service Control Manager on Windows, launchd
on macOS). The three services are `atrium`, `atrium-room` and `atrium-pty`. They start **on demand**, when a person
or `atrium start` asks, not at boot unless someone opts in. The operating system's package (deb, rpm, MSI, pkg)
registers them, never the binary itself, and the binary sits where each OS expects programs to sit. There is no
`atrium install`. A first-time user installs the package, joins a room to a hub with the join string atrium already
uses, and starts the services with the OS's own command or `atrium start`, which only asks the service manager.
One room per machine holds because the service manager is the only thing that starts a room, and a service manager
runs at most one copy of a service. Rollback and updates offered by the hub are explained in sections 6 and 7.

**Round 3.** clint settled rollback, board deploys, starting without elevation, `hub.json` and signing (section 2).
He reopened who runs what. Section 11 lays that out and recommends that only the hub be a service, under a
non-privileged account, while each room and its pty hosts run as the room's own account from a per-user
registration and a binary that account owns. That is what lets a room take an update from the board (section 7)
and roll its pty hosts without ever restarting one (section 14). Section 12 explains sg4-control, section 13 shows
the Windows service handler, and section 15 lists what clint still has to decide.

## 1. What exists today

### 1.1 In the binary

| Command | What it does now | Gap for a human |
| --- | --- | --- |
| `atrium run` | the hub, and starts this machine's room detached when none answers (`ensureRoom`) | the room it starts dies with the session that ran `atrium run` |
| `atrium room` | the room, from what `room join` saved under `<state dir>/room` | needs someone or something to start it |
| `atrium room --detach` | starts the room outside the calling shell | survives the ssh session, not a log-off or reboot |
| `atrium room join <string>` | makes a key, gets a certificate, saves `room.json`, runs (`--no-run` to stop there) | nothing |
| `atrium rooms add <name>` | on the hub machine, records the room and prints a join string, good once for an hour | nothing |
| `atrium rooms token <name>` | mints a fresh join string, retiring the old one | nothing |
| `atrium stop` | asks a room to wind down (`--now`, `--runners`), never a kill | does not know about any supervisor |
| `atrium version` | version, commit, tree state, board hash, platform, stamped by the linker | nothing |
| `atrium hook install` | writes the runner's hook config against an installed binary | a person must know to run it |
| `atrium room setup --spec` | applies a room.yaml (work root, caches, agent pack) | nothing |
| `--accept-upgrades` and the hub's builds directory | the hub offers a binary per platform, the room fetches, checks it, renames it in and stops (section 7) | "a room started by hand stays down", and there is no rollback |
| hub-asked room restart (`roomrestart.go`) | the room parks its agents, spawns a detached restarter, winds down | fine for a restart, not an update |
| `atrium backups` | the hub snapshots its own store every ten minutes, `backups restore` puts one back | the room's store has no such snapshot |

The **state dir** is the folder a hub or room keeps everything in, `~/.atrium` unless `WORKTREE_ROOT` names another
(`internal/cardroot`). A **join string** is the one-time token `atrium rooms add` prints, which a room trades for a
certificate the hub will accept. A **supervisor** is whatever restarts a process that stops: today a scheduled task,
a systemd unit or a launchd job, and in this design always the service manager.

### 1.2 In the repo, not in the binary

| File | What it does | State |
| --- | --- | --- |
| `scripts/atrium-service.ps1`, `scripts/atrium-autostart.ps1` | Windows logon task with install, start, stop, restart, status | proven on claudevm, stops at log-off |
| `scripts/atrium-service.sh`, `packaging/atrium.service`, `packaging/atrium.plist` | systemd user unit (linger optional), macOS LaunchAgent | proven on WSL and m1mini, the LaunchAgent cannot load over headless ssh |
| `packaging/` and `scripts/package-*` | deb, rpm, pkg, MSI, scoop manifest, with a postinstall that enables the unit | built, none published |
| `scripts/provision-room.ps1` | a remote machine becomes a room over ssh: binary, toolchain, git, account checks, work root, join, autostart, folders, mcp, auth, smoke | in use, 1400 lines, Windows hub only |
| `website/docs/install.md` | build from source, copy the binary, run the service script | accurate, and long for what it achieves |

A **logon task** is a Windows scheduled task that starts when a named person logs in and ends when they log off. A
**user unit** is a systemd service that belongs to one person rather than to the machine, and **linger** is the
systemd setting that keeps a person's user units running when they are not logged in. A **LaunchAgent** is the macOS
equivalent of a user unit, which runs only inside a desktop login.

### 1.3 The private scripts, which are the real lifecycle

About 2000 lines of PowerShell under `C:\Users\claude\.atrium2\scripts` on sg4. None of it is reachable by a person
who downloaded the binary.

| Script | What it does that nothing else does |
| --- | --- |
| `build-deploy.ps1` | builds only from a clean `claude/main`, stamps commit and tree |
| `live-common.ps1` | `Save-Revert` (keeps the old binary, named by commit and board hash), `Install-Atrium` (stage, rename the live one aside, rename the new one in, put the old back on failure, keep three), `Start-Jobless` (start a process outside the Windows job object of the shell that started it, so closing that shell does not end it), `Wait-RoomAttached` (this room is connected to the hub on a connection made after the swap, and still is ten seconds later), `Test-RoomServes` (the room answers `/v1/settings` from its store, twice) |
| `deploy-batch.ps1` | the full local sequence: preflight, deploy hold, snapshot, stop room, stop hub, swap, start hub, start room, check, revert when not serving |
| `atrium-deploy.ps1` | cross-builds remote rooms, copies by scp, stops, swaps keeping `atrium.prev`, starts detached, asks the hub whether the room came back |
| `start-atrium-*.ps1`, `stop-atrium.ps1` | start the hub and room with their exact arguments after a reboot, stop them before one |
| `refuse-supervised.ps1` | refuses to stop a room from a shell that room supervises |
| `install-live-scripts.ps1` | copies the repo's `scripts/live` over the live ones |

A **job object** is a Windows grouping of processes that can be ended together. Every process a scheduled task or a
terminal starts is in one, which is why a process meant to outlive its starter has to leave it.

The hub's arguments (`--no-room`, `--link 0.0.0.0:7779`, `--link-advertise`, the ziti identity, `--board`,
`--board-views`) and the room's (`--dir`, `--db`, `--http`, `--agent`) exist only as variables in `live-common.ps1`.
A reboot of sg4 brings nothing back until someone runs `start-atrium.ps1`.

`bug-install-live-scripts-skips-room-ssh-json` is already fixed in the live copy: `install-live-scripts.ps1` now copies
every file but `.md`, and its header names `room-ssh.json`. The test the item asks for is still owed until section 10
retires the script.

### 1.4 What goes wrong for a person today

1. There is no install. The binary lands wherever it was unpacked, and hooks, tasks and swaps name that path.
2. Claude sessions on a fresh install get no atrium-control, the MCP server through which an agent talks to atrium
   (`f-install-wires-atrium-control`). Only provision step 11 writes `~/.atrium/mcp.json` and adds `--mcp-config` to
   the claude row.
3. Nothing starts the hub when the machine starts. The room's registration exists but stops at log-off on Windows and
   macOS, and the sg3 room died that way on 10/21.
4. A second room was started by hand on sgg while one was already up (10/21). Only the store's lock stopped it.
5. Update and rollback exist only in private scripts, with paths for one machine written into them.
6. `packaging.md` names self-update against a package manager as unsolved.

## 2. What is decided

clint's answers to the first draft, and the rules that follow from them.

| | Decision | Consequence here |
| --- | --- | --- |
| 1 | The binary does not register its own services. `packaging.md`'s "registration belongs in packaging" stands | packages and the release's service files register. `atrium install` is dropped (section 5) |
| 4 | The binary goes where each OS expects it | section 4 gives the paths. The binary no longer lives in `~/.atrium/bin` |
| 5 | No `--allow-second-room` and no "room already running" check | one room per machine holds by design (section 3.5) |
| 8 | GitHub Releases with `checksums.txt`. Evaluate SignPath for signing | section 10.1 |
| 9 | A package-managed install never replaces its own binary. `atrium update` prints the manager's command | section 10.2 |
| A | Start on demand, not at boot by default. Three services | section 3 |
| C | macOS keeps FileVault on. Planned reboots use `fdesetup authrestart`. An unplanned reboot waits for a human | section 9 |
| D | No `atrium install` | section 5 |

clint's answers to the second draft (`o-install-round3`). The numbers are the second draft's section 11.

| | Decision | Consequence here |
| --- | --- | --- |
| R2-1 | Rollback as recommended: the binary rolls back by itself when the gate fails, the store is never restored by itself, and a mark on a non-additive migration makes a rollback across it refuse | section 6 |
| R2-2 | Under the board's rooms chip a room shows "an update is available", and the user deploys it from there | section 7. It needs a binary the room's account can write, which feeds section 11 |
| R2-5 | Start and stop without elevation on Linux and Windows, by a grant the package makes. sudo stays, documented, on macOS | section 3.3 |
| R2-6 | Hub settings live in `hub/hub.json`, written by `atrium hub config` | section 3.1 |
| R2-9 | SignPath (signpath.org) signs the Windows binary and MSI. cosign signs `checksums.txt` | section 10.1 |

Reopened or still open from the second draft: 3 (who runs as whom, section 11), 4 (ConPTY under S4U, on hold behind
3, section 11.4), 7 (sg4-control, section 12), 8 (the Windows service handler, section 13) and 10 (never restart
the pty host on update, section 14).

Rules carried from the first draft, unchanged:

1. **As a user, never as SYSTEM or root.** Runners (claude, codex) need the account's PATH, home, sign-in and ssh
   agent. Every service runs as the account that owns its role, even when root or an administrator registered it.
   Section 11 goes further and asks which of the three needs to be a machine service at all.
2. **Stop is a wind-down, never a kill.** Every stop below asks the process to finish its work and exit, and a
   service manager's stop is configured to wait long enough for that.
3. **Elevation is a command a human runs.** The binary never asks for administrator rights and never sets
   `LocalAccountTokenFilterPolicy` (the Windows registry switch that lets a local administrator be one over ssh).

## 3. The three services

### 3.1 What each one is

| Service | Runs | Holds | Needs | Runs as |
| --- | --- | --- | --- | --- |
| `atrium` | `atrium run --no-room` | the hub store, the board, the link listener rooms dial | nothing | the hub's owner |
| `atrium-room` | `atrium room` | the room store, agents, cards, git work | a pty host on this machine, and a hub on any machine | the room's account |
| `atrium-pty` | `atrium ptyhost` | every terminal and the runner under it | nothing | the room's account, always the same as the room |

The service command lines carry no per-machine flags, because a package writes them and a package cannot know them.
Each role reads its settings from its state dir. The room already does, from `room/room.json`. The hub gains the
same: `<state dir>/hub/hub.json` holds what `live-common.ps1` passes as flags today (link address, advertise
address, transport identity, board options), `atrium run` reads it for any flag not given, and `atrium hub config
--link 0.0.0.0:7779 --link-advertise sg4.lan:7779` writes it. A flag on the command line still wins, for development.

The pty host, run as a service, does not exit when idle. Today it ends itself after ten minutes with no terminal and
no room connected (`DefaultIdleExit`), which is right for a host the room started and wrong for one the service
manager owns, where an idle exit would look like a crash or a stop. A flag turns it off for the service.

Section 14 revises the rest of this subsection: with updates that never restart a pty host, the pty host stops being
one registered service and becomes a series of generations the room starts. What follows is the second draft's model.

A room run by a service never starts a pty host itself. Today the room starts one when none answers, and under a
service manager that host would land inside the room's own group of processes (a systemd cgroup, a Windows job, a
launchd job) and die with the room, which defeats the point of a pty host. Instead the room waits for `atrium-pty`,
and when it is not there the card says `pty host not running. start it with: atrium start pty`.

### 3.2 How they depend on each other

- **The room wants the pty host, and only at start.** Starting the room starts the pty host first if it is not
  running. Stopping or restarting the room never touches the pty host. That one-way link is the whole reason the pty
  host is its own service: the terminals and the runners in them survive a room restart and a room update.
- **The room does not depend on the hub through the service manager**, even on the same machine. A room already
  dials its hub and redials with backoff when the hub is down or restarting, and a hub on another machine is out of
  the local service manager's reach anyway. So the hub can restart under a room, and the room reconnects.
- **The hub depends on nothing** and holds no terminal.
- **Stopping the pty host ends every terminal** on the machine. The room stays up, marks those cards as without a
  terminal, and offers to start them again once the host is back.

How each service manager says this:

| | Linux (systemd) | Windows | macOS (launchd) |
| --- | --- | --- | --- |
| room starts pty first | `Wants=atrium-pty.service` and `After=atrium-pty.service` in `atrium-room.service` | `atrium start` starts the pty task first (section 8 explains why the pty host is a task, not a service) | `atrium start` starts the pty job first. launchd has no dependencies between jobs |
| room stop leaves pty alone | `Wants` is not `Requires` or `BindsTo`, so a stop does not travel | separate services and tasks, nothing travels | separate jobs, nothing travels |
| room without pty | starts, and waits for the host | starts, and waits for the host | starts, and waits for the host |

`Wants`, `Requires` and `BindsTo` are systemd's three strengths of dependency. `Wants` starts the other unit and
nothing more. `Requires` also stops this unit when the other stops. `BindsTo` also stops it when the other dies.

### 3.3 On demand, and at boot for those who want it

By default nothing starts when the machine starts. A service runs from when someone starts it until it is stopped,
it crashes and is restarted, or the machine shuts down. A machine whose job is to be a room (sgg, sg3, m1mini) opts
into starting at boot, per service.

| | Linux | Windows | macOS |
| --- | --- | --- | --- |
| registered as | system units `atrium.service`, `atrium-room.service`, `atrium-pty.service` with `User=` set to the account | services `atrium` and `atrium-room` in the Service Control Manager, logged on as the account. `atrium-pty` is a scheduled task (section 8) | LaunchDaemons with labels `io.github.dovholuknf.atrium`, `.room`, `.pty`, each with `UserName` set to the account |
| on demand (default) | installed, not enabled | start type Manual | the plists live in `/Library/Application Support/atrium/launchd/`, outside the folder launchd reads at boot |
| at boot (opt in) | `sudo systemctl enable atrium-room` | `sc.exe config atrium-room start= delayed-auto` | `sudo cp` the plist into `/Library/LaunchDaemons/` |
| start | `sudo systemctl start atrium-room` | `net start atrium-room`, elevated | `sudo launchctl bootstrap system <plist path>` |
| stop | `sudo systemctl stop atrium-room`, or `atrium stop` | `net stop atrium-room`, or `atrium stop` | `atrium stop`, then `sudo launchctl bootout system/io.github.dovholuknf.atrium.room` |
| restart after a crash | `Restart=on-failure` | recovery actions set to restart, and `sc.exe failureflag atrium-room 1` so a non-zero exit counts as a failure | `KeepAlive` with `SuccessfulExit` false |
| logs | journald, `journalctl -u atrium-room` | `~\.atrium\logs`, and the Windows event log for starts and stops | `~/.atrium/logs` |

A **system unit** is a systemd service that belongs to the machine and starts as whatever account its `User=` line
names, so it needs no linger. The **Service Control Manager** (SCM) is the Windows component that runs services,
which is what `net start` and the Services console talk to. A **LaunchDaemon** is a launchd job in the machine's
domain that can run as a named account without anyone logged in. `delayed-auto` starts a Windows service shortly
after boot instead of during it.

**Without elevation (decided, R2-5).** The package grants the owning account start, stop and query on its own
services and nothing else. On Linux that is a polkit rule naming the account and the units. On Windows it is an
entry in each service's security descriptor (and the task's permissions), which `sc.exe sdset` writes (section 13.3).
macOS has no such grant for a LaunchDaemon, so `sudo` stays there and the docs say so. The elevated commands in the
table above are then needed only to register or to opt into boot.

`launchctl bootstrap` loads a job from any path, and a job whose plist sits outside `/Library/LaunchDaemons` is
forgotten at the next reboot. That is how on demand works on macOS without a disabled flag that a later boot might
undo. `KeepAlive` with `SuccessfulExit` false starts the job as soon as it is loaded, which is what `start` wants.

The same exit-code rule holds on all three: **exit 0 means stopped, anything else means restart me.** `atrium stop`
makes the room exit 0, so it stays down. A hub-asked restart (`roomrestart.go`) and an update make it exit with a
code the manager restarts (75), which replaces the detached restarter it spawns today.

Every manager's stop is set to wait for the wind-down: `TimeoutStopSec=10min` on systemd, a stop-pending wait hint
the room keeps extending on Windows, `ExitTimeOut` of 600 seconds on launchd. A manager's stop sends the polite
signal first (SIGTERM, or the SCM's stop request), and the room treats it exactly as `atrium stop`. The kill comes
only after that wait, and only from the manager.

### 3.4 `atrium start` wraps the service manager, and only that

`atrium start [hub|room|pty]` (no argument meaning every service registered on this machine) does three things:

1. Reads which of the three are registered here, from the service manager.
2. Asks the service manager to start them in order, pty host before room, by running the same command a person
   would (`systemctl start`, the SCM, `schtasks /Run`, `launchctl bootstrap`).
3. Waits until each answers (`/_hub/health` for the hub, `/v1/settings` twice for the room, a probe for the pty
   host) and prints one line each.

It never starts an atrium process itself. When nothing is registered it says so and prints the command that would
register (section 5). When it needs elevation it says which command to rerun elevated, rather than asking for it.

`atrium stop` keeps its meaning: a wind-down of the room, then exit 0, so the service stays down. `atrium stop hub`
and `atrium stop pty` do the same for the other two, and `atrium stop pty` refuses while terminals are open unless
`--runners` says to end them. `atrium status` prints the service manager's view (registered, on demand or at boot,
running, pid) beside atrium's own (build, attached to which hub, the pty host's build, whether runners can sign in).

So `start`, `stop` and `status` add only what the service manager cannot know: the order, the wind-down, the health
answer and the attached state. Anything the service manager already does well (boot, crash restart, logs) is left
to it.

### 3.5 One room per machine, by design

There is no "is a room already running" check. The guarantee comes from there being one way in:

1. **The service is the only starter of the machine's room.** A service manager runs at most one copy of a named
   service, so `net start atrium-room` twice, or `atrium start` twice, is one room.
2. **The two other starters are removed.** `atrium run` no longer starts a room of its own (`ensureRoom` goes), and
   `atrium room --detach` goes, since it existed only to stand in for a supervisor. Those two paths are how a second
   room appears today.
3. **A hand-run `atrium room` stays for development**, in its own state dir with its own ports, which is a different
   room and not the machine's. Pointed at the machine's state dir it meets the store's lock, as it does today, and
   the error now names `atrium start`.
4. **The pty host is one per state dir already.** A second host for one state dir exits at once because its
   listener refuses it.

## 4. Where things live

The binary goes where each OS keeps programs. State stays in each account's home, because runners need the account's
home and the files in it.

| What | Windows | macOS | Linux |
| --- | --- | --- | --- |
| the binary | `C:\Program Files\atrium\atrium.exe` | `/usr/local/bin/atrium` (`/opt/homebrew/bin/atrium` from Homebrew on Apple silicon) | `/usr/bin/atrium` from the deb or rpm, `/usr/local/bin/atrium` by hand |
| the pty host's copy | `C:\Program Files\atrium\atrium-pty.exe` | beside the binary, `atrium-pty` | beside the binary, `atrium-pty` |
| service definitions | in the SCM's registry and Task Scheduler | `/Library/Application Support/atrium/launchd/`, and `/Library/LaunchDaemons/` for at boot | `/usr/lib/systemd/system/` from a package, `/etc/systemd/system/` by hand, account drop-ins in `/etc/systemd/system/atrium-room.service.d/` |
| which account runs what | the service's logon account and the task's principal | `UserName` in each plist | `User=` in a drop-in |
| how it was installed | `installed-by` file beside the binary (`msi`, `scoop`) | `installed-by` (`pkg`, `brew`) | `installed-by` (`deb`, `rpm`) |
| the hub | `%USERPROFILE%\.atrium\hub\` | `~/.atrium/hub/` | `~/.atrium/hub/` |
| the room | `%USERPROFILE%\.atrium\room\`, `atrium.db` | `~/.atrium/room/`, `atrium.db` | same |
| logs | `%USERPROFILE%\.atrium\logs\` | `~/.atrium/logs/` | journald, and `~/.atrium/logs/` |
| runner wiring | `%USERPROFILE%\.atrium\mcp.json`, `%USERPROFILE%\.claude\settings.json` | `~/.atrium/mcp.json`, `~/.claude/settings.json` | same |

The pty host runs from its own copy so that replacing `atrium` never touches the file a running host was started
from. The package now ships that copy, where today the room makes it. A package upgrade replaces `atrium` and leaves
a running `atrium-pty` alone (section 10.2).

A **drop-in** is a small file in `<unit>.service.d/` that systemd merges into a unit, so the package's unit stays as
shipped and only the account line is local. `installed-by` is the marker `packaging.md` asked for, read by `atrium
update` to know whether a package manager owns the binary.

A state dir other than `~/.atrium` (sg4's room uses `~/.atrium2`) is set in the service's environment: an
`Environment=WORKTREE_ROOT=` drop-in on Linux, the service's `Environment` registry value on Windows, and
`EnvironmentVariables` in the plist on macOS.

## 5. What a first-time user runs

### 5.1 One machine, hub and room as you

| OS | Commands |
| --- | --- |
| Linux | `sudo apt install ./atrium_0.7.0_linux_amd64.deb` (or `sudo dnf install ./atrium-0.7.0.x86_64.rpm`), then `sudo systemctl start atrium atrium-room` |
| Windows | run `atrium-0.7.0-windows-amd64.msi`, which asks for the account and its password, then `net start atrium` and `net start atrium-room` in an elevated shell |
| macOS | open `atrium-0.7.0.pkg` (or `brew install dovholuknf/tap/atrium`), then `atrium start`, which runs the `sudo launchctl bootstrap` lines |

Then `atrium status`, and the board at `http://127.0.0.1:7778`.

The package registers all three services for the person who installed it: `SUDO_USER` on Linux (as
`packaging/postinstall.sh` finds it today), the console user on macOS, the installing account on Windows. It starts
nothing.

On first start with no `room.json`, a room whose hub runs on the same machine as the same account joins it by
itself, the way `ensureRoom` does today. That is what keeps this case to two commands.

The account-side wiring moves into the room's join. When a room joins, by itself or through `atrium room join`, it
runs `atrium hook install` for each runner on PATH and writes `~/.atrium/mcp.json` with the atrium-control entry
(never over one the operator made), which closes the install half of `f-install-wires-atrium-control`. A join is
once per room and is already done as the room's account, so it is the right place for wiring that account.

### 5.2 Hub as you, room as `localai`, on one machine

`website/docs/accounts.md` recommends that agents never run as the operator. The package reads which account runs
which role from `/etc/atrium/accounts` on Linux and macOS (`hub=clint`, `room=localai`), and from the MSI's
`HUB_ACCOUNT` and `ROOM_ACCOUNT` on Windows. Changing it later is editing that file and running the package's
`/usr/lib/atrium/configure-services` (or `/usr/local/libexec/atrium/configure-services` on macOS) with sudo, or an
MSI repair. That script is packaging, as decision 1 asks, and it writes only the drop-ins or the plists' `UserName`.

```
# as you
atrium rooms add lab1                  # prints a join string
# as localai
atrium room join <string> --no-run     # key, certificate, room.json, hooks, mcp.json
# elevated
sudo systemctl start atrium-room       # or net start atrium-room, or atrium start room
```

### 5.3 A room on another machine

On the hub's machine, once:

```
atrium hub config --link 0.0.0.0:7779 --link-advertise sg4.lan:7779
sudo systemctl restart atrium          # or net stop atrium && net start atrium
atrium rooms add m1mini
```

On Windows a hub listening beyond loopback also needs an inbound firewall rule for 7779, one elevated
`New-NetFirewallRule` line that `atrium hub config` prints. With ziti or zrok there is no inbound port.

On the room's machine, install the package with `room=` set to the room's account, then as that account `atrium room
join <string> --no-run`, then start `atrium-room`. The hub service is registered there too and never started.

`scripts/provision-room.ps1` keeps everything that is not this (account and rights checks, the toolchain, git, the
work root and agent pack, Defender, folders, auth and the smoke), and its binary, join and autostart steps become the
package install, `atrium room join` and `atrium start room`, run over ssh.

### 5.4 An `atrium service` verb, or the OS's own commands

Decision 1 rules out a verb that registers. The remaining question is whether atrium should have a verb for running
the services once registered (`atrium service start|stop|restart|status|logs|enable-boot`), or whether people use
`systemctl`, `net` and `launchctl` directly with three small atrium verbs beside them.

| | `atrium service ...` | the OS's commands, plus `atrium start`, `stop`, `status` |
| --- | --- | --- |
| what a person learns | one spelling on every OS | their OS's spelling, which they may already know, and three atrium verbs |
| elevation | the verb needs it for most actions, and can only print the elevated command, so the person types two commands | the person runs the OS command elevated, once |
| what it can show | everything | the OS command shows the service, `atrium status` shows atrium's view |
| drift | atrium's idea of a service and the OS's can disagree, and the verb has to read the OS every time to avoid lying | none: the service manager is the only truth |
| code | a wrapper per OS for every action, with tests on three platforms | `start` only, plus the reads `status` needs |
| docs and support | atrium's docs explain atrium's verb | every OS's own docs, forums and tools apply as they are |

**Recommendation: the OS's commands, plus `atrium start`, `atrium stop` and `atrium status`.** No `atrium service`.
The service manager is already the one place that knows what runs, the person debugging a stuck service will reach
for `journalctl` or the Services console anyway, and a wrapper that must print an elevated command for most of what
it does saves nothing. The three verbs that stay are the ones that add what the service manager cannot know (order,
wind-down, health, attached). Boot opt-in stays an OS command, because it is a one-time decision and the OS's
command is the clearest record of it.

## 6. Rollback, explained (decided)

**What rollback is.** An update replaces the binary. Rollback puts the previous binary back when the new one turns
out not to work, and restarts the services on it. It has three parts: the previous binary kept beside the new one, a
check after the start that decides whether the new one works (called the gate here), and the switch back.

**A worked example.** sg4's room runs v0.6.0. At 14:00 someone runs `sudo atrium update`.

1. It downloads v0.6.1, checks its hash against `checksums.txt`, and runs `atrium.next version` to see it start.
2. It copies v0.6.0 aside as `atrium.prev`, and copies the room's store to
   `~/.atrium/backups/atrium-20261023-1400.db`.
3. It stops the room (a wind-down), then the hub, renames v0.6.1 into place, and starts the hub and the room. The pty
   host is never stopped, so the runners in the terminals keep working through all of this.
4. **The gate.** The hub answers `/_hub/health` and reports v0.6.1. The room should answer `/v1/settings` now and
   again ten seconds later, and be attached to the hub on a fresh connection. Instead the room exits after thirty
   seconds: v0.6.1 cannot open this store.
5. **The rollback.** It stops both, renames `atrium.prev` back, starts both, and runs the gate again, which passes.
   It records `rolled back from v0.6.1: room not serving` with the room's last twenty log lines, and exits 1. Total
   time with no room: about two minutes. Nobody lost a card.

**Where it gets hard: the store.** A room's store is a database whose shape changes over releases through
migrations, small numbered changes each build applies on start. An older build skips migrations it does not know.

- Suppose v0.6.1 ran for an hour before anyone saw a bug, and in that hour it applied migration 0142, which adds a
  column, and forty cards changed. `atrium update --rollback` puts v0.6.0 back. v0.6.0 ignores the new column and
  runs. The forty changes stay. This is the common case, because most migrations only add.
- Suppose instead 0142 renamed a column v0.6.0 reads. v0.6.0 fails its gate on the new store. The choices are to
  restore the 14:00 copy of the store (the forty changes are lost), or to stay on v0.6.1 and wait for a fix.

**Decided (R2-1).**

1. Rollback of the binary is automatic when the gate fails, as in the example. `atrium update --rollback` does the
   same by hand.
2. The store is never restored automatically. `--restore-store` puts the pre-update copy back, by hand only.
3. A migration that is not additive carries a mark (a convention in `schema.go`, no edit to a shipped migration).
   A rollback across a marked migration refuses before it stops anything, names the migration, and says that
   `--restore-store` is the way past it and what it loses.
4. A package-managed install rolls back through the manager (installing the older package). It gets the gate as a
   report and not the switch back.

With section 14 the rollback never touches a pty host either: the room switches back to the previous binary and
restarts, and every terminal stays where it was.

## 7. What the hub offers a room (decided)

**Today.** A hub tells every connected room which build it has for that room's OS and processor, as a note on the
link: version, sha256 (a fingerprint of the file's bytes), size, OS and architecture. The hub's own binary is one
such build, and `atrium run` can be pointed at a folder of builds for other platforms. The hub never sends a binary
unasked (`internal/link/upgrade.go`: the hub offers, the room decides). A room that was started with
`--accept-upgrades` and sees a different version dials a new connection, receives the bytes, checks the sha256,
writes the file beside its own binary and renames it in, then winds down. A room started without the flag ignores
every offer.

What goes wrong today: after the wind-down nothing starts the room again unless a supervisor does, so a room
started by hand stays down. Nothing checks that the new build works, and nothing puts the old one back.

**What would change under this design.**

1. **The flag has nowhere to go.** Service command lines carry no per-machine flags, so `--accept-upgrades` would
   become a setting in `room.json`, set by the room's own account and never by the hub.
2. **The restart is solved.** The room exits with the restart code (section 3.3) and its service manager starts it
   again on the new file.
3. **The file cannot be written.** The binary now lives in `/usr/local/bin` or `C:\Program Files\atrium`, which the
   room's account cannot write. So the room cannot rename an offered build in, whichever way the other questions go.
   The ways around that are a per-account binary for rooms that take offers (which undoes decision 4), a privileged
   helper that installs for the room (a root process that executes what a hub offered, which `upgrade.go` exists to
   refuse), or making the offer a notice only.
4. **A package-managed room cannot take an offer at all** (decision 9).
5. If offers are kept as installs, they would run the same gate and rollback as section 6.

**Decided (R2-2): the board shows it and a person deploys it from there.** Under the board's rooms chip, a room
whose build differs from the one the hub offers for its platform shows `an update is available: v0.6.1`, and a
Deploy action beside it. Nothing installs until someone presses it. Pressing it:

1. The board asks the hub, and the hub asks that room over the link to deploy the offered build. The hub still sends
   no bytes unasked: the room dials for them, as `upgrade.go` does today, and checks the sha256.
2. The room runs `atrium update` for itself, detached from its own process (section 6's sequence with section 14's
   rollover): stage, preflight, snapshot, restart on the new build, gate, roll back on a failed gate.
3. The room reports the outcome to the hub, and the chip shows it (`sgg: v0.6.1, ok` or `sgg: rolled back, room not
   serving`).

`--accept-upgrades` goes. Taking an update is now a press on the board for each deploy, so a standing flag that lets
every offer install by itself is no longer needed. A room can still refuse board deploys with a setting in
`room.json`, made by its own account (a room that only a package updates, for example).

**What this decision forces.** Point 3 of the list above stands: a room cannot write a binary in `Program Files` or
`/usr/bin`. So a board deploy works only for a room whose account owns the binary it runs. That is the strongest
argument in section 11 for running rooms from a per-account binary. A package-managed room shows the update on the
chip with the manager's command instead of a Deploy action (decision 9 of the first draft).

## 8. Windows with nobody logged in

**Sessions.** Windows gives each person who logs in a session, numbered from 1, with its own screen and desktop.
Session 0 is set aside for services and has no screen anyone sees. Every service runs there, whatever account it
runs as.

**ConPTY.** Windows' pseudo console: a program like claude expects to run in a terminal window, and ConPTY gives it
an invisible console and hands what it would draw to another program as a stream of bytes. The pty host makes one
ConPTY per terminal, and the board shows that stream.

**The working assumption here is that a service in session 0 cannot host the room's ConPTY terminals.** That is why
`atrium-pty` is not a service on Windows while `atrium` and `atrium-room` are: the hub and room hold no terminal, so
session 0 is fine for them. One fact cuts the other way and is worth the spike below: Windows' own OpenSSH server
runs as a session 0 service and gives each ssh login a ConPTY. If atrium's runners work the same way, `atrium-pty`
could simply be a third service.

**S4U.** Short for "Service for User", a logon type for scheduled tasks. Task Scheduler logs the account on by
itself, without the account's password and without anyone at the machine, at a trigger such as boot or a `schtasks
/Run`. What it cannot reach is anything that needs the password: network shares as that user, and secrets Windows
protects with the user's password (Credential Manager). claude and codex keep their sign-in in files, so that limit
is acceptable for runners. An S4U task is also non-interactive and also runs in session 0, so whether ConPTY works
under it is the same open question with a different kind of logon. **It is unproven.**

**The spike** (from `room-autostart-design.md` section 6, on claudevm only): register `atrium-pty` as an S4U task
for a non-administrator account, reboot, log nobody in, start the room, attach to a card from the board and run a
claude session through one tool call. Record whether registering needed elevation, whether the profile and PATH
loaded, and whether claude signed in from its file. Run the same with `atrium-pty` as an SCM service.

**If it passes,** `atrium-pty` is an S4U task (or a service, if that passed too) and Windows matches the other two
platforms. **If it fails,** the options are:

1. **Logon only.** `atrium-pty` is a logon task in the room account's own session, as today's task is. The hub and
   the room still run as services with nobody logged in. Terminals exist while that account has a session. A session
   opened over Remote Desktop and then disconnected (not signed out) keeps running, so one login after each reboot is
   enough. Cost: after a reboot, cards cannot start terminals until someone logs in.
2. **Auto-logon.** Windows logs the room account in at boot by itself, a logon task starts `atrium-pty`, and a
   second task locks the screen at once. Cost: the account's password is stored on the machine (Sysinternals
   Autologon keeps it as an encrypted LSA secret rather than in plain text), and the console shows an unlocked
   desktop for a few seconds after each boot.
3. **No Windows rooms at boot.** Windows rooms are on demand only, started by someone logged in.

Because on demand is now the default, this matters only for a Windows machine that opts into starting at boot.
Section 11.4 says how much it matters under each way of running atrium, and section 15 recommends.

## 9. macOS and FileVault (decided)

**FileVault** encrypts the Mac's disk. After the Mac powers on, the disk stays locked until someone types a password
at a screen that appears before macOS itself starts. Until then nothing of macOS runs: no LaunchDaemon, no ssh, no
atrium.

**`fdesetup authrestart`** is a restart that unlocks once. Run with sudo and a FileVault user's password, it keeps
an unlock key for exactly the next boot, so the Mac comes back up unlocked and LaunchDaemons start as normal.

**Decided:** FileVault stays on. Planned reboots (a macOS update, a reboot someone wants) go through `sudo fdesetup
authrestart`. An unplanned reboot (power loss, a crash) waits at the unlock screen for a human. macOS updates on a
room machine are set to download but not install, so a person installs them and reboots with `authrestart`.

A Mac room that should come back by itself after a planned reboot also opts into boot (its plists in
`/Library/LaunchDaemons`, section 3.3). Without that, after the reboot it is on demand like any other machine.
`atrium status` on a Mac with FileVault on says `at boot: after a planned reboot only`.

## 10. Releases, updates, the private scripts, and the plan

### 10.1 Releases and signing

Releases are GitHub Releases. Each carries the archives (`atrium_<v>_<os>_<arch>.zip` or `.tar.gz`), the packages
(deb, rpm, MSI, pkg), and `checksums.txt`, a list of each file's sha256. `atrium update` and the package recipes
(scoop's manifest, the Homebrew formula) check every download against it.

**Decided (R2-9): SignPath for Windows, cosign for `checksums.txt`.** What follows is what that means.

SignPath (signpath.io) is a code-signing service, and SignPath Foundation (signpath.org)
signs open-source projects with it for free. Per its published terms, to confirm when applying:

- It signs Windows files (the `.exe` and the MSI) with Authenticode, the signature Windows checks before it runs a
  download. Signed files stop SmartScreen's "unknown publisher" warning and are trusted sooner by Defender, which
  matters on sgg and sg3.
- The certificate belongs to SignPath Foundation, so Windows names the Foundation as publisher, not clint.
- It signs only what a CI build of the public repository produced (GitHub Actions), and each release is approved by
  a person before it is signed. The project needs an OSI license (atrium is Apache 2.0) and a public repository.
- It does not cover macOS. Gatekeeper, macOS's check on downloaded programs, wants an Apple Developer ID signature
  and Apple's notarization, which needs an Apple developer account. Linux packages are signed with a GPG key, which
  needs nothing outside.
- `checksums.txt` itself is a separate signature. cosign signing it from GitHub Actions, with no key to keep, is the
  natural fit and needs nothing from SignPath.

The work is: apply to SignPath Foundation, wire signing into the release workflow, sign one MSI, install it on
claudevm and check that SmartScreen and Defender are quiet. cosign signs `checksums.txt` from the first release, and
`atrium update` checks that signature before it trusts a hash. An Apple Developer ID is a separate decision, once a
Mac room is used by someone other than clint. Until `checksums.txt` is signed, `atrium update` without `--from` asks for
`--yes` before it fetches from the internet.

### 10.2 Updating

| Install | `atrium update` does |
| --- | --- |
| a package (`installed-by` says so) | prints the manager's command (the new MSI, `scoop update atrium`, `brew upgrade atrium`, `sudo apt install ./atrium_<v>.deb`) and stops |
| by hand, binary in the OS's folder | needs elevation (`sudo atrium update`, or an elevated shell). Fetches, checks, swaps, restarts the hub and room through the service manager, gates, and rolls back as section 6 settles |
| `--from ./atrium` | the same, from a local file, which must report a commit and a clean tree. This is what `build-deploy.ps1` feeds |

Either way the hub and room restart on the new build and no pty host does. Section 14 is how: a new build adds a
pty host generation beside the old one, and the old one retires once it holds no terminal. A package's scriptlets
restart the hub and room if they were running and never stop a pty host.

`atrium update --rooms` from the first draft is replaced by the board's Deploy action (section 7).

### 10.3 The private scripts, retired

| Today | Becomes |
| --- | --- |
| `build-deploy.ps1` | stays on the build machine, feeding `atrium update --from` |
| `live-common.ps1` `$HubArgs`, `$RoomArgs` | `hub/hub.json` written by `atrium hub config`, and `room.json` |
| `Save-Revert`, `Install-Atrium`, `Test-NewBuildStamped` | `atrium update` |
| `Stop-Room`, `Stop-Hub`, `Start-Jobless` | the service manager, through `atrium stop` and `atrium start` |
| `Wait-RoomAttached`, `Test-RoomServes`, revert in `deploy-batch.ps1` | `atrium start`'s wait and `atrium update`'s gate |
| `deploy-batch.ps1`, `deploy-hub-only.ps1`, `deploy-ready.ps1` | `atrium update` |
| `atrium-deploy.ps1` remote rooms | the board's Deploy action on each room (section 7) |
| `start-atrium*.ps1`, `stop-atrium.ps1` | `atrium start` and `atrium stop`, and boot opt-in on sg4 |
| `refuse-supervised.ps1` | `atrium stop` knows its caller's ancestry (`internal/cli/ancestry*.go`) and refuses there |
| sg4-control and its `-IKnowItKillsMyShell` | section 12 |
| `install-live-scripts.ps1` | deleted with the scripts it copies |
| `scripts/atrium-service.*`, `atrium-autostart.ps1` | deleted once the packages register the three services. Their registration logic moves into `packaging/` |

### 10.4 Staged plan

Written for the second draft's three services. Once section 15's decisions 1 and 3 are made, steps 2, 5 and 7 are
redrawn to them.

1. **Spike on claudevm only:** the SCM handler of section 13 under the hub. ConPTY under S4U (section 8) only when a
   Windows room machine wants to start at boot (section 11.4).
2. **The binary under a service manager.** `hub/hub.json` and `atrium hub config`. The exit-code rule. SIGTERM and the
   SCM's stop request as a wind-down (section 13). `pty_host` always on for a registered room. `ensureRoom` and
   `room --detach` removed. @runtime.
3. **Pty host generations (section 14):** the build suffix on the address, discovery by listing, every relaunch on
   the newest generation, disconnecting from an empty older one, the cap and the idle move. @terminal.
4. **`atrium start`, `atrium status`, and `atrium stop` for all three.** @runtime.
5. **Join does the wiring:** hooks and `mcp.json` at join, the same-machine first-start join. @runtime.
6. **The service definitions in `packaging/`**: three systemd units and the account drop-ins, three plists, the MSI's
   two services and the pty task, `configure-services`. One source for all of them, which the deb, rpm, pkg and MSI
   all use. @fabric.
7. **Releases** with `checksums.txt` signed by cosign, and SignPath for the Windows binary and MSI. @fabric.
8. **`atrium update`** with the gate, the automatic rollback and the preflight of section 14.5, ported from
   `deploy-batch.ps1` and proven against it on claudevm before sg4 uses it. @runtime.
9. **Board Deploy** under the rooms chip (section 7), which runs step 8 on the room. @runtime and @ui.
10. **provision-room.ps1** installs the package and joins, over ssh. @fabric.
11. **Docs**: `website/docs/install.md` rewritten to section 5, `packaging.md` updated to the three services, and
   `website/docs/control-mcp.md` fixed (`f-install-wires-atrium-control` step 4). @merge.
12. **Retire the private scripts** on sg4, one at a time, each after its replacement has done one real deploy.
13. Owed regardless: the `install-live-scripts.ps1` test from the bug item, until step 12 deletes the script.

Nothing touches sg3, sgg, m1mini or sg4 until clint says so, machine by machine.

## 11. Who runs what, and as whom (item 3, explored)

clint reopened this in round 3: no room runs as SYSTEM, maybe not the hub either, everything runs as a
non-privileged user, and perhaps only the hub is a service.

### 11.1 The words

- **SYSTEM** (Windows) and **root** (Unix) are the machine's all-powerful accounts. A program running as one of them
  can read every file and change anything. No draft has run atrium as either. The real question is narrower: which
  account runs each part, what kind of registration starts it, and who must be an administrator to set that up.
- A **non-privileged account** is an ordinary user that is not an administrator, such as `localai`. It can write its
  own home folder and nothing that belongs to the machine.
- A **machine service** here is a registration in the machine's service manager (section 3.3): an SCM service, a
  systemd system unit, a LaunchDaemon. Registering one needs an administrator, once. It can run as any account, and
  it can start with nobody logged in.
- A **per-user registration** belongs to one account, and that account makes it without an administrator: a systemd
  user unit, a Windows scheduled task that runs as oneself while logged on (`scripts/atrium-autostart.ps1` registers
  one with no elevation today), a macOS LaunchAgent. It runs as that account, and only while that account has a
  session, except a Linux user unit with linger.
- A **virtual service account** (Windows, `NT SERVICE\<name>`) is an account Windows makes for exactly one service.
  It has no password anyone stores, no rights beyond what is granted to it, and a profile folder of its own. Linux
  and macOS have the same idea as a dedicated system user (`atrium`, `_atrium`) that nobody logs in as.

### 11.2 The options

| | A. Three machine services (the second draft) | B. Nothing is a machine service | C. Only the hub is a machine service |
| --- | --- | --- | --- |
| the hub | machine service, as the hub's account | per-user, as the hub's owner | machine service, as a non-privileged account |
| the room | machine service, as the room's account | per-user, as the room's account | per-user, as the room's account |
| the pty hosts | a machine service or S4U task (section 8) | started by the room, one per build (section 14) | started by the room, one per build |
| administrator needed | to install, and on Windows and macOS for each new pty host generation (section 14.4) | never on Windows (logon task) or macOS (LaunchAgent). Linux only where linger needs `sudo` | once, to register the hub |
| starts with nobody logged in | all three | Linux with linger. Windows only if the S4U spike passes. macOS no | the hub everywhere. Rooms as in B |
| Windows password stored | for each service's account, unless S4U | none | none with a virtual account. Yes if the hub runs as a person |
| binary a room runs | `Program Files` or `/usr/bin`, which the room cannot write | the account's own, under `~/.atrium/bin` | the account's own, under `~/.atrium/bin` |
| board Deploy (R2-2) | needs a privileged helper, which `upgrade.go` exists to refuse | works: the room writes its own binary | works for rooms. The hub updates by package or elevated `atrium update` |
| pty host rollover (section 14) | a registration per generation on Windows and macOS. A template unit on Linux | free | free |
| ConPTY (item 4) | the spike decides | in the account's own desktop session, as today, which works | as B |
| one room per machine | the service manager | only the account named as the room's has the registration | as B |
| SCM handler (item 8) | hub, room, and the pty host if a service | not needed | the hub only |

**A. Three machine services.** The second draft. Everything starts at boot with nobody logged in, and each OS
restarts what crashes. It runs into both of clint's round 2 answers. A board deploy cannot write the binary, and a
pty host per build means registering a new service or daemon, elevated, on every update. On Windows it also needs
each account's password stored, and the pty host's ConPTY in session 0 is unproven.

**B. Nothing is a machine service.** Each account registers its own parts. No administrator ever, no password
stored, ConPTY in the account's own session where it is proven, and the room owns its binary. The cost is the
hub: on Windows and macOS it stops when its owner logs off, and every remote room loses its hub with it. That is the
failure that took the sg3 room down on 10/21, moved to the one process every room depends on.

**C. Only the hub is a machine service.** The hub is what other machines dial, it holds no terminal and needs no
desktop, so it is the one part that gains from starting at boot as a machine service and loses nothing in session 0.
Rooms and pty hosts get everything B gives them. The cost is two kinds of registration to document, and a Windows
room that stops at log-off unless the S4U spike passes (11.4). A disconnected Remote Desktop session keeps a logon
task running, so one login after each reboot is enough.

**Recommendation: C.** It is the only option that keeps both round 2 decisions about updates (board Deploy and never
restarting a pty host) without a privileged helper, and it keeps the part remote rooms depend on running at boot.

### 11.3 Under C, in detail

- **The hub's account.** Two choices, decision 2 in section 15.
  - A virtual account (`NT SERVICE\atrium` on Windows, an `atrium` system user elsewhere). No password, nothing of a
    person's to read. But the hub's state moves into that account's folder (`C:\ProgramData\atrium\hub` or
    `/var/lib/atrium/hub`), and the operator's `atrium rooms add`, `rooms token` and `backups` stop working as they
    do today, because they open `hub.db` directly as the operator (`hubStoreFlags.open` in
    `internal/cli/atrium_rooms.go`). They would have to ask the running hub over its loopback board instead.
  - The operator's own account (or `localai`). Every hub verb works as today. On Windows the SCM stores that account's
    password, and it has to be re-entered when the password changes.
- **The room and its pty hosts** run as the room's account, from `~/.atrium/bin`, registered per user.

| | Linux | Windows | macOS |
| --- | --- | --- | --- |
| room | user unit `atrium-room.service` in `/usr/lib/systemd/user/` from the package, enabled by the room's account. Linger for at boot | a scheduled task for the room's account, at logon, `InteractiveToken`. The MSI can register it for `ROOM_ACCOUNT` | a LaunchAgent in `~/Library/LaunchAgents` of the room's account |
| pty hosts | transient user units from `systemd-run --user` (section 14.4) | started by the room outside its job, as today | started by the room with `AbandonProcessGroup` on the room's agent |
| at boot | linger, which most distros allow per user without sudo | only with S4U (11.4) | no |
| one room | only the account in `/etc/atrium/accounts` (or the MSI's `ROOM_ACCOUNT`) has it enabled | only that account has the task | only that account has the agent |

- **The binary.** The package installs the machine's copy (`Program Files`, `/usr/bin`) for the hub and as the seed
  for rooms. A room copies it into `~/.atrium/bin/versions/<version>-<commit7>/` on first start, and from then on
  runs its own copy, which a board Deploy replaces. This reverses round 1 decision 4 for rooms only, and is
  decision 3 in section 15.

### 11.4 ConPTY under S4U (item 4, on hold) for each option

**S4U** and **ConPTY** are explained in section 8. The spike there asks whether a pty host started with nobody
logged in can host a claude session. How much that matters depends on 11.2:

- **A.** It decides whether a Windows room works at all without a login. A failure leaves the pty host a logon task
  while the hub and room are services, the split section 8 already describes.
- **B and C.** The pty host runs in the room account's own desktop session, where ConPTY is proven today. The spike
  decides only whether a Windows room may start at boot with nobody logged in. A machine that does not opt into boot
  never needs it.

So under C the spike stays on hold until a Windows room machine (sgg, sg3) wants to start at boot, and the hub, the
one part that must start at boot, does not depend on it.

## 12. sg4-control (item 7, explored)

### 12.1 What it is

A **room** is one `atrium room` process with its own state dir, store, ports, pty host and identity on the hub. sg4
runs two of them, both as the account `claude`:

- **claude-sg4**, the machine's room, where most agents work. State in `~/.atrium2`, board `127.0.0.1:7781`.
- **sg4-control**, the second room, which holds the orchestrator. State in `~/.atrium2/ctl`, board `127.0.0.1:7791`,
  agents `127.0.0.1:7787`. It runs its own copy of the binary from `~/.atrium/ctl-bin`, and `--isolated`, so the
  machine's hooks keep pointing at claude-sg4.

**Hand-started** means a person or script starts it and nothing restarts it. `start-atrium-control.ps1` starts it
detached (`Start-Jobless`). After a crash or a reboot it stays down until someone runs the script again. Its binary
is copied once and updated by hand, on purpose: the deploy scripts never touch it, and `atrium-deploy.ps1` includes
it only with both `-Control` and `-IKnowItKillsMyShell`, because deploying it ends the orchestrator's own terminal,
which may be the shell running the deploy.

**Why it exists.** It is a lifeboat. The orchestrator runs the deploys of claude-sg4, and an agent inside the room
being deployed would end its own session by deploying it. A build that broke claude-sg4 would also leave no agent
able to notice and put the old one back. sg4-control sits on an older build no deploy touches, so it survives both.

### 12.2 What this design changes about that

- **The pty host and section 14** keep every terminal through a room update, so an orchestrator inside claude-sg4
  would no longer lose its session to a deploy.
- **The detached update engine** (section 6) is never a child of the room's terminals, so a deploy no longer kills
  the shell that started it.
- **Automatic rollback** (R2-1) puts a broken build back without an agent.

What it does not change: while claude-sg4 restarts (seconds) or rolls back (a minute or two), an orchestrator inside
it has no atrium-control. And a bug in a build that every room runs reaches the orchestrator's room too, while a
pinned lifeboat would not run that build.

### 12.3 The options

| | What it is | Cost |
| --- | --- | --- |
| A. As today | hand-started, own state dir and binary copy, named in the docs as the one exception | down after a crash or reboot until someone notices, binary updated by hand |
| B. Supervised and pinned | a second per-user registration for the same account (`atrium-room-control`, its own state dir and ports), never offered board Deploy, updated deliberately | a second room per machine becomes a supported shape. Section 3.5's one way in becomes "one machine room, and named extra rooms in their own state dirs" |
| C. Off sg4 | the orchestrator's room on another machine or a VM (claudevm), the only room there | another machine to keep up. The orchestrator reads its workers' commits from the same repository on sg4 today, and would read them from hub branches instead |
| D. Retired | the orchestrator becomes a card on claude-sg4 | no lifeboat. A broken build that rollback does not catch takes the orchestrator with it |

**Recommendation: A now, B once section 14 ships.** A costs nothing to keep. B is a small step once rooms are per-user
registrations (11.3), since it is the same registration with another state dir, and it ends the "down until
noticed" problem. Keep the lifeboat rather than D, because its whole value is that it does not run the build being
deployed. C is worth it only if sg4 itself becomes the problem.

What would settle it, if clint wants an interview instead: how often sg4-control has been down without anyone
noticing, whether the orchestrator has ever needed to outlive a bad claude-sg4 build, and whether it needs the
repository on sg4 or could work from hub branches.

## 13. The Windows service handler (item 8, spiked)

### 13.1 What it is, and why it is needed

A Windows service is not any program the SCM happens to start. The SCM starts the program and then waits for it to
call back (`StartServiceCtrlDispatcher`) and report that it is running. A program that never calls back is ended
after about thirty seconds with error 1053, "the service did not respond in a timely fashion". The program must also
answer the SCM's stop and shutdown requests and report progress while it stops. That callback and those answers are
the **service handler**.

There are two ways to get one:

- **In the binary**, with `golang.org/x/sys/windows/svc`. atrium already depends on `golang.org/x/sys` (v0.48.0 in
  `go.mod`), so this adds no new module. The same `atrium.exe` notices the SCM started it and answers it.
- **A wrapper**, a second program (WinSW, NSSM, shawl) registered as the service, which speaks to the SCM and runs
  `atrium.exe` as a child. One more binary to ship, sign and trust, and its stop becomes a console signal or a kill
  that atrium has to translate back into a wind-down.

### 13.2 The sketch

This compiles: it was type-checked with `GOOS=windows go vet` against x/sys v0.48.0 in a scratch package, which was
then removed. It has not been run under the SCM. It would live in a small `internal/winsvc` package with a
`//go:build windows` tag and a no-op twin for other platforms.

```go
//go:build windows

package winsvc

import (
	"context"
	"time"

	"golang.org/x/sys/windows/svc"
)

// Under reports whether the SCM started this process.
func Under() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

// Run answers the SCM while role runs. role gets a context the SCM's stop cancels, which is the same wind-down
// ctrl-c and `atrium stop` get, and returns the process's exit code.
func Run(name string, role func(ctx context.Context) int) error {
	return svc.Run(name, &handler{role: role})
}

type handler struct{ role func(ctx context.Context) int }

func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, st chan<- svc.Status) (bool, uint32) {
	st <- svc.Status{State: svc.StartPending}
	ctx, windDown := context.WithCancel(context.Background())
	defer windDown()
	done := make(chan int, 1)
	go func() { done <- h.role(ctx) }()
	st <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	var check uint32
	stopping := false
	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				st <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				windDown()
				stopping = true
				check++
				st <- svc.Status{State: svc.StopPending, CheckPoint: check, WaitHint: 30000}
			}
		case <-tick.C:
			// The SCM gives up on a stop whose checkpoint stops moving, so a long wind-down keeps moving it.
			if stopping {
				check++
				st <- svc.Status{State: svc.StopPending, CheckPoint: check, WaitHint: 30000}
			}
		case code := <-done:
			st <- svc.Status{State: svc.StopPending}
			if code == 0 {
				return false, 0
			}
			return true, uint32(code)
		}
	}
}
```

And the two places it touches atrium:

```go
// cmd/atrium/main.go
func main() {
	if winsvc.Under() {
		os.Exit(cli.ExecuteUnder(winsvc.Run))
	}
	os.Exit(cli.Execute())
}
```

`cli.ExecuteUnder` runs the same cobra command inside `role`, with the SCM's context as the root context. The one
change in the roles is that `atrium run` and `atrium room` derive their signal context from `cmd.Context()`
instead of `context.Background()` (`atrium_run.go` line 607, `roomrun.go` line 382), so the SCM's stop and ctrl-c
end in the same wind-down.

What the sketch shows:

- **Stop is a wind-down.** The SCM's stop cancels the context, which is what ctrl-c does today. The kill stays the
  SCM's, after the wait, never atrium's.
- **The exit code rule of section 3.3 carries over.** `role` returning 0 tells the SCM the service stopped. Any other
  code (75 for an update or a hub-asked restart) is returned as a service-specific exit code, and with
  `failureflag` set the SCM's recovery actions restart it.
- **It registers nothing.** No `CreateService`, no elevation, no SYSTEM. Registration stays with the package (round 1
  decision 1).

### 13.3 What the package registers

For the hub under option C, with a virtual account:

```
sc.exe create atrium binPath= "\"C:\Program Files\atrium\atrium.exe\" run --no-room" obj= "NT SERVICE\atrium" start= demand
sc.exe failure atrium reset= 86400 actions= restart/5000/restart/30000/restart/60000
sc.exe failureflag atrium 1
sc.exe sdset atrium "<the default descriptor>(A;;RPWPLCLORC;;;<owner's SID>)"
```

The last line is R2-5: it adds start (`RP`), stop (`WP`), query (`LC`, `LO`) and read (`RC`) for the owning account,
so `net start atrium` and `atrium start hub` need no elevation. The exact descriptor is for the build to settle.

### 13.4 Which option needs it

Under A: the hub, the room, and the pty host if it is a service. Under C: the hub only. Under B: nothing, because a
scheduled task, a user unit and a LaunchAgent do not speak the SCM's protocol. The next step, if clint wants one, is
to run it on claudevm: register the hub with the lines above, `net start`, `net stop` during a long wind-down, and
an exit 75 that the recovery actions restart.

## 14. Updates that never restart the pty host (item 10, designed)

### 14.1 What clint asked, in atrium's terms

clint: never restart the pty host on an update. An update adds a second process beside the old one, and the old one
rolls over when it holds no terminals, so rooms can be rolled.

With the `pty_host` setting on, a room holds no terminal itself. The pty host holds every terminal and the runner in
it (`docs/rnd/rolling-restart-design.md`), and the room is its client. So the process that "holds terminals" and
must retire only when empty is the pty host. The room can restart in a few seconds at any time and every terminal
stays up, which is what the pty host was built for. This design therefore rolls the pty host and restarts the room in
place, and section 14.6 explains why a second room process beside the old one is not the better reading.

Every update below needs `pty_host` on. A room started by a registration has it on always (decision 9 in section 15).

### 14.2 Generations

- A **generation** is one pty host process on one build. It runs from its own copy,
  `~/.atrium/bin/versions/<version>-<commit7>/atrium-pty[.exe]`, which nothing ever overwrites. So Windows never
  needs to replace a file a running host was started from, and the MSI's "replace only when stopped" rule goes.
- The **newest** generation takes every new terminal. An older one takes none, keeps the ones it has, and exits by
  itself once the last of them has ended.
- Its address is today's (a hash of the state dir and the user) with the build as a suffix, `<hash>.<commit7>`. This
  is the naming `rolling-restart-design.md` section 3.2 already plans, keyed by build rather than by protocol. The
  room finds every generation by listing (`\\.\pipe\` on Windows, socket files in the state dir elsewhere) and counts
  a name only once `hello` answers on it.
- **Protocol.** A room must speak every generation it finds. Each build speaks its own `proto` and the one before. An
  update whose new room could not speak a live generation refuses before it stops anything, and names the
  generation and its cards.
- **Retiring.** Today a host exits after ten idle minutes with no pty AND no room connected (`DefaultIdleExit`). The
  room disconnects from an older generation as soon as that generation holds nothing, and the existing idle exit then
  ends it. That needs no new verb, so it works with hosts already deployed. The newest generation never idle-exits
  while its room is registered.

### 14.3 How a generation drains

A terminal ends when its runner exits: the card finishes or is closed, a new context starts (on its own at 200k
tokens), someone presses restart, or the card is parked and later resumed. Each of those already ends the runner,
and the relaunch goes to the newest generation. This changes `rolling-restart-design.md` section 6, where an ordinary
restart stays on the host it was on. Staying served a host upgrade that was rare. With a generation per update,
moving on every relaunch is what makes the old ones drain without anyone acting.

A card that never relaunches (an idle card left open for days) keeps its generation alive. To bound that, at most
three generations hold runners at once. An update that would make a fourth first runs the idle move from
`rolling-restart-design.md` section 6 on the oldest's cards: at an idle moment (no turn, no question, no queued
message, no background work), the runner is restarted on the newest generation and resumes the same conversation.
That is a runner restart, not a pty host restart, and it happens only past the cap. The board shows each card's
generation, with a "move now" for an operator who will not wait. A plain shell is never moved: a shell's state would
not survive it, so the board marks it as on an old generation and the operator closes it.

### 14.4 Who starts a generation

A generation is started by the room, as the room's account, outside the room's own lifetime, and no service manager
restarts it after a crash. Its terminals end with it, and a restart would not bring them back. The room starts a new
newest generation at the next launch when none answers.

| | Linux | Windows | macOS |
| --- | --- | --- | --- |
| under C (and B) | `systemd-run --user --unit atrium-pty-<commit7> --collect`: a transient user unit, outside the room's cgroup, no file, no sudo | started outside the room's job object as today (`CREATE_BREAKAWAY_FROM_JOB`, or WMI through `internal/detach`) | spawned with `AbandonProcessGroup` set on the room's LaunchAgent |
| under A | a template system unit, `systemctl start atrium-pty@<commit7>`, allowed by the R2-5 polkit grant | a new service or S4U task per build, which needs an administrator on every update | a new LaunchDaemon plist per build, which needs `sudo` on every update |

This is the row that makes option A a poor fit for item 10 on Windows and macOS.

### 14.5 The update, revised

Section 6's sequence, with the rollover:

1. **Fetch and check** into `~/.atrium/bin/versions/<version>-<commit7>/`, the hash against `checksums.txt` or the
   hub's offer.
2. **Preflight the new build** while the old room still runs: `atrium version`, then `atrium room --preflight` from
   the new copy, which opens the store read-only, lists the migrations it would apply (and refuses across a marked
   one on a rollback, R2-1), reads `room.json` and the keys, and says hello to every live pty generation to confirm
   it speaks their protocol. Any failure stops here, and the old room never stopped.
3. **Snapshot** the store, as section 6.
4. **Switch.** Rename the live `~/.atrium/bin/atrium` aside and the new copy in, as `Install-Atrium` does today (a
   running file can be renamed on Windows, though not overwritten). The registration always names that one path. The
   old version's folder stays for rollback.
5. **Restart the room in place.** It exits 75 and its registration starts it on the new build. Every terminal stays
   in its pty host. The board and the hooks see a gap of a few seconds (to be measured on claudevm), and a hook that
   cannot reach the room fails open, as every hook does.
6. **Reattach.** The new room says hello to every generation and reattaches every terminal, as a room restart does
   today (`reattachRuns`). Until the gate passes, new terminals still go to the previous newest generation.
7. **Gate**, as section 6.
8. **Pass:** the room starts its own generation, which becomes the newest, and new terminals go there. **Fail:**
   rename the previous build back in, exit 75, gate again. No terminal was ever on the failed build's generation,
   because none was started, so the rollback strands nothing.

The hub updates the same way, minus the generations: it holds no terminal, so a restart in place is the whole of it,
and every room redials.

### 14.6 Why not a second room process beside the old one

A room's store takes a lock, and the room's board and agent addresses are single listeners. Two room processes on one
state dir cannot both serve, so "beside" could only mean the new one waits while the old one winds down. That buys
little over the in-place restart above: the old room's wind-down is short when it has no runner to stop, and step 2's
preflight already gives the main benefit of an overlap, finding a bad build before the old room stops. It would also
make the room process a second starter beside the registration, which is what section 3.5 removed to keep one room
per machine. The rolling happens where the terminals are, in the pty hosts.

## 15. Decisions for clint

1. **Who runs what (item 3, section 11).** Recommend option C: only the hub is a machine service, under a
   non-privileged account. Rooms and their pty hosts run as the room's account from per-user registrations. Nothing
   runs as SYSTEM or root.
2. **The hub's account under C (section 11.3).** Recommend the account that owns the hub today for the first
   release, with its password stored by the SCM on Windows, because every hub verb works unchanged. Move to a
   virtual account (`NT SERVICE\atrium`) once `rooms add`, `rooms token` and `backups` ask the running hub instead of
   opening `hub.db`.
3. **A per-account binary for rooms (section 11.3).** Recommend yes: a room runs its own copy under
   `~/.atrium/bin/versions/`, seeded from the package. This reverses round 1 decision 4 for rooms only, and it is
   what board Deploy (R2-2) and pty host generations (section 14) need without a privileged helper.
4. **ConPTY under S4U (item 4, section 11.4).** Recommend leaving the spike on hold under C until a Windows room
   machine wants to start at boot. Until then Windows rooms start at logon, and a disconnected Remote Desktop
   session keeps them running.
5. **sg4-control (item 7, section 12).** Recommend keeping it hand-started as today, then making it a supervised,
   pinned second room (option B) once rooms are per-user registrations, and keeping it as a lifeboat rather than
   retiring it. If unsure, the three questions at the end of 12.3 would settle it.
6. **The Windows service handler (item 8, section 13).** Recommend the in-binary handler with
   `golang.org/x/sys/windows/svc`, used by the hub only under C, over a wrapper. Next step: run the sketch on
   claudevm as in 13.4.
7. **Pty host generations (item 10, section 14.2).** Recommend one generation per build, the newest taking every
   new terminal, and an older one retiring when it holds nothing, with no pty host ever restarted by an update.
8. **Draining (section 14.3).** Recommend that every relaunch that already ends a runner (new context, restart,
   resume) lands on the newest generation, which changes `rolling-restart-design.md` section 6, and a cap of three
   generations holding runners, past which the oldest's cards get the idle move.
9. **`pty_host` always on for a registered room (section 14.1).** Recommend yes. Rollover depends on it, and a room
   whose terminals die with it cannot be updated without ending them.
10. **The room on update (section 14.6).** Recommend a preflight from the new build and then a restart in place,
    rather than a second room process beside the old one, so the registration stays the only starter of the room.
