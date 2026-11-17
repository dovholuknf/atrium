# Which daemon and link packages can leave the root module without a refactor

Status: a measurement, nothing moved. Taken at `8e85f0a55` on `claude/r-go-modules-movability`. Re-run it with
`go run scripts/go-modules-movability.go`, which takes the union of `go list -tags integration,git` for `GOOS` windows
and linux, then parses each package's files to name the symbols behind every edge into the root module. The raw output
(every edge, every symbol) is what this note quotes. Terms follow the dropped `docs/design/go-modules.md` (at
`4953138ab^`): a module is a `go.mod` dropped into the package's own directory, so its import path does not change, and
a package's layer is one above the highest layer it imports, as in `scripts/check-imports.go`. This updates the
unmerged `notes/modules-movability.md` on `claude/modules-movability` (`22e6a575f`, taken 729 commits earlier).

## The rule

The modules today are the root, `internal/base`, `internal/store`, `internal/gitsync`, `internal/hubstore` and
`internal/ptyhost`. Every package below is imported by a root package (`cli` at least), so the root requires whatever
module it lands in. If the moved package still imports a root package, prod or test, that module requires the root
back. Go builds the cycle, but the module then pulls the whole root into its build and its tests, and the boundary is
gone. So the cycle column below is exactly the root edges. Into `internal/base` is stricter: `base` requires nothing,
so the package may import only `base` and only third party that `base/go.mod` already has.

- **Movable**: no root edge. The work is a `go.mod`, a `require` and `replace` in the root, a `use` in `go.work`, and a
  row in `scripts/check-imports.go`. Into `base` it is a directory move plus import path edits.
- **Movable after chain**: its only root edges are packages that are themselves movable, so it moves right after them.
- **Small cut**: one named, bounded edit first (a name moved to a leaf, a one or two method interface, a root leaf
  moved out on its own, a few test files moved).
- **Struct split**: needs `Daemon` or `Proxy` (the types in `docs/design/go-modules-struct-split.md`) taken apart.

## Summary

Tests are `func Test` counts, integration files included. "Base" says whether it could go into `internal/base` as is.

| Package | Layer | Files | Tests | Root edges, the module cycle if moved alone | Base | Verdict |
| --- | --- | --- | --- | --- | --- | --- |
| `daemon/launch` | 0 | 1 | 6 | none | yes | movable |
| `daemon/promptline` | 0 | 1 | 0 | none | yes | movable |
| `link/wire` | 0 | 7 | 1 | none | yes | movable |
| `daemon/screen` | 2 | 12 | 66 | none | no (`store`) | movable |
| `link/jsonio` | 4 | 1 | 0 | none | no (`hubstore`) | movable |
| `daemon/shared` | 3 | 13 | 1 | `daemon/screen` | no | after chain |
| `link/ctl` | 1 | 6 | 1 | `link/wire` | no | after chain |
| `link/settings` | 1 | 5 | 0 | `link/wire` | no | after chain |
| `link/docs` | 4 | 5 | 0 | `link/ctl`, `link/wire` | no | after chain |
| `link/transport` | 1 | 12 | 31 | `link/wire`, `zitictx` | no | small cut |
| `link/conn` | 3 | 17 | 88 | `link/transport`, `link/wire` | no | after chain |
| `link/notify` | 4 | 11 | 84 | `link/conn`, `link/ctl`, `link/wire` | no | after chain |
| `link/deploy` | 3 | 6 | 0 | `deployready`, `link/ctl`, `link/wire` | no | small cut |
| `link/forge` | 5 | 20 | 59 | `scripts/recognisers`, five `link/*`, test `daemon/home` | no | small cut |
| `daemon/hublink` | 6 | 10 | 0 | `daemon/publish`, `daemon/shared`, `roomstats`, `small` | no | after chain |
| `daemon/pulls` | 5 | 5 | 29 | `api`, `daemon/promptline`, `small` | no | small cut |
| `daemon/publish` | 5 | 18 | 0 | `api`, `link/transport`, `zitictx` | no | small cut |
| `daemon/roomconf` | 6 | 8 | 0 | `api`, `daemon/publish`, `small` | no | small cut |
| `daemon/home` | 5 | 7 | 2 | `api`, `daemon/shared`, `roomstats`, `small`, `wtfree` | no | small cut |
| `daemon/cycle` | 6 | 10 | 1 | `api`, `daemon/home`, `daemon/shared`, `small` | no | small cut |
| `link` | 6 | 92 | 843 | `link/*`, `deployready`, `zitictx`, test `api`, `daemon`, `daemon/shared` | no | small cut |
| `daemon` | 7 | 156 | 2080 | `api` and its helpers, every `daemon/*` | no | design (no split) |

