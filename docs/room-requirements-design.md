# What a room needs to take a project's work: `atrium.requirements.yaml`

clint, 2026-09-29: a declarative per-project file naming what a room needs to take that project's work, and a command
that reads it, checks a room, fixes what it can, and lists what needs a human. Atrium is the first file.

DESIGN ONLY. Nothing here is built. Item f-005 (`docs/backlog/fabric/f-005.md`) is the step log it is written from,
and every requirement below traces to a row there. The three scripts it builds on are `scripts/provision-room.ps1`,
`scripts/room-toolchain.ps1` and `scripts/room-git.ps1`, whose headers are their spec. The reboot survival half is
@rnd's `docs/rnd/room-autostart-design.md` (claude/rnd d10992c), and this design points at its section 4 rather
than copying it.

## 1. Why a file

f-005 says a machine contributes when six things hold: a room joined and attached, a runner signed in, the toolchain
on the PATH the ROOM sees, a clone with a fresh `hub-main` made by push, a smoke worker that launches, reports and
exits, and branches coming back by fetch. Getting sg3 and m1mini there on 2026-09-29 took three scripts, two hub API
calls, a room restart by hand, an npm install by hand, and four things nobody had written down as requirements at
all:

- neither room had ever had atrium's hooks wired (f-005 m1mini row 6), so there was no activity badge, no session
  open or close, and no permission gate
- the permission gate is not one of those hooks. It is a dotfiles script, and a room without dotfiles has none
- `-Install codex` dropped `codex-code-mode-host`, so codex could run no command (row 9)
- `room-git.ps1` on a Windows room ran Cygwin git, and every worktree's gitfile said `/cygdrive/c/...`, which Git for
  Windows cannot read (the room-git section)

Each of those was found by a worker failing. A file that says what the project needs turns each into a line that
fails BEFORE a worker is launched, with the fix beside it. And it is per project because the answer is: atrium needs
go and a Git for Windows, an OpenZiti SDK review needs cmake and a FIPS controller, and one room may take both.

## 2. The file

`atrium.requirements.yaml` at the project's root, committed with the project. YAML, for the reasons
`docs/scm-design.md` part two gives (a human edits it and reviews its diffs, and comments matter in a policy file).
`go.yaml.in/yaml/v3` is already in atrium's module graph as an indirect dependency, so parsing it adds no new
module.

It describes the PROJECT, never a room. It holds no hostname, no room name, no absolute path, and no credential.
Paths are templates (`{home}`, `{owner}`, `{repo}`, `{clone}`) resolved on the room. That is scm-design's absolute
path rule, and it holds for the same reason: the file is read on machines that are not this one.

### 2.1 Atrium's own file

```yaml
# atrium.requirements.yaml: what a room needs to take atrium's work
version: 1

atrium: { min: 7eb1555 }     # the room's build has item 67's trust fix. an ancestor check on the hub side

git:
  base: claude/main          # what hub-main mirrors, pushed from the hub side
  mirror: hub-main           # the branch the room's clone has checked out. nobody commits on it
  clone: "{home}/git/github/{owner}/{repo}"
  worktrees: "{clone}-worktrees"
  fresh: true                # hub-main must equal base on the hub side before a launch

toolchain:
  go:   { from: go.mod }                      # >= what go.mod says
  node: { min: "24" }
  git:  { min: "2.39", windows: git-for-windows }   # a Cygwin or MSYS git counts as missing
  pwsh: { min: "7", os: [windows] }

runners:
  claude:
    hooks: atrium            # the ten atrium hooks, everything but Stop
    gate: required           # the permission gate, first in PreToolUse
    mcp: [atrium-control]    # on the runner row's --mcp-config
    smoke: true
  codex:
    helpers: [codex-code-mode-host]
    smoke: true

room:                        # @rnd's slot, room-autostart-design section 4
  survives: none             # none | logoff | reboot. atrium asks for reboot once the spikes land
  runner_auth: [claude, codex]   # atrium-87300: codex joins once its smoke passes. it did, f-005 m1mini row 10

env: {}                      # names the runner must see. values only when they are not secrets
services: []                 # names from the inventory (f-003), never a URL. nothing for atrium
```

### 2.2 What each section means

- **`atrium`** is the floor the room's build must reach, as a sha, checked on the hub side with
  `git merge-base --is-ancestor <min> <room's commit>`. There are no release tags yet, so a sha is the only honest
  floor. A room whose hello says `dev`, or no version at all, is `warn cannot tell`, never `ok`.
- **`git`** is room-git's shape made explicit. `fresh` is checked on the hub side: the room's `hub-main` equals `base`
  here. The clone and worktree templates are room-git's defaults, and a project that differs says so here instead of
  in flags every director has to remember.
