# Splitting `Daemon` and `Proxy` so features can leave the package

Status: design only, no code. Measured at `bb68751e3` (the tip of the mechanical go-modules stack). Follows
`go-modules.md` section 6 steps 9 and 10, and `go-modules-movability.md`, which found every large set "needs design"
because of these two structs.

## How it was measured

A throwaway program (not in the repo) loaded `internal/daemon` and `internal/link` with `go/packages` and `go/types`,
once per `GOOS` of darwin, linux and windows with tags `integration,git`, and took the union. For a named struct it
records every field, every method and the file that declares it, and every file that selects a field or method of
it. Files were put in feature groups by name (the groups below, every prod file assigned, none left over). A member is
counted once per group however often it is used. "Calls out" is distinct methods of the struct a group calls that are
declared in another group. "Called in" is the reverse. "Tests" is test files in the group that select a member.

## 1. `Daemon` measured

`Daemon` (`daemon.go:105`) has **92 fields and 831 methods** across 178 prod files. 255 of the 353 test files select a
`Daemon` member directly, and 21 build one with a `&Daemon{...}` literal that skips `newWith`.

| Group | Files (prod) | Meth | Fields read | Owned | Calls out | Called in | Tests |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `peerbus` (`peers`, `messages`, `pendinginject`, `say*`, `a2a`, `replies*`, `dispatch` and the rest) | 16 | 99 | 17 | 3 | 54 | 58 | 14 |
| `sup` (`supervisor`, `term`, `hostterm`, `shell`, `attach`, `reaper`, `reopen`, `stopkeep`, `shutdown`) | 18 | 86 | 26 | 9 | 51 | 33 | 11 |
| `publish` (`overlay_*`, `docshare`, `auth*`) | 14 | 85 | 12 | 7 | 1 | 33 | 7 |
| `launch` (`launch`, `lean*`, `runner*`, `modelswitch`, `resumeclaim`, `workerpolicy`) | 17 | 82 | 24 | 11 | 40 | 38 | 16 |
| `activity` (`activity`, `park`, `idle*`, `usage`, `context*`, `seen`, `transcriptreader`) | 17 | 71 | 26 | 8 | 24 | 42 | 12 |
| `cycle` (`keepalive*`, `newcontext*`, `restartwrap`, `restartwake`, `roomhold`) | 7 | 70 | 15 | 0 | 12 | 35 | 6 |
| `reclaim` (`reclaim`, `cull`, `landed`, `sweep`, `finish`, `ledger`, `closehub`) | 16 | 66 | 15 | 3 | 30 | 34 | 13 |
| `home` (`cardhome`, `homesize`, `gitclone`, `hubremote`, `hubforge`, `forgeaccess`) | 9 | 60 | 11 | 4 | 10 | 38 | 5 |
| `items` (`item*`, `task_launch`, `task_capture`, `sources`) | 7 | 50 | 5 | 3 | 19 | 18 | 6 |
| `hublink` (`relay*`, `hubask`, `hubdial`, `stream`, `rooms`, `roomalert`) | 7 | 45 | 8 | 4 | 14 | 26 | 5 |
| `perm` (`permwait`, `letthrough`, `drain`, `shelve`, `owed`, `orphans`, `escalate`) | 7 | 31 | 6 | 1 | 20 | 20 | 7 |
| `roomconf` (`export`, `import`, `whereami`, `health`, `lifecycle`, `ready`) | 9 | 24 | 7 | 1 | 12 | 14 | 6 |
| `core` (`daemon.go`) | 1 | 24 | 30 | 4 | 209 | 5 | 2 |
| `move` (`move`, `moveroutes`) | 2 | 21 | 6 | 0 | 10 | 5 | 1 |
| `session` (`session`, `actions`, `need`, `cardlabel`) | 4 | 13 | 8 | 0 | 18 | 7 | 2 |
| `pulls` (`pulls_routes`, `hubci`) | 2 | 4 | 4 | 0 | 4 | 3 | 0 |

What the numbers say:

- **Fields are not the problem, methods are.** 58 of 92 fields are read by one group only. The wide ones are `st` (16
  groups), `hk` (14), `sup` (12), `ap` (11), `opts` and `act` (10), `nctx` (9), `pending` (7), `ctx` (6), and
  `wake`, `usage`, `launching` (5). Most are already pointers to a struct of their own.
- **33 of the 65 group pairs that call each other call both ways.** That is the real cycle. `peerbus` alone calls 54
  methods elsewhere and is called for 58. `publishTask` (`daemon.go:1160`) is called from 14 groups.