All names are under `internal/` unless they start with `scripts/`. No package here needs the struct split to change
modules. The struct split is about breaking `daemon` (2080 tests) and `link` (843) into smaller packages, which is what
would narrow their tests, not about where their module line sits.

Two edges are new since `22e6a575f` and both cross the room and hub line: `daemon/publish` now imports `link/transport`
(prod), and `link/forge` has a test that imports `daemon/home`.

## Root leaves the chains wait on

These are not under `daemon` or `link`, but every small cut above names one of them.

| Package | Layer | Files | Tests | Root edges | Third party | Moves as |
| --- | --- | --- | --- | --- | --- | --- |
| `zitictx` | 0 | 1 | 8 | none | ziti SDK | own module (in `base` it would pull the ziti SDK into `base`) |
| `small` | 2 | 2 | 4 | none | none | own module (needs `store`) |
| `deployready` | 2 | 3 | 38 | none | none | own module, or into `base` (imports only `base/gitexec`) |
| `wtfree` | 2 | 3 | 0 | none | none | into `base` (imports only `base/gitexec`) |
| `scripts/recognisers` | 1 | 1 | 3 | none | none | own module (tests need `store`) |
| `roomstats` | 2 | 14 | 32 | `zitictx` | 1 | own module once `zitictx` is out |
| `runnerprofile`, `runnersetup`, `worktrees` | 2 to 3 | 9 | 54 | `runnerprofile` | none | with `api` |
| `api` | 4 | 103 | 459 | `cardreview`, `runnersetup`, `wtfree`, `zitijoin`, test `link` | none | room module |

## Daemon, package by package

Only root edges are listed. Edges to `base`, `store`, `gitsync` and `ptyhost` are already fine.

### `daemon/launch`, `daemon/promptline`

| Root edge | Symbols |
| --- | --- |
| none | |

Imported by `daemon`, and `promptline` by `daemon/pulls` too. No atrium imports, no third party. Movable into `base`
or as its own module.

### `daemon/screen`

| Root edge | Symbols |
| --- | --- |
| none | |

Imports `store` only. Imported by `daemon`, `daemon/shared` and `cli`. Movable, 66 tests leave the root.

### `daemon/shared`

| Root edge | Symbols |
| --- | --- |
| `daemon/screen` | `SizeCut` |

Imported by `daemon`, `cycle`, `home`, `hublink`, and tests of `cli` and `link`. Movable after `screen`, or in one
module with it.

### `daemon/pulls`

| Root edge | Symbols |
| --- | --- |
| `api` | `ChangesError` |
| `daemon/promptline` | `PromptOf`, `QueuedPromptOf` |
| `small` | `FirstLine`, `WriteJSONErr` |

Small cut: `api.ChangesError` (`internal/api/changes.go`) to a `base` leaf that `api` also imports, `small` out as its
own module. 29 tests. Three of its tests read `base/prreview/render/testdata`, which works in a checkout.

### `daemon/publish`

| Root edge | Symbols |
| --- | --- |
| `api` | `Server` (`Broadcast`, `Handler`), `DocShareError`, `DocShareRequest`, `EmbeddedBoard`, `ViewImageType` |
| `link/transport` | `LoadZrokRoot`, `EnableZrokRoot`, `DisableZrokRoot` |
| `zitictx` | `SharedFile` |

Small cut, three parts: a two method interface for `*api.Server`, the four `api` names to a `base` leaf with the board
passed in as an `fs.FS`, and the three zrok root functions out of `link/transport` into a leaf both sides import (the
design forbids the room requiring the hub). Imported by `daemon`, `hublink`, `roomconf` and `cli`. No tests.

### `daemon/roomconf`

| Root edge | Symbols |
| --- | --- |
| `api` | `Server` (`Broadcast` only), `SettingBoardSkin`, `SettingEditor` and nine more `Setting*` |
| `daemon/publish` | `SettingAuth`, `SettingAuthKey`, two `SettingOverlay*`, `ZitiConfig`, `ZrokConfig` |
| `small` | `ErrString`, `WriteJSON`, `WriteJSONErr` |