- **`toolchain`** uses room-toolchain's "WHAT IS GOOD ENOUGH" rules, and names the same four tools. A new tool is a
  new key, and a key room-toolchain has no installer for is check-only (the fix is `human`, naming the tool).
  `os:` limits a tool to some platforms.
- **`runners`** is per-runner setup beyond "is it installed". `hooks` and `gate` are separate on purpose, because on
  today's rooms they are separate things (section 4). `hooks` takes one value, `atrium`, meaning atrium's own set,
  and never names a command, so a project cannot bring a hook of its own onto a room. `helpers` are files that must sit beside the runner binary the
  room resolves. `smoke` asks for that runner's round trip.
- **`room`** belongs to @rnd. `survives` and `runner_auth` mean what room-autostart-design section 4 says they mean,
  including the rule that `runner_auth` is only believed when the answering room is the supervised one.
- **`env`** is a map from a name to either a plain value or `{ required: true }`. A required name is checked for
  presence only, in the room process (section 4, the preflight), and its value is never read or printed. A secret
  never goes in as a value: the file names the variable and a human sets it on the room.
- **`services`** are names resolved against the inventory of f-003, so the file never publishes an internal address.
  Until f-003 exists this section is parsed and reported as `skip`.

## 3. The command

```
pwsh -File scripts\room-check.ps1 <room> [-Project <checkout>] [-Fix] [-Yes]
```

- It runs on the hub side, like the three scripts, and for the same reason: all git and ssh live there, and the
  binary never learns git (`docs/remote-launch.md` section 3).
- `-Project` defaults to the checkout the script sits in, so a bare `room-check.ps1 sg3` checks sg3 against atrium's
  own file.
- Without `-Fix` it writes nothing anywhere. With `-Fix` it runs every `apply` fix whose scope is machine or room
  (section 4). Two kinds also need `-Yes`: a room restart, because a real room's restart is clint's call (@rnd's
  review of `-Restart`), and any ACCOUNT-scope fix, because it changes every project and every room on that account,
  and the file asking for it may come from somebody else's repository. Before either, the line prints the change and,
  for a file it edits, the backup path.
- It does NOT reimplement a check a script already has. It calls `room-toolchain.ps1 -Check`, the room-git verbs,
  provision's `-SmokeOnly` and `-Restart`, and the room's own API through the hub (`/v1/harnesses`,
  `/v1/hooks/install`). The new code is the file, the table in section 4, and the dispatch.
- The file is parsed by the binary, `atrium requirements <file> --json`, so the schema has exactly one parser and
  one set of error messages. The script reads the JSON. This is the only part the binary gains.
- `provision-room.ps1` runs it as its last step for `-Repo`'s project, so a bare machine ends at "meets atrium's
  requirements", not just "a room".

One line per requirement, the shape the other scripts use:

```
room-check <requirement> <status> <detail>
```

`status` is `ok`, `done` (fixed now), `warn`, `fail`, `skip`, or `human`. `human` is a failure whose fix only a
person can do, and its detail is the exact command and who runs it. The last line is `room-check done ok` or
`room-check done fail <code>`. The exit codes are 0 (met), 1 (local: bad file, no hub), 2 (ssh), 3 (unmet, and fixable
with `-Fix`), 4 (unmet, and a human is needed), and 5 (met except a restart it was not allowed to do).

## 4. Every requirement, its check, its fix, and who

"Scope" says what the requirement is a fact about. It matters as soon as two rooms share a machine (section 5).
"Fix" is `apply` (the script does it with `-Fix`, and with `-Yes` too at account scope), `restart` (needs a room
restart, and so `-Yes`), or `human`.