- **`core` calls 209 methods because it is the wiring.** `newWith` assigns 119 `d.ap.X = ...` function fields and 27
  `api.X = ...` package variables. `api` already reaches the daemon only through function values, so `api` needs no
  change when a method moves.
- **Six back-pointers hold the whole `Daemon`**: `pendingInjector.d` (`pendinginject.go:101`), `opencodeReader.d`
  (`opencodereader.go:116`), `claudeReader` (`transcriptreader.go:35`), `zrokDocBinder` (`docshare.go:556`),
  `newContexts.sameDirBusy(d)` (`newcontext.go:798`) and the `hooks.idleLeave` seam typed `func(*Daemon, ...)`
  (`hooks.go:34`, `idletick.go:38`).
- **`hooks` has 37 fields.** 21 are read by one group, 6 by two or more (`windDownKeyGap` by 4), and 10 by no prod
  file outside `hooks.go`.
- **The inner types are already narrow.** `activityTracker` has 36 methods, 24 used outside `activity`. `newContexts`
  has 27, 12 used outside `cycle`. `supervisor` has 16, 7 used outside `sup`, and `Registry` (`registry.go`) exists.

## 2. `Proxy` and `controlMCP` measured

`Proxy` (`proxy.go:50`) has **58 fields and 425 methods** across 111 prod files, and 68 test files select a member, 6
of them through a `&Proxy{...}` literal. `controlMCP` (`control_mcp.go:57`) has **17 fields and 102 methods**, 38 test
files select a member and 30 build one by literal.

| Group (`Proxy`) | Files | Meth | Fields read | Owned | Calls out | Called in | Tests |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `backlog` (`backlog`, `items`, `tasks`, `work*`, `deps`, `flow*`, `lifecycle*`, `efficacy*`, `audit`) | 18 | 126 | 18 | 12 | 7 | 22 | 15 |
| `route` (`proxy`, `fanout`, `cardroute`, `events`, `views`, `launchroute`, `setupwatch`, `roomspec`) | 22 | 108 | 27 | 20 | 64 | 7 | 11 |
| `forge` (`forgeroute`, `forgeci`, `prclaim`, `pulls`, `merge*`, `git_*`, `cihook*`, `eventhook`) | 14 | 101 | 14 | 11 | 14 | 22 | 13 |
| `settings` (`contextlimits`, `workerpolicy`, `inputlag*`, `mincols`, `recognis*`, `hubtransports`) | 6 | 29 | 5 | 2 | 7 | 15 | 4 |
| `notify` (`notify*`, `push*`, `webpush`, `growl`, `autoapprove`) | 4 | 22 | 6 | 3 | 5 | 9 | 5 |
| `deploy` (`deployready`, `deployqueue`, `deploy_mcp`, `newcontexts`) | 4 | 20 | 5 | 1 | 5 | 5 | 2 |
| `docs` (`docs_api`, `files_api`, `docs_mcp`, `files_mcp`) | 2 | 16 | 2 | 1 | 1 | 3 | 2 |
| `control` (`control_mcp`, `ctl*`, `control_relay*`, `hubask`, `*_mcp` with no feature) | 1 | 3 | 2 | 1 | 2 | 2 | 2 |

| Group (`controlMCP`) | Files | Meth | Fields read | Owned | Calls out | Called in |
| --- | --- | --- | --- | --- | --- | --- |
| `control` | 10 | 54 | 14 | 6 | 15 | 11 |
| `docs` | 2 | 14 | 4 | 0 | 1 | 4 |
| `deploy` | 1 | 12 | 3 | 1 | 3 | 1 |
| `backlog` | 8 | 11 | 0 | 0 | 6 | 7 |
| `forge` | 9 | 10 | 2 | 0 | 8 | 3 |
| `route` | 2 | 1 | 7 | 0 | 2 | 1 |

What the numbers say:

- **`Proxy` is a bag of features, not a tangle.** 51 of 58 fields are read by one group. Shared are `mu` (8 groups),
  `hub` (6), `feeds` (5), `ctl` (3), and `landGit`, `hubBuild`, `capStore` (2 each).
- **`route` calls 64 methods out, and they are dispatch.** `proxy.go` calls each feature's `serveX`. The features
  call back only `RecordAudit` (5 groups), `roomClient` and `inventory` (4), `roomGet` and `forgeSide` (3).
