# CI speed: what can be cached or cut

Read-only evaluation. Nothing in the workflow, `scripts/ci.sh` or any test was changed. Every number below was read or
computed from the hub's artifact store (`steps.tsv`, `board.log`, `go-flaky/timings.tsv`), from the jobs' start and end
times in the GitHub run record, or from the timestamps in the raw logs of the three flaky jobs of run 39322279044.
Where a number is a guess it says INFERRED.

Runs used:

| run | commit | what it shows |
| --- | --- | --- |
| 39313597529 | 5ab23b1 | no flaky gate, every regular job ran |
| 39318045255 | e341904 | flaky gate in front, every job ran |
| 39322279044 | 48e4b3a | flaky jobs only (the regular jobs were skipped because mac flaky was red) |
| 39321214465 | 48e4b3a parent | windows flaky only, board units on 4 shards (1m15s) |

## 1. Jobs per OS

Wall is the job's own start to end. `+start` is when it began, counted from the run's creation. Steps are the seconds in
`steps.tsv`. "outside ci.sh" is wall minus the sum of the steps: checkout, setup-go (with its cache restore),
setup-node,
upload, post steps.

Run 39318045255 (gate in front). The run took 21m11s (20:17:17 to 20:38:28).

### ubuntu

| job | +start | wall | steps (seconds) | outside ci.sh |
| --- | --- | --- | --- | --- |
| flaky | 3 | 91 | machine 0, toolchain (see 39322279044 below), flaky, clean-tree | n/a here |
| go-all | 225 | 449 | machine 1, toolchain 31, gofmt 1, go-vet 16, go-build 4, go-test 235, go-race 38, time-guard 6, govulncheck 16, board-visual 71, contrast 1 | 29 |
| board-1 | 225 | 347 | machine 0, toolchain 42, board 277 | 28 |
| board-2 | 225 | 387 | machine 0, toolchain 105, board 253 | 29 |

### macos

| job | +start | wall | steps (seconds) | outside ci.sh |
| --- | --- | --- | --- | --- |
| flaky | 99 | 124 | n/a here | n/a here |
| go-test | 530 | 347 | machine 0, toolchain 1, go-build 20, go-test 272, time-guard 11 | 43 |
| board-1 | 565 | 448 | machine 0, toolchain 16, board 384 | 48 |
| board-2 | 346 | 551 | machine 0, toolchain 14, board 479 | 58 |
| go-checks-visual | 971 | 280 | toolchain 13, gofmt 1, go-vet 15, go-race 44, govulncheck 24, board-visual 131 | 52 |

### windows

| job | +start | wall | steps (seconds) | outside ci.sh |
| --- | --- | --- | --- | --- |
| flaky | 3 | 182 | n/a here | n/a here |
| go-test-1 | 225 | 419 | machine 7, toolchain 0, go-build 12, go-test 305, time-guard 6, clean-tree 1 | 88 |
| go-test-2 | 225 | 518 | machine 7, toolchain 0, go-build 19, go-test 381, time-guard 16 | 95 |
| board-1 | 225 | 476 | machine 7, toolchain 24, board 355 | 90 |
| board-2 | 225 | 439 | machine 8, toolchain 23, board 277 | 131 |
| go-checks-visual | 225 | 341 | machine 7, toolchain 26, gofmt 1, go-vet 21, govulncheck 20, board-visual 94, ps-tests 78 | 94 |

Flaky jobs of run 39322279044 (the only run whose flaky steps carry the current go and board split):

| job | wall | machine | toolchain | flaky | clean-tree | outside ci.sh |
| --- | --- | --- | --- | --- | --- | --- |
| ubuntu | 191 | 0 | 24 | 134 | 0 | 33 |
| macos | 211 | 1 | 18 | 145 | 1 | 46 |
| windows | 270 | 7 | 24 | 150 | 1 | 88 |

The windows flaky wall is 270 s by the job record (20:47:43 to 20:52:13).

The flaky step is two parts that run one after another (timestamps in the raw logs): the flaky Go tests took 27 s on
ubuntu (20:48:35 to 20:49:02), 22.5 s on macos and 32 s on windows. The 14 flaky board units on one shard took 1m45s
ubuntu, 2m01s macos and 1m56s windows (`flaky-board.log`).

The three Go flaky tests run 0.02 to 1.8 s each (`go-flaky/timings.tsv`). Nearly all of the 22 to 32 s is compiling and
linking two test binaries and the `go run scripts/ci-report.go` reporter.

### The toolchain step is mostly the Playwright download

`toolchain` in `ci.sh` (lines 341 to 378) checks versions, then in any job that wants board, board-visual or flaky runs
`npm ci` and `playwright install chromium`. From the raw logs of the flaky jobs of run 39322279044:

| | npm ci | playwright browser download | rest of toolchain |
| --- | --- | --- | --- |
| ubuntu | 2 s | about 21 s (20:48:14 to 20:48:35) | about 0 |
| macos | 2 s | about 16 s (20:48:31 to 20:48:47) | about 0 |
| windows | 4 s | about 19 s (20:49:16 to 20:49:35) | 3 s |

