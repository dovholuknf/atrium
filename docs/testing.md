# Running the integration tests

The whole integration suite (`go test -tags integration ./...`) takes 20 minutes or more on a busy machine and prints
one long wall of output. Run it in groups instead. Each group is a slice of the suite that runs in about two minutes
or less, and each leaves its own output file.

```
pwsh scripts/test-groups.ps1 -List             every group, what it covers, its packages, -run pattern, last time
pwsh scripts/test-groups.ps1 -Group link-git   one group (several: -Group link-git,api)
pwsh scripts/test-groups.ps1 -All              every group in turn, keeps going after a failure, ends with a table
pwsh scripts/test-groups.ps1 -Failed           re-run only the tests that failed last time
pwsh scripts/test-groups.ps1 -Check            fail if a test is in no group or in two groups
```

The script runs on PowerShell 7 (`pwsh`) on Windows, Linux and macOS, from anywhere inside the repository. It prints the
exact `go test` command it runs, so you can copy it. The full output of a group is in
`build.claude/test-groups/<group>.txt` (its first line is the full command with the whole `-run` pattern). A short
result is printed after each group: PASS or FAIL, the wall time, and for every failing test its name, `file:line` and
the first error line. A panic is listed as a failure too, with the test it was in.

`-Failed` reads the failing test names of the last run (kept in `build.claude/test-groups/last.json`) and runs just
those. A panic stops the rest of its package, so tests that never ran are not in the list. Run the group again to
see them.

## The groups

Times are wall time on a Windows dev machine, one group at a time. `-List` shows the time of your own last run.

| Group | Covers | Time |
| --- | --- | --- |
| `link-git-url` | link: git over the room URL, pass-through of pushes | 1m13s |
| `link-git` | link: git receive, hub and store plumbing, end to end git | 0m55s |
| `link-changerequest` | link: change requests and the deploy-ready gate | 1m40s |
| `link-forge` | link: forge routes and CI, announce, upgrade, PR claim, move and prune, rooms events | 0m21s |
| `link-messaging` | link: relay, everywhere, notify, growl, push, fan-out | 0m14s |
| `link-core` | link: everything else (restart gate, routes, MCP, docs, deps) | 1m22s |
| `daemon-cull` | daemon: timed culling, reclaim, reaper, worktree cleanup | 0m23s |
| `daemon-mergedcull` | daemon: cull of merged cards, merge proof, close stash, changes | 0m56s |
| `daemon-messaging` | daemon: say, ask, relay, a2a, replies, FYI, owed, peers, holds | 0m19s |
| `daemon-screen` | daemon: screen, attach, cursor, width, paste, typing, replay | 0m10s |
| `daemon-lifecycle` | daemon: keepalive, restart, resume, reopen, shutdown, park, shelve, usage | 1m21s |
| `daemon-launch` | daemon: launch, overlay, new-context, PR runner, models, lean agents | 1m00s |
| `daemon-core` | daemon: everything else | 1m15s |
| `gitsync-receive` | gitsync: receive-pack handling | 2m25s |
| `gitsync-store` | gitsync: store, forge store, pushed refs, commit lookup | 2m25s |
| `gitsync-hub` | gitsync: hub, hub push, remote hub, rooms, served, scm | 1m57s |
| `api` | api: HTTP handlers | 1m21s |
| `cli` | cli: command line | 0m37s |
| `store` | store: sqlite store | 0m52s |
| `hubstore` | hubstore: hub database | 0m15s |
| `deployready` | deployready: the deploy-ready checks | 0m40s |
| `ptyhost` | ptyhost: pty and ConPTY host | 0m16s |
| `guard` | guard: git guard | 0m10s |
| `misc` | 14 small packages: cardproc, claudeconf, edge, forge, linkfetch, nowindow, prreview/render, requirements, resources, roomspec, roomstats, runnersetup, safepath, testdiag | 0m11s |

All groups in a row take about 25 minutes. The git-heavy groups (link-git*, link-changerequest, gitsync-*,
deployready) are slow because each test makes real repositories, and they slow down further when other work is using
the machine.

## How a group is made

No test file is edited and there are no build tags. A package that is too slow for one run is split by test file: the
group lists file name patterns, and the script reads which tests each file defines and builds the
`-run '^(TestA|TestB|...)$'` pattern from that. The group table is at the top of `scripts/test-groups.ps1`. To move
a file to another group, change the patterns there and run `-Check`.

`-Check` lists every test with `go test -list` and fails when a test (or a test file, or a package with an integration
test file) belongs to no group or to two. Run it after adding a test file. A new file lands in the `Rest` group of its
package by itself, and a new package has to be added to the table.

The Windows command line holds about 32000 characters, so a `-run` pattern longer than that is refused. If a group
grows past it, split the group.

## One test alone

```
go test -count=1 -tags integration -run '^TestName$' ./internal/link/
```

Add `-v` to see each step. Name the package: without `-run`, `-tags integration` runs all of it.

## Known noise

- Clear `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` before running by hand. The script clears them for its own runs
  and puts them back after. A room sets `ATRIUM_LOCATION`, which points a test at the real daemon.
- `TestRealSessionsKeepTheirText` reads machine data and fails on some machines. See `docs/known-failing-tests.md`, as
  with `TestReposIsOpenLikeGrowlsAndExactlyShaped`.
- Timing tests (in `link-changerequest`, `link-core` and `api`) fail on a loaded machine with "timed out after 5s
  reading git". Run the group again alone before suspecting the code.