- **`p.mu` is the late-wiring guard**: 100 `p.mu.Lock()` sites in 40 files, nearly all a `SetX` and its getter.
- **`p.hub` is read as `p.hub.Rooms()` 44 times and `Has` 12.** The rest of `*conn.Hub` is used once or twice.
- **`controlMCP` is reached by features through six methods**: `ask` (4 groups), `resolvePeer` (3), `relay`,
  `launchOnRoom`, `deliverAs` (2) and `resolveCard`. The 102 methods are mostly one tool's handler each, already in
  that feature's `*_mcp.go`, so the cut is the registration seam, not the handlers.
- **Five back-pointers hold the whole `Proxy`**: `autoApprover.p` (`autoapprove.go:108`), `feeds.p`
  (`events.go:94`), `proxyCIDeliverer` (`cihook.go:623`), `deployReadyState.installed(ctx, p)` (`deployready.go:79`)
  and the `hubForge` methods taking `p` (`forgeroute.go:460,541,549,616`).

## 3. The pattern

`internal/daemon/pulls` already shows it (`pulls.Config`, `prrunner.go:93`): a feature owns a struct, the composition
root builds it with the shared state it needs and a `Config` of function values for every call into another feature,
and nothing in the feature imports the parent. Every step below applies that pattern and nothing more.

For each feature `F`:

1. **State.** `F`'s owned fields (the "Owned" column) move from `Daemon` or `Proxy` into `type F struct`. Fields that
   are a `sync.Map`, `atomic.Bool` or mutex and are shared (`windingDown` 4 groups, `launching` 5, `startedAt` 3)
   become pointers first, so every holder shares one instance.
2. **Shared state by narrow type.** `*store.Store`, `*api.Server` and `*hubstore.Store` stay concrete, since they sit
   below. `sup` is passed as `Registry`. `act` becomes an `Activity` interface of the 24 methods used outside it.
   `nctx` becomes a `NewContexts` interface of 12. `opts` is passed as the few values read. On the hub, `p.hub` is
   passed as a `Rooms` interface (`Rooms`, `Has`, `Only`, `LastSeen`, `Dial`), `feeds` as a publish function.
3. **Calls out become `Config` function fields.** The size of each `Config` is the "Calls out" column. `publishTask`
   becomes `Publish func(id string)` everywhere. Where a group calls more than about 20, a grouped interface owned by
   the callee (`peerbus.Sayer`, `launch.Launcher`) replaces a long list of fields.
4. **Calls in stay as methods on `F`**, exported in the move step. `Daemon` keeps a one-line forwarder for every
   exported name `cli` or `api` calls (52 `Daemon` names appear as a call in `internal/cli`, `cmd` or `internal/api`,
   and 48 `Proxy` names in `internal/cli`, an upper bound by name), so no caller outside the package changes.
5. **Seams.** `hooks` splits into one `Hooks` struct per feature, embedded in `hooks` so `newWith(opts, tune)` and
   every test that sets `d.hk.x` still compile. Each feature holds a `*Hooks` into the daemon's copy, never a copy.

Each feature is two commits. **Carve**: inside `package daemon` (or `link`), add `type F struct`, change the
receivers from `*Daemon` to `*F`, add the `Config`, wire it in `newWith`. Nothing moves and the compiler lists every
hidden reach. **Move**: `git mv` the files to `internal/daemon/F`, export what the "Called in" column names, update
the tests that move with them and `scripts/check-imports.go`. The carve is the refactor and the move is move-only.

## 4. Where each feature goes

