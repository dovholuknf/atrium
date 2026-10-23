# Test layout: one bucket per board page and per API surface

Backlog item rd-test-layout. Design only, no code, no mockups.

The ask: a change to one board page or one route runs that page's or route's tests, and a red names the page or the
route, not "board" or "go test".

## Where things stand

| Thing | Shape today | Cost |
| --- | --- | --- |
| Board tests | one file, `scripts/test-board-headless.js`, 36,400 lines, 311 `await unit(...)` blocks sharing one mock server and a page of mock state (`mockVars`) | 1,509 s of weighted unit time serial (267 units weighed, 44 unweighed take the median). CI step `board` runs it all, 26 to 30 min |
| Board node tests | 18 `scripts/test-*.js` and 20 `scripts/check-*.js`, each about one subject | seconds each |
| Go tests | by package, 1,813 tests in `internal/daemon`, 819 in `internal/link`, 462 `store`, 369 `api`, 265 `cli`, 255 `gitsync`, 198 `hubstore` | see the Go table below |
| Unit vs integration | build tag `integration`, CI runs `scripts/test.sh full -p $cpus -timeout 20m` | one `go test` step |
| Touched-file mapping | `scripts/test-touched-plan.js` reads names off the code (tokens declared by a web file versus tokens a unit's text uses) | a guess, and says so when it maps nothing |
| Reports | `ci-report.go` prints a line per Go package, `test-board-sharded.js` a merged per unit report | the failure names a unit (`mOlder`) or a package (`internal/link`) |

Two things make this hard to organise, and the layout has to answer both:

1. **The harness is one process shape.** Units are separable in principle (`HEADLESS_UNITS`, `PIN_GROUPS`) but they
   share module level mock state, 52 `let`s reset by `mockSet` between units, plus `const`s mutated in place
   (`qDismissed`, `LAND`, `walkDirs`, `gateCalls`, `HON`, `FON`) that nothing resets. Moving a unit to its own file
   means moving the mock state it needs.
2. **Go packages are not surfaces.** `internal/link` holds the hub's `/_hub/*`, the control MCP and the git door.
   `internal/api` holds the room's `/v1/*` and the file endpoints. `internal/daemon` holds the agent listener, the
   permission chain and supervision. A route's tests sit in 88, 128 and 279 files, discoverable only by filename.

## Measured time today

Board, from `scripts/board-suite-weights.json` (ms per unit in a sharded run). Buckets are my cut of unit names by
prefix and are approximate, the unit to bucket table at the end is the proposal. Unweighted units are counted at
8,000 ms, which is why this sums past 1,509 s.

| Bucket | Units | Seconds | Slowest |
| --- | --- | --- | --- |
| terminals (list, peek, paste, switch, popout, restart) | 95 | 528 | peekEverywhere 25, peekOneHover 18, restartStays 17, pasteBig 15 |
| phone `m/` | 65 | 421 | mChangeReq 48, mOlder 25, mFollow 24, mDocs 19 |
| cards and groups (the main list, bootClean, core2) | 42 | 255 | core2 26, bootClean 19, groupColor 18 |
| growls and questions | 35 | 218 | questionsClick 17, growlActions 17, gauto 13 |
| docs, files, land, change requests | 11 | 88 | changeReq 32, land 19 |
| usage | 17 | 85 | usageGroups 20, cacheChip 11 |
| items | 9 | 68 | readyTwoWindows, readyOnce, readyPopout |
| rooms and hub gate | 9 | 67 | atriumDown 22 |
| settings, themes, skins | 15 | 58 | skinScope, themePreview |
| hub repos | 5 | 27 | hubReposFeed 7 |
| pulls | 5 | 26 | pulls 16 |
| chat | 3 | 18 | chatterTab 7 |

Sharded, the suite takes roughly 1,509 s divided by shards plus browser start per shard. CI today is 26 to 30 min,
which is the serial figure, so CI is **not sharded**. `scripts/ci.sh` runs `check-board.sh`, which runs the plain
harness. `test-board-sharded.js` exists and is only used by workers through `test-touched.sh`.

The claude/ci-board-time worker (item bug-ci-board-time, on sg3) had not committed anything when this was written:
its ref on the hub was still at d7413077. Its findings are not folded in. Open question 1.

Go, measured here on macOS, `go test -count=1 -tags integration`, one package at a time:

| Package | Wall | Notes |
| --- | --- | --- |
| `internal/api` | 50 s | 1 red, `TestNewWorkWithAnExistingNameAnswersTheWorktreeAndItsLiveCard`, not chased |
| `internal/link` | 58 s | |
| `internal/store` | 48 s | |
| `internal/cli` | 13 s | |
| `internal/hubstore` | 5 s | |
| `internal/gitsync`, `claudeconf` | under 1 s | |
| `internal/daemon` | not measured here | the comment in `ci.sh` says 550 to 600 s on windows-latest |

Measuring daemon was left to the ci-go-tests worker, which shares this room. Every test's time is already in
`build.claude/ci/go-test.json`, so per route times come free once the Go side is bucketed (stage G1).

## The target layout

A **bucket** is a name, the source files it owns, the tests that prove them, and one command that runs them. Buckets
are declared in one file, `scripts/test-buckets.json`, read by `test-touched-plan.js`, by CI's shard step and by the
reports. Nothing else keeps a list.

### Board buckets: one per page

A page is a `view` of the board or a `m/` screen. The bucket names:

`terminals`, `cards` (the list, groups, tabs), `chat`, `items`, `backlog`, `pulls`, `hubrepos`, `growls`, `usage`,
`docs-files`, `rooms`, `settings`, plus the phone twins `m-home`, `m-card`, `m-compose`, `m-docs`, `m-changes`,
`m-growl`, `m-perms`, and `boot` for what every page needs (`bootClean`, the service worker, `atriumDown`, skins
contrast). The phone is split into several buckets because it is 421 s alone and `m/` has one script per screen.

On disk the harness becomes a folder and a library:

```
scripts/board-tests/
  lib/mock.js          the mock server and its state, with snapshot and reset as one object, not 52 loose lets
  lib/browser.js       launch, needPage, the shared helpers now at the top of the 36k line file
  terminals.js  cards.js  chat.js  items.js  backlog.js  pulls.js  hubrepos.js  growls.js  usage.js
  docs-files.js  rooms.js  settings.js  boot.js
  m-home.js  m-card.js  m-compose.js  m-docs.js  m-changes.js  m-growl.js  m-perms.js
  index.js             lists the buckets, runs one or all (what test-board-headless.js is today)
```

A bucket file exports `units`, each `{ name, run(ctx) }`, where `ctx` carries the mock and a fresh browser context.
Unit names stay as they are, so `board-suite-weights.json`, `board-suite-flaky.json` and every `HEADLESS_UNITS=`
habit keep working. `test-board-headless.js` stays as a thin entry that runs `board-tests/index.js`, so no caller
breaks. `PIN_GROUPS` becomes "units in one file that say so", with the same rule.

Each bucket also gets its own named state in the mock: a unit may only read and write the mock keys its bucket
declares. That is what lets a bucket run alone and be sharded safely, and it is where most of the migration work is.

### API buckets: one per surface

| Surface | Source it owns | Tests today |
| --- | --- | --- |
| room `/v1/*` by route family: `cards`, `sessions`, `settings`, `files`, `prs`, `launch`, `close`, `intake`, `usage`, `skins`, `providers`, `runnersetup`, `termopen` | `internal/api/<family>.go` | `internal/api/<family>*_integration_test.go`, 88 files |
| agent listener `:7777`: `perm`, `hooks`, `activity`, `session`, `finish`, `peers`, `messages` | `internal/daemon/` | 279 files, 1,813 tests |
| hub `/_hub/*`: `rooms`, `backlog`, `reports`, `pulls`, `notify`, `push`, `git`, `views`, `deploy`, `files`, `docs` | `internal/link/` | 128 files, 819 tests |
| control MCP tools: `launch`, `items`, `backlog`, `deploy`, `files`, `docs`, `resources`, `flow`, `deps`, `git_push`, `ci` | `internal/link/*_mcp.go`, `control_mcp*.go`, `ctl*.go` | the `*_mcp_integration_test.go` and `ctlclass*` files |
| store (room) and hubstore | `internal/store`, `internal/hubstore` | by package already, no change needed |
| everything else | `gitsync`, `claudeconf`, `ptyhost`, `cli`, small packages | by package already |

Go cannot run a file, only packages and `-run` patterns. So the surface bucket is **a package plus a `-run` regex**,
and the regex is built from the test functions in the bucket's files. There is no renaming of existing tests. A new
test lands in a file named for its family (`prs_foo_integration_test.go`) and the bucket picks it up by the glob.

Where a family has no clean file prefix (the 40 or so `*_settings_integration_test.go` files that test a setting
through `/v1/settings`), the bucket lists the files explicitly. A file in no bucket falls into its package's
`rest` bucket, and a lint (stage G1) fails CI when `rest` grows past a ceiling, so nothing silently escapes.

Failures name the bucket because the CI report groups the `go test -json` stream by bucket: `link/backlog: 2 failed`
instead of `internal/link: 2 failed`.

## How `test-touched.sh` maps a changed file to a bucket

Today the planner infers the board map by token overlap and the Go map by package directory. Keep both as the
fallback, and put an explicit map on top.

1. **Declared first.** `scripts/test-buckets.json` has, for each bucket, `sources` (globs) and `tests`. A changed file
   matching a bucket's `sources` selects that bucket. Examples: `internal/api/web/js/pulls.js`, `css/pulls.css`,
   `m/js/...` map to `pulls`, `m-*` as declared, `internal/api/prs*.go` maps to the room `/v1` `prs` bucket,
   `internal/link/pulls.go` to hub `pulls`, `internal/link/items_mcp.go` to control MCP `items`.
2. **Shared files fan out by declaration, not by guess.** `core.js`, `boot.js`, `cards.js`, `index.html` are listed as
   `shared`. A change there selects `boot` plus the buckets whose `needs` list names that file, and says why.
3. **Inferred second.** A file in no bucket's `sources` goes through today's token mapper, unchanged, and the plan
   says "inferred, add it to test-buckets.json". A lint in CI fails when a file under `internal/api/web`, `internal/api`
   or `internal/link` is in no bucket, so the inference fallback is a transition aid and not a permanent path.
4. **Budget stays.** The time budget (150 s from the weights) still caps what runs, filled round robin across the
   selected buckets. What was cut is printed.
5. **The plan prints buckets.** `test-touched: pulls (internal/api/web/js/pulls.js), hub/pulls (internal/link/pulls.go)`
   and then the commands. A failure then reads `FAILED bucket pulls: units pulls, pullsDrawer`.
6. **Go packages touched without a bucket** run the whole package as today.

`scripts/test-touched-selftest.js` gains one fake diff per bucket, so the map cannot rot.

## How CI shards by page

The `check` job today runs everything in one `bash scripts/ci.sh` on each of three operating systems, so the board
suite (about 26 to 30 min) and the Go tests (daemon alone about 10 min on Windows) run one after another.

Shape: keep `ci.yml` free of logic. Add a `plan` job that prints a matrix, and a `bucket` job per matrix entry.

- `bash scripts/ci.sh --list-shards` prints the matrix JSON. It balances the board buckets across N shards from
  `board-suite-weights.json` (greedy, longest first, as `test-board-sharded.js` does now, but with a bucket as the
  atom so one shard owns whole pages) and puts the Go surfaces on their own shards.
- Each shard runs `bash scripts/ci.sh --shard board:3` or `--shard go:link-hub`. The shard's job name is the
  bucket list, so a red job already names the pages: `board 2/5 (terminals, chat)`.
- The cheap steps (gofmt, vet, build, skins, contrast, release, parse, clean-tree) stay in one `lint` job that is not
  sharded. `go-race` stays a separate job.
- A `merge` step collects each shard's `build.claude/ci` artefact and writes one `summary.md` that groups by bucket.
- The full suite is still what `main` and `claude/main` get. A pull request's CI runs the buckets the touched-file
  planner picks plus a **nightly or on-land full run**, so a missing `sources` entry shows up within a day.

Only ubuntu needs the shard split to start with. macOS and Windows keep one full run each, because they find the
platform bugs (ConPTY, paths), which are Go bugs, not board bugs. Open question 3.

The expected win: the board step goes from about 1,500 s to about the slowest shard, 1,500 / 6 shards, around 5 to
6 minutes plus browser start, and the Go side from one 10 min `daemon` tail to a few parallel jobs.

## What moves where

### Board units

All 311 unit names are assigned by prefix. The prefix is the page the unit exercises, which is how the names are
already written. Ambiguous ones are in Open question 4.

| Bucket | Units (by name or prefix) |
| --- | --- |
| `terminals` | `term*`, `tallPty`, `attachAtOnce`, `switch*`, `copySelect`, `cleanCopy`, `clear*`, `paste*`, `quickPaste`, `walk*`, `peek*`, `crossedPane`, `worn`, `resume*`, `noResume*`, `details*`, `gear*`, `launch*`, `keepAlive`, `keepalive`, `keepMemory`, `idle*`, `looksIdle`, `joined*`, `liveHome`, `presence`, `parked`, `pausedDead`, `greyMeansDead`, `busyGuard`, `bridge`, `collapsedBridge`, `groupBridge`, `sshBadge`, `popout*`, `popup*`, `compose*`, `typing`, `sendArrow`, `replies*`, `restart*`, `reselect`, `tabsOrder`, `u002`, `u016` |
| `cards` | `core`, `core2`, `history` (one pin group), `card*`, `carryLink`, `child*`, `group*`, `discreteGroup`, `hidden*`, `laneFold`, `nest*`, `untaggedSort`, `newCard`, `newWorkDialog`, `oneTooltip`, `tooltip`, `offCardActions`, `hostLabelGroup`, `repoColors`, `shiftMenu`, `borderColourLag`, `backForward`, `eventDriven`, `foldStill`, `revealFolds`, `topNav`, `trayHead`, `termsDrawerPush`, `linkBoard`, `linkReuse`, `arrivedOnce`, `controlsCompact`, `closeOneDialog`, `deployReady` |
| `chat` | `chatTab`, `chatter`, `chatterTab` |
| `items` | `itemsView`, `unifiedCard`, `interviewTop`, `readyNoSeed`, `readyOnce`, `readyPopout`, `readyTwoWindows`, `noReadyChildren`, `kidCounts` |
| `backlog` | no unit today. The backlog page has `js/backlog.js` and only `bootClean` covers it. A gap |
| `pulls` | `pulls`, `pullsAbsent`, `pullsDrawer`, `pullsPanelStage`, `prMove` |
| `hubrepos` | `hubRepos`, `hubReposFeed`, `hubReposLedger`, `hubReposShelf`, `hubReposTimelineDots` |
| `growls` | `growl*`, `askAgain`, `alertKinds`, `questionsClick`, `quietDoer`, `popoutBell`, `windowMute`, `notify*`, `sayEnter`, `sayWhen`, `toast*`, `heldLine`, `heldLink`, `blockerMark`, `stuck`, `autoPerm`, `gauto`, `soundPhone` |
| `usage` | `usage*`, `boardUsage`, `burnChart`, `burnReadout`, `cacheChip`, `contextCycle`, `contextSize`, `ctx*`, `turnLimits`, `clock` |
| `docs-files` | `boardDocs`, `boardFiles`, `docsPage`, `fileView`, `fileOpenOutside`, `land`, `landThePlane`, `changeReq`, `coverPoll`, `coverSteps` |
| `rooms` | `atriumDown`, `hubGate`, `rooms*`, `pollsGone`, `recogniserRepo`, `transportChips`, `zitiLogo` |
| `settings` | `theme*`, `skin*`, `websiteSkin`, `readTabSize`, `readWidthSkin`, `settingsOnce`, `dropdownsInstant`, `prefsEverywhere`, `alias` |
| `boot` | `bootClean` |
| `m-*` | `mHome*`, `mCard*`, `mCompose*`, `mDocs`, `mChange*`, `mGrowl*`, `mPerms`, `mBell`, `mFollow`, `mOlder`, `mReload` and the rest of the `m*` and `phone*` units, 65 in all. The split into `m-home`, `m-card`, `m-compose`, `m-docs`, `m-changes`, `m-growl`, `m-perms` follows the file the unit's section reads in `m/js/` |

### Node scripts

`scripts/test-*.js` and `check-*.js` each go to the bucket their `// subject:` names. `test-term-*`, `test-morph`,
`check-terminal*` go to `terminals`. `test-notify-*`, `check-growl-ladder`, `check-web-push` go to `growls`.
`check-phone`, `audit-phone-u001` go to the `m-*` buckets. `test-seen`, `test-sort-order`, `test-custom-groups` go to
`cards`. `check-skins`, `check-contrast` go to `settings` and `boot`. `test-hub-link-line` goes to `rooms`.
`test-inputlag-route` goes to the room `/v1` bucket and `terminals`. They run in their bucket and are no longer a
separate list in the planner.

### Go tests

| Package | Goes to |
| --- | --- |
| `internal/api` (88 integration files) | one bucket per route family by filename glob: `cards*`, `files*`, `file*`, `prs*`, `close*`, `launch*`, `new*`, `settings*`, `skins*`, `providers*`, `runnersetup*`, `sessions*`, `term*`, `turn*`, `intake*`, `keepalive*`, `sshwizard*` |
| `internal/daemon` (279 files) | agent listener buckets: `perm` (`autoapprove`, rules, auto mode), `session`, `activity`, `hooks`, `messages` and `peers`, `launch`, `supervision` (`supervisor`, `attach`, shutdown), `sweep`, `overlay`, `finish`. This is the big one, 10 min on Windows, and the best candidate to split first |
| `internal/link` (128 files) | hub `/_hub/*` by file stem: `backlog`, `rooms*`, `pulls`, `notify*`, `push*`, `git*`, `views`, `deploy*`, `relay*`, `restartgate*`, `growl*`. Control MCP: `control_mcp*`, `*_mcp_*`, `ctl*`, `items_mcp*`, `deploy_mcp*`, `files_mcp*`, `docs_mcp*`, `resources_mcp*`, `flow*`, `deps*` |
| `internal/store`, `hubstore`, `gitsync`, `claudeconf`, `ptyhost`, `cli`, `roomspec`, `runnersetup`, etc. | stay one bucket per package |

## Migration, in stages that each leave CI green

One worker per stage. Every stage ends with the suite passing exactly as before, and none changes what a test
asserts. Stages B and G are independent, so two workers can run them side by side after stage 0.

0. **Bucket file and planner (no test moves).** Add `scripts/test-buckets.json` with the board and Go buckets, a
   lint that every web, api and link source file and every unit name is in exactly one bucket, and the planner's
   "declared first" step with the inference as fallback. Extend the self-test. CI is untouched. Green because nothing
   is moved.
1. **B1: the library.** Extract `lib/mock.js` and `lib/browser.js` from the top of the harness, turn the 52 loose
   mock `let`s into one object with `snapshot` and `reset`, and the in-place `const`s into the same object (this
   fixes the known "unit that leaves one changed reaches the next" hazard). The harness still holds every unit. The
   serial run and the sharded run both must give the same pass list as before.
2. **B2: move the small pages.** `chat`, `pulls`, `hubrepos`, `items`, `usage`, `rooms`, `settings`, `boot` out to
   `board-tests/<bucket>.js`, about 55 units and 400 s. Each bucket must pass alone. The harness's `index.js` runs
   them all, so CI is unchanged.
3. **B3: move `growls`, `docs-files`, `cards`.** `cards` holds the `core`, `history`, `core2` pin group, move it last
   and keep the pin.
4. **B4: move `terminals`.** The largest, 95 units, and the most shared state with `cards`.
5. **B5: move the phone.** `m-*` buckets, one per screen. The mock state here is the least shared.
6. **G1: Go surfaces, no moves.** Bucket the Go tests by file glob and `-run` regex, group the `go test -json` report
   by bucket in `ci-report.go`, and print per bucket times. The lint that fails when `rest` grows. Nothing renamed.
7. **G2: split `internal/daemon`** (and link if needed) into parallel CI shards using the buckets.
8. **C1: CI shards.** `--list-shards`, `--shard`, the `plan`, `bucket` and `merge` jobs, ubuntu first. Run the old
   and new in parallel for a week (the old as a nightly), then drop the serial board step from the per-push run.
9. **C2: touched-file planning on buckets.** Drop the token inference except as the "not in a bucket" fallback, once
   the lint in stage 0 has been green for a while. Self-test per bucket.
10. **A new page costs one entry.** A page doc: "add a page, add `board-tests/<page>.js`, a `sources` entry". Goes in
    `internal/api/web/AGENTS.md`.

B2 to B5 are one worker each and can run one after another or two at a time once B1 has landed.

## What this does not do

- It does not rename any test. Unit names and Go test names stay, so the weights, the flaky list and every habit work.
- It does not change `ci.yml`'s rule that it holds no logic. Matrix, balancing and bucket choice live in `ci.sh`.
- It does not turn integration tests into unit tests. The 20 ms unit budget in `scripts/test.sh` is untouched.
- It does not touch a single assertion. A bucket move that needs one is a separate change.

## Decisions

The orchestrator decided these on 2026-10-15, about 17:30. clint approves the design as a whole before any stage is
built. Each names the stage it binds.

1. **The ci-board-time finding.** Go ahead. That fix (CI running the board sharded) lands on its own as a bug fix, and
   stage C1 starts from whatever it left. C1 reads its report first. (C1)
2. **The board on three operating systems.** The full headless suite runs on ubuntu only. macOS and Windows run the
   `boot` bucket, so a board that does not load on them is still caught. The board runs against mocks in Chromium,
   so the operating system changes nothing it asserts. (C1)
3. **Go sharding.** Shard on ubuntu. macOS and Windows keep one full Go run each, since that is where the ConPTY and
   path bugs are found. (G2, C1)
4. **The unclear cuts.** `restart*` goes to `rooms`, since it is the board's restart menu. `compose*` and `replies*` go
   to `terminals`, and the phone's own composer units stay in `m-compose`. `deployReady` and `land` get their own
   `deploy` bucket. `launch*` and `newWorkDialog` get their own `launch` bucket. The backlog page gets units, written
   in B2, since a page with no unit is the gap this design exists to close. (0, B2)
5. **Stage order.** One stage at a time: 0, G1, B1, B2, B3, B4, B5, G2, C1, C2, then the page doc. G1 goes before B1
   because it is cheap and its per-bucket Go times inform G2.
6. **The bucket file.** JSON in `scripts/test-buckets.json`. One place to read and lint, and the lint is what keeps a
   new file from escaping a bucket. (0)
7. **The touched-file run.** It runs each selected bucket in full, sharded locally across the machine's cores, and the
   150 s budget becomes a warning, not a cut. `terminals` at 528 s is about 90 s on six shards, and a run that skips
   the tests of the page that changed defeats the point. (C2)
8. **The safety net.** Pushes to `claude/main` and `main` run the full suite, and a nightly full run covers the rest.
   A pull request runs its selected buckets plus `boot`. (C1)
