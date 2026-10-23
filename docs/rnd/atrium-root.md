# One root for everything atrium keeps on a machine

Status: proposed, not built.

Origin: asked for by clint through @orchestrator-sg4-control, 2026-10-23. Everything atrium keeps on a machine lives
under one folder, for example `D:\atrium`. The hub in `D:\atrium\hub`, each room in `D:\atrium\room\<name>`, the
binaries, and the address file a hook uses to find atrium. A new hub and room run under it on their own ports, beside
today's atrium, without taking the old sessions' hooks.

## The answer

- One setting, `--root` on every subcommand and `ATRIUM_ROOT` in the environment. With it set, every path atrium
  derives today from the home folder, `WORKTREE_ROOT` or `%LOCALAPPDATA%` is derived from the root instead. A flag
  naming one path (`--dir`, `--db`, `--location-file`) still wins for that path. Without it nothing changes.
- A binary at `<root>\bin\atrium.exe` finds its root from its own location, through a marker file `<root>\root.json`.
  That is how a hook in `settings.json` reaches the right atrium with no environment at all.
- A session a room launched keeps finding its own room the way it does today: the room puts `ATRIUM_LOCATION` in the
  runner's environment, and the hook reads it before anything else. Two atriums never share an address file.
- The new atrium runs on 7877, 7878, 7879 and 7881 while the old one keeps 7777, 7778, 7779 and 7781. Cutover moves
  the hub's store, CA and git mirror into the root and back onto 7778 and 7779, so remote rooms do not change.
- Defender excludes the root by path. Firewall prompts stop because tests bind loopback, Go's temporary folder sits
  under the root, and `NotifyOnListen` is off as the backstop.

## 1. Every path atrium uses today

Paths as the code builds them. "State dir" is `cardroot.StateDir`: `$WORKTREE_ROOT\hub` when `WORKTREE_ROOT` is set,
else `~/.atrium`. On sg4 the live scripts pin most of it by flag (`scripts/live/live-common.ps1:17-41`).

### The state dir and the stores

| What | Today | Where in code |
| --- | --- | --- |
| state dir | `$WORKTREE_ROOT\hub`, else `~/.atrium` | `internal/cardroot/cardroot.go:33-40`, `internal/daemon/daemon.go:361` |
| room database | `<state dir>\atrium.db`, or `--db` | `internal/daemon/daemon.go:363-373`, `internal/cli/atrium_defaults.go:38-40` |
| hub dir (CA, keys, store) | `<state dir>\hub`, or `--dir` / `--atrium-dir` on `run` | `internal/cli/atrium_defaults.go:27-29`, `internal/cli/atrium_run.go:77` |
| hub store | `<hub dir>\hub.db`, or `--db` | `internal/cli/atrium_run.go:150`, `internal/cli/atrium_rooms.go:213` |
| hub backups | `<hub dir>\backups`, or beside `--db` | `internal/cli/atrium_rooms.go:218-224` |
| hub CA and certificates | `ca.crt`, `ca.key`, `hub.crt`, `hub.key` in the hub dir | `internal/link/certs.go:80-152` |
| room dir (key, certificate, `room.json`) | `<state dir>\room`, or `--dir` on `room` | `internal/cli/atrium_defaults.go:32-34`, `internal/cli/roomrun.go:167` |
| room certificate | `room.crt`, `room.key`, `ca.crt`, `room.json` in the room dir | `internal/link/certs.go:562-618` |
| room log, restart log | `<room dir>\room.log`, `<room dir>\restart.log` | `internal/cli/atrium_runroom.go:35,186`, `internal/cli/roomrestart.go:48,101` |
| scrollback carried over | `<db dir>\scrollback` | `internal/daemon/carryover.go:60,303-305` |
| last database opened | `lastdb.json` beside the address file | `internal/daemon/whichdb.go:57`, `internal/cli/control.go:545` |
| item test logs | `<state dir>\items\<id>\tests` | `internal/daemon/record.go:131`, `internal/link/item_brief.go:22` |
| keepalive hook file | `<state dir>\keepalive-block-tools.json` | `internal/daemon/keepalive.go:625` |
| worktree setup hooks | `~/.atrium/hooks` unless set | `internal/api/newwork.go:44,266-270` |
| room lock | `~/.atrium/room.lock` | `internal/roomspec/apply.go:54`, `internal/cli/roomsetup.go:120` |
| pty host channel | pipe `\\.\pipe\atrium-ptyhost-<hash of state dir and user>` | `internal/ptyhost/channel.go:11-36` |
| preview daemon | `.atrium\preview` inside the folder previewed | `internal/cli/preview.go:62,149,186` |