| Feature | Package | Takes | Exposes to the root |
| --- | --- | --- | --- |
| `publish` | `internal/daemon/publish` (exists) | `st`, `ap`, `Registry`, `Options` values, `Publish` | `Service`: overlays, card and doc shares, auth |
| `pulls` | `internal/daemon/pulls` (exists) | `st`, `ctx`, `usage`, `Config` of 4 | the two routes and `hubci` |
| `roomconf` | `internal/daemon/roomconf` (exists) | `st`, `ap`, `NewContexts`, `Config` of 12 | export, import, health, whereami |
| `hublink` | `internal/daemon/hublink` | `st`, `peerLimit`, `Config` of 14 | relays, streams, hub dial, rooms |
| `home` | `internal/daemon/home` | `st`, `Registry`, `NewContexts`, `Config` of 10 | card homes, hub remote, hub forge |
| `items` | `internal/daemon/items` | `st`, `Launcher`, `Config` of 19 | items, tasks, sources, pools |
| `reclaim` + `move` | `internal/daemon/reclaim` | `st`, `Registry`, `Activity`, `Config` of 40 | cull, sweep, finish, ledger, move |
| `cycle` | `internal/daemon/cycle` | `st`, `Registry`, `Activity`, `Config` of 12 | `NewContexts`, keepalive, wrap, wakes, holds |
| `activity` | `internal/daemon/activity` | `st`, `Registry`, `NewContexts`, `Config` of 24 | `Activity`, usage, context sizes |
| `perm` | `internal/daemon/perm` | an ordered `[]Step` from the root, `Config` of 20 | `permWait`, decide, drain, shelve, owed |
| `peerbus` | `internal/daemon/peerbus` | `st`, `Registry`, `Activity`, `Launcher`, `Config` | `Sayer`, pending injection, replies |
| `launch` | `internal/daemon/launch` (exists) | `st`, `Registry`, `Activity`, `Sayer`, `Config` | `Launcher`, `Launch`, model switch |
| `sup` | `internal/daemon/sup` | `Activity`, `[]Sweep` from the root, `Config` | `Registry`, attach, shell, shutdown |
| `session`, `core` | stay in `internal/daemon` | | `Daemon`, `New`, the permission chain wiring |
| `docs` | `internal/link/docs` | `AuditLog`, `ctl.Caller` | routes, `registerDocs`, `registerFiles` |
| `deploy` | `internal/link/deploy` | `Rooms`, `AuditLog`, `capStore`, `ctl.Caller` | deploy ready, queue, owner |
| `notify` | `internal/link/notify` | `Rooms`, `AuditLog`, `Inventory`, publish | notifier, push, growls, approver |
| `settings` | `internal/link/settings` | `Rooms`, room client, `Config` of 7 | the fan-out settings, recognisers |
| `forge` | `internal/link/forge` | `Rooms`, `AuditLog`, `ctl.Caller`, `Config` of 14 | forge, PR claims, merge, git, CI hook |
| `backlog` | `internal/link/backlog` | `Rooms`, `AuditLog`, `ctl.Caller`, `Config` of 7 | backlog, items, tasks, deps, flow |
| `route`, `control` | stay in `internal/link` | | `Proxy`, `controlMCP`, mounting |

Two cuts are design, not plumbing:

- **`sup` calls 51 methods out, and 15 of them are `sweepX`, `watchX` and `reapX` run from its ticker.** The ticker
  takes an ordered `[]Sweep` (name, func) built in `newWith`, in today's call order. What remains is 36 calls, which
  is why `sup` is last.
- **`route` dispatches by calling `serveX`** from the if-chain in `serveHubAPI` (`proxy.go:1637`), where each branch
  matches differently (exact, prefix, either, a pair). The chain stays as written and each branch calls the feature's
  exported handler through a field, so the match order cannot change. `control_mcp.go` does the same with
  `Register(srv, ctl.Caller)` per feature, where `ctl.Caller` is the six methods above, added to `internal/link/ctl`.

## 5. The steps

Each step lands alone and leaves `claude/main` green. Steps 1 to 3 are prerequisites, then daemon steps 4 to 16 and
link steps 17 to 24 are independent of each other and can run in parallel. Risk is low (compiler-checked, no logic
touched), medium (a seam changes shape) or high (a lock or an order changes).

