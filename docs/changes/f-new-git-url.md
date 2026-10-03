## Test plan

## @LETTER@. A card asks the hub where to fetch code instead of asking for a paste

### @LETTER@1. The real-world plan: sg3's unpushed work, found by a card on m1mini

On sg3, in its atrium clone, make a card commit a trivial change on a `claude/<x>` branch and do NOT push it. From a card
on m1mini, call `atrium_git_url` with `repo` = `<owner>/<repo>` and `branch` = `claude/<x>`. The answer has source `room`,
room `sg3`, `online` true, a URL under `/git/room/sg3/...` and the text `fetch it with: git fetch <url> claude/<x>`. Run
that command: it succeeds and `git diff FETCH_HEAD~1 FETCH_HEAD` is the commit. Nobody was asked for a paste.

### @LETTER@2. Finished work answers hub; both answers both

Push a branch to the hub's store (`git push` to `/git/hub/...`) and ask for it: source `hub`, URL under
`/git/hub/<host>/<owner>/<repo>.git`. Now have a live card on sg3 commit once more on that same branch and ask again: both
sources are answered with their shas and `ahead` is true on the room's. Push that commit: `ahead` is false.

### @LETTER@3. No branch lists the branches

Call `atrium_git_url` with only `repo`. Every branch the hub holds and every branch an attached room would serve is listed
once, each with its sources. `refs/stash`, a `refs/notes/*` ref, `claude/main`, `hub-main` and a branch of sg3's clone's
own checkout (`main`, or `develop` when that is `origin/HEAD`) are not in the list.

### @LETTER@4. A miss names the closest

Ask for a repo with a typo (`<owner>/<repo>x`) and for a real repo with a branch typo. Each answers `not found` with at
most 5 closest repos, or branches, and sg3's atrium log shows no `info/refs` request for the unknown repo.

### @LETTER@5. An offline room answers offline

Detach sg3 (stop its room, or `atrium rooms` shows it offline). Ask for a branch that exists only there: `offline`, naming
sg3, with no fetch tried. Ask for one that is also pushed to the hub: it is answered with the hub source, and the room
shown as not online.

### @LETTER@6. The URL host is the host the caller used

Ask over the board's loopback and over the OpenZiti service name (and a zrok private share). Each answer's URLs begin with
the host and scheme that request came in on, never another.

### @LETTER@7. Who may ask

From the hub machine's loopback, over the OpenZiti service and over a zrok private share it answers. Over a zrok public
share `GET /_hub/git/url?repo=...` answers 404, the same as a path that is not there. A request from a non-loopback
address with no overlay answers 403.

### @LETTER@8. A repeat within 10 s does not ask the room again

Ask for the same repo twice in a few seconds and watch sg3's atrium log: one `info/refs` request, not two. After 10 s a
new one is asked.

### @LETTER@9. Automated

`env -u ATRIUM_LOCATION go test -timeout 120m -race -run 'ABranchOn|AMissAnswers|AnOfflineRoomAnswers|ABranchTheRoomWouldNot|TheURLIsOnTheHost|OnlyTheReachesOfAFetchMayAskForAURL|ARoomsAnswerIsHeld|TheToolReturns|ABranchPushedByARoom|ParseAdvert|ClosestNames|EditDistance|ResolveRepo|TheLineAModelReads' ./internal/link ./internal/gitsync ./internal/cli`
passes, and `env -u ATRIUM_LOCATION go test -timeout 120m ./internal/link ./internal/cli ./internal/gitsync` passes.