### The address file

| What | Today | Where in code |
| --- | --- | --- |
| `ATRIUM_LOCATION` | names the file outright, read first | `internal/daemon/whereami.go:103-106` |
| per-user default | `%LOCALAPPDATA%\atrium\daemon.json` on Windows, `$XDG_RUNTIME_DIR/atrium` elsewhere | `internal/daemon/whereami.go:113-134` |
| the same, duplicated | `%LOCALAPPDATA%\atrium\daemon.json`, kept in step by a test | `internal/claudeconf/whichexe.go:131-149` |
| shared copy | `ATRIUM_SHARED_LOCATION`, else `%WORKTREE_ROOT%\atrium\daemon.json`, `-` is none | `internal/daemon/whereami.go:153-170` |
| room's file | the default, or `<room dir>\daemon.json` with `--isolated`. A room never writes the shared one | `internal/cli/roomrun.go:617-621,346-357`, `internal/cli/atrium_defaults.go:45-51` |
| `atrium daemon` | `--location-file`, else the default | `internal/cli/cli.go:190` |
| told to runners | `ATRIUM_LOCATION` set in a runner's env when the daemon has its own file | `internal/daemon/whereami.go:463-467` |
| read order | `ATRIUM_LOCATION` or per-user, then shared | `internal/daemon/whereami.go:207-230` |
| hook address | `--url`, `ATRIUM_HUB_URL`, the address file, `127.0.0.1:7777` | `internal/cli/join.go:119-131` |
| board address | `--url`, `ATRIUM_BOARD_URL`, the address file, `localhost:7778` | `internal/cli/stop.go:60-76` |

### Card homes, clones and worktrees

| What | Today | Where in code |
| --- | --- | --- |
| `card_root` | the setting, else `$WORKTREE_ROOT\atrium-cards`, else `~/atrium/cards` off Windows, else none | `internal/cardroot/cardroot.go:46-63` |
| card home env | `TMP`, `TEMP`, `TMPDIR` in the home, `ATRIUM_HOME` the home | `internal/daemon/cardhome.go:270-276` |
| card handoff | `context_handoff_dir`, else `<home>\.atrium\handoff.md`, else `<temp>\atrium\handoffs` | `internal/daemon/newcontext.go:238-240` |
| room clones `git_root` | the setting, else `~/git` | `internal/gitsync/room.go:12-23` |
| scm folder `git.scm_root` | the setting, else `~/git` when it exists | `internal/gitsync/scm.go:55-80` |
| scratch cards | `<scm root>\scratch` | `internal/api/openkinds.go:440-446` |
| PR worktrees | `<worktree_root>\<org>\<repo>\<branch>`, else `<scm>\worktrees\<host>` | `internal/api/prworktree.go:53,284-288` |
| `reviews_root` | room setting | `internal/store/prs.go:35` |
| `WORKTREE_ROOT` itself | read for the state dir, card root and shared address | `internal/cardroot/cardroot.go:34,52`, `internal/daemon/whereami.go:165` |

### The hub's git store

| What | Today | Where in code |
| --- | --- | --- |
| hub git store | hub setting `git.store`, else `<hub dir>\git` | `internal/hubstore/settings.go:337,346-356` |
| repos in it | `<hub dir>\git\<name>.git` | `internal/gitsync/hub.go:185`, `internal/gitsync/store.go:118` |

### Ports

| Port | What | Where in code |
| --- | --- | --- |
| 7777 | room agent listener, and where a hook with no address file posts | `internal/cli/atrium_defaults.go:22`, `internal/cli/join.go:130`, `internal/cli/cli.go:177` |
| 7778 | hub board, control MCP, `atrium daemon` board | `internal/cli/atrium_defaults.go:19`, `internal/cli/cli.go:179` |
| 7778 hardcoded | MCP relay, restart, ssh proxy, `--url` help text | `internal/cli/mcp_relay.go:36`, `internal/cli/control.go:328`, `internal/cli/sshproxy.go:50`, `internal/cli/stop.go:75`, `internal/link/control_mcp.go:2531` |
| 7779 | hub room link | `internal/cli/atrium_defaults.go:20`, `internal/cli/atrium_run.go:72` |
| 7781 | room's own board | `internal/cli/atrium_defaults.go:21`, `internal/cli/roomrun.go:169` |
| 7787, 7791 | sg4-control's agent and board, by flag | `scripts/live/start-atrium-control.ps1:32-33` |