| # | Step | Files touched | Kind | Risk |
| --- | --- | --- | --- | --- |
| 1 | `publishTask` to a `Publish func(string)` field set in `newWith` | `daemon.go`, 47 calling files | in place | low |
| 2 | Shared `sync.Map`, `atomic.Bool`, `KeyedMutex` fields to pointers (`windingDown`, `launching`, `startedAt`) | `daemon.go`, 10 files | in place | low |
| 3 | `hooks` into per-feature `Hooks` embedded in `hooks`. Retype `idleLeave` off `*Daemon` | `hooks.go`, `idletick.go` | in place | medium |
| 4 | `publish`: carve, then move | 14 files, 7 tests | carve, move | low, 1 call out |
| 5 | `pulls` rest: `pulls_routes.go`, `hubci.go` into `pulls` | 2 files | carve, move | low |
| 6 | `roomconf`: carve, then move | 9 files, 6 tests | carve, move | low |
| 7 | `hublink`: carve, then move | 7 files, 5 tests | carve, move | medium, 26 called in |
| 8 | `home`: carve, then move. Retype `zrokDocBinder` | 9 files, 5 tests | carve, move | medium |
| 9 | `Activity` and `NewContexts` interfaces, `sup` callers onto `Registry` only | `activity.go`, `newcontext.go`, 41 files | in place | medium |
| 10 | `cycle`: carve, then move. `sameDirBusy` takes a func, not `*Daemon` | 7 files, 6 tests | carve, move | medium |
| 11 | `activity`: carve, then move. `opencodeReader` and `claudeReader` take a `Config` | 17 files, 12 tests | carve, move | medium |
| 12 | `items`, then `reclaim` with `move` | 7 and 18 files, 20 tests | carve, move | medium |
| 13 | `perm`: steps 2 to 5 of the chain become a `[]Step` built in `newWith`, then move | 7 files and `daemon.go:968` | carve, move | high, order |
| 14 | `peerbus`: carve with `Sayer`, `pendingInjector` takes a `Config`, then move | 16 files, 14 tests | carve, move | high, 58 in |
| 15 | `launch`: carve with `Launcher`, then move into the existing package | 17 files, 16 tests | carve, move | high |
| 16 | `sup`: ticker `[]Sweep`, carve, then move | 18 files, 11 tests | carve, move | high |
| 17 | `Rooms` interface over `*conn.Hub`, `AuditLog` and room client passed as values | `proxy.go`, 37 files | in place | low |
| 18 | `ctl.Caller` in `internal/link/ctl`, every `registerX` takes it | `ctl/ctl.go`, 14 `*_mcp.go` | in place | medium |
| 19 | `serveHubAPI` branches call feature handlers through fields, the chain unchanged | `proxy.go` | in place | low |
| 20 | Per-feature mutex in place of `p.mu` for that feature's fields | 40 files, 100 sites | in place | high, see 6 |
| 21 | `docs`, then `deploy`, then `notify`: carve, then move | 4, 4, 7 files | carve, move | low |
| 22 | `settings`, then `forge`: carve, then move. `hubForge` takes `Rooms`, not `*Proxy` | 6 and 14 files | carve, move | medium |
| 23 | `backlog`: carve, then move | 18 files, 15 tests | carve, move | medium |
| 24 | Tests that build `&Daemon{}` (21), `&Proxy{}` (6) or `&controlMCP{}` (30) go through a constructor | per package | test only | low |

Step 24 is not last in time. Each feature step converts the literal tests it touches, and step 24 sweeps the rest.

## 6. Where behavior changes, unavoidably

The aim is zero. These are the places where the split cannot be a pure refactor, each to be argued in its step:

1. **`p.mu` split (step 20).** Three critical sections read fields of two features in one hold: `proxy.go:1354` (`stock`
   and `approver`, the approver built under the lock), `setupwatch.go:258` (`hubCode`, `hubBuild`, `landGit`) and
   `lifecycle_git.go:214` (`landGit`, `hubBuild`). After the split these are separate reads. Safe only if the setters
   run before `Serve`. The step must prove that, or keep `hubBuild`, `hubCode` and `landGit` set once at construction,
   which is a change to `SetX` being callable late.
2. **The permission chain (step 13).** Today `onPermRequest` calls each step inline and may read state between them.
   A `[]Step` evaluates the same functions in the same order, but a step that read a value computed by an earlier one
   now needs it passed. Any such value is named in the step, and the chain tests in `perm` run before and after.
3. **The `sup` ticker (step 16).** The sweeps run in a fixed order today by being written in that order. As a slice
   built in `newWith` the order is data, so a later edit to `newWith` can reorder it silently. A test pins the names.
4. **Hooks seen at call time (step 3).** Tests set `d.hk.x` after `newWith` returns, and every read is `d.hk.x` at
   call time. A feature that copied its `Hooks` at construction would not see `tune`. Holding `*Hooks` keeps it.
5. **`newWith` order.** Goroutines start in `newWith` and `Run` in a fixed order. Each carve keeps the constructor
   call where the fields were initialised. Moving one is a behavior change and is called out in its step.

Nothing else changes: the 119 `api` function fields and 27 `api` package variables are reassigned to the same
functions on a new receiver, and every exported `Daemon` and `Proxy` name `cli` uses stays as a forwarder.

## Risks

- **`peerbus`, `launch` and `sup` (steps 14 to 16) are each larger than everything before them.** If the carve in step
  14 shows a `Config` past about 30 fields, stop and regroup the features before moving more.
- **Open worktrees.** Every carve renames receivers in many files. Land them one at a time and announce each.
- **Counts drift.** These are at `bb68751e3`. Re-measure the "Calls out" and "Called in" of a feature before its step.
