# Splitting atrium into Go modules

Status: plan only. No Go code moves with this note. Numbers come from `go list` on `claude/go-modules-plan` at
`3c3126ff0` and from reading the code, and each claim that a later step depends on names its file and line.

## What this is for

Three gains, in the order they matter:

1. **Separation the compiler enforces.** Today `internal/daemon` (181 files, 58k lines) holds supervision, the
   permission
   chain, the peer bus and launching in one package, so any file can reach any other concern's state.
2. **A test suite and a CI path filter per module.** A change to the pty host should not run the daemon's 1989 tests.
3. **Less context for an agent.** One module, one `AGENTS.md`, one import list to hold in mind.

Two facts shape everything below, so they come first.

**Fact 1: Go's `internal/` rule is by path, not by module.** `github.com/dovholuknf/atrium/internal/store` can be
imported by anything whose path starts with `github.com/dovholuknf/atrium/`, in any module. A nested `go.mod` does not
change that. So "a module cannot import another module's `internal/`" is true only for an `internal/` that sits INSIDE
the module (`store/internal/x` is private to `store/...`). The module line itself is enforced a different way: the
`require` graph. A module that does not require another cannot build against it when built alone with `GOWORK=off`.
Inside a `go.work` workspace every member is visible to every other, so the workspace hides a missing `require`. The
check that makes the boundary real is therefore a per-module `GOWORK=off go build ./... && go vet ./...` in CI, plus a
script that pins each module's allowed `require` set (section 6).

**Fact 2: a package split is cheaper and stronger than a module split for daemon and link.** Two packages in one module
cannot import each other cyclically, so a child package cannot reach back into `Daemon`. That alone ends "one file
reaches into another concern". Modules add test and CI scoping on top, and only pay for themselves at coarse grain.
The plan therefore cuts daemon and link into PACKAGES first, and into modules later, if at all.

## 1. The import graph

Produced with `go list -f '{{.ImportPath}} {{.Imports}}' ./...`, filtered to `internal/` and `cmd/`. 57 packages. The
list is by layer: a package sits one layer above the highest layer it imports. There are no import cycles (Go forbids
them), so the questions are which edges would turn into a cycle across a module line, and those are in "Blockers".

### Layer 0, leaves (import no atrium package)

`cardcolors`, `cardproc`, `cardroot`, `cardurl`, `codeid`, `dblock`, `deployready`, `detach`, `edge`, `forge`,
`inputlag`, `itemgate`, `land`, `linkfetch`, `mcprule`, `nowindow`, `prreview/render`, `remoteaccess`, `requirements`,
`resources`, `rolebin`, `safepath`, `scratch`, `shellpick`, `sshcfg`, `sshkey`, `sshtarget`, `tokenprice`, `webasset`,
`zitictx`. Test helpers `testdiag`, `testguard`, `testwait`. Programs `cmd/ptyhost-spike`, `tools/devloop`.

### Layer 1

| Package | Imports |
| --- | --- |
| `claudeconf` (7 files) | `mcprule`, `rolebin` |
| `guard` (7) | `nowindow` |
| `ptyhost` (14) | `detach`, `rolebin` |
| `store` (80) | `dblock`, `mcprule` |
| `wtfree` (2) | `nowindow` |
| `tools/codeid` | `codeid` |

### Layer 2

| Package | Imports |
| --- | --- |
| `gitsync` (50) | `claudeconf`, `nowindow`, `safepath` |
| `cardreview` (1) | `nowindow`, `safepath`, `store` |
| `runnerprofile` (1) | `store` |
| `roomstats` (9) | `store`, `zitictx` |
| `sshwizard` (4) | `nowindow`, `remoteaccess`, `sshcfg`, `sshkey` |
| `scripts/recognisers` | `store` |

### Layer 3

| Package | Imports |
| --- | --- |
| `hubstore` (38) | `cardcolors`, `dblock`, `gitsync`, `itemgate`, `sshkey`, `tokenprice` |
| `runnersetup` (6) | `claudeconf`, `runnerprofile`, `store` |
| `shepherd` (5) | `gitsync`, `nowindow` |
| `worktrees` (1) | `gitsync` |

### Layer 4

