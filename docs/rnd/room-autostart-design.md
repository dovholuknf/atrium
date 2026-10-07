# A room that starts again by itself: after a logoff, and after a reboot

Status: shelved by clint, 2026-09-29. Rooms are started by hand. `scripts/atrium-autostart.ps1` is the one piece that
exists.

clint, 2026-09-29, through @orchestrator: rooms on sg3, sgg (Windows) and m1mini (macOS) should come back by
themselves after a reboot or a logoff. Linux is covered in `docs/release/packaging.md`. This should be one decision
across the three, and it should fit @fabric's `atrium.requirements` (f-005), which does not exist yet and will be
written from @fabric's step log.

**SHELVED by clint, 2026-09-29.** clint starts rooms by hand and @orchestrator reaches them over ssh when needed,
which works for him. No spike and no build. This design is kept as the record of what was found and decided, for
the day reboot survival is wanted. Two parts of it stand on their own and are in use elsewhere: the one preflight
verb (section 4, adopted by `docs/fabric/room-requirements-design.md`), and the pinned state dir (section 2.4, adopted by
`provision-room.ps1 -Restart`).

Nothing here is built, and nothing here is tested against a real room. The standing rule holds: autostart and service
tests run on claudevm, never on sg3, sgg or m1mini.

The facts are @fabric's step log in `docs/backlog/fabric/f-005.md` (on `claude/fabric`), `docs/release/packaging.md`
("Running as a service, on three operating systems"), `scripts/atrium-autostart.ps1`, `scripts/atrium-service.sh`
and `scripts/provision-room.ps1`.

## 1. What is true today

| | Linux | Windows (sg3, sgg) | macOS (m1mini) |
|---|---|---|---|
| What `-Autostart` installs | systemd user unit, `enable --now`, `-Linger` optional | logon task `atrium`, `InteractiveToken`, `LeastPrivilege` | LaunchAgent, `launchctl bootstrap gui/<uid>` |
| Needs admin to install | no (lingering for yourself is allowed on most distros) | no | no |
| Starts after a reboot with nobody at the machine | yes, with linger | no: fires only at an interactive logon (console or RDP). An ssh logon does not fire it, and `schtasks /Run` brings nothing up with nobody logged on (provision's own message, "an Interactive task needs the user logged in at the machine". No exact scheduler error has been captured) | no: the `gui/<uid>` domain exists only during a desktop login. Auto-login would give one, and FileVault hides auto-login |
| Survives a logoff | yes, with linger | no | no |
| State left on the real rooms | none running | sg3: detached room, no task. sgg: unreachable since 09-21, its room dies with its ssh | detached room, no LaunchAgent |

Three constraints from the step log decide the design:

1. **localai's ssh token is filtered.** This is clint's note of 09-21, not observed since: sgg did not answer on port
   22 on 09-29. It matches how Windows treats a local administrator logging in over ssh, which gets a
   medium-integrity token, so nothing that needs admin can be done over ssh on sgg. The documented way round it,
   `LocalAccountTokenFilterPolicy=1`, turns off that filtering for every local admin account, over every remote
   logon, for good. That is too wide a change to make to get one task registered.
2. **With FileVault on, an Apple silicon Mac that reboots waits at the unlock screen.** Before a password is typed
   there, macOS itself has not started: no LaunchDaemon runs, no ssh answers. So FileVault blocks MORE than
   auto-login. It blocks anything unattended after a reboot nobody planned.
3. **The runners keep their credentials in files.** On m1mini claude's credential is `~/.claude/.credentials.json`,
   not the login keychain (f-005, m1mini step 2). codex keeps its sign-in in `~/.codex/`. Clones are made by push
   (`room-git.ps1`), so a room holds no GitHub credential. This matters because the natural fix on every platform
   runs WITHOUT a desktop login, and a locked keychain or an absent Windows credential vault would otherwise break
   sign-in silently. As it stands nothing a room needs lives there, and the preflight in section 4 keeps that true.

## 2. The decision: a room is a process of its user, started the way Linux lingering starts one

Linux already has the right shape. `loginctl enable-linger` starts the user's own service manager at boot, runs the
unit AS the user with the user's home and environment, keeps it through logoff, and needs no GUI. The decision is to
give Windows and macOS the same shape, each in its own spelling, and to stop depending on a desktop login anywhere.

| | Linux | Windows | macOS |
|---|---|---|---|
| Mechanism | systemd user unit + `enable-linger` (as today) | a scheduled task that runs whether or not the user is logged on, as the user, with no stored password (S4U), started at boot and re-checked every 5 minutes | a LaunchDaemon in `/Library/LaunchDaemons` with `UserName` set to the room's account, started at boot, `KeepAlive` on failure |
| Runs as | the user | the user | the user |
| Needs a desktop login | no | no | no |
| Survives logoff | yes | yes | yes |
| Survives an unplanned reboot | yes | yes | only with FileVault off (question 3) |
| Survives a planned reboot | yes | yes | yes, with `sudo fdesetup authrestart` in place of a plain restart |
| The one elevated step | `loginctl enable-linger` if the distro reserves it for root | registering the task (question 1 names the spike that decides whether it is needed) | `sudo` to write and load the LaunchDaemon |

**Why not auto-login on Windows and macOS.** It is the other family, and it works: log the account in at boot, and
the logon task or LaunchAgent that exists today fires. It is rejected as the default because it leaves an unlocked
desktop on the machine's screen, stores the password where auto-logon keeps it (the LSA secret, or
`/etc/kcpassword`, which is obfuscation and not encryption), needs admin anyway, and on macOS is not available while
FileVault is on. It stays as the fallback on Windows if the S4U spike fails (section 6).

