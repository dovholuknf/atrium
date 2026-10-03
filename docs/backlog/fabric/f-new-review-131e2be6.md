# Review: f-hub-fetch-pass 131e2be6

Range `3212e158..131e2be6`, 4 commits, on `claude/f-hub-fetch-pass`. This is the hub half of hub-forge stage 3: the
pass-through `/git/room/<room>/<repo>.git/{info/refs,git-upload-pack}`, which goes over the link to the room's
`/v1/git/<name>.git/...`. It also settles the lows from the room half's review, f-new-review-453e754a.

Verdict: **hold** on M1. A request that stalls holds one of the room's two slots for as long as its connection stays
open. The other three questions come out clean. The room-half lows are fixed, and two of their three mutants are
caught.

## How it was checked

- I read the four commits, plus `pass.go`, `pktline.go`, `receive.go` (readBody), the pass branch in
  `link/git_store.go`, `edge/local.go` and `reach.go`, and the board listeners in `atrium_run.go`.
- gofmt is clean. `go vet` is clean on gitsync, link and cli. `go test` passes on gitsync, link and cli, with
  ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG unset.
- I ran throwaway probes against the link test harness: paths, headers, gzip bodies, protocol v2 and slot counts. I
  also ran nine mutants, listed below.
- The range merges cleanly onto `claude/landing` `29efa251`. The merge builds and passes the same three packages.

## The four questions

1. **Non-served refs are unreachable. Yes.**
   - info/refs over the pass-through shows only the served refs. That leaves out stash, notes, claude/main, hub-main,
     main and master, any branch that is not served, and a card that is no longer live.
   - A want by sha for an unadvertised object is refused.
   - `Git-Protocol: version=2` (either case) is not forwarded, and the room answers with a v0 advertisement under the
     same hide.
   - A v2 `ls-refs` POST gets a 400.
   - Shallow, deepen and filter requests, and the `filter` and `deepen-relative` capabilities, are refused before git
     runs. That includes a gzip body with `Content-Encoding: GZIP`. The refusal is `ERR atrium`, and nothing reaches
     the room.
   - A plain gzip fetch returns a PACK.
2. **The per-reader key cannot be spoofed. Yes.**
   - Reach comes from the listener's `MarkReach`, never from a header.
   - On loopback, `edge.LocalOperator` refuses any request that carries proxy headers: `X-Forwarded-For` gets a 403.
   - A public zrok share gets a 404.
   - The key is `reach + ":" + host(RemoteAddr)`, and nothing in it comes from the request. See L3 for how coarse it is.
3. **No traversal. Yes.**
   - The escaped path is checked first, and `%2e`, `%5c`, `%2f` and `%00` are refused.
   - `..%2f`, `%2e%2e`, `../`, `%252f`, `sg3%2fx`, `info/refs/x`, `r.gitx` and `r/x.git` all get a 404.
   - `info/refs?service=git-receive-pack` gets a 403, and so does info/refs with no service.
   - `HEAD` and `objects/info/packs` get a 404.
   - The room's name is looked up only among the attached rooms, without case, so `SG3` is the same room.
   - `s%40g3` and `sg3~x` get a 503 (not connected), since they are not the name of any room.
   - The target URL is rebuilt from the parsed parts. So `r.git.git` (the same repo) and a duplicate `service` param
     are harmless.
4. **Streaming timeouts and cancellation: cancellation yes, timeouts no.** See M1.
   - Closing the client frees the slot in all three cases: a reader that stalls, a room that stalls, and a body that
     stalls. The `sync.Once` release runs, and running goes from 1 to 0 when the client closes.
   - Nothing ends a connection that stays open.

## The room-half lows, re-checked

- **L1, the clone matches on host, org and repo.** It is fixed with `RepoMatches(name, host, org, repo)`.
  - A mutant that drops the org check is caught by `TestSelectLiveServesRunningAndWaitingAndParkedCardsAndNoOthers`.
  - A mutant that drops the host check survives. That is L1 below.
- **L2, `main` and `master` are never served.** It is fixed with `neverServed` = {claude/main, hub-main, main,
  master}. A mutant that drops `main` is caught by `TestServedHideIsAPureFunctionOfTheLiveBranches`.
- **L3, a parked card stays served.** It is fixed: `selectLive` keys on status (running, needs-input,
  needs-permission). A mutant that drops needs-input is caught by `TestSelectLive…`.

## Mutants of what the range claims

All of these are caught:

- `fetchRefusal` without `deepen`: caught by `TestARefusedRequestIsNotPassedOn` and
  `TestShallowDeepenAndFilterFetchesAreRefusedBeforeGit`.