`npm ci` says `added 3 packages`. The node tree is tiny.

Jobs that run no board unit (windows go-test-1 and go-test-2, macos go-test) show toolchain 0 or 1 s. They already skip
`npm ci` and the browser, so that candidate is already in place.

### Windows pays the most outside ci.sh

The windows flaky log shows setup-go at 61.7 s (`duration_ms=61716`): about 18 s fetching Go 1.26.9 into the tool cache
(`Adding to the cache ...`) and 42 s restoring a 331 MB cache (`Cache hit` at 20:48:16, `Cache restored` at 20:48:58).
The same step is 18.1 s on ubuntu and 29.8 s on macos (570 MB cache). Outside ci.sh each windows job costs 88 to 131 s,
macos 43 to 58 s, ubuntu 28 to 33 s (tables above).

## Critical path of a whole run

Run 39318045255, from run creation:

- The three flaky jobs end at +94 s (ubuntu), +185 s (windows) and +223 s (macos, which first waited 99 s for a runner).
- Every regular job lists `needs` on all three, so the first one starts at +225 s.
- The slowest non-macos regular job is windows go-test-2, ending at +743 s (wall 518 s).
- The run ends at +1251 s with macos go-checks-visual, which started at +971 s. The macos jobs started at +99, +346,
  +530,
  +565 and +971 s while ubuntu and windows jobs all started at +225 s.

So the chain is: flaky (slowest of the three, 185 to 274 s in the two runs that have it), then the slowest regular
job on its OS. What the gate adds: it delays the start of every regular job by 222 s in run 39318045255 (+3 s without
a gate in run 39313597529, +225 s with). In run 39322279044 the slowest flaky job was 270 s.

Without the gate (run 39313597529) the non-macos regular jobs ended by +776 s and the run by +941 s (macos board-2).
With the gate the non-macos regular jobs ended by +743 s and the run by +1251 s. The 310 s difference in the run's end
is not the gate alone: the macos jobs queued (see below), and job times vary run to run (windows go-test-2 was 475 s
in one run and 518 s in the other).

macos is the tail in both runs. In run 39313597529 only macos board-1 started at +9 s and the other three macos jobs
started at +340 s. INFERRED: macos runners are scarce for this repository, so the number of macos jobs sets the run's
end more than any one job does.

Why the gate gives no protection: `scripts/ci.sh` lines 181 to 182 take the flaky units out of every regular board
shard and lines 466 to 490 give every `go test` in a regular job `-skip` for the flaky tests. No regular job runs a
flaky test, so no regular job can be affected by the flaky result. The `needs` is a stop-early policy, not a data
dependency.

## 2. What is cached today

| thing | cached | proof |
| --- | --- | --- |
| Go toolchain | no. windows downloads Go 1.26.9 each job | windows flaky log: `Adding to the cache ...` then `Successfully cached go` |
| GOMODCACHE and GOCACHE | yes, one cache per OS, via setup-go `cache: true` | `.github/actions/ci-run/action.yml` line 15. Log: `Cache hit for: setup-go-Windows-x64-go-1.26.9-bc3d...` and, on ubuntu, `Cache hit occurred on the primary key ..., not saving cache.` |
| go test results | no. `-count=1` on every run | `scripts/test.sh` line 42 and lines 71 and 74 |
| node itself | tool cache only. `Found in cache @ ...\node\22.23.3\x64` | setup-node has no `cache:` input in action.yml |
| node_modules, `~/.npm` | no. `npm ci` runs every job | `ci.sh` line 371 and `added 3 packages in 2s` in the logs |
| Playwright browser | no. Downloaded every job | `Downloading Chrome for Testing 156.0.8078.4 ...` in every flaky job log, no cache step for `ms-playwright` anywhere in the workflow |

Two things follow from the setup-go key. It is derived from `go.sum` and the OS only (the hash is the same on all three
OS, `bc3d7b59...`). On a hit the post step does not save. So the saved GOCACHE holds what the first run with this
`go.sum` compiled and is never refreshed. It carries the standard library and the modules. The repository's own
packages are compiled again every run. The cache is correct (a go build cache is content addressed, so a stale entry
cannot hide a failure) but it does not grow with the code.

The go-build step (`go build -o build.claude/ ./...` plus the three script checks) costs 4 s on ubuntu, 12 to 19 s on
windows and 20 to 25 s on macos with the restored cache.

## 3. Candidates

Savings are seconds on the run's critical path unless the line says "per job". The risk column states first whether the
candidate can change what a test sees.

