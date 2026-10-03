## Test plan

## @LETTER@. A card asks the hub where to fetch code instead of asking for a paste

### @LETTER@1. The real-world plan: finished work, found by a card on m1mini

On sg3, in its atrium clone, have a card finish a change on a `fix/<x>` branch and push it to the hub
(`atrium_git_push`). From a card on m1mini, call `atrium_git_url` with `repo` = `<owner>/<repo>` and
`branch` = `fix/<x>`. The answer has source `hub`, and its URL is on m1mini's OWN forwarder
(`http://127.0.0.1:<m1mini's agent port>/git/hub/<host>/<owner>/<repo>.git`), not the hub's address, with the text
`fetch it with: git fetch <url> fix/<x>`. Run that command in the card's own shell: it succeeds with no token typed
(the card's environment carries it), and `git diff FETCH_HEAD~1 FETCH_HEAD` is the change. Nobody was asked for a paste.
The same call from a shell with no card token (a script the card runs) fetches nothing: the forwarder refuses it.

### @LETTER@1a. A room's work in progress is not given to a card

Have sg3's card commit on `claude/<x>` and NOT push. From the m1mini card ask for `claude/<x>`: the room source has no
URL and a note, and the text says a card cannot fetch it and to ask the card on sg3 to `atrium_git_push` it. After sg3's
card pushes, ask again: the hub source answers, with its forwarder URL. The operator (the tool called with no card,
or the board's loopback) is still given the room's `/git/room/sg3/...` URL, and fetches the unpushed commit with it.

### @LETTER@1b. A room that does not say where its forwarder is

On a room older than this build (it answers 404 to `GET /v1/hub-remote`), or with the room stopped mid-call, a card's answer
has no URL for any source, with the note that its room did not say where its hub remote is, and the text does not tell
it to fetch.

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

Ask the endpoint (`GET /_hub/git/url?repo=...`) over the board's loopback and over the OpenZiti service name (and a zrok
private share). Each answer's URLs begin with the host and scheme that request came in on, never another. (The tool
rewrites a card's hub URLs onto its room's forwarder; the operator's tool call is on the hub's own board address.)

### @LETTER@7. Who may ask

From the hub machine's loopback, over the OpenZiti service and over a zrok private share the endpoint answers. Over a zrok public
share `GET /_hub/git/url?repo=...` answers 404, the same as a path that is not there. A request from a non-loopback
address with no overlay answers 403.

### @LETTER@8. A repeat within 10 s does not ask the room again

Ask for the same repo twice in a few seconds and watch sg3's atrium log: one `info/refs` request, not two. After 10 s a
new one is asked.

### @LETTER@9. Automated

`env -u ATRIUM_LOCATION go test -timeout 120m -race -run 'ABranchOn|AMissAnswers|AnOfflineRoomAnswers|ABranchTheRoomWouldNot|TheURLIsOnTheHost|OnlyTheReachesOfAFetchMayAskForAURL|ARoomsAnswerIsHeld|TheToolReturns|ABranchPushedByARoom|ACardOnAnotherRoom|ACardIsGivenNoURL|ACardWhoseRoom|ForwarderBase|AHostThatIsNot|ParseAdvert|ClosestNames|EditDistance|ResolveRepo|TheLineAModelReads|AnAdvertisement|AnAnswerIsCut|AnAmbiguous|ADownRoom|OnlyAnOkOrBehind|ARoomNamedAtLength|ForCard' ./internal/link ./internal/gitsync`
passes, and `env -u ATRIUM_LOCATION go test -timeout 120m ./internal/link ./internal/cli ./internal/gitsync ./internal/api` passes.
`TestACardOnAnotherRoomIsGivenItsRoomsForwarderAndFetchesThroughIt` runs a real forwarder in front of the hub's board and
a real `git fetch` of the URL it was given, with the card's token.