- Copying `Git-Protocol` to the room: caught by
  `TestThePassThroughSendsNoProtocolHeaderCredentialOrCardAndOnlyTheFetchPaths`.
- Never calling `release`: caught by `TestAtMostTwoPassThroughsRunAtOncePerRoom` and
  `TestARefusedRequestIsNotPassedOn`.
- Letting a public zrok share through: caught by `TestOnlyTheOperatorsReachesMayFetchThroughTheHub` and
  `TestThePushReachesAreTheOperatorsOrRefused`.

## Mediums

### M1: a stalled pass-through holds a room slot with no deadline

`admit` allows two pass-throughs at once per room, and the slot is freed when `ServeHTTP` returns. Nothing makes it
return while the connection stays open:

- The board listeners (`atrium_run.go`, the loopback and share servers) set only `ReadHeaderTimeout: 10s`. There is
  no `ReadTimeout`, `WriteTimeout` or `IdleTimeout`.
- `readBody` reads the request up to 64 MiB with no deadline.
- The request to the room over the link has no timeout. It runs on the request's context.
- The copy back to the reader has no write deadline.

The probes, each holding the connection open:

- A reader that sends info/refs and then never reads: running = 1 after 3 s.
- A room that never answers: running = 1.
- A body that sends a few bytes and stops: running = 1.

In each case running goes to 0 only when the client closes. Two such connections from any operator reach, such as a
phone that drops off a zrok share without a FIN, block every fetch from that room until TCP gives up.

Fix: give the pass-through its own deadlines, without changing the board servers.

- Use `http.NewResponseController(w)` with `SetReadDeadline` for the body. About 30 s is enough for 64 MiB at any
  real link speed.
- Put a context timeout on the room request, a few minutes to cover a large pack.
- Set a write deadline that each `Flush` moves forward, so a pack that keeps moving is never cut, but a reader that
  stops reading is.

Add a test with a reader that stalls and a room that stalls. Each should show the slot freed within the deadline.

## Lows

- **L1: the host part of `RepoMatches` is untested.** The org mutant is caught, but the host mutant survives. Add a
  case with the same org and repo on two hosts (`github.com/o/r` and `gitlab.com/o/r`), and check that only the
  clone whose host matches is served.
- **L2: `neverServed` names only `main` and `master`.** A clone whose default branch is `develop` or `trunk` is
  served while a card works on that branch, which is the case the room half meant to close. Read the clone's
  `origin/HEAD`, or `init.defaultBranch` for a clone with no origin, and add it to the hide.
- **L3: the rate key on overlay and zrok-private may be shared by every reader.** The key's host is `RemoteAddr`.
  That is a real address on loopback. A zrok share's listener and a ziti service listener may give one fixed or
  service-named address for every peer, which I did not confirm. If they do, all readers on that reach share one
  budget, and one busy phone can use up another's. Every reader there is operator-equivalent, so this is a fairness
  point, not a security one. Confirm what each listener puts in `RemoteAddr`, and say in the code that the key is per
  reach where it is not per peer.

Atrium-Verdict: hold 3212e158..131e2be6
Quality: a careful pass-through. Every refusal fires before the room sees a byte, the hide holds under v2 and gzip, and
the tests catch nearly every mutant. Only the missing deadlines keep it from landing. m1mini commits are unsigned.

## Re-read: 156fcd2c

The range is `3212e158..156fcd2c`: one new commit on top of 131e2be6, with no amend. It merges onto landing 8bfdcdb1
cleanly (the only auto-merge is `internal/cli/roomrun.go`), and the merged tree builds and vets.

Verdict: **hub-ok**. M1 and L1 to L3 are closed. The notes below can follow.

### How it was checked

- I read the code diff (`pass.go`, `served.go`, `backend.go`, `room.go`, `git_store.go`), the new tests and the
  changelog and test-plan text.
