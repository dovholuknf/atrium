

## Re-read: 3dffc19e

Range `e9be3fc3..3dffc19e`. `5b93d981` is the fix, `ddec2520` is the docs, and `3dffc19e` merges landing in, with
a test-plan conflict only. The section is lettered IS, and IS appears once on landing. The fix adds room code:
`GET /v1/hub-remote` in `internal/api/api.go`, wired in `internal/daemon/daemon.go`.

Verdict: **OK** for hub and room. M1 and L1 to L6 are closed. Three new notes follow, none holds it.

## How it was checked

- I read the fix: `forwarderBase`, the card branch of `gitURLHandler`, `URLAnswer.ForCard`, the new `pick` and
  `Text` paths, `/v1/hub-remote`, and how a card's token reaches the forwarder (`daemon/hubremote.go`).
- I merged 3dffc19e onto landing 7ad42ff2, then stage 5 at 1ea71a3c on top. Neither merge conflicts, and the
  result builds. That confirms stage 5's C1 rename.
  - `go vet` passes on gitsync, link, api, daemon, hubstore and cli.
  - `gofmt -l` lists only the known `daemon/fyi_test.go`.
  - gitsync, hubstore, cli and link (run alone) pass. api fails only on the known
    `TestTheWalkerLaunchSetAndClear`. The daemon's hub-remote, forwarder, clone and push tests pass.
- I ran 17 mutants of my own, with backups in my scratch dir:

  | Mutant | Result |
  |---|---|
  | No rewrite for a card | caught (`TestACardOnAnotherRoomIsGivenItsRoomsForwarderAndFetchesThroughIt`) |
  | The operator rewritten too | caught (`TestTheToolReturnsTheEndpointsJSONAndALine`) |
  | Any host accepted as the base | caught (`TestACardWhoseRoomDoesNotSayWhereItsForwarderIsIsGivenNoURL`) |
  | `localhost` refused | caught (`TestForwarderBaseIsOnlyAForwarderOnTheRoomsOwnLoopback`) |
  | Userinfo, scheme, path or query check dropped | each caught |
  | The port check loosened | caught |
  | A room source keeps its URL for a card | caught (`TestACardIsGivenNoURLForARoomsWorkInProgress`) |
  | `pick` may choose a source with no URL | caught (same test) |
  | `Text` prints a fetch line with no URL | caught (same test) |
  | No note when the room does not say | caught |
  | The hub asks no room for `/v1/hub-remote` | **survives** (N1) |
  | L1: the 500-branch cut, the 300-character cap in `resolveRepo` | both caught |
  | L1: the 4 MiB check after the LimitReader | survives. Equivalent: the read is already cut and the parse fails, as @fabric said |
  | L1: the early exit in `editDistance` | survives (N3) |
  | L2: the `hostForURL` check | caught (`TestAHostThatIsNotANameAndAPortIsNotMadeIntoAURL`, now on the overlay) |
  | L4: a down room's answer held | caught |
  | L5: the room echo not cut | caught |

## M1: closed

- **The rewrite.** For a call with an agent header, the hub asks the card's room for `GET /v1/hub-remote`. It puts
  the hub's URLs on that base, with the same path after `/git/`. The base the forwarder reads (`HubRemoteBase`) is
  the exact string the daemon uses to scope the card's token, which is
  `http.<base>.extraHeader` in the card's own environment. So the URL the tool gives is one git sends the token
  to, from any checkout.
- **The real fetch is real.** The test mints a card token, runs `git fetch` through a live `HubForwarder` and gets
  the right sha. Without the token the same URL is refused. Without the rewrite the test fails on its first assert.
- **The base is held to the card's own loopback.**
  - It must be `http`, have no userinfo, query or fragment, use a path of exactly `/git/` and a port from 1 to
    65535, and have a loopback IP or `localhost` as its host.
  - `127.0.0.1.evil.io`, `10.x`, `https`, `/git` and `/git/hub/` are all refused, and each is in the table test.
  - A room that predates the route (404), is not answering, or says anything else gives no URL and the
    `NoHubRemote` note.
- **Room sources give a card nothing to fetch.** A card gets no URL and `NoRoomForCards`, which says to ask that
  room's card to `atrium_git_push`. `pick` never chooses a source with no URL. On a branch that is on both, the
  line gives the hub URL and adds the room's push hint.
- **The operator path is unchanged.** With no agent header there is no rewrite, the hub's own address is used, and
  room sources keep the pass-through. Both tests assert this.
- **`/v1/hub-remote` reveals only the room's loopback port.** It is on the room's own board API, reached by the hub
  through the room scope or by the operator. It holds no secret, and the forwarder still wants the card's token.

## L1 to L6: closed

- **L1.** The answer cut, the input cap and the advertisement cap all have tests. The `editDistance` early exit is
  N3.
- **L2.** The Host test now goes through `serveGitURL` off loopback, so the regex is what refuses.
- **L3.** `TestAnAmbiguousShortNameIsAnsweredWithTheCandidates`.
- **L4.** A down room is asked again, and an answering one is held, with a test. Only `ok` and `behind` count a room
  as having the repository, with a test.
- **L5.** `room` and an unknown `branch` are cut like every other echo (`cutText`), with a test.
- **L6.** The test-plan section is IS.

## New notes, none holds it

- **N1: which room the hub asks is untested.** The room comes from the call's `X-Atrium-Room` header, which the
  card's own MCP config sets. That is the same trust as `agentOf`. The rig answers `/v1/hub-remote` for any room,
  so asking no room still passes. A wrong room gives that room's loopback port. Git sends the card's token only to
  the exact base it was scoped to, so this is a fetch that fails, not a leak. Add a two-room rig, with a different
  base for each, and assert that the card gets its own room's base.
- **N2: the base check returns the room's raw string.**
  - `http://127.0.0.1:7777/git%2F`, `/%67it/`, a bare `#` and a bare `?` all parse to path `/git/` and pass.
  - The host is still loopback in every case, so nothing leaves the machine, and the worst outcome is a URL that
    does not fetch.
  - Also require `u.RawPath == ""`, `!u.ForceQuery` and no `#` in the string, so the base is exactly the form the
    comment promises.
- **N3: the `editDistance` early exit has no test.** It is a cost bound, not a correctness one. A timed case on a
  300-character input against many names would pin it, or leave it.

Atrium-Verdict: hub-ok e9be3fc3..3dffc19e
Atrium-Verdict: room-ok e9be3fc3..3dffc19e
Quality: M1 is fixed where it belongs. The card gets a URL on its own room's forwarder, on the exact base its token
is scoped to, and no URL when there is no honest one. A real fetch through a real forwarder proves it.
