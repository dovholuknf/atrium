# Install, update and run atrium as services, for a human on a fresh machine

Status: proposed, not built. A design for review, revised 2026-10-23 with clint's answers to the first draft.

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
runs at most one copy of a service. Rollback and updates offered by the hub are explained in sections 6 and 7 and
left open.

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

Rules carried from the first draft, unchanged:

1. **As a user, never as SYSTEM or root.** Runners (claude, codex) need the account's PATH, home, sign-in and ssh
   agent. Every service runs as the account that owns its role, even when root or an administrator registered it.
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

## 6. Rollback, explained (open)

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

**What is open.**

1. Whether rollback is automatic when the gate fails, as above, or only by hand with `atrium update --rollback`.
2. Whether the store is ever restored automatically, or only with `--restore-store` by hand.
3. Whether migrations should carry a mark when they are not additive, so a rollback across one refuses and says
   why, instead of failing its gate. The mark would be a convention in `schema.go` and needs no edit to a shipped
   migration.
4. A package-managed install rolls back through the manager (installing the older package), which has none of the
   above, so it would get the gate as a report and not the switch back.

## 7. What the hub offers a room, explained (open)

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

**Notice only** would look like this: the hub keeps telling rooms what it runs, and the room keeps checking, but the
outcome is a line in `atrium status` and on the board (`room sgg: v0.6.0, hub offers v0.6.1`), and a person updates
with the package manager or `sudo atrium update` on that machine, over ssh for a remote one. The cost is that
updating several rooms becomes one command per room again, as `atrium-deploy.ps1` does today.

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
Recommendation in section 11.

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

**SignPath, to evaluate.** SignPath (signpath.io) is a code-signing service, and SignPath Foundation (signpath.org)
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

The evaluation is: apply, wire signing into the release workflow, sign one MSI, install it on claudevm and check that
SmartScreen and Defender are quiet. Until `checksums.txt` is signed, `atrium update` without `--from` asks for
`--yes` before it fetches from the internet.

### 10.2 Updating

| Install | `atrium update` does |
| --- | --- |
| a package (`installed-by` says so) | prints the manager's command (the new MSI, `scoop update atrium`, `brew upgrade atrium`, `sudo apt install ./atrium_<v>.deb`) and stops |
| by hand, binary in the OS's folder | needs elevation (`sudo atrium update`, or an elevated shell). Fetches, checks, swaps, restarts the hub and room through the service manager, gates, and rolls back as section 6 settles |
| `--from ./atrium` | the same, from a local file, which must report a commit and a clean tree. This is what `build-deploy.ps1` feeds |

Either way the hub and room restart on the new build and the pty host does not. The package's scriptlets restart
`atrium` and `atrium-room` if they were running and never `atrium-pty`, because restarting it ends every terminal.
Windows cannot replace a file a running program was started from, so the MSI replaces `atrium-pty.exe` only when the
pty host is stopped, and schedules it otherwise. A pty host older than its room is supported, because the room and
host agree a protocol version when they connect (`docs/terminal/ptyhost-protocol.md`). `atrium status` shows
`pty host v0.6.0, room v0.6.1, restart pty when idle`.

`atrium update --rooms` through the hub, in the first draft, depends on section 7 and is not part of this design
until that is settled.

### 10.3 The private scripts, retired

| Today | Becomes |
| --- | --- |
| `build-deploy.ps1` | stays on the build machine, feeding `atrium update --from` |
| `live-common.ps1` `$HubArgs`, `$RoomArgs` | `hub/hub.json` written by `atrium hub config`, and `room.json` |
| `Save-Revert`, `Install-Atrium`, `Test-NewBuildStamped` | `atrium update` |
| `Stop-Room`, `Stop-Hub`, `Start-Jobless` | the service manager, through `atrium stop` and `atrium start` |
| `Wait-RoomAttached`, `Test-RoomServes`, revert in `deploy-batch.ps1` | `atrium start`'s wait and `atrium update`'s gate |
| `deploy-batch.ps1`, `deploy-hub-only.ps1`, `deploy-ready.ps1` | `atrium update` |
| `atrium-deploy.ps1` remote rooms | stays until section 7 is decided |
| `start-atrium*.ps1`, `stop-atrium.ps1` | `atrium start` and `atrium stop`, and boot opt-in on sg4 |
| `refuse-supervised.ps1` | `atrium stop` knows its caller's ancestry (`internal/cli/ancestry*.go`) and refuses there |
| sg4-control and its `-IKnowItKillsMyShell` | open decision 7 |
| `install-live-scripts.ps1` | deleted with the scripts it copies |
| `scripts/atrium-service.*`, `atrium-autostart.ps1` | deleted once the packages register the three services. Their registration logic moves into `packaging/` |