- Gates, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` unset:
  - gitsync, link (the hub side) and cli pass;
  - vet is clean;
  - gofmt flags only the known `fyi_test.go` and `cmd/ptyhost-spike/pipe_windows.go`, and this branch changes
    neither.
- Twelve mutants of my own, beyond @fabric's 13:

| Mutant | Result |
|---|---|
| The write deadline is not moved forward on each write | caught (`TestAFetchThatKeepsMakingProgressIsNotCut`) |
| No `idleReader` on the body | caught (`TestABodyThatStopsGivesTheSlotBack`) |
| A stalled room answers 503, not 504 | caught (`TestARoomThatNeverAnswers...A504`) |
| The room's first-byte wait multiplied by 1000 | caught, after 300 s |
| The room's idle reset multiplied by 1000 | **passes after 300 s** (N2) |
| A Flush error is ignored | survives: equivalent, since the next Write fails |
| No write deadline before `admit` | survives (N3) |
| `readerKey` per connection, as before | caught (`TestReadersWithNoAddress...`) |
| `readerKey` always per reach | caught (same test) |
| Defaults ignored in `ServedHide` | caught |
| `origin/HEAD` read as `HEAD` | caught |
| `init.defaultBranch` not read | not run (the build failed). Read instead: the test sets `init.defaultBranch` to trunk and asserts trunk is hidden. |

### M1: closed

- **Each deadline covers one read or one write, and moves forward on progress.**
  - The body's read deadline is set before every `Read` (30 s), and cleared once the body is read.
  - The room has a watchdog that cancels the room request's context: 2 min to the first byte, then reset to 60 s
    before every `Read`.
  - The reader's write deadline is set before `WriteHeader` and before every `Write` (30 s). A Flush error ends the
    request.
- **Each stall gives the slot back, and each has a test:**
  - a reader that stops reading;
  - a room that never answers, which gives the reader a 504 `<room> did not answer in time`;
  - a room that goes quiet mid-answer;
  - a body that stops.
- **A slow but moving fetch is not cut,** and there is a test for it.
- **The deadlines are cleared on the way out,** in a defer, and there is a test. Go 1.26 clears them too, so the
  mutant that drops the clear is equivalent, as @fabric said.
- **The numbers in the changelog** (30 s, 2 min, 60 s, 30 s) match the constants. Test-plan 5a and 5b describe the
  checks on the real machines.

### L1 to L3: closed

- **L1.** `selectLive` is tested with the same org and repo on github.com and gitlab.com, and only the clone whose
  host matches is served.
- **L2.** `DefaultBranches` reads `origin/HEAD` and `init.defaultBranch` on every request, through `HideFor(name,
  gitDir)`. Each git command has a 5 s timeout, and `neverServed` still holds if a read fails. The test covers develop
  through `origin/HEAD` and trunk through `init.defaultBranch`.
  - `git config --get` also reads the operator's global `init.defaultBranch`. That only ever hides more, which is the
    safe direction.
- **L3.** I agree with @fabric's finding. The ziti `RemoteAddr` is per connection and names nobody. The old code
  failed `SplitHostPort` and keyed on the whole string, so every connection was a new reader and the rate did nothing
  on the overlays.
  - Now any address that is not an IP counts as the whole reach.
  - The code, the test plan and a test all say so.

### The shared budget (@fabric's question)

There are two separate limits:
- **The rate.** A sliding one-minute window per reader key: 6 info/refs and 60 rounds. A refused request is not
  counted.
- **The concurrency cap.** 2 running requests per room, across every reader and every reach. This cap was already
  shared by everyone before this change.

One reader on the overlay or a zrok private share can use all 6 fetches a minute for that reach. Another reader on the
same reach then gets `429 that is 6 fetches a minute from one reader. wait a little`, with `Retry-After: 10`, until the
oldest fetch leaves the window, which is at most a minute. Loopback readers and other reaches are not affected.

That is not worse than before:
- Before, the overlays had no working rate at all, so one reader could open fetches without limit. The lockout that
  mattered then was the 2-slot room cap, and a stalled reader held a slot until TCP gave up.
- Now a stalled reader frees its slot within 30 s, a stalled body within 30 s, and a stalled room within 60 s, or 2 min
  before the first byte.
- So the worst case now is a minute without new fetches on one reach, or a 30 s wait behind a stall. Before, it was
  every fetch from that room blocked for as long as TCP took to give up.
- Every reader on those reaches is the operator, so this is a fairness cost, not a security one, as the comment says.

### Notes (not holds)

- **N1: a trickle still holds a slot.** A reader that reads a little every few seconds, or a body that sends a byte
  every 29 s (up to 64 MiB), is "progress", so it keeps the slot as long as it likes. That is the price of never
  cutting a slow fetch, and it is fine for operator-only reaches. If the slots ever need to be fair between readers,
  give the body (a want list) a total time, such as 2 min, as well as the idle one.
- **N2: the quiet-room test has no bound of its own.** `io.ReadAll` waits until the watchdog fires. So a watchdog
  that resets to something far longer than `RoomIdle` still passes, just slowly: 300 s in my mutant. Give the test
  client a timeout a little above the test's `RoomIdle`.
- **N3: no test covers the write deadline set before `admit`.** It covers the 429, 400 and 503 bodies to a reader that
  does not read. Those bodies are small, so this is low.

Atrium-Verdict: hub-ok 3212e158..156fcd2c
Quality: a careful fix. Every deadline moves with progress and every stall has its own test. The ziti `RemoteAddr`
finding turned a guess into the right key.