### What stays out of the root

Other tools' files atrium writes into on their own terms: `~/.claude/settings.json`
(`internal/claudeconf/hooks.go:214`), `~/.codex/hooks.json` (`internal/claudeconf/target.go:154`), `~/.ssh/config`
(`internal/cli/sshconfig.go:32`), the zrok environment (`internal/daemon/overlay_zrok.go:83`) and ziti identities. The
root holds atrium's state, not theirs. The control MCP every session uses is registered in the account's Claude
config, not by atrium, and the relay reads `ATRIUM_HUB_MCP` when set, else 7778 (`internal/cli/mcp_relay.go:36,52-56`).
Nothing in the room sets `ATRIUM_HUB_MCP` today.

## 2. The root setting

### Precedence

For every path in section 1, most explicit first:

1. The flag naming that path: `--db`, `--dir`, `--atrium-dir`, `--location-file`, or a stored setting such as
   `card_root`, `git.scm_root`, `git_root` or `git.store`.
2. `--root <path>`, a persistent flag on the root command, so `run`, `room`, `hook`, `rooms`, `backups` and the rest
   all take it.
3. `ATRIUM_ROOT`.
4. The marker: when the running binary sits at `<X>\bin\atrium.exe` and `<X>\root.json` exists, the root is `<X>`.
5. Today's behaviour, unchanged: `WORKTREE_ROOT`, the home folder, `%LOCALAPPDATA%`.

With a root from steps 2 to 4, `WORKTREE_ROOT` is never read for atrium's own state, and the per-user and shared
address files under `%LOCALAPPDATA%` and `%WORKTREE_ROOT%` are neither read nor written. That one rule is what keeps
the new atrium off the old one's files without a second flag to remember.

The name is `ATRIUM_ROOT` because `ATRIUM_HOME` is taken: it is the card's home (`internal/daemon/cardhome.go:274`).
`testguard.Scrub` already removes every `ATRIUM_*` variable, so tests stay off the root with no change
(`internal/testguard/testguard.go:31-36`).

### The marker

`<root>\root.json`, written by the first `atrium run --root` or `atrium room --root` that finds none:

```json
{"version": 1, "hub": "127.0.0.1:7878", "room": "sg4"}
```

It says this folder is a root, and which room under it answers a hook that was told nothing. It holds no secret.

### How a hook finds the right atrium when two run

A hook process is `atrium hook ...` started by Claude Code from the command in `settings.json`, with the session's
environment. The order becomes:

1. `--url`, then `ATRIUM_HUB_URL`. Unchanged.
2. `ATRIUM_LOCATION`. Every room already sets it for the runners it launches when it keeps its own file, and a room
   under a root always does. So a session the new room started reports to the new room, and a session the old room
   started reports to the old one, whichever binary the hook line names.
3. The root's address file, `<root>\room\<name>\daemon.json` for the room named in `root.json`, when the hook has a
   root from `ATRIUM_ROOT` or from its binary's marker.
4. Today's per-user then shared file, then 7777.

Step 2 is what makes side by side safe today. The old binary in `settings.json` already honours `ATRIUM_LOCATION`, so
a session in the new room whose hook runs the old binary still reaches the new room. Step 3 matters after cutover,
when `settings.json` names `<root>\bin\atrium.exe` and a session nobody launched (a claude typed into a plain
terminal) has to land somewhere.

What a room under a root sets in each runner's environment: `ATRIUM_LOCATION` (its own file), `ATRIUM_ROOT`, and
`ATRIUM_HUB_MCP` pointing at its hub's `/_hub/mcp`. The last one is new and needed, because the relay otherwise dials
7778 and a session in the new room would talk to the old hub.

### Rules the new atrium keeps

- A room under a root never writes `%LOCALAPPDATA%\atrium\daemon.json` or `%WORKTREE_ROOT%\atrium\daemon.json`.
- No subcommand run with a root writes `~/.claude/settings.json` except an explicit `atrium hook install`.
- The pty host channel needs nothing: its name is a hash of the state dir, so a new state dir is a new pipe.
- The live scripts match a process by image directory and subcommand (`scripts/live/live-common.ps1:8-15`), so
  `<root>\bin` is never matched by a deploy of `.atrium\bin`.

## 3. The layout