**Why not a real service.** `docs/release/packaging.md` already weighed it and the reasons stand: a stored password,
a different session and profile, and service-control code in the binary. The S4U task and the user-scoped
LaunchDaemon are not services. They are the platform's scheduler starting the user's own process.

### 2.1 Windows, spelled out

The task `atrium` changes in three places, written by `scripts/atrium-autostart.ps1` with a new `-Start boot`
(`-Start logon` keeps today's task):

- **Principal:** `<LogonType>S4U</LogonType>`, `<RunLevel>LeastPrivilege</RunLevel>`, the same user. S4U runs the task
  as that user without a password and whether or not anyone is logged on. What it cannot do is reach anything that
  needs the password: network shares as that user, and secrets the user's DPAPI key protects (Credential Manager,
  and anything that stores through it). Section 1 constraint 3 is why that is acceptable for a room.
- **Triggers:** a `BootTrigger`, AND a `TimeTrigger` that repeats every 5 minutes indefinitely, with
  `MultipleInstancesPolicy` `IgnoreNew`. The boot trigger is the fast path. The repeating trigger means a room that
  died (a crash, a kill, an `atrium stop` that was not followed by a start) is back within 5 minutes, and it is the
  fallback if registering a boot trigger turns out to need admin where S4U does not.
- **Everything else as today:** `conhost.exe --headless`, no time limit, three restarts a minute apart, `schtasks.exe`
  rather than the CIM cmdlets.

The header of `atrium-autostart.ps1` says a task that runs whether or not you are logged on "cannot open a pseudo
terminal". That is the claim this design stands on, and it is recorded, not tested. OpenSSH's own server on Windows
runs as a service in session 0 and hands every session a ConPTY, which suggests ConPTY does not need an interactive
desktop. The first spike (section 6) settles it. If the claim holds, Windows falls back to auto-logon plus the logon
task that exists today (section 2.1.1), and the rest of this design is unchanged.

#### 2.1.1 The fallback, if S4U cannot host ConPTY

Written down now so the Windows half is buildable either way. It is built only if the spike fails, and only after
clint answers question 2.

- **Auto-logon with Sysinternals `Autologon.exe`,** run once by a human in an elevated session at the machine or over
  RDP (the one elevated step). It stores the password as an LSA secret, not in the clear `DefaultPassword` registry
  value that the manual registry method uses. `LocalAccountTokenFilterPolicy` is not touched. Where the account has no
  password (a Microsoft account with Hello only), auto-logon is not possible, and the room stays manual.
- **The logon task that exists today** (`InteractiveToken`, `LeastPrivilege`, a logon trigger, `conhost --headless`)
  starts the room in that session. No change to it.
- **A second logon task, `atrium-lock`,** runs `rundll32.exe user32.dll,LockWorkStation` 30 seconds after logon, so
  the console is locked by the time anyone could reach it. A locked session keeps running, and so do its ptys.
