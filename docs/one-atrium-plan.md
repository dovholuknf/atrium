# One atrium: one binary, no Mode A, and the hub renamed

Stages 1 to 3 are built. Stage 4, the cutover on this machine, is written out step by step in
`docs/one-atrium-cutover.md`, which also moves the deploy scripts in the same window rather than a day later.

The plan for three decisions clint made on 2026-09-24. Written against `claude/main` at
`fd95da4`. There is no release, so no user needs a compatibility shim. The one thing that must keep working through
every stage is this machine, which runs the board, its own room and every agent working on atrium.

1. **Mode A goes.** `atrium hub` (the v1 TUI), `atrium agent`, `internal/tui` and `internal/agent` are removed, not
   renamed. The part of `internal/hub` the daemon still uses moves into `internal/daemon`.
2. **No more `atrium2`.** One binary, `atrium`, built from one `cmd/`.
3. **"The hub" becomes "the atrium".** The atrium is the central hall and rooms open off it. `atrium run` starts the
   atrium and this machine's room together.

## Contents

- [Inventory](#inventory)
- [Decision 1: what the daemon still takes from Mode A](#decision-1-what-the-daemon-still-takes-from-mode-a)
- [Mode B: dead too, with evidence](#mode-b-dead-too-with-evidence)
- [Decision 2: merging the two binaries](#decision-2-merging-the-two-binaries)
- [Decision 3: the vocabulary](#decision-3-the-vocabulary)
- [The stages](#the-stages)
- [Cutover on this machine](#cutover-on-this-machine)
- [Keeping clear of parallel work](#keeping-clear-of-parallel-work)
- [Docs and CLAUDE.md files to rewrite](#docs-and-claudemd-files-to-rewrite)
- [Open Questions](#open-questions)

## Inventory

### What runs on this machine today

| Thing | Where | What it is |
| --- | --- | --- |
| Hub process | `C:\Users\claude\.atrium2\bin\atrium2.exe hub --addr 127.0.0.1:7778 --link 0.0.0.0:7779 --link-advertise 192.168.1.68:7779 --dir C:\Users\claude\.atrium2\hub` | the board and the link, built from `cmd/atrium2` |
| Room process | `atrium2.exe room --dir C:\Users\claude\.atrium2\room --db C:\Users\claude\.atrium\atrium.db --http 127.0.0.1:7781 --agent 127.0.0.1:7777` | the store, the terminals, every agent |
| Hook binary | `C:\Users\claude\.atrium\bin\atrium.exe` | built from `cmd/atrium`. Every atrium hook line in `settings.json` names it |
| Stdio control MCP | `C:\Users\claude\.atrium\bin\atrium-control.exe control`, user scope in `~/.claude.json` | a copy of an old `cmd/atrium` build (2026-09-06), so the MCP server never locks the hook binary |
| HTTP control MCP | `http://127.0.0.1:7778/_hub/mcp` in `~/.atrium/mcp.json` | what every atrium-launched session actually uses (`--strict-mcp-config`) |
| Permission hook | `pwsh -File C:/Users/claude/.claude/hooks/atrium-perm-hook.ps1` (dotfiles) | POSTs to `http://localhost:7777/permission`, the room's agent listener |
| Room address file | `%LOCALAPPDATA%\atrium2\room\daemon.json`, named by `ATRIUM_LOCATION` in every launched session | records `"exe": "C:/Users/claude/.atrium2/bin/atrium2.exe"` |
| Hub state | `C:\Users\claude\.atrium2\hub\` | `hub.db`, `ca.*`, `hub.*`, `backups\` |
| Room keys | `C:\Users\claude\.atrium2\room\` | `room.json` (`{"hub": "127.0.0.1:7779", "room": "claude-sg4"}`), `room.*`, `ca.crt` |
| Room database | `C:\Users\claude\.atrium\atrium.db` | the v1 daemon's database, which the room took over |
| Deploy scripts | `C:\Users\claude\.atrium2\scripts\` | `deploy-batch.ps1`, `deploy-hub-only.ps1`, `start-atrium-room.ps1`, `start-atrium-hub.ps1`, `maintenance-window.ps1` |
| Older scripts | `C:\Users\claude\.atrium2\` | `start-atrium2.ps1`, `stop-atrium2.ps1`, `restart-room.ps1`, `cutover.ps1`, `deploy-orchestrator.ps1`, `sgg-monitor.ps1` |
| Remote room | sgg, `C:\Users\localai\.atrium2\bin\atrium2.exe` | joined over ziti, see `scripts/sgg/` |

Stale leftovers the cutover can clean: `%APPDATA%\atrium2\room\` (a test room called `sg4`),
`%LOCALAPPDATA%\atrium\lastdb.json`, 30 `atrium2.exe.old-*` files in `.atrium2\bin`.

Every deploy script finds its processes with `Name='atrium2.exe'` and `' hub '` or `' room '` in the command line.
That match is the first thing the cutover breaks.

### Grep counts, per term and per area

Case-insensitive line counts from `claude/main` at `fd95da4`. `internal/api` includes `internal/api/web`.

| Area | `atrium2` | `hub`/`hubs` | `/_hub/` | `[hub]` | `daemon` | Go files (src / test) |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| `cmd/atrium2` | 61 | 266 | 1 | 21 | 28 | 19 / 7 |
| `cmd/atrium` | 0 | 0 | 0 | 0 | 0 | 1 / 0 |
| `internal/api/web` | 6 | 215 | 16 | 0 | 314 | board: 208 `hub` hits over 25 files |
| `internal/api` (all) | 6 | 235 | 16 | 0 | 433 | 36 / 27 |
| `internal/link` | 15 | 875 | 26 | 22 | 25 | 21 / 22 |
| `internal/daemon` | 7 | 112 | 0 | 0 | 859 | 73 / 109 |
| `internal/cli` | 0 | 120 | 0 | 0 | 235 | 26 / 10 |
| `internal/hubstore` | 1 | 178 | 0 | 8 | 2 | 9 / 5 |
| `internal/claudeconf` | 0 | 0 | 0 | 0 | 32 | 5 / 9 |
| `internal/store` | 3 | 14 | 0 | 0 | 65 | 37 / 47 |
| `internal/{hub,tui,agent}` | 0 | 93 | 0 | 0 | 3 | 4 / 0 |
| `internal/{server,state}` | 0 | 0 | 0 | 0 | 0 | 2 / 0 |
| `docs/` | 57 | 619 | 15 | 0 | 593 | 41 files mention `atrium2` or `hub` |
| `README.md` | 0 | 5 | 0 | 0 | 17 | |
| `FEATURES.md` | 7 | 37 | 1 | 0 | 8 | |
| `CHANGELOG.md` | 14 | 106 | 4 | 0 | 25 | history, left alone |
| `website/` | 41 | 81 | 2 | 0 | 22 | 11 pages under `website/docs` |
| `scripts/` | 36 | 206 | 16 | 1 | 73 | |
| `packaging/` | 0 | 0 | 0 | 0 | 24 | every unit and manifest runs `atrium daemon` |

Board files with the most `hub` hits: `rooms.js` 54, `runners.js` 24, `index.html` 24, `hubrestart.js` 21,
`notes-files.js` 11, `solo.js` 11, `settings-spine.js` 11. Most are comments and identifiers. The user-visible strings
are a small subset, counted per file when stage 5 is cut.

`/_hub/` paths in use: `restart`, `restart/pause`, `restart/resume`, `restart/input`, `audit`, `mcp`, `rooms`,
`health`, `inventory`, `inventory/forget`, `inventory/mark`.

Environment variables that carry a location or a role: `ATRIUM_LOCATION`, `ATRIUM_SHARED_LOCATION`, `ATRIUM_ROOM`,
`ATRIUM_AGENT_NAME`, `ATRIUM_TASK_ID`, `ATRIUM_RUNNER`, `ATRIUM_HUB_URL`, `ATRIUM_BOARD_URL`, `ATRIUM_HOOK_EXE`,
`ATRIUM_PERM_GATE`. Mode A alone reads `ATRIUM_DISCONNECTED_LOG_INTERVAL` and `ATRIUM_LONG_POLL_TIMEOUT`.

### The two binaries' commands, and where they collide

| Command | `cmd/atrium` (`internal/cli`) | `cmd/atrium2` | Collides |
| --- | --- | --- | --- |
| `hub` | Mode A server and TUI | the hub, plus `hub room add/ls/token/mark/rm/log`, `hub backups`, `hub restore` | **yes** |
| `room` | v1 report-in: `atrium room --hub <url>`, the legacy `/v1/rooms` federation | run the room from saved keys | **yes** |
| `join` | put the current session on the board (`/atrium-join` skill) | first-time room join from a join string | **yes** |
| `version` | tag, commit, board hash, platform | the bare version string | **yes**, merge to the richer one |
| `daemon` | a standalone board plus agents, no hub | | no |
| `db`, `ledger` | | `db compact`, `ledger` | no |
| `agent`, `serve`, `status`, `watch` | Mode A and Mode B | | no, and deleted |
| everything else | `hook`, `session`, `turn`, `finish`, `peers`, `tell`, `answer`, `ask`, `control`, `launch`, `leave`, `stop`, `name`, `preview`, `open`, `dispatch`, `replay` | | no |

## Decision 1: what the daemon still takes from Mode A

CLAUDE.md says `internal/hub` holds "the permission long-poll the daemon reuses". Exactly this is used, all of it
from `internal/daemon`:

| Used | From | What it does for the daemon |
| --- | --- | --- |
| `hub.New(longPoll)`, field `d.hb` | `daemon.go:209` | builds the in-memory bus |
| `d.hb.HandlePermission` on `/permission` | `daemon.go:839` | **live.** The dotfiles permission hook POSTs every gated Bash call here and blocks on the answer |
| `hub.Hooks{PermRequest, PermDecided}` | `daemon.go:541` | routes each request through `onPermRequest`, the permission chain |
| `hub.PermissionRequest`, `hub.AutoDecision` | `daemon.go:606` and on | the chain's input and a rule's answer |
| `d.hb.DecideByStoreID` | `daemon.go:434` | a decision from the board wakes the parked hook |
| `d.hb.LiveStoreIDs` | `orphans.go:58`, `live.go` | which pending requests still have a hook waiting, the orphan reaper's input |
| `hub.HubDir()` | `daemon.go:179`, `DefaultDBPath` | `$WORKTREE_ROOT/hub` or `~/.atrium`, the default database directory |
| `d.hb.HandleSubmit` on `/submit`, `hub.Hooks{Submit, Prompt}`, `d.hb.SendPrompt` | `daemon.go:838`, `:544`, `:537` | Mode A only. Only `internal/agent` POSTs `/submit` |
| `d.Hub()` | `daemon.go:452` | only `atrium daemon --tui` calls it (`cli.go:204`) |

Tests that import it: `internal/daemon/auto_test.go` and `shelve_test.go`, for `hub.PermissionRequest` and
`hub.AutoDecision`.

**Where it moves.** A new `internal/daemon/permwait.go` takes the pending map, the reply channels, the id sequence,
`HandlePermission`, `DecideByStoreID`, `LiveStoreIDs`, `PermissionRequest`, `PermissionResponse` and `AutoDecision`.
It is roughly a third of the 855 lines. The rest is the submit bus and the TUI. The `Hooks` indirection goes, because the daemon was its only implementer: the
handler calls `d.onPermRequest` and `d.onPermDecided` directly. `HubDir` moves to `internal/daemon` as `StateDir`
and keeps returning `$WORKTREE_ROOT/hub` exactly. Renaming that `hub` path segment would move the default database
for anybody with `WORKTREE_ROOT` set, and a daemon that opens a new empty database shows a board that has lost every
card.

**What must not change.** The `/permission` URL, its request body (`agent`, `command`, `tool`, and the dedup and
tool-input fields the hook sends) and its response (`decision`, `reason`, `command`) are a contract with a script in
clint's dotfiles repo. So are `/gate` and the fail-open posture. The move is internal only.

**What goes with Mode A:**

- `internal/hub/`, `internal/tui/`, `internal/agent/`.
- `atrium hub`, `atrium agent`, and `atrium daemon --tui`.
- `/submit`, `onSubmit`, `onPrompt`, `d.prompt`, `SendPrompt`, and `POST /v1/tasks/{id}/prompt` in `internal/api`.
  The board never calls that route (no `/prompt` fetch anywhere in `internal/api/web`). It only reaches a session
  parked in the Mode A submit loop, and nothing parks there once `atrium agent` is gone. Open Question 11.
- The `atrium-agent` entry in the repo's `.mcp.json`, which runs `./build.claude/atrium.exe agent`.
- The Bubble Tea, Lip Gloss and Bubbles modules, through `go mod tidy`.
- `docs/test-plan.md` section A, D (the choices picker) and F1 (ANSI sentinels).

**One trap.** The permission hook auto-detects: with `ATRIUM_PERM_GATE` unset it gates any session whose working
directory has an `.mcp.json` mentioning `atrium-agent`. The repo's `.mcp.json` does, so today a session in any atrium
checkout is gated by that rule. Removing the entry stops that. Sessions the board launched and sessions that ran
`atrium join` are still gated, through `/gate`. The comment and the auto-detect in `atrium-perm-hook.ps1` belong to
the dotfiles repo, which is clint's. Open Question 10.

## Mode B: dead too, with evidence

Mode B is `atrium serve` (an MCP server over the gwt ledger), `atrium status` and `atrium watch`, in
`internal/server` and `internal/state`.

- **No code calls it.** The only importer of `internal/server` and `internal/state` is `internal/cli/cli.go`.
- **Nothing registers it.** `~/.claude.json` holds `mcp-gateway` and `atrium-control` at user scope. No project
  `.mcp.json` names `atrium serve`. `~/.atrium/mcp.json` holds `mercurius` and `atrium-control`.
- **No script calls it.** A search of `D:\git\github\dovholuknf\dotfiles` finds no `atrium serve`, `atrium status`,
  `atrium watch`, `snapshot`, `wait_for_change` or `focus_session`. The dotfiles hooks write
  `$WORKTREE_ROOT\watch\state.log`, and `gwt watch` and `agent-log` (both in dotfiles) already tail it, so
  `atrium watch` duplicates a tool clint uses.
- **Nobody has touched it.** The last commit to `internal/server` or `internal/state` is 2026-09-02.
- **What still names it.** `Makefile` (`run-status`, `run-watch`, `run-serve`), `README.md`, `FEATURES.md`,
  `website/docs/cli.md`, `docs/user-guide.md`, `docs/test-plan.md` section E, `docs/how-atrium-works.md`,
  `docs/state-of-the-art.md`, and a comment in `internal/cli/control.go`.

Recommendation: remove it in the same stage as Mode A. The board shows everything Mode B shows and more, and the gwt
ledger it reads keeps working without it. Open Question 1.

## Decision 2: merging the two binaries

### The shape

`cmd/atrium2` is `package main`, so its files cannot be imported. They move into `internal/cli`, which already holds
every other command, under names that say which half they are: `hub*.go` becomes `atrium_*.go` for the central
process, `room.go` becomes `roomrun.go`, and so on. `cmd/atrium/main.go` stays three lines. The linker variable
becomes one: `internal/cli.Version`, which `atrium version` already prints. `cmd/atrium2/main.go`'s own `version`
goes.

### Commands after the merge

| Today | After | Notes |
| --- | --- | --- |
| `atrium2 hub [flags]` | `atrium run --no-room [flags]` | the atrium alone, for a split setup and for this machine |
| `atrium2 hub` + `atrium2 room` | `atrium run` | the common case. See "What `atrium run` is" below |
| `atrium2 room [flags]` | `atrium room [flags]` | **flags unchanged**, see "Why `room` keeps its flags" |
| `atrium2 join <string>` | `atrium room join <string>` | `atrium join` stays the session command the `/atrium-join` skill runs |
| `atrium2 hub room add/ls/token/mark/rm/log` | `atrium rooms add/ls/token/mark/rm/log` | managed from the atrium's side, so plural |
| `atrium2 hub backups`, `atrium2 hub restore` | `atrium backups`, `atrium backups restore <snapshot>` | |
| `atrium2 db compact`, `atrium2 ledger` | `atrium db compact`, `atrium ledger` | |
| `atrium2 version` | `atrium version` | |
| `atrium room --hub <url>` (v1) | removed | legacy `/v1/rooms` federation, Open Question 3 |
| `atrium daemon` | kept until stage 6, then removed | packaging runs it, Open Question 2 |
| `atrium control` (stdio) | Open Question 8 | its `restart_atrium` starts `atrium daemon`, the wrong process on this machine |

Flags that collide when `atrium run` takes both halves: the hub's `--dir`, `--db`, `--identity` and `--service`
against the room's. The room keeps its names, because `atrium room` must stay flag-compatible. The atrium's side
takes a prefix: `--atrium-dir`, `--atrium-db`, `--atrium-identity`, `--atrium-service`. `--addr`, `--link`,
`--link-advertise`, `--transport`, `--board*`, `--builds` and `--open` are unique and keep their names.

### What `atrium run` is

Two processes, on purpose. The whole point of the split is that the atrium restarts freely and the room does not.
One process holding both would make every board fix a restart of every agent, which is what `atrium2` was built to
stop.

`atrium run` therefore serves the atrium in the foreground and, before it does, makes sure a room is running for this
machine: when the room's address file answers, it leaves the room alone, and when it does not, it starts
`atrium room` detached (`detach_windows.go` already has the flags). Stopping or restarting `atrium run` never touches
the room. `atrium run --no-room` skips the check. `atrium room` alone runs a room that dials an atrium elsewhere.
Open Question 4.

The first `atrium run` on a fresh machine also mints the room's keys against its own atrium, the way
`atrium2 hub room add` plus `atrium2 join` do by hand today, so the one-machine case needs no join string at all.

### Why `room` keeps its flags

sgg runs `atrium2.exe room` and takes upgrades from its hub (`internal/link/upgrade.go`). After the merge the hub
offers the merged binary, sgg writes it over its own `atrium2.exe`, and its restarter runs `atrium2.exe room --dir
... --db ...` again. That works only if the merged binary accepts `room` with the same flags and speaks the same link
protocol. So `room` keeps `--dir`, `--db`, `--http`, `--agent`, `--identity`, `--mtls`, `--zrok-private`,
`--openziti`, `--service`, `--accept-upgrades`, `--isolated` and the hidden `--restart-after`. The file name on sgg
stays `atrium2.exe` until somebody renames it there, which is harmless.

`cmd/atrium2/builds.go` names per-platform builds `atrium2_<goos>_<arch>`. It learns `atrium_<goos>_<arch>` and keeps
accepting the old prefix, so a builds directory from either era works.

### Hook lines in settings.json

Today, from `~/.claude/settings.json`:

```
C:/Users/claude/.atrium/bin/atrium.exe hook --event tool-start
C:/Users/claude/.atrium/bin/atrium.exe hook --event tool-end
C:/Users/claude/.atrium/bin/atrium.exe hook --event tool-failed
C:/Users/claude/.atrium/bin/atrium.exe hook --event prompt
C:/Users/claude/.atrium/bin/atrium.exe hook --event notification
C:/Users/claude/.atrium/bin/atrium.exe hook --event subagent-start
C:/Users/claude/.atrium/bin/atrium.exe hook --event subagent-end
C:/Users/claude/.atrium/bin/atrium.exe session --event start
C:/Users/claude/.atrium/bin/atrium.exe session --event end
C:/Users/claude/.atrium/bin/atrium.exe session --event compact
C:/Users/claude/.atrium/bin/atrium.exe turn --event end
```

**After: identical.** The merged binary installs at `C:\Users\claude\.atrium\bin\atrium.exe`, the path every line
already names, and `hook`, `session` and `turn` do not change. No settings file is rewritten.

**A latent bug the merge fixes.** `claudeconf.HookExe` (`internal/claudeconf/whichexe.go`) answers "which binary goes
in a hook line" from the running daemon's address file. The room's file records `atrium2.exe`, which has no `hook`
subcommand. So the board's "install hooks" button, pressed today, would write eleven lines naming a binary that
cannot run them, and a hook that fails is silent by design. After the merge the room runs the same binary the hooks
run, and the address file names it. Nothing in `whichexe.go` changes. Its comment names the daemon and gains one
line saying a room is a daemon.

**A new cost.** Today a hub or room deploy never touches the file hooks run. After the merge every deploy does, and
hooks spawn that file several times a second. The deploy scripts must place the new binary in two renames and no
copy: copy the build to `atrium.next.exe` first (nobody has it open), then rename `atrium.exe` aside and rename
`atrium.next.exe` into place. The gap between the two renames is microseconds. A hook that lands in it fails, and a
failed atrium hook is a non-blocking error by design. A copy onto the live name would widen the gap to the length of
the copy.

### The atrium-control MCP registration

Two registrations exist today:

- **User scope, stdio**: `C:\Users\claude\.atrium\bin\atrium-control.exe control`. A separate copy so the MCP server
  never locks the hook binary. Its `restart_atrium` (`internal/cli/control.go:348`) starts `atrium daemon`, the v1
  daemon that `start-atrium2.ps1` refuses to run beside the room because both would open `atrium.db`. Any
  interactive session outside atrium that calls it would do exactly that.
- **Launched sessions, HTTP**: `http://127.0.0.1:7778/_hub/mcp` with `X-Atrium-Agent` and `X-Atrium-Room` headers,
  served by the hub (`internal/link/control_mcp.go`). Nine tools, the ones every agent here uses.

Recommendation: point the user-scope entry at the same HTTP URL and delete `atrium control`, its restarter,
`atrium-control.exe` and the `atrium.next.exe` swap in `internal/cli/control.go`. An HTTP MCP server holds no file
open, so the reason for the second copy disappears. A session outside atrium sends empty agent headers, and the
tools that need a caller answer that they need one. Open Question 8.

### Installed paths and data directories

| | Today | After the cutover | After stage 7 (optional) |
| --- | --- | --- | --- |
| The one binary | `.atrium\bin\atrium.exe` (hooks) and `.atrium2\bin\atrium2.exe` (hub, room) | `.atrium\bin\atrium.exe` for all three | same |
| Control MCP | `.atrium\bin\atrium-control.exe` | gone, if Open Question 8 goes that way | |
| Atrium state | `.atrium2\hub\` | same, named by `--atrium-dir` | `.atrium\hub\` by default |
| Room keys | `.atrium2\room\` | same, named by `--dir` | `.atrium\room\` by default |
| Room database | `.atrium\atrium.db` | same, named by `--db` | same, and the default |
| Room address file | `%LOCALAPPDATA%\atrium2\room\daemon.json` | `%LOCALAPPDATA%\atrium\daemon.json` | same |
| Logs and scripts | `.atrium2\*.err`, `.atrium2\scripts\` | same | `.atrium\logs\`, `.atrium\scripts\` |

The room's own address file moves to the ordinary default. `roomLocation()` in `cmd/atrium2/room.go` exists so a room
could run beside a v1 daemon without stealing its hooks. Once the room is the only daemon on the machine that reason
is gone. `--isolated` stays for a throwaway second room.

The code defaults in `cmd/atrium2` use `os.UserConfigDir()`, so `%APPDATA%\atrium2\...`, which is neither where this
machine keeps anything nor where `docs/packaging.md` says atrium lives (`~/.atrium`). Stage 6 changes the defaults to
`~/.atrium/hub`, `~/.atrium/room` and `~/.atrium/atrium.db`. Open Question 7.

### The deploy scripts

All five in `.atrium2\scripts\`, and the older ones beside them, change in the same three ways:

1. `$bin` becomes `C:\Users\claude\.atrium\bin\atrium.exe` and `$new` becomes the orchestrator's
   `build.claude\atrium.exe`.
2. The process match becomes the path plus the subcommand, `Path -eq $bin` and `' run '` or `' room '`, never the
   image name alone. `atrium.exe` is also the name of every hook process in flight, and matching the name alone would
   stop those.
3. The swap becomes stage-then-two-renames, as above.

`deploy-hub-only.ps1` starts `atrium.exe run --no-room --addr 127.0.0.1:7778 --link 0.0.0.0:7779 --link-advertise
192.168.1.68:7779 --atrium-dir C:\Users\claude\.atrium2\hub`. `deploy-batch.ps1` and `start-atrium-room.ps1` start
`atrium.exe room` with the flags they pass today. `scripts/hub-restart-gate.ps1` in this repo uses `/_hub/restart` and
needs nothing. The HUB-SIDE versus ROOM-SIDE split in commit subjects keeps its meaning: the processes stay separate,
only the file is shared.

These files live outside the repo, so the new versions are written into the repo under `scripts/live/` in stage 3,
reviewed there, and copied over at cutover. That also puts them under version control, which the current ones are
not.

## Decision 3: the vocabulary

### The rule

**What a person reads says "atrium". What a program reads keeps "hub".** The central process is the atrium, the
machines are rooms, and "the atrium" replaces "the hub" in every sentence a user sees. Names that a running room, a
hook line, an open browser tab, a stored file or an external controller depends on stay as they are, because
renaming them buys nothing a reader sees and breaks something that is running.

This follows how the code already treats `/_hub/`: the underscore marks it as plumbing.

### User-facing: changes

| Surface | Today | After |
| --- | --- | --- |
| CLI | `atrium2 hub`, "A hub that serves the board, and rooms that run the agents" | `atrium run`, "The atrium serves the board. Rooms open off it and run the agents." |
| CLI help | "the hub", "this hub's rooms", "attach them to a hub" | "the atrium", "this atrium's rooms", "attach them to an atrium" |
| Board | the restart toast and cover ("the hub is restarting"), the rooms panel, the offline banner, runners and settings copy | "the atrium is restarting", "rooms", and so on |
| Docs | `docs/hub-room-*.md`, `hub-restart-gate.md`, user guide, how atrium works | rewritten, file names kept (links point at them) |
| README, FEATURES, website | `atrium2`, "the hub" | `atrium run`, "the atrium" |

The word the board uses for its own central process is "the atrium" throughout. Where a sentence has to distinguish
the program from the product ("atrium is restarting the atrium"), it says "the board" instead, which is what the user
is looking at. Open Question 12.

### Internal: stays, and what renaming each one would break

| Name | Where | Would break if renamed |
| --- | --- | --- |
| `/_hub/*` HTTP paths | `internal/link/proxy.go`, board JS, deploy scripts | **open board tabs** (their JS calls the old paths until reload), `~/.atrium/mcp.json` which every launched session reads for `/_hub/mcp`, the health checks in every deploy script, `hub-restart-gate.ps1` |
| `room.json` key `hub` | `internal/link/room.go:367`, both room dirs | **every joined room**, which reads its hub address from it on restart, sgg included |
| Join-string fields (`hub`, `transport`, `service`) | `internal/link/certs.go` | **every join string already issued**, and sgg's saved join |
| `hub.db`, `hub.crt`, `hub.key`, `ca.*` | `.atrium2\hub\` | **the stored atrium state**: a renamed default opens an empty database and a new CA, and every room's certificate stops verifying |
| Ziti service `atrium-hub`, zrok targets `atrium-hub`, `atrium-hub-board` | `hub.go`, `hubrooms.go`, `zrok.go`, `hubshare.go` | **external config** on the ziti controller and the zrok reservation |
| Audit events `hub-started`, SSE event `hub-restart` | `hub.go:248`, `restartgate.go:35` | stored audit rows lose their label, and an **open tab** misses the restart event |
| `ATRIUM_HUB_URL` | `internal/cli/hook.go`, dotfiles perm hook | **hooks** in running sessions. It names the room's agent listener, a v1 name that was already wrong, and still harmless |
| `ATRIUM_LOCATION`, `ATRIUM_ROOM`, `ATRIUM_AGENT_NAME`, `ATRIUM_TASK_ID`, `ATRIUM_HOOK_EXE` | launch env | **every running session's hooks**, which inherited them |
| `/permission`, `/gate`, `/activity`, `/session` | the room's agent listener | **hooks**, including the dotfiles script |
| `--link`, `--link-advertise` | atrium flags | the deploy scripts. No reader confuses "link" with "hub" |
| `internal/link`, `internal/hubstore` | packages | nothing at runtime. Renaming is churn across 60 files and a merge hazard for every open branch |
| `[hub]` log prefix | `internal/link`, `internal/hubstore`, `cmd/atrium2` | nothing. Logs are read by people, so this one could change, but a grep over old `hub.err.*` files would then miss half the history. Left alone |

`ATRIUM_SHARED_LOCATION` exists for the same reason `roomLocation()` does, a room beside a v1 daemon. After the
cutover nothing needs it. It stays readable, and the room stops setting it, in stage 6.

### The federation doc disagrees

`docs/federation-design-v2.md` section 8 picked a vocabulary where each machine is "an atrium", plural "atria", and
the aggregator is "the forum". Decision 3 inverts that: the central process is the atrium and each machine is a room.
The section also rejected the word `hub` on the grounds that `atrium hub` was taken by Mode A, which stage 1 removes.
The doc needs a note at section 8 saying the naming was superseded, and by what. The forum design itself was never
built. The hub and rooms are what shipped.

## The stages

Each stage lands on `claude/main` alone, builds, passes `make check`, and deploys alone. None adds a migration to
`internal/store/schema.go` or `internal/hubstore/schema.go`, which is what keeps the old binary able to open the same
database for a rollback at every step.

### Stage 1: Mode A out (ROOM-SIDE)

- Add `internal/daemon/permwait.go` with the permission long-poll, as listed above. Port `auto_test.go` and
  `shelve_test.go` to it, and add a test that a board decision wakes a parked `/permission` request and that
  `LiveStoreIDs` empties when the request's connection drops, since that pair is what the move could break.
- Move `HubDir` to `daemon.StateDir`, same answer.
- Delete `internal/hub`, `internal/tui`, `internal/agent`, `newHub`, `newAgent`, `--tui`, `/submit`, the submit and
  prompt hooks, `SendPrompt`, `POST /v1/tasks/{id}/prompt`, and the `atrium-agent` entry in `.mcp.json`.
- `go mod tidy`.
- `docs/test-plan.md`: retire A, D and F1 with a line each saying why.
- Deploy: the room, through `deploy-batch.ps1` as it is today, plus the hook binary copied the way it is copied today.
  `hook`, `session` and `turn` do not change.
- Risk: **medium.** The permission gate is the one path where a mistake blocks every agent. Mitigations: the
  handler moves verbatim before anything is simplified, the new tests run against it, and the room restart that
  deploys it is the moment to watch one gated Bash call go through before walking away. Fail-open still holds if
  the room is down.

### Stage 2: Mode B out (hook binary only)

Only if Open Question 1 says so. Delete `internal/server`, `internal/state`, `newServe`, `newStatus`, `newWatch`, the
three `run-*` targets in `Makefile`, and their rows in `README.md`, `FEATURES.md`, `website/docs/cli.md`, the user
guide and test plan section E. Nothing running uses it, so the deploy is the next hook binary copy. Risk: **low.**
Could fold into stage 1.

### Stage 3: one cmd (ROOM-SIDE and HUB-SIDE, but nothing switches yet)

- Move `cmd/atrium2/*.go` into `internal/cli` and wire the commands under their new names.
- Remove the v1 `atrium room` (`internal/cli/room.go` and the parts of `roomlaunch.go` only it uses). The `/v1/rooms`
  server side and the board's legacy rooms UI in `runners.js` stay, for a later cleanup (Open Question 3).
- `builds.go` accepts both prefixes. `atrium version` is the one version command.
- Keep `cmd/atrium2/main.go` as a **temporary shim**: the same root, re-exposing `hub`, `join` and `room` under their
  old names, so the live deploy scripts keep working between this merge and the cutover. It is ten lines and it is
  deleted in stage 4. It exists for this machine, not for users.
- `Makefile` builds `build.claude/atrium.exe`, and `build.claude/atrium2.exe` from the shim until stage 4.
- Write the new live scripts under `scripts/live/`, with a `-WhatIf` that prints every process it would stop and
  every file it would rename.
- Update `scripts/test-board-headless.js` (the empty-board string that says `atrium2 join`),
  `scripts/walkthrough/hubroom.spec.js` and `scripts/sgg/`.
- Implement `atrium run` as described, including the one-machine key minting.
- Deploy: nothing new has to deploy. The shim build can go out through the old scripts as an ordinary batch deploy,
  which proves the moved code on the live machine before any path changes.
- Risk: **medium.** Mostly a large move. Test coverage in `cmd/atrium2` (seven test files) moves with the code.
  `room_isolated_test.go` and `roomlisten_test.go` are the ones that guard hook hijacking and must pass unchanged.

### Stage 4: the cutover on this machine, then delete the shim

The cutover is a deploy, step by step in the next section. Once it has run a day, delete `cmd/atrium2` and the second
`Makefile` output. Risk: **high for one deploy window**, low after. Every supervised session restarts once, and the
hook binary, the room and the atrium all change file at the same moment.

### Stage 5: the words (HUB-SIDE for the board, docs are docs)

- CLI help text across `internal/cli`.
- Board strings, in the files listed in the inventory, strings and comments only.
- `README.md`, `FEATURES.md`, `website/docs/*`, `website/src/pages/index.js`, and the `docs/` files listed below.
- `CHANGELOG.md` gets an entry naming the rename. Past entries stay as written.
- Deploy: hub-only, through the gate. The board is embedded in the binary, so this is an ordinary board fix.
- Risk: **low** at runtime, **high as a merge hazard.** See "Keeping clear of parallel work".

### Stage 6: `atrium daemon` out, packaging on `atrium run`, defaults moved

- Only if Open Questions 2, 6 and 7 go that way.
- `packaging/atrium.service`, `packaging/atrium.plist`, `packaging/postinstall.sh`, `packaging/scoop-atrium.json`,
  `scripts/atrium-service.sh`, `scripts/atrium-service.ps1`, `scripts/atrium-autostart.ps1` and
  `scripts/release.sh` move from `atrium daemon --db ...` to `atrium run`.
- Default ports, directories and the database path change in code. The room stops setting `ATRIUM_SHARED_LOCATION`.
- `docs/packaging.md` and `docs/reload-design.md` are rewritten against it.
- Deploy: none on this machine, whose scripts pass every path explicitly. Risk: **low** here, **medium** for the
  next person who installs, which is the point of doing it before a release.

### Stage 7: this machine's data under `~/.atrium` (optional)

Stop both processes in a batch window, move `.atrium2\hub` to `.atrium\hub` and `.atrium2\room` to `.atrium\room`
whole (the CA, the certificates and `room.json` travel together, nothing is regenerated), drop the explicit path
flags from the scripts, start. Moving the room's keys does not change what they say, so the link comes back as it
was. Risk: **low**, one deploy window. Only worth doing if Open Question 7 says the defaults move.

## Cutover on this machine

Run by the orchestrator, detached, in a batch window with every peer idle, from a shell atrium does not supervise.
Stopping the room ends every supervised terminal, the orchestrator's included, and the room resumes them on start.

**Before the window**

1. Stage 3 is on `claude/main` and has run on the live machine as a shim build for at least one batch deploy.
2. `make build` in the orchestrator worktree produces `build.claude\atrium.exe`.
3. `scripts\live\*.ps1 -WhatIf` has been read, and names exactly the two `atrium2.exe` processes and the files below.
4. Copy the build to `C:\Users\claude\.atrium\bin\atrium.next.exe`. Nothing holds that name open.
5. Snapshot for rollback: `.atrium2\bin\atrium2.exe` stays where it is, untouched. Copy `.atrium\bin\atrium.exe` to
   `.atrium\bin\atrium.pre-cutover.exe`. Copy `.atrium\atrium.db` and its `-wal` and `-shm` with the room running is
   not safe, so the database snapshot is taken in step 7, after the room stops.

**The window** (one detached script, logging every step to `.atrium2\deploy-batch.log`)

6. Stop the room: `POST http://127.0.0.1:7781/v1/shutdown`, wait up to 45 seconds for pid exit, force only after
   that. The room parks its runners and saves what to reopen.
7. Copy `atrium.db*` to `atrium.db.pre-cutover*`. The room is down, so the files are quiet.
8. Stop the hub. It holds nothing, so `Stop-Process` is fine.
9. Swap the hook binary: rename `.atrium\bin\atrium.exe` to `atrium.old-<timestamp>.exe`, rename `atrium.next.exe` to
   `atrium.exe`. Retry each rename up to 20 times at 400ms. Hooks from sessions outside atrium may be mid-run on the
   old file, which a rename permits and a delete would not. This is the only file on the machine that is open while
   it changes.
10. Start the atrium: `atrium.exe run --no-room --addr 127.0.0.1:7778 --link 0.0.0.0:7779 --link-advertise
    192.168.1.68:7779 --atrium-dir C:\Users\claude\.atrium2\hub`. Wait for `/_hub/health` to answer `ok`.
11. Start the room: `atrium.exe room --dir C:\Users\claude\.atrium2\room --db C:\Users\claude\.atrium\atrium.db
    --http 127.0.0.1:7781 --agent 127.0.0.1:7777`. Wait for `/_hub/health` to report `rooms >= 1`. The room writes
    `%LOCALAPPDATA%\atrium\daemon.json` with `"exe": ".../.atrium/bin/atrium.exe"`, and every session it reopens
    inherits the new `ATRIUM_LOCATION`.

**Verify, before anybody walks away**

12. `/_hub/health` build matches the new build. The board loads, and an open tab reloads itself on the new board hash.
13. A reopened session's tool call shows its activity badge (the `hook --event tool-start` path) and a gated Bash call
    appears as a permission and is answered (the `/permission` path, stage 1's moved code).
14. `atrium.exe version` prints the new commit. `atrium_status` over `/_hub/mcp` answers.
15. `%LOCALAPPDATA%\atrium\daemon.json` names `atrium.exe`, and the hooks panel on the board shows every hook as
    current, which it could not before (the latent bug above).

**Rollback** (any of 10 to 15 failing)

16. Stop whatever of the new pair is running. Rename `atrium.exe` aside and `atrium.pre-cutover.exe` back. Start
    `atrium2.exe hub` and `atrium2.exe room` with today's exact commands from `deploy-batch.ps1`, with
    `ATRIUM_LOCATION` set to `%LOCALAPPDATA%\atrium2\room\daemon.json`. No stage adds a migration, so the old binary
    opens the database the new one used. The database copy from step 7 is there if it does not.

**After a day on the new binary**

17. Replace `.atrium2\scripts\*` with `scripts\live\*` for good, and retire `start-atrium2.ps1`, `stop-atrium2.ps1`,
    `restart-room.ps1`, `cutover.ps1` and `deploy-orchestrator.ps1` (the last rewrites `~/.atrium/mcp.json` with a
    `/_hub/mcp` URL, which stays correct).
18. Re-register the user-scope `atrium-control` if Open Question 8 goes that way, and delete
    `.atrium\bin\atrium-control.exe`.
19. Delete `%LOCALAPPDATA%\atrium2\room\daemon.json`, `%APPDATA%\atrium2\` (the stale `sg4` test room) and
    `.atrium2\bin\atrium2.exe.old-*`. Keep `.atrium2\bin\atrium2.exe` one more week as the rollback binary.
20. Delete the `cmd/atrium2` shim (the rest of stage 4).
21. Owed outside this repo, for clint: a line in `claude/tuning-changelog.md`, and the orchestrator's memories that
    name `atrium2` (`atrium2-is-the-live-hub`, `throwaway-atrium2-needs-atrium-location`, `atrium-binary-not-on-path`,
    and the deploy-script paths).

**sgg** needs nothing at cutover. Its room keeps running its own `atrium2.exe` and reconnects to the new atrium
through the unchanged link. The next upgrade it pulls is the merged binary under its old file name, which runs
`room` with the same flags.

## Keeping clear of parallel work

The two merge hazards in this repo are the board (`internal/api/web/`, above all `index.html` and the large JS files)
and `internal/store/schema.go`.

- **No stage touches `schema.go` or `hubstore/schema.go`.** None needs a migration, and that is also what keeps the
  rollback safe.
- **Stages 1 to 4 do not touch `internal/api/web/`.** Stage 1 touches `internal/api/api.go` for one route, and
  `internal/daemon/daemon.go`, which parallel work also edits. The edits there are deletions of a few lines each, so
  a conflict is small and obvious.
- **Stage 3 is a move, and a move conflicts with every open branch that edits `cmd/atrium2`.** Land it right after a
  wave closes, with no `cmd/atrium2` branch in flight. `git log --follow` keeps the history of each moved file.
  Ask the orchestrator to hold new `cmd/atrium2` work for the day it takes.
- **Stage 5 is the only stage that edits the board, and it is strings and comments only.** No restructuring, no
  renamed identifiers, no moved functions, so every hunk is a line or two and rebases cleanly or conflicts
  obviously. Cut it into one commit per file group (board JS, board HTML, CLI help, docs, website) so a conflict in
  one does not hold the rest. Rebase onto `claude/main` immediately before merging, run `scripts/check-board.sh`
  after, and land it at a wave boundary.
- **Docs are separate commits from code** in every stage, so a docs conflict never blocks a code landing.

## Docs and CLAUDE.md files to rewrite

### CLAUDE.md (symlinks into clint's dotagents repo: edits listed, not made)

`CLAUDE.md` at the repo root, `D:\git\github\dovholuknf\dotagents\github\dovholuknf\atrium\CLAUDE.md`. 36 lines match
Mode A, Mode B, `atrium daemon`, `hub` or `atrium2`.

1. The opening: "`atrium daemon` is the thing that gets used" becomes "`atrium run` starts the atrium, which serves
   the board, and this machine's room, which holds the database and the agents".
2. The daemon diagram: redraw as atrium plus room, with the ports this machine uses (7778 board, 7779 link, 7777
   room agent listener, 7781 room board).
3. "Two older modes": delete the whole section, Mode A and Mode B both (Mode B pending Open Question 1).
4. Repo layout: `cmd/atrium/main.go` is the only entry point. Remove the `internal/hub`, `internal/agent`,
   `internal/tui`, `internal/server` and `internal/state` lines. Add `internal/link` (the atrium's proxy and the
   room link), `internal/hubstore` (the atrium's own small store), and `internal/daemon/permwait.go`. The
   `internal/cli` line lists the new commands.
5. Documentation: `docs/test-plan.md` letters lose A and E. Add `docs/one-atrium-plan.md` until it is done, and the
   hub-room docs.
6. Subcommands table: remove `hub`, `agent`, `serve`, `status`, `watch`. Add `run`, `room`, `room join`, `rooms`,
   `backups`, `db`, `ledger`. Mark `daemon` for removal. Rework `control` per Open Question 8.
7. "The wire protocol (Mode A)": delete `/submit` and the `kind` list. Keep `/permission` under a new heading, "The
   permission endpoint", since the dotfiles hook still depends on its exact shape.
8. "Agent-facing formatting affordances" and "Behavior the LLM agent must NOT do": delete. Both describe
   `atrium agent`.
9. "Resilience guarantees (Mode A)": delete. Point 1 to 3 are about the submit loop.
10. Conventions: "The hub is reader-only of agent identity" becomes "The room trusts the agent name a hook sends".
11. Out of scope: delete "Persistence in the hub", "Replacing gwt sessions. Mode B ...", and the "WebSocket transport
    for the agent listener" item that describes the submit loop.

`internal/agent/CLAUDE.md` (`...\dotagents\github\dovholuknf\atrium\internal\agent\CLAUDE.md`): delete, with the
symlink. `internal/api`, `internal/api/web`, `internal/claudeconf`, `internal/daemon`, `internal/safepath` and
`internal/store` have no Mode A, Mode B, `hub` or `atrium2` references. `internal/claudeconf/CLAUDE.md` could gain a
line that a room is a daemon for `HookExe`'s purposes.

### Docs in this repo

| File | What changes | Stage |
| --- | --- | --- |
| `docs/architecture-v2.md` | "What carries over from v1" points 1 to 3 cite `internal/agent`. Staged migration step 5 (cut the TUI pointer) is abandoned, not pending | 1 |
| `docs/test-plan.md` | retire A, D, E, F1 | 1, 2 |
| `docs/user-guide.md`, `docs/how-atrium-works.md`, `docs/state-of-the-art.md` | Mode A and Mode B passages, `atrium2`, "the hub" | 1, 2, 5 |
| `docs/reload-design.md` | the whole restarter describes `atrium control --restart-now` starting `atrium daemon`. Rewrite against the room's restart and the rename pair, or mark historical | 4, 6 |
| `docs/packaging.md` | every `atrium daemon` line, the database section, and a note that there is one binary | 6 |
| `docs/federation-design-v2.md` | a superseded note at section 8 | 5 |
| `docs/hub-room-plan.md`, `docs/hub-room-requirements.md`, `docs/hub-restart-gate.md`, `docs/restart-wake.md` | `atrium2` commands and "hub" in prose. File names stay | 5 |
| `docs/cold-start.md`, `docs/wrapup.md` | orchestrator boot and wrap-up paths name `atrium2.exe` and `.atrium2\scripts` | 4 |
| `README.md`, `FEATURES.md` | commands and vocabulary | 2, 5 |
| `website/docs/{cli,control-mcp,hooks,install,intro,rooms,settings,story,terminals,history}.md`, `website/src/pages/index.js`, `website/DECISIONS.md` | commands and vocabulary. `website/scripts/test-gate-hook.js` names the hub URL | 5 |
| `CHANGELOG.md` | one entry per stage | all |
| `docs/backlog.md` | items that name `atrium2` or Mode A. **Not edited here**: it is off limits to this work, and needs clint's pass | |

## Open Questions

1. **Mode B.** Remove `atrium serve`, `status`, `watch`, `internal/server` and `internal/state` with Mode A?
   Recommended: yes. No caller, no registration, no script uses them, `gwt watch` covers `watch`, untouched since
   2026-09-02.
2. **`atrium daemon`.** Remove it in stage 6 and move packaging to `atrium run`? Recommended: yes. After the merge a
   standalone daemon is a room with no atrium, and two ways to run the same store is how two processes end up on one
   database.
3. **The v1 `/v1/rooms` federation.** Remove the `atrium room --hub` client in stage 3, since its name collides?
   Recommended: yes, and leave the server side and the board's legacy rooms UI in `runners.js` for a separate
   cleanup that is allowed to touch the board.
4. **What `atrium run` is.** The atrium in the foreground plus a detached room it starts only when none is running?
   Recommended: yes. One process for both would make every board fix restart every agent.
5. **Command names.** `atrium run [--no-room]`, `atrium room`, `atrium room join <string>`, `atrium rooms
   add/ls/token/mark/rm/log`, `atrium backups [restore]`, and the atrium-side flags prefixed `--atrium-*`?
   Recommended: yes as listed. `atrium join` stays the session command the `/atrium-join` skill runs.
6. **Default ports.** `atrium run` defaults to this machine's layout (7778 board, 7779 link, 7777 room agents, 7781
   room board) rather than `atrium2`'s (7800, 7801, 7810, 7811)? Recommended: this machine's. 7777 is what the
   dotfiles permission hook and the hook binary assume when no address file answers.
7. **Default directories.** `~/.atrium/hub`, `~/.atrium/room`, `~/.atrium/atrium.db` rather than
   `%APPDATA%\atrium2\...`, and do stage 7 to move this machine onto them? Recommended: yes to the defaults, and stage
   7 whenever a batch window is convenient. It is optional.
8. **atrium-control at user scope.** Point it at `http://127.0.0.1:7778/_hub/mcp` and delete stdio `atrium control`,
   its restarter and `atrium-control.exe`? Recommended: yes. Its `restart_atrium` starts the v1 daemon, which is
   wrong on this machine today.
9. **Internal names keep "hub".** `/_hub/`, `room.json`'s `hub` key, join-string fields, `hub.db` and the certificate
   files, ziti and zrok names, audit and SSE event names, `ATRIUM_HUB_URL`, the `internal/link` and
   `internal/hubstore` packages, the `[hub]` log prefix, and HUB-SIDE in commit subjects? Recommended: all stay. Each
   rename breaks a running room, an open tab, a hook or stored state, and none is seen by a user.
10. **The permission hook's auto-detect.** Removing `atrium-agent` from the repo's `.mcp.json` stops the dotfiles
    hook gating sessions in atrium checkouts by that rule. Accept it, and have the dotfiles hook rely on `/gate`
    alone? Recommended: yes. Launched and joined sessions are still gated. The hook is in your repo.
11. **`POST /v1/tasks/{id}/prompt`.** Delete it with Mode A? Recommended: yes. The board never calls it and only a
    Mode A agent could receive it.
12. **The board's word for itself.** "The atrium" for the central process, and "the board" where a sentence would
    otherwise say "atrium restarts the atrium"? Recommended: yes.