Small cut after `publish` and `small`: the same `Broadcast` interface, and the eleven setting names read from `store`
or `base/rules`. No tests.

### `daemon/home`

| Root edge | Symbols |
| --- | --- |
| `api` | `Server` (`Broadcast` only) |
| `daemon/shared` | 14 names (`CullResult`, `DefaultCullInto`, `GitIn`, `LaunchRequest` and more) |
| `roomstats` | `DiskOf` |
| `small` | `FirstLine`, `HasTag`, `WriteJSONErr` |
| `wtfree` | `DirSize`, `LongPath` |

Small cut after `shared`, `small`, `roomstats` and `wtfree`: the `Broadcast` interface. 2 tests.

### `daemon/cycle`

| Root edge | Symbols |
| --- | --- |
| `api` | `Server` (`Broadcast` only), `TranscriptPath`, `OverrideColdHandoff` |
| `daemon/home` | `DirsOf` |
| `daemon/shared` | 12 names |
| `small` | `ErrString`, `FirstLine`, `IsParked`, `WriteJSON`, `WriteJSONErr` |

Small cut after `home`: the `Broadcast` interface, `TranscriptPath` (a pure function) and the `OverrideColdHandoff`
constant to a `base` leaf. 1 test.

### `daemon/hublink`

| Root edge | Symbols |
| --- | --- |
| `daemon/publish` | `Native`, `OverlayKind`, `OverlayZiti`, `OverlayZrok`, `ZitiConfig` |
| `daemon/shared` | 11 names |
| `roomstats` | `ListTop`, `Proc`, `ProcByPID`, `TopProcs` and four `Proc*` |
| `small` | `ErrString`, `WriteJSONErr` |

No `api`. Movable after `publish`, `shared`, `roomstats` and `small`. No tests.

### `daemon`

| Root edge | Symbols |
| --- | --- |
| `api` | 51 names in prod, 9 more in tests (`ActivityOf`, `CardsDir`, `Server`, `TranscriptPath` and the rest) |
| every `daemon/*` | its own sub-packages |
| `runnersetup`, `runnerprofile`, `worktrees`, `roomstats`, `small`, `wtfree` | 38 names in all |

Only `cli` imports it in prod, and `link` in tests. It cannot leave while `api` is in the root, and `api` cannot leave
alone: `api/state_integration_test.go` (3 tests) imports `link`, whose tests import `daemon`. Once the `link` test edge
below is cut, `api` with its helpers and then `daemon` with every `daemon/*` can form the room module of design
section 4 with no struct split. That is a design call (one room module or two), not a cut.

## Link, package by package

### `link/wire`

| Root edge | Symbols |
| --- | --- |
| none | |

Imported by `cli`, `link` and every `link/*` but `jsonio`. Movable into `base` or alone. First in every link chain.

### `link/jsonio`

| Root edge | Symbols |
| --- | --- |
| none | |

Imports `hubstore` only. Imported by `link` and `link/forge`. Movable as its own module, or with the hub module.

### `link/ctl`, `link/settings`, `link/docs`

| Package | Root edge | Symbols |
| --- | --- | --- |
| `ctl` | `link/wire` | `RelayAnswer`, `RelayRequest`, `RoomHeader` |
| `settings` | `link/wire` | `Attached`, `ErrStatus` |
| `docs` | `link/wire`, `link/ctl` | `DocsCrossOrigin`, `RoomHeader`, `TagFor`, and 13 `ctl` names |

All movable after `wire` (and `docs` after `ctl`). `ctl` and `docs` bring the MCP SDK. One test among the three.

### `link/transport`

| Root edge | Symbols |
| --- | --- |
| `link/wire` | 9 names (`Dialer`, `Hello`, `ReadJSON` and more) |
| `zitictx` | `Close`, `SharedFile`, and in tests `CloseAll`, `Live`, `SetSDK` |

Small cut: `zitictx` out first as its own module (it holds the ziti SDK, and `publish`, `roomstats` and `link` use it
too). Imported by `cli`, `link`, `link/conn` and `daemon/publish`. 31 tests, plus 8 of `zitictx`.

### `link/conn`

| Root edge | Symbols |
| --- | --- |
| `link/transport` | 13 names (`Arrange`, `Direct`, `Join`, `LegacyTransport` and more) |
| `link/wire` | 36 names |