- **The `survives` check** for this path reads `HKLM\...\Winlogon\AutoAdminLogon` = `1` and `DefaultUserName` = the
  room's account (readable without admin), both tasks registered, and the room's `--started-by` = `task-logon`
  corroborated as in section 4. `task-logon` satisfies `reboot` only on this path, where auto-logon is on.
- **What it costs, stated:** a stored password on the machine, and a desktop that is unlocked for up to 30 seconds
  after every boot.

**The elevated step on sgg.** Nothing is done over ssh with a filtered token, and `LocalAccountTokenFilterPolicy` is
not touched. `provision-room.ps1 -Autostart` prints ONE command for a human to run once, in an elevated PowerShell at
the machine or over RDP:

```
& "$HOME\.atrium\provision\scripts\atrium-autostart.ps1" -Verb room -Start boot
```

Everything after that, including restarts, upgrades that keep the binary path, and removal, works over ssh
without elevation, because it only runs or ends a task that already exists. On sg3, where `claude` is not an
administrator and nothing is filtered, the same command may work over ssh directly. The spike says which.

### 2.2 macOS, spelled out

`scripts/atrium-service.sh install --boot` writes `/Library/LaunchDaemons/io.github.dovholuknf.atrium.room.plist`
with `sudo`, from the same template as the LaunchAgent, with these differences:

- `UserName` and `GroupName` set to the room's account, so it runs as `claude` and never as root.
- `EnvironmentVariables` sets `HOME`, `USER` and `LOGNAME`, because a daemon gets none of the session's.
- **No login shell at boot.** A login shell's startup files may assume a terminal, prompt, start an ssh agent or wait
  on a keychain, and at boot there is none of those, so a room could hang before it starts. Instead, at install,
  `atrium-service.sh` runs the login shell ONCE from the install session, bounded to 10 seconds and with no terminal
  (`$SHELL -l -c 'printf %s "$PATH"' < /dev/null`), and writes the PATH it printed into the plist's
  `EnvironmentVariables`. The daemon then runs `atrium room` directly with that PATH, which is what the Linux unit
  already does with its captured PATH. The `survives` check compares the captured PATH with the login shell's current
  one and warns on drift, and a reinstall recaptures it. (The LaunchAgent keeps its login-shell start, because it
  starts inside a desktop session where one is safe.)
- `RunAtLoad` true, `KeepAlive` `SuccessfulExit: false`, `ExitTimeOut` 30, as the LaunchAgent has them.
- `launchctl bootstrap system <plist>` loads it now, and it loads by itself at every boot.

The LaunchAgent stays for a person's own desktop atrium, where a GUI session is the point. A room gets the daemon.
The two must never both be installed for one database. `install --boot` boots the agent out first, and `status`
refuses to call a machine healthy when it finds both.

**FileVault.** Planned restarts (software updates, a reboot someone asks for) go through `sudo fdesetup
authrestart`, which unlocks the disk once, by itself, at the next boot. `atrium-service.sh` gains `reboot` that does
exactly this, so the room's own "restart the machine" path never strands it at the unlock screen. An unplanned
reboot (power loss, a panic) waits for a human while FileVault is on. That is the platform's rule, and only turning
FileVault off on m1mini removes it. Question 3.

### 2.3 Linux

No change to the mechanism. Two things change around it: `-Linger` becomes the default when `-Autostart` is given
for a room (a room that stops at logoff is not what anyone asked for), and the preflight in section 4 runs there too.

### 2.4 Every platform: the supervised start must find the same join

`atrium room join <string> --no-run` writes the room's key, certificate and `room.json` under `StateDir()/room`, and
`atrium room` reads them from there (`internal/cli/atrium_defaults.go`, `roomDir`). `StateDir()` is
`$WORKTREE_ROOT/hub` when `WORKTREE_ROOT` is set, and `~/.atrium` otherwise (`internal/daemon/daemon.go`). So a
supervised start whose environment differs from the join's, a `WORKTREE_ROOT` present at join and absent under S4U,
or a `HOME` launchd did not set, starts a room that finds no `room.json`, does not reconnect as that room, and looks
healthy to the scheduler.

So every registration pins the state it was joined with, and none relies on the environment it happens to get:

- It records the resolved state directory at install time (read from the install shell with the same rule), and
  sets `WORKTREE_ROOT` to match it in the registration's environment (or clears it when the join used `~/.atrium`),
  along with `HOME` on Unix and `USERPROFILE` on Windows.
