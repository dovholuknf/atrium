# Review: f-git-url a1a89240

Range `e9be3fc3..a1a89240`, three commits on `claude/f-git-url`. This is hub-forge stage 4's lookup.

- `7ef4118b` adds `GET /_hub/git/url` and the control tool `atrium_git_url` (`internal/gitsync/lookup.go`,
  `internal/link/git_url.go`). It also moves the reach rule into one function, `gitReach`, and splits `Store.ViewOne`
  out of `View`.
- `9c7fe1fa` names a room that pushed a branch and is now gone as away.
- `a1a89240` adds the changelog, `docs/changes` and the backlog item.

All the code is hub code (`internal/link`, `internal/gitsync`). Commits on m1mini are unsigned.

Verdict: **hold** on M1. The endpoint is sound for the operator. The URL the tool hands a card cannot be fetched by a
card on any room other than the hub's own machine, and the brief line about to land sends every card to it. The Lows
can follow.

## How it was checked

- I read the diff in full. I also read the code it relies on: `serveGit`, `gitReach` against the stage-3 store and
  pass-through, `controlMCP.ask`, the hosts rebinding guard, the room route's `HideFor` (`ServedHide` with
  `DefaultBranches`), and r-hub-remote's forwarder prefix on landing.
- Gates, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` unset:
  - gofmt is clean on link, gitsync and cli;
  - vet is clean;
  - gitsync, link and cli pass in full.
- `git merge-tree` onto `claude/landing` a9307bc7 is clean.
- Mutants of my own, beyond the worker's 12:

  | Mutant | Result |
  |---|---|
  | `gitReach` dropped from `serveGitURL` | caught (`TestOnlyTheReachesOfAFetchMayAskForAURL`) |
  | The GET-only check off | caught (same test) |
  | The `neverServed` filter dropped from `parseAdvert` | caught (`TestParseAdvertTakesServedBranchesAndNothingElse`) |
  | The 40-hex check dropped from `parseAdvert` | caught (same test) |
  | The 500-branch cap per room dropped | caught (`TestParseAdvertIsBounded`) |
  | A miss with an offline room answers `not found` | caught (`TestAnOfflineRoomAnswersOfflineBeforeAnyoneFetches`) |
  | The 300-character cap in `closestNames` dropped | caught (`TestClosestNamesAreNearAndBounded`) |
  | A single suffix hit not resolved | caught (`TestResolveRepoFindsOnlyWhatTheHubKnows`) |
  | `pick` ignores `ahead` | caught (`TestTheLineAModelReads`) |
  | The `hostForURL` check off | survives. The hosts guard refuses those Hosts first (L2) |
  | The 4 MiB advertisement cap dropped | survives (L1) |
  | The 500-branch cap on the answer dropped | survives (L1) |
  | The early exit in `editDistance` dropped | survives (L1) |
  | The 300-character cap in `resolveRepo` dropped | not caught by a lookup test (L1) |
  | An ambiguous short name not reported by `Lookup` | survives (L3) |
  | A `down` room's answer cached | survives (L4) |
  | Every sync state counts a room as having the repo | survives (L4) |
  | A room without git asked | survives. Harmless: its route answers 404, which reads as `none` |
  | `main` not sorted first | survives. Cosmetic |

## The points asked

1. **Who may ask. Right, and the rule is the pass-through's.**
   - `serveGitURL` calls the same `gitReach` that the store and the pass-through now share. A zrok public share gets
     the 404 a path that is not there gets. The overlay and a zrok private share are the operator's, and anything else
     must be loopback on the hub's machine.
   - A reach the pass-through refuses therefore learns nothing here. The case runs before the method check, so even a
     POST on a public share is a 404.
   - The control tool is a worker tool, so any card on any room can ask. That is the design ("every card brief keeps
     the line"). A card can therefore learn every room's repository and branch names and shas for repositories the hub
     knows. That is the same reach a fetch would give it once the URL works.
2. **The URL from the request's Host. Safe.**
   - The hub's listener already refuses a Host it does not answer to (the rebinding guard in `hosts.go`). The new
     `hostForURL` check refuses anything that is not a plain host and port.
   - On the overlay or a zrok private share, the caller chooses its own Host, but the answer goes back to that same
     caller and is not stored.
   - For the tool, the Host is the hub control MCP's own `c.board`, which no card can change.
   - The URL carries no credential, and the answer carries no token.
3. **Hidden branches. Nothing leaks.**
   - The rooms' side is the advertisement of the room's own git route (`HideFor`, meaning `ServedHide` with
     `DefaultBranches` from `origin/HEAD` and `init.defaultBranch`). That is the same set the pass-through serves.
   - `parseAdvert` also keeps only `refs/heads/*`, never a `neverServed` name, and only a 40-hex sha.
   - The hub's side is `ViewOne`, which skips `claude/main`.
   - "closest" is computed only over those same lists, so it never suggests a hidden name.
   - The request goes without `Git-Protocol`, so the answer is v0, as `parseAdvert` expects.
4. **Bounds on a miss and on closest. Fine.**
   - An unknown repository asks no room.
   - A known one asks each git room once per 10 s, with a 5 s deadline and a 4 MiB read.
   - `closestNames` has a 300-character input cap, a length precheck and an early exit from a banded distance. Its
     cost is the known names times a few forms, with at most 300 × 200 cells each.
   - Not every bound has a test (L1).
5. **Offline and `ahead`. Right.**
   - `offline` covers both rooms the hub saw sync the repository and rooms that pushed a branch that are not attached.
     A missing branch with any offline room answers `offline` ("may have it"), which is the honest answer.
   - `ahead` is true only when the room's tip differs from the hub's and the hub's store lacks that commit. A room
     that is behind is not ahead, and the test covers equal, ahead and behind.
   - `cat-file -e` is given a sha checked as 40 hex, so it cannot be an option.
6. **Input. Nothing reaches git or a path.**
   - `repo` is resolved against the known list, and only the canonical name is used after that.
   - `branch` is only compared.
   - `room` only selects among attached rooms, and the room name in a URL comes from `RoomInfo`.
   - `shown` cuts what is echoed back to 80 runes on one line. One exception: `room` is echoed whole in the offline
     note (L5).

## Medium

### M1: the tool's URL is the hub's loopback address, which a card on another room cannot fetch

`serveGitURL` builds every URL on `scheme://r.Host`. For the tool, the request is the control MCP's own `ask` to
`c.board`, the hub's board on loopback. So every card is handed `http://127.0.0.1:<hub board port>/git/hub/...` or
`/git/room/...`, whatever room it is on.

- **A card on the hub's machine** can fetch the URL. The fetch arrives as loopback, so it runs with the operator's
  reach and needs no card token. That is stage 3's rule, not new here, but this tool now points cards at it.
- **A card on any other room**, such as m1mini or sg3, runs `git fetch` against its own machine's loopback. That port
  is either not listening, or belongs to something that is not the hub.
- **The tool's own description says "Call this, then `git fetch <url>`. NEVER ASK FOR A PASTE"**, and @runtime's brief
  line (b5e61438/bf291380) is held only until this lands. A card that follows both is stuck with no way to get the
  code.

The changelog says this holds "until @runtime's stable forwarder (stage 1) lands". It has landed: r-hub-remote 89847d7d
is on landing (8bfdcdb1). Its prefix is `HubRemotePrefix = StorePrefix = "/git/hub/"`, served on each room's agent
listener with the card's own token. So the hub sources can be fixed now. Room sources still have no card path: the
link's git kind does not route `/git/room/`.

Fix, smallest first:
- **Give a card the right base for hub sources.** When the tool answers a card on another room, rewrite `hub` URLs to
  that room's forwarder base (`HubRemoteBase()`, the URL its `hub` remote already uses). The room's relay of the tool
  call is one place to do it. The other is to answer a path and let the room add its base.
- **Do not hand a card a room source it cannot reach.** For a card, leave `room` sources out of `pick`, or mark them
  "the operator's only" until a forwarder route for `/git/room/` exists. If a branch is only on a room, the line
  should say to ask that room's card or the operator, not give a URL.
- **Test it:** a tool call from a card on another room gets a URL on that room's forwarder base, and a real `git
  fetch` through the forwarder with the card's token works.
- Until then, @runtime's brief line should not land.

## Lows

- **L1: four bounds have no test.** Dropping any of these still passes: the 4 MiB advertisement cap, the 500-branch
  cap on the answer, the early exit in `editDistance`, and the 300-character cap in `resolveRepo`. Add one oversize
  case for each.
- **L2: the bad-Host test does not reach `hostForURL`.** The three bad Hosts are refused by the hosts guard first, so
  the regex can be removed and the test still passes. Either test `serveGitURL` with an allowed name that is not
  URL-safe, or say in the comment that the guard is the first line and the regex the second.
- **L3: an ambiguous short name has no `Lookup`-level test.** `resolveRepo` returns the candidates, but nothing checks
  that the answer says "more than one repository here, say which" and lists them.
- **L4: two choices with no test.**
  - A `down` answer is not cached, so a dead room is asked again on every call, at 5 s each. Every parallel ask waits
    for the slowest room.
  - Only `ok` and `behind` syncs count a room as having the repository.

  Each is a reasonable choice. Pin each with a test so they stay choices.
- **L5: `room` is echoed whole.** A miss on `room` writes `q.Room` into the note with no cut, and lists every connected
  room's name. Put it through `shown` as the other echoes are.
- **L6: the test plan.** The steps are in `docs/changes/f-new-git-url.md`. When this lands, give the section a letter
  in `docs/test-plan.md`.

Atrium-Verdict: hold e9be3fc3..a1a89240
Quality: a careful lookup. It shares one reach rule, lists only what a fetch would be allowed, bounds its inputs and
never hands a name to git. The hold is the base of the URL it gives cards: right for the operator, unreachable for a
card off the hub's machine, and the stage-1 forwarder that fixes the hub half has already landed.