| # | candidate | saving | can it change what a test sees | risk |
| --- | --- | --- | --- | --- |
| A | Remove `needs` from the regular jobs (the flaky jobs and the regular jobs start together) | 185 to 274 s per run, the length of the slowest flaky job. A softer version, `needs` only on `flaky-ubuntu`, saves about 129 s (223 - 94) in 39318045255 and 79 s (270 - 191) in 39322279044 | no. No regular job runs a flaky test (see above). Test environment is identical | low. Costs runner minutes when a flaky test is red, because the rest of the run is no longer stopped. macos jobs then compete with the macos flaky job for the scarce macos runners |
| B | Fewer macos jobs, or give macos a separate lighter job split | up to the macos tail beyond the other OS, +1251 vs +743 s in 39318045255, so up to about 500 s, INFERRED, not measured | removes coverage if a job is dropped. Merging jobs changes nothing a test sees | medium. Needs an experiment to learn if the queue is runner concurrency |
| C | Windows only: `cache: false` for setup-go in jobs that compile little, starting with flaky | up to 42 s (restore of 331 MB) on windows flaky, minus the compile the cache would have saved, which is not in the data | no. A cold cache only makes the build slower | low. Could cost more than it saves: the compile time without the cache was never measured. Try on one job and compare |
| D | Split windows go-test into three | go-test-2 step is 338 to 381 s and go-test-1 is 305 to 419 s. An even three-way split is about 229 s per job, so the slowest job falls by about 150 s, but each new job pays its own 88 to 95 s outside ci.sh, so the third job is cheap only in wall time | no | low. One more windows runner. Not on the critical path once A is done if macos is the tail, so it only pays after B |
| E | Cache the Playwright browser keyed on the playwright version (`~/.cache/ms-playwright`, `~/Library/Caches/ms-playwright`, `%LOCALAPPDATA%\ms-playwright`) | at most 21 s ubuntu, 16 s macos, 19 s windows per board or flaky job, minus the restore time. The windows go cache restored 331 MB in 42 s, so the net on windows may be near zero | yes if the key misses a version bump, a stale browser would run the units. The key must carry the playwright version, and the browser dependencies on ubuntu (`--with-deps`, apt) are not covered by a file cache | low to medium |
| F | Run the flaky Go test and the flaky board units in parallel | 22.5 to 32 s per flaky job (the Go part) | YES. The flaky board units were put on one shard on purpose (`ci.sh` line 404, "none of them runs under another's load") and the Go tests are timing tests. This is the candidate most likely to change a result | medium. Pointless if A lands, since the flaky job is then no longer on the critical path |
| G | Cache node_modules or `~/.npm` keyed on `package-lock.json` | at most 4 s windows, 2 s elsewhere | no | low, and not worth a cache entry |
| H | Key the Go cache on the source too, so the repo's packages are cached | not measurable here. go-build is 4 to 25 s and the test binaries link every run anyway | no. The go build cache is content addressed. It would save a 330 to 570 MB entry per commit per OS, which fills the repository's 10 GB cache and evicts the ones that matter (INFERRED) | medium, because of the cache churn |
| I | Skip `npm ci` and the browser in jobs that run no board unit | 0 s, already done | n/a | n/a |

## 4. Ranking: seconds saved per risk

| rank | candidate | saves | risk | can alter what a test sees |
| --- | --- | --- | --- | --- |
| 1 | A, drop the gate (or keep it on ubuntu only) | 79 to 274 s per run | low | no |
| 2 | C, windows setup-go without the cache in the flaky job | up to 42 s on windows, unproven | low | no |
| 3 | B, macos job count | up to about 500 s, unproven | medium | no, unless a job is dropped |
| 4 | D, windows go-test in three | about 150 s on one job, not on the critical path after A | low | no |
| 5 | E, Playwright browser cache | 0 to 21 s per job | low to medium | yes, with a bad key |
| 6 | F, flaky go and board in parallel | 22 to 32 s on the flaky job | medium | yes, by design |
| 7 | G, node_modules cache | 2 to 4 s | low | no |
| 8 | H, source keyed Go cache | unknown | medium | no |

Candidates that can change what a test sees: E (a stale browser, if the key is wrong) and F (load on timing tests).
None of A, B, C, D, G, H changes the environment a test runs in. A Go build cache hit cannot hide a failure because
`go test -count=1` always runs the tests. The caches here only save compile and download time.

## What this evaluation could not prove

- The cold-cache compile time on any OS, so C and H have no measured saving.
- Why macos jobs start late. The start times are measured and the cause is a guess.
- The 23 s to 105 s spread of the ubuntu toolchain step (ubuntu board-2 was 105 s in run 39318045255 and 23 to 42 s in
  the other ubuntu jobs). The step includes `playwright install --with-deps`, which runs apt on Linux. The cause of the
  outlier was not read from a log.
- The go-build step is FAILED in the three go-test jobs of run 39318045255 and in the go-test jobs of run 39313597529,
  while the go-test step after it passed. The cause was not read from a log here. Commit fc5f663 later made the flaky
  names check read the list from source with no Playwright, which fits a failure of that check in jobs that never run
  `npm ci`. That reading is INFERRED. It does not change any time above.
- Per-step times for the regular jobs outside ci.sh exist only as wall minus steps. The windows flaky log gives the only
  direct split (setup-go 61.7 s).