- It passes `--db` explicitly, as today, so the database follows the same pin.
- The `survives` check (section 4) fails when `<state dir>/room/room.json` is missing, naming the directory it looked
  in, so a wrong pin is found on the first check and not at the first reboot.

A `--state-dir` flag on `atrium room` would say this in one argument instead of an environment variable. It is
worth adding, and until it exists the environment pin is the contract. @runtime.

## 3. One restart verb

f-005 found that provision has no `-Restart`, and a PATH change only lands at the room's next start. With a
supervisor on every platform, a restart is the supervisor's, and it is always a wind-down first:

| | stop | start | restart |
|---|---|---|---|
| Linux | `systemctl --user stop atrium` | `systemctl --user start atrium` | `systemctl --user restart atrium` |
| Windows | `atrium stop`, then `schtasks /End /TN atrium` | `schtasks /Run /TN atrium` | stop, then start |
| macOS | `atrium stop`, then `launchctl kill TERM system/<label>` | `launchctl kickstart system/<label>` | `launchctl kickstart -k system/<label>` after `atrium stop` |

`atrium stop` goes first everywhere, because a kill is not a stop (CLAUDE.md, daemon resilience 5).

**What stop means on a supervised room.** A supervised room is meant to be running, so the rule is one and the same
everywhere:

- **`service stop` is sticky.** It keeps the room down until `service start`, across reboots too. Linux: `systemctl
  --user disable --now atrium`. Windows: `atrium stop`, then `schtasks /Change /TN atrium /DISABLE`, which
  disables the boot trigger and the repeating trigger together. macOS: `atrium stop`, then `launchctl disable
  system/<label>`. `service start` enables it again and starts it. `restart` never changes whether it is enabled.
- **A bare `atrium stop` is a wind-down the supervisor may undo.** On Windows the repeating trigger brings the room
  back within 5 minutes. On Linux and macOS a clean exit stays down until the next boot, because both restart on
  failure only. Different on purpose: the repeating trigger is Windows' only way back from a crash. So `atrium stop`,
  when the room it stops reports a supervised start (its `--started-by`, section 4), prints which of the two will
  happen and names `service stop` for staying down.
- `-Remove` unregisters, as today. These become
`scripts/atrium-service.ps1 restart` and `atrium-service.sh restart`, which exist today for the logon task and the
LaunchAgent, and `provision-room.ps1 -Restart`, which runs the platform's verb over ssh. None of them needs
elevation once the one elevated step is done.

## 4. Two entries for `atrium.requirements`

@fabric's file declares what a room needs to take a project's work, and a command checks a room against it. This
design adds two entries to it. Each has a check that runs over ssh without elevation, and a fix that says whether it
needs a human.

```yaml
room:
  survives: reboot          # none | logoff | reboot
  runner_auth: [claude]     # every runner a worker of this project may start, e.g. [claude, codex]
```

@fabric's file calls this requirement a "durable start" and leaves a slot for it. `survives` is that slot, and
`runner_auth` sits beside it. The requirements design points here for what each value means, and this design points
there for the file's format.

### `survives`