```
D:\atrium\
  root.json                      the marker: which room answers an untold hook
  bin\
    atrium.exe                   hub, room, hooks, CLI
    atrium-pty.exe               the pty host's copy, beside it as today
    atrium.next.exe              a staged build, swapped in by two renames
  scripts\                       the live scripts, today in ~/.atrium2/scripts
  hub\
    hub.db  backups\             the hub store and its snapshots
    ca.crt ca.key hub.crt hub.key
    git\<name>.git               the hub's git mirror (git.store unset)
    logs\                        hub.out, hub.err, the live scripts' log
  room\
    <name>\
      daemon.json                this room's address file
      atrium.db  scrollback\  lastdb.json
      room.crt room.key ca.crt room.json
      room.log  restart.log  logs\
      items\  hooks\  room.lock  keepalive-block-tools.json
      git\<host>\<owner>\<repo>  the scm folder and clones (git.scm_root, git_root)
      git\scratch\  git\worktrees\<host>\
      cards\<id>\                card homes (card_root), each with its own tmp\
      worktrees\                 WORKTREE_ROOT for this room's runners
  go\
    cache\                       GOCACHE
    tmp\                         GOTMPDIR, where go test links its binaries
```

Each room keeps everything under its own folder, so two rooms under one root never share a database, a pipe, a clone
or an address file, and removing a room is removing its folder.

`card_root` under the room folder passes `cardroot.Check` today: it is not the home, not a drive root, not inside the
state dir. It must not be inside `git.scm_root`, which is why clones are `room\<name>\git` and homes are
`room\<name>\cards`, siblings.

## 4. Starting beside the old one, then moving over

The old atrium stays exactly as it is: `.atrium\bin\atrium.exe`, `.atrium2\hub` on 7778 and 7779, claude-sg4 on 7777
and 7781, sg4-control on 7787 and 7791.

### Phase 1: the new atrium beside the old

Run from a shell atrium does not supervise, as the `claude` account.

```powershell
New-Item -ItemType Directory -Force D:\atrium\bin, D:\atrium\go\cache, D:\atrium\go\tmp | Out-Null
Copy-Item D:\git\github\dovholuknf\atrium\build.claude\atrium.exe D:\atrium\bin\atrium.exe

$env:ATRIUM_ROOT = 'D:\atrium'
D:\atrium\bin\atrium.exe run --no-room --addr 127.0.0.1:7878 --link 127.0.0.1:7879
D:\atrium\bin\atrium.exe rooms add sg4
D:\atrium\bin\atrium.exe room join '<join string>' --http 127.0.0.1:7881 --agent 127.0.0.1:7877
```

The room is called `sg4`, not `claude-sg4`, so its `Host sg4.atrium` line in `~/.ssh/config` cannot replace the old
room's. Then check that nothing moved:

```powershell
Get-Content $env:LOCALAPPDATA\atrium\daemon.json     # still 7777 and the old binary
Get-Content D:\atrium\room\sg4\daemon.json           # 7877
Invoke-RestMethod http://127.0.0.1:7781/v1/health    # old room
Invoke-RestMethod http://127.0.0.1:7881/v1/health    # new room
```

Launch one card in the new room and one in the old. Each card's activity must arrive on its own board only. A card in
the new room shows `ATRIUM_LOCATION=D:\atrium\room\sg4\daemon.json` in its environment.

### Phase 2: the hub moves into the root

The backlog, reports and the rooms list are on the hub, and remote rooms (m1mini, sg3, sg4-wsl) trust its CA and dial
7779. So the hub moves with its CA and its address:

1. Stop the trial hub and move `D:\atrium\hub` aside to `D:\atrium\hub.trial`.
2. Take a snapshot of the old hub (`atrium backups`), stop it, and copy `.atrium2\hub\ca.*`, `hub.*` and `git\` into
   `D:\atrium\hub`. Put the snapshot back with `atrium backups restore <snapshot> --atrium-dir D:\atrium\hub`.
3. Start the new hub on 7778 and 7779 with the old hub's other flags (`--link 0.0.0.0:7779`, `--link-advertise`, the
   ziti transports, `--board`, `--board-views`). Edit `D:\atrium\root.json` to `"hub": "127.0.0.1:7778"`.
4. Remote rooms reconnect with nothing changed. `sg4` was joined to the trial CA, so it is added again
   (`atrium rooms add sg4`, `atrium room join`), keeping its store.

### Phase 3: the room moves

1. New work goes to `sg4`. claude-sg4 drains: its cards finish or hand off.
2. When claude-sg4 holds nothing live, stop it with the live script.
3. `D:\atrium\bin\atrium.exe hook install`, which rewrites `~/.claude/settings.json` to the new binary. From here a
   session nobody launched finds `sg4` through the marker.
4. Optionally restart `sg4` on 7777 and 7781 so the documented defaults hold again.
5. Leave `.atrium`, `.atrium2` and `%LOCALAPPDATA%\atrium` in place for a week, then delete them.

sg4-control moves the same way later, to `D:\atrium\room\sg4-control`, keeping `--isolated` and its own binary copy.

## 5. Defender exclusions

From an elevated PowerShell. Use the literal root. `$env:LOCALAPPDATA` in an admin shell is the admin's profile, which
is the trap `docs/user-guide.md` Pattern 13 describes, and one root on `D:\` removes it.

```powershell
Add-MpPreference -ExclusionPath 'D:\atrium'
```

That one path covers the stores, the binaries, clones, card homes, worktrees, `GOCACHE` and `GOTMPDIR`, which is the
point of the root. Process exclusions are broader, since they skip every file the process opens anywhere, so they are
for the atrium binaries only:

```powershell
Add-MpPreference -ExclusionProcess 'D:\atrium\bin\atrium.exe', 'D:\atrium\bin\atrium-pty.exe'
```

Not recommended, and listed so the choice is explicit. Test binaries by folder, which covers every file they open:

```powershell
Add-MpPreference -ExclusionProcess 'D:\atrium\go\tmp\*'
```

Read back, and take back:

```powershell
(Get-MpPreference).ExclusionPath
(Get-MpPreference).ExclusionProcess
Remove-MpPreference -ExclusionPath 'D:\atrium'
Remove-MpPreference -ExclusionProcess 'D:\atrium\bin\atrium.exe', 'D:\atrium\bin\atrium-pty.exe'
```

An excluded folder that agents write into is a folder nothing scans. That is the trade the existing
`scripts/room-defender.ps1` already makes for `GOCACHE`, `GOTMPDIR`, `build.claude` and the worktree root, and the
root is the same set in one place. `room-defender.ps1` learns `-Root` and excludes it instead of the four.

## 6. Firewall popups

### Why they come

The Windows firewall prompts when a program with no rule listens on a non-loopback address. `go test` links each test
binary to a new random folder, `<GOTMPDIR or TEMP>\go-build<random>\b001\<pkg>.test.exe`, and since Go 1.24 `go run`
and `go tool` binaries are kept in `GOCACHE`, `%LOCALAPPDATA%\go-build` by default. A rule names one program path, so
no rule ever matches the next build, and every wide listen raises a new prompt. A loopback listen raises none.

In this repo the helpers already bind loopback (`internal/daemon/daemon_integration_test.go:21-28`,
`internal/cli/fakerunner_integration_test.go:647-660`, `internal/cli/preview.go:263-274`). The tests that name
`0.0.0.0` or `:7781` pass them as strings to address parsers and never bind them. The prompts come from code that
listens on `:port`, from other repos agents build, and from anything that defaults to every interface.

### Go's folders under the root

The room sets these in every runner's environment, from the root, unless the runner's harness row says otherwise:

```
GOCACHE=D:\atrium\go\cache
GOTMPDIR=D:\atrium\go\tmp
```

For a human shell, the same for the account, which also changes what the old atrium's runners see:

```powershell
New-Item -ItemType Directory -Force D:\atrium\go\cache, D:\atrium\go\tmp | Out-Null
go env -w GOCACHE=D:\atrium\go\cache GOTMPDIR=D:\atrium\go\tmp
go env GOCACHE GOTMPDIR
```

`GOTMPDIR` must exist before `go` runs, or every build fails. The first build after the move refills the cache.

### Turn the prompt off

Elevated:

```powershell
Set-NetFirewallProfile -Profile Domain,Private,Public -NotifyOnListen False
Get-NetFirewallProfile | Select-Object Name, Enabled, DefaultInboundAction, NotifyOnListen
```

With `DefaultInboundAction` at `Block` (the default) a wide listen with no rule is now blocked silently from other
machines and still answers on loopback. So the hub's link, which remote rooms dial, needs its own rule:

```powershell
New-NetFirewallRule -DisplayName 'atrium hub link (D:\atrium)' -Direction Inbound -Action Allow `
  -Program 'D:\atrium\bin\atrium.exe' -Protocol TCP -LocalPort 7779 -Profile Domain,Private
```

The rules earlier prompts left behind, one per random test binary, come out with:

```powershell
Get-NetFirewallApplicationFilter | Where-Object { $_.Program -like '*\go-build*' } |
  Get-NetFirewallRule | Remove-NetFirewallRule
```