Movable after `transport`. Imported by `cli`, `link`, `forge` and `notify`. 88 tests, the most of any `link/*`.

### `link/notify`

| Root edge | Symbols |
| --- | --- |
| `link/conn` | `CardState` |
| `link/ctl` | `HasTag`, `OriginTag` |
| `link/wire` | `Attached`, `DocsCrossOrigin`, `Event`, `TagFor` |

Movable after `conn` and `ctl`. 84 tests.

### `link/deploy`

| Root edge | Symbols |
| --- | --- |
| `deployready` | `Checker`, `Live`, `Queue`, `QueueEntry`, `Report`, `StateUnknown` |
| `link/ctl` | 9 names |
| `link/wire` | `Attached`, `EqualFold`, `Event` |

Small cut: `deployready` leaves the root (into `base` or alone), and takes its 38 tests along. `deploy` has none.

### `link/forge`

| Root edge | Symbols |
| --- | --- |
| `scripts/recognisers` | `Seed` |
| `daemon/home` (test only) | `RepoIsRef`, in `merger_repo_integration_test.go` (1 test) |
| `link/conn`, `link/ctl`, `link/jsonio`, `link/notify`, `link/wire` | 32 names |

Small cut: `scripts/recognisers` out as its own module (it embeds only its own JSON), and `home.RepoIsRef` (a pure
function on a string) to a `base` leaf both sides import. 59 tests.

### `link`

| Root edge | Symbols |
| --- | --- |
| every `link/*` | its own sub-packages |
| `deployready` | `Needs`, `RoomSide`, and five more in tests |
| `zitictx` | `Close` |
| `daemon` (test only) | six `*Tag` constants, three `*EndingLine` constants, `PromptToolNames` |
| `daemon/shared` (test only) | `LaunchRequest` |
| `api` (test only) | `New`, `Server` |

Prod needs nothing from the root once `link/*`, `deployready` and `zitictx` are out. Only `cli` imports it, plus
`api/state_integration_test.go`. The block is 9 test files with 32 tests: `a2a`, `ctlclass_prompt`, `interview`,
`need`, `relay_kind`, `review` (daemon constants), `launch_schema` (`shared.LaunchRequest`), and `fanout_size`, `page`
(`api.New`), all `_integration_test.go` in `package link`. Small cut: the nine constants are strings, so they move to a
`base` leaf that `daemon` re-exports, `PromptToolNames` with them. The three files that need a real `LaunchRequest` or
a real `api.Server` move to a root test package, which means exporting the parts of the `link` test kit they use
(`Proxy` built by the kit, not split). That also removes the hub to room test edge that blocks `api` and `daemon`.

## Proposed order, smallest first

1. **`daemon/launch`, `daemon/promptline`, `link/wire` into `base`.** No atrium imports, no third party. 7 tests.
2. **`daemon/screen`, `link/jsonio` as modules.** No root edge. 66 tests.
3. **Root leaves out**: `wtfree` and `deployready` into `base`, `zitictx`, `small` and `scripts/recognisers` as their
   own modules, then `roomstats`. 85 tests. Each is movable and together they unblock every chain below.
4. **`daemon/shared`** with or after `screen`. 1 test.
5. **The link chain**: `ctl`, `settings`, `docs`, `transport`, `conn`, `notify`, `deploy`. All movable after steps 1
   and 3. 204 tests.
6. **`link/forge`** after moving `home.RepoIsRef` to a `base` leaf. 59 tests.
7. **`link`** after the 9 test files are cut. 843 tests, the biggest single gain. With steps 1 to 6 about 1265 tests
   leave the root.
8. **The daemon cuts**: one `Broadcast` and `Handler` interface for `*api.Server` (used by `publish`, `roomconf`,
   `home`, `cycle`), `api.ChangesError`, `TranscriptPath`, `OverrideColdHandoff`, the doc share names and the eleven
   setting names to `base` leaves, and the zrok root functions out of `link/transport`. Then `pulls` (29 tests),
   `publish`, `roomconf`, `home`, `cycle`, `hublink`. 32 tests in all, so their value is the boundary.
9. **`api` and `daemon` as the room module**, a design call. No struct split is needed for the module line, only for
   narrowing `daemon`'s 2080 tests.

The numbers still favour one hub module holding `link` and every `link/*` over eleven modules for steps 5 to 7: those
packages import only each other and the leaves, so one module loses no test scope and saves ten `go.mod` files.