| Package | Imports |
| --- | --- |
| `roomspec` (12) | `runnersetup` |
| `api` (89 files, the room's HTTP surface) | `cardcolors`, `cardproc`, `cardreview`, `cardroot`, `cardurl`, `claudeconf`, `edge`, `forge`, `gitsync`, `inputlag`, `mcprule`, `nowindow`, `prreview/render`, `runnersetup`, `safepath`, `scratch`, `shellpick`, `store`, `webasset`, `wtfree` |
| `link` (131 files, the hub and the room link) | `cardcolors`, `cardurl`, `deployready`, `edge`, `forge`, `gitsync`, `hubstore`, `inputlag`, `itemgate`, `land`, `linkfetch`, `nowindow`, `remoteaccess`, `resources`, `store`, `tokenprice`, `webasset`, `zitictx` |

### Layer 5

`daemon` (181 files) imports `api`, `cardroot`, `claudeconf`, `edge`, `forge`, `gitsync`, `hubstore`, `inputlag`,
`linkfetch`, `mcprule`, `nowindow`, `prreview/render`, `ptyhost`, `roomstats`, `runnerprofile`, `runnersetup`,
`safepath`,
`scratch`, `shellpick`, `store`, `worktrees`, `wtfree`, `zitictx`.

### Layer 6 and 7

`cli` (101 files) imports 26 packages including `api`, `daemon`, `link`, `hubstore`, `store`, `gitsync`, `ptyhost`,
`guard`, `shepherd`, `roomspec`. `cmd/atrium` imports only `cli`. `cli` is the one place the hub and the room meet.

### What the graph says

- **`daemon` and `link` do not import each other.** They are siblings under `cli`. `link/AGENTS.md` says as much and
  `internal/link/address.go` is a deliberate copy of `internal/daemon/address.go`. That is the best news here: the hub
  and the room already split cleanly, and a `hub` module and a `room` module have no edge to cut between them.
- **`daemon` imports `api`**, not the reverse. The room's wiring sits on top of its HTTP surface, so `api` is a
  dependency of the room and must be cut before, or together with, it.
- **`hubstore` imports neither `store` nor `api`.** The hub's database and the room's database are already separate
  packages, which is what the architecture wants.
- **`link` imports both `hubstore` and `store`**, which the hub should not do, see Blocker B1.

### Blockers for a module line

| | Edge | Why it blocks | Cut |
| --- | --- | --- | --- |
| B1 | `link` to `store` in six files: `contextlimits.go:13`, `control_mcp.go:23`, `openroute.go:13`, `recognisers.go:14`, `recogniseroute.go:19`, `workerpolicy.go:13` | The hub pulls the room's 658-symbol database package for pure values: `store.Recogniser` (`store/recognisers.go:36`), `MatchRecogniserIn` (`:409`), `FillArgv` (`:610`), `DescribeCwd` (`:572`), `CheckRecogniser` (`:288`), `WorkerPolicy` (`store/workerpolicy.go:15`), `CheckContextLimits` (`store/contextcycle.go:48`), `ClassifyLink` (`store/linkkey.go:28`), the `Setting*` names | Move those values to a new leaf package, `internal/rules`. `store` and `link` both import it |
| B2 | `daemon` to `hubstore` at `daemon/docshare.go:209,216,250` | The room reaches the hub's database package for `SecretFileName` (`hubstore/docs_secrets.go:134`) and `SecretContent` (`:89`). `daemon/item_hub.go:41` already says "which must not import the hub's store" | Move the two functions to a leaf `internal/secrets`. After that `daemon` has no `hubstore` import |
| B3 | `gitsync` to `claudeconf` | `gitsync` is layer 2 only because it reads Claude's config. A `gitsync` module needs `claudeconf` below it | `claudeconf` goes in the `base` module |
| B4 | `ptyhost` to `rolebin`, `claudeconf` to `rolebin` | `rolebin` is shared by two candidates | `rolebin` goes in `base` |
| B5 | `link/address.go` copies `daemon/address.go` | Two copies with a table test each. They cannot share while neither imports the other | Move one copy to `base/address`, delete the other |
| B6 | `hubstore/prclaim.go:33` and `store/prs.go:931` both define `PRKey` | Same function in both databases | Keep one in `internal/rules` |
| B7 | Test helpers `testwait`, `testguard`, `testdiag` are imported by tests in `api`, `cli`, `daemon`, `gitsync`, `link` | A test helper is a module dependency. Every module that tests with it must require it | They go in `base` |
| B8 | The ldflags in `Makefile:23` name `internal/api.CodeID` | A moved or renamed package breaks the stamp silently (the build still succeeds, the stamp is empty) | Change the path in the same commit as the move, and check `atrium version` in the step |

## 2. Splitting `internal/daemon`

181 files, 58k lines, 1989 tests, and 806 methods on `*Daemon`. The architecture note in `daemon/AGENTS.md` lists
supervision, the permission chain and launching as separate concerns. The code does not.

### What ties it together today

These are the things to cut FIRST, in this order, because every package below needs them gone.

1. **`Daemon` is one struct of over 80 fields** (`daemon.go:100` to `:376`). The fields are the hidden coupling.
   `d.sup *supervisor` (`daemon.go:148`) is read in 56 files, `d.act *activityTracker` (`daemon.go:186`) in 32. A
   subpackage cannot see `Daemon`, so each concern must receive what it needs, not the whole struct.
2. **`runner` is a 70-field struct** (`supervisor.go:690`) that holds the pty, the screen model (`esc escParser`, `buf
   *ringBuffer`, `carried *carryover`, `views`), the launch spec (`spec *launchSpec`, `supervisor.go:2228`) and the exit
   cause. 53 methods hang off it in `supervisor.go` alone. Messages, the reaper, the shell and the attach socket all
   hold `*runner` directly. Cut: an exported `Runner` interface (write text, read screen, alive, exit cause, resize) in
   the supervision package, and consumers take that.
3. **`supervisor` (`supervisor.go:2095`) is reached as `d.sup.get(taskID)`** in 56 files. Cut: a `Registry` interface
   with
   `Get`, `Has`, `All`, `Add`, `Remove`, injected into each concern.
4. **`hooks` (`hooks.go:15`) is one struct of test seams for the whole daemon** and `d.hk` is read in `newcontext.go`
   (23 places), `pendinginject.go` (13), `replies.go`, `lean_agents.go`, `daemon.go`. A seam that belongs to one concern
   must live with it. Cut: each package owns its own `Hooks` struct, `hooks.go` shrinks to the composition.
5. **Package-level seams that take `*Daemon`**: `stopAfterReport` (`exitonreport.go:33`), `commitOnHub`
   (`finish.go:210`), `healthHalted` (`health.go:20`). A package variable typed with `*Daemon` pins its package to
   `Daemon` for good. Cut: take an interface, or move the seam into the owner's `Hooks`.
6. **Package-level mutable state**: `lastReplyCache` and `lastReplyScans` (`keepalive.go:310,317`), `discovered`
   (`auth_flow.go:46`), `jwksCache` (`auth_jwks.go:29`), `forkCarried` (`keepalive.go:874`). These are process
   singletons that cross daemon instances and so cross concerns. Cut: hang them off the owning struct.
7. **`LaunchRequest` (`launch.go:26`) is the type every launch path builds.** `launchCore` (`launch.go:913`) is 1230
   lines with launch, cardhome, lean, hub remote and login checks interleaved. Cut: the type moves to the launch
   package first and the other packages only ever hand it through.
8. **The permission chain is one function in `daemon.go`** (`onPermRequest`, `:938`) that calls into messages (step 2),
   shelving (step 3), rules (4), auto mode (5) and the human wait (6) directly. The ORDER is the invariant. Cut: each
   step becomes a function the chain package calls from one ordered slice, so the order is in one place and each step
   lives with its concern.

### Files with no `Daemon` dependence at all

36 files never touch `*Daemon` or a `d.` field, so they can move with a `git mv` and a package clause, no refactor:
`screen.go` (1535), `screen_repair.go`, `screen_width.go`, `screen_width_tables.go`, `escstate.go`, `escclear.go`,
`idleframe.go`, `pastewrap.go`, `flatten.go`, `keyedmutex.go`, `prrunner.go` (1556), `lean.go` (709), `lean_agents.go`,
`firstrun.go`, `gitbash.go`, `prepare*.go`, `script_*.go`, `killtree_*.go`, `alive_*.go`, `opencodeproc_*.go`,
`overlay_zrok_errors.go`, `auth_jwks.go`, `auth_limit.go`, `inputlag.go`, `interview.go`, `usagebackfill.go`,
`replay_api.go`, `pprof.go`, `nowindow.go`, `ziti_enroll_sdk.go`. That is 9k lines that move first because they cost
nothing.

### The proposed packages

Each lives under `internal/daemon/` and may import only the packages marked below it. `daemon` itself stays as the
composition root: `Options` (`daemon.go:35`), `Daemon`, `New` (`:377`), the two listeners, shutdown and the wiring of
every package into every other.

| Package | Concern | Files |
| --- | --- | --- |
| `screen` | pure terminal model, no I/O | `screen.go`, `screen_repair.go`, `screen_width*.go`, `escstate.go`, `escclear.go`, `idleframe.go`, `pastewrap.go`, `flatten.go`, `typedline.go`, `typedproof.go`, `keyedmutex.go` |
| `sup` | supervision: ptys atrium owns | `supervisor.go`, `term.go`, `hostterm.go`, `shell.go`, `attach.go`, `reaper.go`, `carryover.go`, `fixtures.go`, `reopen.go`, `stopkeep.go`, `shutdown.go`, `alive_*`, `killtree_*`, `opencodeproc_*`, `prepare*.go`, `script_*.go`, `settling.go` |
| `perm` | the permission chain | `permwait.go`, the chain from `daemon.go:938`, `letthrough.go`, `drain.go`, `shelve.go`, `owed.go`, `orphans.go`, `escalate.go`, `permwait`'s `AutoDecision` (`permwait.go:81`) |
| `peerbus` | messages between sessions, queued and delivered by hook | `peers.go`, `pendinginject.go`, `messages.go`, `keep.go`, `saywhen.go`, `forcesay.go`, `launchersay.go`, `saylog.go`, `a2a.go`, `endonce.go`, `fyi.go`, `help.go`, `nosession.go`, `replies.go`, `repliesall.go`, `dispatch.go` |
| `activity` | what a runner is doing now, and what it costs | `activity.go`, `looksidle.go`, `idletick.go`, `idlepark.go`, `park.go`, `outputat.go`, `contextsize.go`, `contextlog.go`, `telemetry.go`, `usage.go`, `usagebackfill.go`, `turncost.go`, `turns.go`, `autocontext.go`, `autocompact.go`, `seen.go`, `fleet.go` |
| `launch` | spawning a runner and the home it gets | `launch.go`, `cardhome.go`, `homesize.go`, `homedisk.go`, `lean.go`, `lean_agents.go`, `runnersetup.go`, `runnerupdate.go`, `firstrun.go`, `gitbash.go`, `loginneeded.go`, `preflight.go`, `gitclone.go`, `hubremote.go`, `hubworktree.go`, `hubforge.go`, `forgeaccess.go`, `task_launch.go`, `task_capture.go`, `throwaway.go`, `worktrees.go`, `scratch.go`, `roomsize.go`, `launchstuck.go`, `relay_launch.go`, `recognise.go`, `item*.go`, `learnings.go`, `reportto.go`, `interview.go`, `workerpolicy.go`, `modelswitch.go`, `resumeclaim.go`, `restart_session.go` |
| `reclaim` | how a card ends and what is taken back | `reclaim.go`, `cull.go`, `mergedcull.go`, `landed.go`, `sweep.go`, `vacuum.go`, `archiveworkers.go`, `finish.go`, `exitonreport.go`, `unexpectedexit.go`, `worktreegone.go`, `move.go`, `moveroutes.go`, `mergeproof.go`, `closehub.go`, `closestash.go`, `ledger.go`, `record.go` |
| `cycle` | restart and context cycles | `keepalive.go`, `keepalive_cold.go`, `newcontext.go`, `newcontext_journal.go`, `restartwrap.go`, `restartwake.go`, `roomhold.go` |
| `publish` | reaching the board from elsewhere | `overlay_*.go`, `ziti_enroll_sdk.go`, `docshare.go`, `overlay_guest.go`, `auth*.go` |
| `pulls` | the pulls view | `prrunner.go`, `changes.go`, `hubci.go` |
| `roomconf` | what the room says about itself | `export.go`, `import.go`, `whichdb.go`, `whereami.go`, `health.go`, `roomstats.go`, `lifecycle.go`, `pprof.go`, `inputlag.go`, `relay.go`, `hubask.go`, `hubdial.go`, `stream.go`, `address.go` |

Allowed direction inside the daemon, lowest first. An arrow means "may import":

```
screen            (imports nothing in daemon)
sup               -> screen
activity          -> screen
perm              (imports nothing in daemon, receives its steps)
peerbus           -> sup, activity
launch            -> sup, activity, peerbus
reclaim           -> sup, activity, launch
cycle             -> sup, activity, peerbus
publish           -> sup
pulls, roomconf   (leaves)
daemon            -> all of them
```

`perm` is the one with no downward import and the most important rule: it takes its steps as values from `peerbus`
(a queued message), `reclaim` (a shelved card) and the store (a rule), and calls them in the order the AGENTS.md lists.

### Why this order is workable

Of the eight ties above, only ties 2 and 3 (`runner`, `supervisor`) are deep. Everything in `screen`, `pulls`,
`publish` (minus `overlay_guest.go`, which reads `d.sup` to build its allowlist) and the pure files of `launch` have
no tie to cut. `sup` is the hard one: its 56 callers each need the `Registry` interface in place before it can move.

## 3. Splitting `internal/link`

131 files, 45k lines, 1036 tests. This split is easier than the daemon's. Only 12 files have methods on `*Hub`
(`hub.go:29`) and 11 on `*Room` (`room.go:25`). Most of the code is free-standing types that already reach the hub's
store through a narrow interface: `AuditLog` (`audit.go:37`), `Inventory` (`proxy.go:1258`), `DepsStore` (`deps.go:46`),
`GrowlStore` (`growl.go:150`), `NotifyStore` (`notify.go:126`), `PushStore` (`push.go:69`), `HubSettings`
(`launchcaps.go:38`), and the `Hub`'s own handler fields (`hub.go`, `Git`, `GitStore`, `PRClaim`, `RoomSpec`, `Forge`).
The package is a set of features that share a directory.

### What ties it together today

1. **`protocol.go` and `link.go`** define the frames and the types everything speaks (`CardState`, `RelayRequest`,
   `RelayAnswer`, `Timings`, `Path`). They are the shared vocabulary and become the first package.
2. **A concrete `*hubstore.Store` in 40-odd files** (`cihook.go:84`, `eventhook.go:86`, `control_mcp.go`, `backlog.go`,
   `merge.go`, `lifecycle*.go`, `efficacy*.go` and more). The interfaces exist for some features and not for others.
   Each new package keeps the concrete type for now, since `hubstore` is below it anyway. This is not a blocker.
3. **Package-level policy tables** read across files: `workerTools` (`ctlclass.go:50`), `itemWithheld` (`:88`), `merged`
   (`fanout.go:46`), `everywhereKinds` (`events.go:360`), `notCards` (`cardroute.go:44`), `lifecycleKinds`
   (`audit.go:151`). Each belongs to one package. A table read by two must move down to the shared one.
4. **`address.go`** is a copy (B5). **`store` imports** (B1).
5. **`control_mcp.go` (2543 lines) registers every tool** and so imports every feature's types. It is the composition
   root of the hub's MCP surface and should stay the top package, with each `*_mcp.go` file registering from its own
   package.

### The proposed packages

Under `internal/link/`:

| Package | Concern | Files |
| --- | --- | --- |
| `wire` | frames, timings, the vocabulary | `link.go`, `protocol.go`, `address.go` (the shared copy) |
| `transport` | how a connection is made | `direct.go`, `certs.go`, `overlaytls.go`, `ziti.go`, `zrok.go`, `dials.go`, `dialgate.go`, `transports.go`, `transportoff.go`, `hubtransports.go`, `detach_*.go` |
| `conn` | the hub's half and the room's half | `hub.go`, `room.go`, `hosts.go`, `restartgate.go`, `upgrade.go`, `setupwatch.go`, `predates.go` |
| `route` | scoped versus aggregate | `proxy.go`, `rooms.go`, `fanout.go`, `cardroute.go`, `events.go`, `announce.go`, `roomsevent.go`, `everywhere*.go`, `views.go`, `resolve.go`, `launchroute.go`, `launchcaps.go`, `openroute.go`, `pathstate.go` |
| `control` | the control MCP and its audit | `control_mcp.go`, `ctlclass.go`, `ctlaudit.go`, `ctl_end.go`, `control_alias.go`, `control_relay*.go`, `*_mcp.go` as registration only |
| `backlog` | items, tasks, work, gates and the lifecycle | `backlog.go`, `items.go`, `item_brief.go`, `tasks*.go`, `work*.go`, `deps*.go`, `flow*.go`, `changerequest.go`, `lifecycle*.go`, `efficacy*.go`, `audit.go`, `docdebt*.go`, `record_mcp.go` |
| `forge` | the forge, PRs and merging | `forgeroute.go`, `forgeci.go`, `prclaim.go`, `prmove.go`, `prworktreeroute.go`, `pulls.go`, `merge.go`, `mergeland.go`, `review*.go`, `git*.go`, `ci_door.go`, `cihook*.go`, `eventhook.go` |
| `notify` | what waits on a human | `notify.go`, `notifyapi.go`, `push.go`, `pushapi.go`, `webpush.go`, `growl.go`, `autoapprove.go` |
| `deploy` | deploying and what it waits for | `deployready.go`, `deployqueue.go`, `deploy_mcp.go`, `newcontexts.go` |
| `docs` | hub documents and the file drop | `docs_api.go`, `docs_mcp.go`, `files_api.go`, `files_mcp.go` |
| `settings` | hub-owned settings handed to every room | `contextlimits.go`, `workerpolicy.go`, `inputlag*.go`, `mincolssetting.go`, `recognisers.go`, `recogniseroute.go` |

Direction: `wire` imports nothing. `transport` and `settings` import `wire`. `conn` imports `wire` and `transport`.
`route`, `notify`, `deploy`, `docs`, `backlog`, `forge` import `wire`, `conn` and `hubstore`, and never each other.
`control` imports all of them and is imported only by `link` itself. The rule "two feature packages never import each
other" is the whole point, and if two need to share something it moves to `wire`.

## 4. Module boundaries

### Which become modules, and the answer on the early candidates

| Candidate | Exported symbols | Importers | Deps | Verdict |
| --- | --- | --- | --- | --- |
| `ptyhost` | 40 | `cli`, `daemon/hostterm.go` | `detach`, `rolebin` | **Confirmed narrow.** 14 files, one real client, a wire protocol already documented. The best first module |
| `gitsync` | 226 | `api`, `hubstore`, `link`, `daemon`, `cli`, `shepherd`, `worktrees` | `claudeconf`, `nowindow`, `safepath` | **Confirmed as a clean leaf, refuted as narrow.** Seven importers and 226 exported names. It cuts easily because it imports almost nothing, but its surface is wide and every change to it will ripple |
| `store` | 658 | `api`, `cli`, `daemon`, `link`, `cardreview`, `roomstats`, `runnerprofile`, `runnersetup` | `dblock`, `mcprule` | **Refuted as narrow, confirmed as a clean leaf.** 658 exported names, and the `*Store` type is passed everywhere. The module boundary still has value: it keeps SQL out of every other package, which `store/AGENTS.md` already demands. But nobody should expect a small API |
| `hubstore` | 393 | `link`, `daemon` (B2), `cli` | `gitsync` and leaves | A clean leaf once B2 is cut. Wide like `store` |

So the claim "narrow interface" holds for `ptyhost` alone. `store`, `gitsync` and `hubstore` are early candidates for a
different reason: they sit low in the graph, so cutting them cannot create a cycle.

### The module list

Directories stay where they are. **A module is a `go.mod` dropped into an existing package directory, and its module
path equals that package's current import path.** `internal/store/go.mod` says `module
github.com/dovholuknf/atrium/internal/store`, so no import in the repo changes and no file moves. This is the cheapest
design, since a mass import rewrite would conflict with every open worktree. The cost is Fact 1: `internal/` does not
fence these off by itself, and the require graph does.

| Module (path under `github.com/dovholuknf/atrium/`) | Holds | Requires |
| --- | --- | --- |
| `internal/base` (new dir) | every layer-0 leaf, plus `claudeconf`, `testwait`, `testguard`, `testdiag`, `rules` (B1), `secrets` (B2), `address` (B5) | nothing |
| `internal/store` | `store` | `base` |
| `internal/gitsync` | `gitsync` | `base` |
| `internal/ptyhost` | `ptyhost` | `base` |
| `internal/hubstore` | `hubstore` | `base`, `gitsync` |
| `internal/api` | `api`, `cardreview`, `runnerprofile`, `runnersetup`, `roomstats`, `wtfree`, `worktrees` | `base`, `store`, `gitsync` |
| `internal/hub` (was `link`) | `link` and its packages | `base`, `hubstore`, `gitsync` |
| `internal/daemon` | `daemon` and its packages | `base`, `store`, `gitsync`, `ptyhost`, `api` |
| root `github.com/dovholuknf/atrium` | `cmd/atrium`, `cli`, `guard`, `shepherd`, `roomspec`, `sshwizard`, `tools`, `scripts/recognisers` | every module above |

`base` is a grab bag, and a grab bag is a smell. It is justified here because its 35 members have no dependencies, are
tiny (most are one file) and cannot grow without gaining a second dependency. A new package that does not meet that bar
does not go in `base`. If it grows past about 40 packages, split it by theme (`base/ssh`, `base/card`).

`internal/hub` renames `link`. The rename is a separate step and optional. Keep `link` until nothing else is moving.

### Public API per module

An API is the packages and names the next layer up may use. Anything else moves under a module-private `internal/`
(for example `internal/store/internal/schema`), which Go does enforce.

| Module | Exposes | Does not expose |
| --- | --- | --- |
| `store` | `Store`, its methods, the row types, `ErrHalted` and the settings keys | migrations, `schema.go`, the halt machinery |
| `gitsync` | the sync loop, `URLQuery` and `URLAnswer`, the handlers | the transport helpers |
| `ptyhost` | `Host`, `Client`, the protocol verbs and `DefaultClientQueue`, `DefaultIdleExit` | `ring.go` (the AGENTS.md says it is deliberately not the daemon's) |
| `hubstore` | `Store` and the hub's row types | the migration set |
| `api` | `Server`, the exported setting names and helpers the daemon uses (`ScrollbackBytes`, `TranscriptPath`, `RunnerFound`, `SessionExists`, `ScrapDir`, `LatestSession`, `ContextLimitFor`) | the handlers |
| `hub` | `Hub`, `Room`, `Timings`, the control server constructor | every feature package |
| `daemon` | `Options`, `New`, `Daemon`'s lifecycle, `Location`, `ReadLocation`, `StateDir`, `RelayResult`, `SplitAddress`, `Replay` | everything else, which is what `cli` uses 15 names from today |

### Allowed import direction between modules

```
base
  ^
  |-- store, gitsync, ptyhost            (each requires base only)
  |-- hubstore                           (requires gitsync)
  |-- api                                (requires store, gitsync)
  |-- hub                                (requires hubstore, gitsync)
  |-- daemon                             (requires api, store, gitsync, ptyhost)
root (cli, cmd)                          (requires all)
```

Three rules, each one checkable by a script:

1. **`hub` never requires `daemon`, `api` or `store`.** The hub holds no work.
2. **`daemon` never requires `hub` or `hubstore`.** The room never reaches into the hub's database.
3. **Nothing requires the root module.** `cli` is the top.

## 5. go.work, go.mod, AGENTS.md, scripts, CI, build

### Layout

```
go.work                      use ( . ./internal/base ./internal/store ./internal/gitsync ./internal/ptyhost
                                   ./internal/hubstore ./internal/api ./internal/hub ./internal/daemon )
go.mod                       the root module: cmd, cli and the leftovers
internal/store/go.mod        module github.com/dovholuknf/atrium/internal/store
```

Each nested `go.mod` carries `require` lines for the modules it may use, with `v0.0.0` and a `replace` pointing at the
sibling directory (`replace github.com/dovholuknf/atrium/internal/base => ../base`). The `replace` lines are ignored
inside the workspace and are what let `GOWORK=off` build a module alone, which is how the boundary is checked. A
`go.work`
alone would be enough to BUILD, and it would hide a missing `require`.

**Third-party versions** (cobra, `modernc.org/sqlite`, the MCP SDK, ziti) are pinned per module. Inside the workspace
Go takes the highest of them, and `GOWORK=off` uses each module's own. A check step runs `go work sync` and fails on a
diff, so the two cannot drift. `go.sum` is per module and `make tidy` loops over them.

**`go vet ./...` and `./...` cover one module.** Every command that used `./...` becomes a loop over `go list -m -f
'{{.Dir}}'`, which `scripts/test.sh` and `Makefile:tidy` and `check` must learn. `build.claude/go.mod` already fences
build output off, and stays.

### Per-module AGENTS.md

Each module root gets an `AGENTS.md` of the existing shape: one paragraph on what it is, "the shape that must not
change", the invariants and the file table. Most already exist (`store`, `gitsync`, `ptyhost`, `hubstore`, `api`,
`link`,
`daemon`, `claudeconf`, `guard`, `safepath`) and move with their directory. New ones: `base` (what may join it, and the
rule above) and each split package of daemon and link (a short one, the file table and the allowed imports). The root
`AGENTS.md` "Read when" table gains one row per module. The package rules in the daemon note (the permission chain, the
two maps in supervision) go with `perm` and `sup`, where the code is.

### Test scoping: `scripts/test-touched.sh` and CI

Today `scripts/test-touched-plan.js` turns a changed file into `go test ./<dir>` (`test-touched-plan.js:358`), and
`scripts/test-buckets.json` assigns every `internal/api` and `internal/link` file to a bucket, linted by
`scripts/check-test-buckets.js`. With modules:

- **Map file to module.** Walk up from the changed file to the nearest `go.mod`. Run `go test` from that directory,
  scoped to the package, with `GOWORK` unset so the workspace applies.
- **Run the dependents.** A change in a module also runs the tests of every module that requires it, read from the
  `require` graph, so a change in `base` runs everything and a change in `ptyhost` runs `ptyhost` and `daemon`. This
  is the one piece that is new logic, and `test-touched-plan.js` already prints the reason per item.
- **Keep the flaky-suite order and the 10ms unit budget.** Both are per package and need no change.
- **`test-buckets.json` follows the files.** The moved paths change in the same step as the move, or the lint fails.

CI (`.github/workflows/ci.yml`, whose logic lives in `scripts/ci.sh`) gets:

1. A `select` output, `modules`, computed by `scripts/ci-select.sh` from `git diff` against the base plus the
   reverse-dependency set. A push to `main` runs every module. A branch run runs only the touched set. This IS the path
   filter, done in a script because Actions' `paths:` works per workflow and not per job.
2. A `go-test-<module>` group per module, replacing `go-test-1` and `go-test-2`, which cut the daemon "by test name".
   When
   `daemon` is split into packages the cut is by package and the test-name cut goes away.
3. **A `go-modules` check, on every run:** per module `GOWORK=off go build ./... && GOWORK=off go vet ./...`, then
   `scripts/check-modules.go`, which fails if a module's `go.mod` requires anything outside its row in section 4, and
   if `go work sync` changes a file. This is the step that makes the boundary real.
4. The full suite still runs after landing, by whoever lands, with `FULL_SUITE=1`, as today.

### One binary

`make build` is unchanged: `go build -ldflags "$(LDFLAGS)" -o build.claude/atrium.exe ./cmd/atrium` run at the root.
With
`go.work` present the root module resolves every sibling from disk. With `GOWORK=off` it resolves them through the
`replace` lines. Both give one binary. The one edit is the `-X` path (B8) when `api` moves.

`go.work` is committed. The release script (`scripts/release.sh`) builds from a clean checkout, so the replace lines
matter there, and its first run after step 3 is the real test of this paragraph.

## 6. The order of cuts

Every step leaves `claude/main` green, touches one thing, and lands by itself. A step that moves files does only that.
Steps 1 to 4 prepare. Step 5 is the first module. The daemon and link splits (steps 9 and 10) are independent of the
module steps and can run in parallel with them once step 4 is in.

1. **Pin the graph. No code moves.** Add `scripts/check-imports.go`, a check over `go list` that encodes the layers in
   section 1 as a table and fails when a package imports a layer it should not. It runs in `go-checks`. Landing it first
   means every later step is held to the plan and no step can quietly add an edge. Lands green by construction because
   it only describes what is true today.
2. **Cut B1 and B6.** Create `internal/rules`, move the recogniser, worker-policy and context-limit values and
   `PRKey` out of `store` and `hubstore`, leave the settings keys' string values untouched. `store` and `link` import
   `rules`. The files are `store/recognisers.go`, `store/workerpolicy.go`, `store/contextcycle.go`, `store/linkkey.go`,
   `store/prs.go:931`, `hubstore/prclaim.go:33`. Update `scripts/check-imports.go` so `link` may no longer import
   `store`.
3. **Cut B2 and B5.** `internal/secrets` for `SecretFileName` and `SecretContent`, and one `address` package for the two
   copies of `address.go`. After this `daemon` has no `hubstore` import.
4. **Introduce `go.work` with only the root module in it**, plus the loops over `go list -m` in `test.sh`, `Makefile`
   and
   `ci.sh`. Nothing is nested yet, so nothing changes, and the plumbing is proven before a module depends on it.
5. **First module: `ptyhost`.** Add `internal/ptyhost/go.mod` and the `internal/base` module holding `detach` and
   `rolebin` (the only two packages it needs), with the replace lines. Add the `go-modules` CI check from section 5
   and the `test-touched` module mapping. `ptyhost` goes first because it has 40 exported names and one real client, so
   the first mistake in the plumbing is cheap. This is also where `GOWORK=off` is first proven.
6. **Grow `base` to the full leaf set**, one commit that adds `go.mod` only to `internal/base` and moves the layer-0
   packages in, since they are all dependency-free. This step does rewrite imports, so it is a mass edit, announced and
   landed fast. Alternative that avoids the churn: give each leaf its own directory-local `go.mod`. Rejected, as 35
   modules is its own cost.
7. **`gitsync`, then `store`, then `hubstore`**, one step each, each adding a `go.mod`, the `require` and `replace`
   lines,
   an `AGENTS.md` entry, and the module row in `check-modules.go`. Order is by importers: `gitsync` (7), then `store`
   (8,
   widest), then `hubstore` (3, after B2).
8. **`api` as a module**, with `cardreview`, `runnerprofile`, `runnersetup`, `roomstats`, `wtfree` and `worktrees`.
   `test-buckets.json` already keys on `internal/api` paths, and those paths do not change. Change `Makefile:23`.
9. **Split `daemon` into packages.** In this order, each its own step with its own tests moving with it:
   1. `screen` and the other 36 files with no `Daemon` dependence (`prrunner.go`, `lean*.go`, `prepare*`, auth helpers).
      No refactor needed.
   2. Tie 4 and tie 5: split `hooks` per concern and retype the `*Daemon` package variables. No file moves, so
      reviewable alone.
   3. Tie 6: the package-level singletons onto their owners.
   4. Ties 2 and 3: the `Runner` interface and the `Registry`, with `supervisor.go` still in `daemon`. This is the
      riskiest step in the whole plan, and it touches 56 files with a mechanical change.
   5. `sup`, then `activity`, then `perm` (tie 8), `peerbus`, `launch`, `reclaim`, `cycle`, `publish`, `pulls`,
      `roomconf`. One package per step, and `go build ./...` is the check.
10. **Split `link` into packages**, in the order `wire`, `transport`, `conn`, then the feature packages, each one step.
    Start with `wire` because everything imports it. The feature packages come in any order and none depends on another.
    `control` is last, since it imports them all.
11. **Module `hub` and module `daemon`.** Add the `go.mod` files once their packages are split. If `daemon` is still one
    big package at this point, the module line adds nothing and this step can wait.
12. **Optional: rename `link` to `hub`**, and publish the module list in `docs/repo-reference.md`.

Steps 1 to 4 should take a day between them and carry no risk to the product. Step 5 is the decision point: if the
`GOWORK=off` check proves fragile in CI on Windows with Git Bash (`ci.sh` runs there), stop and fall back to the
`scripts/check-imports.go` table alone, which enforces the same directions without modules and costs nothing.

## Risks

- **The module boundary is only as strong as the `GOWORK=off` check** (Fact 1). Without it, a missing `require`
  compiles in the workspace.
- **Per-module third-party pins can drift.** The `go work sync` check holds them together.
- **Every moved test file that reaches a private field has to move with its package.** `daemon` has 1989 tests and many
  poke unexported state. Count them per package before each step in 9.
- **`scripts/check-test-buckets.js` and `scripts/check-test-globals.go`** key on paths, and need the module-aware walk.
- **Open worktrees.** Step 6 and every step that moves a file conflicts with branches in flight. Land them one at a
  time and announce each.