| Value | Holds when |
|---|---|
| `none` | a room answers now (today's detached room) |
| `logoff` | the room is started by the platform's supervisor as the user, not by a shell that will close |
| `reboot` | `logoff`, and the supervisor starts it at boot without a desktop login: linger on Linux, the S4U boot task on Windows, the LaunchDaemon on macOS. On macOS with FileVault on, the check says `reboot (planned only)` and fails a strict `reboot` |

The check reads the registration (`systemctl --user is-enabled` and `loginctl show-user -P Linger`, `schtasks /Query
/XML` for the LogonType and triggers, `launchctl print system/<label>`), and never tests it by rebooting. The fix is
`provision-room.ps1 <room> -Autostart`, with the one elevated command printed when it is needed, naming who has to
run it and where.

### `runner_auth`

The preflight that makes a headless start safe. For each runner listed, run its own non-interactive sign-in check
AS THE SUPERVISOR RUNS IT, not from the ssh session: an S4U task on Windows, the LaunchDaemon's user context on
macOS, the user unit on Linux. The ssh session has the user's full token and, on macOS, possibly an unlocked keychain,
so a check from ssh can pass while the room's runners cannot sign in. It is the quiet failure `packaging.md` opens
with: the room is up, and every session it starts is useless.

How the check runs in that context: the room's own API gets a verb, `POST /v1/preflight`, that runs each runner's
status command (`claude auth status`, `codex login status`) from inside the room process and returns what each
printed. The same verb answers @fabric's tool and env questions (`docs/fabric/room-requirements-design.md`, question 3):
the body names what to check (`runner_auth`, `tools`, `env_present`), and the caller names KEYS, never commands.
The room maps each key to a fixed command from a table in the binary, and an unknown key gets a PATH lookup and no
exec, so the verb can never run what a caller sends. `env_present` answers booleans and never a value. Each command
is bounded (10s, 60s total) with capped output, and the verb is on the human listener only. The requirements command calls it over the hub, so the answer comes from exactly the context the
workers will have. The fix is the runner's own login, run once by a human (`ssh -t <room> claude login`), and a
credential that lives in the keychain or in Credential Manager is reported as a failure with that reason.

**The live room must be the supervised one, or the answer is worth nothing.** A room started by hand from ssh, or a
detached room left over from before autostart, runs with the ssh session's token and environment. A preflight
answered by that process passes for credentials the supervised start may not reach, which is the false pass this
check exists to prevent (Mercurius `s_BWyvK9iBa2ht`, rounds 1 to 3).

**The marker is an argument with a nonce, never an environment variable.** An environment variable is inherited by
every child and every debugging shell, and a parent-process shape is not stable enough to prove anything on Windows.
So:

- At install, the registration generates a random nonce and writes it in two places: in the registration's own
  command line, as `atrium room --started-by <kind>:<nonce> ...`, and in `<state dir>/room/supervisor.json`, 0600,
  with the kind and the registration's name. `<kind>` is `systemd-user`, `task-s4u`, `task-logon`, `launchdaemon` or
  `launchagent`. Linux has one kind, because linger is a property of the account and can change after install, so it
  is read at check time rather than baked in.
- The room keeps the flag's value in memory only. It never puts it in its environment or passes it to anything it
  spawns, so no child of a supervised room can claim to be one.
- `POST /v1/preflight` returns the room's pid, its `--started-by` value, and each runner's answer.
- The requirements check passes `runner_auth` only when all of these hold:
  1. the returned nonce equals the one in `supervisor.json`, AND equals the one in the registration as the platform
     holds it now (`systemctl --user cat atrium` ExecStart, `schtasks /Query /TN atrium /XML` Arguments, the plist's
     ProgramArguments). A reinstall writes a new nonce, so a room still running from the old registration fails.
  2. where the platform reports a pid, it is the room's: `systemctl --user show atrium -p MainPID`, the `pid` in
     `launchctl print system/<label>`. Windows reports none, so the nonce in the task's XML plus the task's status
     `Running` is the whole Windows evidence, and nothing reads process parents.
  3. the kind satisfies the `survives` level: `reboot` needs `task-s4u` or `launchdaemon`, or `systemd-user` together
     with `loginctl show-user -P Linger` answering `yes` at check time.
- Anything else reports `runner_auth: unverified, the live room was not started by its registration (<why>)`, and
  the fix is `provision-room.ps1 <room> -Restart`, which restarts it through the supervisor, after which the check
  runs again.

A person can still start a room by hand with a copied nonce. That is a deliberate act and not the accident this
guards against. It is not a security boundary, and nothing trusts the marker for anything but this check.

Atrium holds no credential for either check. It runs commands that already have one, by name, which is the rule
`CLAUDE.md` states for overlays and `docs/runtime/scm-design.md` for `fetch`.

## 5. What changes where

| File | Change | Owner |
|---|---|---|
| `scripts/atrium-autostart.ps1` | `-Start logon\|boot`. `boot` writes the S4U principal, the boot and repeating triggers, and says in its output whether it needed elevation | @fabric |
| `scripts/atrium-service.sh` | `install --boot` (LaunchDaemon, sudo), `reboot` (authrestart on macOS), `status` refusing an agent and a daemon together, sticky `stop`, and a header and helper names that keep the LaunchAgent and LaunchDaemon paths visibly apart | @fabric |
| `scripts/atrium-service.ps1` | sticky `stop` (`/DISABLE`), `start` that enables | @fabric |
| every registration | pins `WORKTREE_ROOT`, `HOME` or `USERPROFILE`, and `--db` to the join's state (section 2.4) | @fabric |
| `cmd/atrium` | `room --state-dir`, `room --started-by` kept in memory and never passed on, and `atrium stop` saying what the supervisor will do | @runtime |
| `scripts/provision-room.ps1` | `-Autostart` means `boot` for a room, prints the one elevated command when it is needed, `-Linger` default on Linux, `-Restart` | @fabric |
| `internal/api` | `POST /v1/preflight`, bounded in time and output like a source, returning the pid and the `--started-by` value | @runtime |
| every registration (task XML, plist, unit) | a fresh nonce in `--started-by` and in `<state dir>/room/supervisor.json` | @fabric |
| `atrium.requirements` and its check command | the two entries in section 4 | @fabric, in the file it is designing |
| `docs/release/packaging.md` | the Windows and macOS trade statements change from "stops when you log out" to the boot start, with its costs | @merge |

`packaging.md` currently says "Windows has no enable-linger" and "macOS has no equivalent of enable-linger". If the
spikes pass, both sentences become wrong, and the packaging doc has to say so, since it is the record of why each
platform starts the way it does.

## 6. Spikes, on claudevm only, before anything is built

1. **Windows, ConPTY under S4U.** On claudevm: register the `boot` task as a non-admin user, reboot, log nobody in,
   and from the hub's board attach to a room card and run a claude session through one tool call. Record whether
   registering needed elevation (with a boot trigger, and with only the repeating time trigger), whether the
   profile loaded (`$env:USERPROFILE`, HKCU PATH), whether claude signed in from its file, and whether an inbound
   listener was silently blocked by the firewall (no prompt can appear in that session). This one spike decides the
   Windows half. If ConPTY fails there, Windows falls back to auto-logon with the existing logon task, and the
   spike's outcome line says so: **if S4U cannot host ConPTY, the auto-logon fallback is written up (how it is set
   without `LocalAccountTokenFilterPolicy`, and how the desktop is locked right after logon) before any Windows boot
   autostart is built.**