### Tests bind loopback

- A test listens on `127.0.0.1:0`, or `httptest.NewServer`, which is loopback. Never `:0`, `""`, `0.0.0.0` or `[::]`.
- A test that has to prove a wide bind works is an integration test, says so in its name, and skips on Windows CI.
- `scripts/check-test-listen.go`, beside `check-test-globals.go`, refuses a `Listen` or `Addr:` in a `_test.go` whose
  host is empty or a wildcard. Run by `scripts/test-touched.sh` on the files it touches.

### A reserved range for fixed test ports

A test that needs a fixed port takes it from 17800 to 17899: far from atrium's own 77xx and 78xx, and below the
dynamic range (49152 and up), so the OS never hands one out. Hyper-V and WinNAT reserve ranges of their own, so check
first, and reserve the range so they cannot take it later:

```powershell
netsh int ipv4 show excludedportrange protocol=tcp
netsh int ipv4 add excludedportrange protocol=tcp startport=17800 numberofports=100 store=persistent
```

The second line is elevated. If it fails because the range is held, `net stop winnat`, add it, `net start winnat`.
Bind one port in the range afterwards to confirm the reservation leaves explicit binds working on this build of
Windows before relying on it.

### The rule an agent follows when it builds and runs an exe

1. Build to a fixed path in your own worktree or card, never `go run` for anything that listens:
   `go build -o build.claude/<name>.exe ./cmd/<name>`. For atrium it is `make build`.
2. Bind loopback, said out loud: `--addr 127.0.0.1:0` or the program's own listen flag with `127.0.0.1`. Never
   `:port`, `0.0.0.0` or `[::]`, even when the program's default is wide.
3. A fixed port comes from 17800 to 17899, never 7777 to 7899.
4. Stop what you started before you hand back: `Stop-Process -Id <pid>`.
5. If a firewall prompt appears anyway, something listened wide. Say so on your card with the exe path and the
   address, and fix the bind. Never ask a human to click Allow.

This goes in `docs/working-here.md` beside the build rule, and in the brief preamble for worker cards.

## Open decisions

1. **The env var's name.** Recommend `ATRIUM_ROOT`. `ATRIUM_HOME` is the card's home already and renaming that breaks
   every running session.
2. **Whether a binary infers its root from its own folder.** Recommend yes, through `root.json` and only that, so a
   copy of `atrium.exe` somewhere else never guesses. Without it, a hook from `settings.json` after cutover needs
   `ATRIUM_ROOT` in the account's environment, which a logon task does not inherit.
3. **Picking the room under a root with several rooms.** Recommend `--room <name>`, with `root.json`'s `room` as the
   default and the only folder under `room\` as the fallback. `room join` takes the name from the join string.
4. **The new room's name.** Recommend `sg4`. Reusing `claude-sg4` fights the old room over `~/.ssh/config` and over
   the hub's record of it during phase 1.
5. **Hub data at phase 2.** Recommend moving the old hub's store, CA, keys and git into the root and keeping 7778 and
   7779, so remote rooms change nothing. The alternative, a fresh hub every room rejoins, loses the backlog history.
6. **The old room's cards.** Recommend draining claude-sg4 rather than moving its `atrium.db` into the root. A moved
   store carries card homes and worktrees whose paths point outside the root, which defeats the layout.
7. **Ports after cutover.** Recommend moving `sg4` back to 7777 and 7781 once claude-sg4 is stopped, so the defaults
   in `internal/cli/atrium_defaults.go` and every doc stay true.
8. **`GOCACHE` and `GOTMPDIR` per room or per account.** Recommend the room sets them for its runners from the root,
   and `go env -w` only at phase 3, so phase 1 changes nothing the old atrium's agents see.
9. **`NotifyOnListen` off for all three profiles.** Recommend yes, with the hub link rule. The prompt cannot be
   answered by an agent, and leaving it on means a modal on the desktop for every wide bind.
10. **Process exclusions for test binaries.** Recommend no. The path exclusion on the root already covers the files
    they are written as, and a process exclusion would skip everything they open anywhere on the machine.
11. **Who reads the root's ACL.** Recommend `claude` full control and clint's account read, so a script run as clint
    reads `root.json` and the address files, which is the job `%WORKTREE_ROOT%\atrium\daemon.json` does today:
    `icacls D:\atrium /grant 'claude:(OI)(CI)F' /grant '<clint account>:(OI)(CI)RX'`.
