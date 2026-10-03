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