2. **Windows, logoff.** The same room keeps running when an interactive session of that user logs off, and when its
   ssh session closes.
3. **macOS, the user-scoped LaunchDaemon.** There is no macOS claudevm. It needs a Mac that is not a real room, or
   clint's yes to try it on m1mini (question 4). Check the login-shell PATH, a claude session through one tool call,
   `atrium stop` then `kickstart`, and that `fdesetup authrestart` comes back unattended.
4. **Linux.** Nothing new to prove. The existing selftest covers linger.

## 7. Staged plan

1. The spikes above. @fabric runs them on claudevm, since it owns provisioning and the standing rule.
2. macOS `install --boot`, Linux's linger default, and `-Restart` on every platform. On Windows, CONDITIONAL on
   spike 1: `-Start boot` if S4U hosts ConPTY, otherwise the fallback of section 2.1.1, and only once clint has
   answered question 2. No Windows boot autostart is built before the spike has a result. @fabric.
3. The runner-auth preflight endpoint. @runtime. It is useful on its own, with or without autostart, because it is
   the only honest answer to "can this room's workers sign in".
4. The two `atrium.requirements` entries, in @fabric's file when it is written.
5. `packaging.md` updated to the new trades. @merge.

Nothing touches a real room until clint says so, room by room.

## 8. Open questions for clint

1. **Windows, if S4U works but its registration needs admin:** is one elevated command, run by you at sgg's console
   or over RDP, acceptable? The alternative, `LocalAccountTokenFilterPolicy=1`, lets every local admin account on sgg
   act as admin over ssh, and this design does not recommend it.
2. **Windows, if S4U fails the spike:** auto-logon plus the existing logon task (an unlocked desktop at sgg's and
   sg3's screens, and the password stored by auto-logon), or rooms on Windows stay manual?
3. **m1mini and FileVault:** keep FileVault on, and accept that an unplanned reboot waits for you while planned ones
   go through `authrestart`? Or turn FileVault off on a machine whose only job is to be a room?
4. **Where the macOS spike runs:** on m1mini, against the standing rule, or on a spare Mac?
5. **The default for a room:** should `-Autostart` mean `boot` for every room from now on, or stay opt-in per room?
6. **Decided (2026-09-29, @orchestrator):** `runner_auth` is `[claude]` now, and becomes `[claude, codex]` once
   @fabric's codex smoke on m1mini passes.