| Requirement | Scope | Check | Fix | Found |
| --- | --- | --- | --- | --- |
| room attached | room | the hub's room list, by the name the hub knows | `provision-room.ps1` | today's flow |
| binary current enough | room | the file's `atrium.min` is an ancestor of the room's hello version. `dev` or none is `warn cannot tell` | provision, then `restart` | sg3 row 2 |
| state dir pinned | room | `room.json` under the dir the manifest recorded | provision rewrites the manifest | @rnd's `-Restart` review, point 2 |
| toolchain on disk | machine | `room-toolchain.ps1 -Check` | `apply`: `room-toolchain.ps1` | sg3 rows 3 and 4 |
| toolchain on the ROOM's PATH | room | `POST /v1/preflight` `tools` (below) | `restart` after the record changes | sg3 rows 7 and 9 |
| env present | room | `POST /v1/preflight` `env_present`, booleans only | `human`: set it on the room | none yet |
| clone exists, mirror checked out | machine | `room-git.ps1 init` in a check mode (new) | `apply`: `room-git.ps1 init` | today's flow |
| `hub-main` fresh | machine | the remote `hub-main` sha against `base` here | `apply`: `room-git.ps1 push-base` | sg3 row 5, m1mini row 4 |
| worktree gitfiles readable | machine | `git worktree list --porcelain` with the git the file names, every gitfile a native path | `apply`: `git worktree repair`, `git worktree prune`, with that same git | the /cygdrive finding |
| runner installed and enabled | room | `/v1/harnesses` row, `found` and `enabled` | provision `-Install`, then the row | m1mini row 6 |
| runner helpers present | machine | each helper beside the binary the row resolves | `apply`: provision's runner install (f-007) | m1mini row 9 |
| runner mcp-config | room | the row's args carry `--mcp-config` with atrium-control | `apply`: provision's `PUT /v1/harnesses/<runner>` | the fb01 flow |
| atrium hooks | account | `/v1/harnesses` setup check `hooks` | `apply` with `-Yes`: `POST /v1/hooks/install`, the ten events, no Stop | m1mini row 7 |
| permission gate | account | PreToolUse's first entry is the gate, and it reaches this room | `human` today, `apply` once f-006 ships | f-005 findings |
| runner signed in | room | `POST /v1/preflight` `runner_auth`, believed per @rnd section 4 | `human`: `ssh -t <room> claude auth login`, `codex login --device-auth` | sg3 and m1mini |
| survives | room | @rnd section 4, `survives` | `human`: the one elevated command @rnd's design prints | reboot findings |
| smoke, per runner | room | a card on the room says back and exits. cwd is the clone, never home (item 67 refuses home) | none. a failing smoke is a `fail` with the card's own words | sg3 row 6, m1mini rows 5 and 9 |

Notes on the rows that are not simple:

- **The preflight, and why the room answers it.** Every toolchain fact that mattered was about the room process, and
  ssh sees a different PATH (sg3: ssh found Git for Windows while the room still ran Cygwin git, until its restart).
  Measuring from ssh is a guess. So the room answers, through one verb on its HUMAN listener, `POST /v1/preflight`
  (agreed with @rnd, replacing the `runner-auth` name in room-autostart-design section 4). The body names what to
  check, `{"runner_auth": ["claude"], "tools": ["go", "node", "git", "pwsh"], "env_present": ["NAME"]}`. The answer
  is per item, with the room's pid and its `--started-by` value, so section 4's corroboration applies to every item
  and not only to sign-in. @runtime builds it.
  - **The caller names keys, never commands.** The room maps each tool key to a fixed command from a table built
    into the binary (`go version`, `node --version`, `git --version`, `pwsh -v`). An unknown key gets a PATH lookup
    only, with no exec, and is reported `unknown key, resolved at <path>`. A verb that ran whatever command a caller
    sent would be remote command execution on every room, for anyone who can reach the board, overlays included.
  - Every command is bounded, 10s each and 60s in all, with its output capped, the way a source is.
  - `env_present` answers booleans and never a value.
  Until it exists the check measures from ssh the way the room starts (room-env dot-sourced on Windows, a login shell
  elsewhere) and says `warn measured from ssh, not from the room`.
- **Hooks.** They are written into the account's `settings.json`, so installing them changes what every tool call on
  that account does, for every project and every room on it. That is why f-005 put the first install to
  atrium-87300, and why an account-scope fix needs `-Yes` as well as `-Fix`: the file asking may come from somebody
  else's repository. The file can only say `hooks: atrium`, which is atrium's own set. Before applying, the line
  prints the one-line change and the backup path, as `/v1/hooks/install` already keeps one.
- **Gitfile repair uses the file's git.** `git worktree repair` rewrites gitfiles in the git's own path form. On a
  machine two rooms share, repairing with a different git than the other room reads would break that room's view,
  which is the /cygdrive bug the other way round. So the check and the repair both run the git the `toolchain` key
  names (Git for Windows on Windows), found through the toolchain record, never whichever git the ssh session finds.
- **The gate.** Today it is `~/.claude/hooks/atrium-perm-hook.ps1` from dotfiles, registered first in PreToolUse with
  `ATRIUM_PERM_GATE=on` and `ATRIUM_HUB_URL` pointing at the room's agent port. Copying it to a room needs clint's
  yes, through @dotfiles. So the check reports `human`, naming that. f-006 files atrium shipping its own gate as an
  atrium hook, after which the gate is one more event in `/v1/hooks/install`, and its fix becomes `apply`.
- **Smoke for codex** is not provision's lean smoke, which is claude only. It is a launch with `harness: codex`, args
  `-a never -s workspace-write -c sandbox_workspace_write.network_access=true`, and a prompt to run
  `atrium finish "<nonce>"`, then the nonce read off the card's recap. It moves into provision's smoke step as a
  per-runner case, so the requirements check has one smoke to call.
- **`-SmokeTo`** is qualified with this side's room name when bare (f-007), because a bare handle is not found from a
  remote room.