### 10.4 Staged plan

1. **Spike on claudevm only:** ConPTY under an S4U task and under an SCM service (section 8).
2. **The binary under a service manager.** `hub/hub.json` and `atrium hub config`. The exit-code rule. SIGTERM and the
   SCM's stop request as a wind-down (the SCM's stop needs a control handler in the binary, which answers the SCM and
   registers nothing). The room never starting its own pty host when run by a service. The pty host's idle exit
   off. `ensureRoom` and `room --detach` removed. @runtime.
3. **`atrium start`, `atrium status`, and `atrium stop` for all three.** @runtime.
4. **Join does the wiring:** hooks and `mcp.json` at join, the same-machine first-start join. @runtime.
5. **The service definitions in `packaging/`**: three systemd units and the account drop-ins, three plists, the MSI's
   two services and the pty task, `configure-services`. One source for all of them, which the deb, rpm, pkg and MSI
   all use. @fabric.
6. **Releases** with `checksums.txt`, and the SignPath evaluation. @fabric.
7. **`atrium update`** for hand installs, with the gate, ported from `deploy-batch.ps1` and proven against it on
   claudevm before sg4 uses it. Rollback per the answer to section 6. @runtime.
8. **provision-room.ps1** installs the package and joins, over ssh. @fabric.
9. **Docs**: `website/docs/install.md` rewritten to section 5, `packaging.md` updated to the three services, and
   `website/docs/control-mcp.md` fixed (`f-install-wires-atrium-control` step 4). @merge.
10. **Retire the private scripts** on sg4, one at a time, each after its replacement has done one real deploy.
11. Owed regardless: the `install-live-scripts.ps1` test from the bug item, until step 10 deletes the script.

Nothing touches sg3, sgg, m1mini or sg4 until clint says so, machine by machine.

## 11. Open decisions for the operator

1. **Rollback (section 6).** Recommend automatic binary rollback when the gate fails, never an automatic store
   restore, `--restore-store` by hand, and a mark on non-additive migrations so a rollback across one refuses and
   names the migration. Left open until clint has read section 6.
2. **What the hub offers a room (section 7).** Recommend notice only: the hub keeps offering, the room keeps
   checking the hash, and the result is a line in `atrium status` and on the board. A person updates each room with
   its package or `sudo atrium update`. It is the only option that keeps both decision 4 and the hub never owning a
   room. Left open until clint has read section 7.
3. **Windows account for the `atrium` and `atrium-room` services.** An SCM service running as a person needs that
   person's password stored by Windows, re-entered when it changes, and there may be no password at all on an
   account signed in with a Microsoft account (`packaging.md` covers this). The alternative is S4U tasks for all
   three, with no stored password, started with `schtasks /Run` rather than `net start`. Recommend SCM services as
   asked, for a local account such as `localai` whose password does not expire, and S4U tasks for an account that
   has no usable password.
4. **If ConPTY fails the spike under S4U (section 8).** Recommend logon only for `atrium-pty`, with the hub and room
   still services, and auto-logon chosen per machine for a dedicated room machine (sgg, sg3) only if its outages
   continue.
5. **Starting without elevation.** Every start in section 3.3 needs sudo or an elevated shell. The packages could
   grant the owning accounts start and stop on their own three services and nothing else: a polkit rule on Linux,
   the services' and task's permissions on Windows. macOS has no such grant for LaunchDaemons. Recommend granting
   it on Linux and Windows, so day to day `atrium start` and `atrium stop` need no elevation, and documenting sudo on
   macOS.
6. **Hub settings in `hub/hub.json`.** Recommend the file and `atrium hub config` over putting flags in the service
   definitions, so a package upgrade never rewrites them. A wide link needs `--link-advertise` too, and a fresh hub
   listens on loopback only, so it is never reachable from the network by accident.
7. **sg4-control, the second room on sg4.** With no second-room flag it cannot be a service on sg4. Recommend it
   stays as it is, a hand-started room in its own state dir outside the services, until it moves to a machine or VM
   of its own.
8. **The SCM control handler.** A Windows service must answer the SCM within about thirty seconds or be ended.
   Recommend a handler in the binary (`golang.org/x/sys/windows/svc`), which answers the SCM and registers nothing,
   over a third-party wrapper (WinSW, NSSM) that would be one more binary to ship and trust.
9. **Signing after the SignPath evaluation.** Recommend SignPath for the Windows binary and MSI if accepted, cosign
   for `checksums.txt` from the start, and an Apple Developer ID decided separately once a Mac room is used by
   someone other than clint.
10. **A stale pty host after an update.** Recommend never restarting `atrium-pty` from a package or `atrium update`,
    showing the difference in `atrium status`, and offering `atrium stop pty` once it holds no terminals.