## 5. Two rooms on one machine (f-004)

f-004 wants a second room on a machine, to bring a new one up beside the old and move cards across. The requirements
check has to be right in that world from the start, or it will "fix" one room by breaking the other. The Scope
column is how:

- **machine** facts (the toolchain on disk, the clone, `hub-main`, worktree gitfiles, runner helpers) are shared by
  every room on the machine. Checking them for room B checks them for room A as well, and a fix is safe for both.
- **account** facts (hooks and the gate in `~/.claude/settings.json`) are shared by every room running as that
  account. This is f-004's known hazard, a second room stealing the hook pointer. The check verifies the hooks point
  at a binary and an address, and when two rooms share the account it reports `warn two rooms share these hooks`
  rather than rewriting them toward the room being checked. Hooks that follow the right room are f-004's design
  problem. This file only refuses to make it worse.
- **room** facts (attached, version, state dir, the room's PATH, runner rows, sign-in as the room sees it,
  `survives`, smoke) belong to one room, and the check names the room it asked.

A second room on one machine is only possible with its state pinned, which is @rnd's section 2.4
(`WORKTREE_ROOT` or a future `--state-dir`). So `state dir pinned` is a requirement of every room now, not just of
f-004's.

## 6. What this does not do

- It does not provision a bare machine. `provision-room.ps1` does, and ends by calling this.
- It does not carry a credential, or read one. Sign-in and `env` secrets are `human`, with the command named.
- It does not restart a room without `-Yes`, and on a real room `-Yes` is clint's.
- It does not route work. A hub that matches a card's project to rooms that meet its file is the obvious next step,
  and it is named here, not promised. It would read the same file and the same check results.
- The binary learns to parse the file and nothing else. No git, no ssh, no fix runs inside atrium.

## 7. Build order and owners

1. The file format and `atrium requirements <file> --json`. The schema, the template rules, and refusing an absolute
   path or anything that looks like a secret, the way scm-design's exporter refuses one. @runtime as r-018
   (moved from @fabric by atrium-87300, 2026-09-29), built together with item 5.
2. `scripts/room-check.ps1` with the rows that already have a checker: attached, toolchain (from ssh, with its
   `warn`), clone, `hub-main`, gitfiles, runner rows, helpers, mcp-config, hooks, smoke. @fabric.
3. `room-git.ps1 init -Check`, the check mode it lacks. @fabric.
4. codex as a smoke case in provision. @fabric, after f-007 lands. Built as f-015, see its backlog file.
5. `POST /v1/preflight` with its three questions and the fixed key table, from inside the room. @runtime, with
   @rnd's section 4.
   Items 1 and 5 were built as r-018 at 2c67bca. The shapes are in its `docs/changes/r-018.md`. The hub route for
   `/v1/preflight` is fabric's, in f-010.
6. `survives` and `runner_auth` rows, once 5 exists and @rnd's spikes have results. @fabric. `--started-by` is not
   passed on to a `--detach` or restart child, so a registration (logon task, systemd unit, LaunchAgent) must start
   `atrium room --started-by <kind>` in the foreground, which the existing registrations already do with `room
   --db`. They gain the flag here.
7. The gate as `apply`, once f-006 ships. @runtime for the hook, @fabric for the check.
8. `atrium.requirements.yaml` committed at atrium's root, which is the file in section 2.1.

## 8. Decided with @rnd (2026-09-29)

1. The name is `atrium.requirements.yaml`, for the editor and to say what it is.
2. It lives at the project root, not under `atrium.d/`, which is one machine's own configuration.
3. One preflight verb, `POST /v1/preflight`, with `runner_auth`, `tools` and `env_present`. Keys, never commands.
4. The binary floor is a key in the file, `atrium: { min: <sha> }`, checked with `merge-base` on the hub side. `dev`
   or unknown is `warn cannot tell`.
5. Hooks are `apply`, but atrium's own set only, and account scope needs `-Yes` as well as `-Fix`.
6. It runs as provision's last step, before Release fetches a room's branches, and before a director's first launch
   on a room. A director's run is the check alone, never `-Fix`, so a director never changes a room on its own. Not
   on a timer, since a source is a suggestion and this is a gate.

## 9. Decided with clint (2026-09-29)

These were the open questions. The answers came through atrium-87300.

1. **The name keeps `.yaml`.** `atrium.requirements.yaml`, as section 8 recommends.
2. **clint alone gives `-Yes` on a real room,** for a restart and for an account-scope fix (installing the hooks)
   alike. A director or atrium-87300 runs the check and `-Fix` at machine and room scope only.
3. **sg3 gets codex, the npm way,** so `runner_auth: [claude, codex]` stays as written and there is no per-room
   exception. Then clint runs `codex login --device-auth` there.
