## Test plan

## @LETTER@. A fetch is passed through the hub to the room that has the work

### @LETTER@1. The real-world plan: sg3's unpushed work, reviewed on m1mini

On sg3, in its atrium clone, make a card commit a trivial change on a `claude/<x>` branch and do NOT push it. From that
card `atrium_say` a card on m1mini: "go review sg3's code, it's at `http://<hub>/git/room/sg3/<owner>/<repo>.git`,
branch `claude/<x>`". The m1mini card runs `git fetch <that url> claude/<x>` and reviews the diff with
`git diff FETCH_HEAD~1 FETCH_HEAD`. The fetch succeeds and the diff is the commit. On the hub machine, the hub's
directory has no new file and the hub's own repository has no new object (`git count-objects -v`, and a listing of the
hub dir before and after).

### @LETTER@2. What is not served is refused

From m1mini, against the same URL: `git fetch <url> refs/stash`, `git fetch <url> refs/notes/commits`, and
`git fetch <url> <a branch sg3 has with no live card>` each fail and write nothing. A branch of a card that IS live on
sg3 (not `main`) fetches, and the same fetch fails after that card ends. `git fetch <url> claude/main` fails.

### @LETTER@3. Shallow and filtered fetches are refused

`git fetch --depth 1 <url> claude/<x>` and `git fetch --filter=blob:none <url> claude/<x>` both fail with "this hub
serves whole fetches only, with no filter or depth", and sg3's atrium log shows no upload-pack request for them.

### @LETTER@4. A detached room answers 503

Detach sg3 (stop its room, or `atrium rooms` shows it offline). `git fetch <url> claude/<x>` fails with
`sg3 is not connected` (HTTP 503), and an unknown room name gives the same sentence.

### @LETTER@5. The caps

Run 7 `git ls-remote <url>` in a minute from one machine: the 7th is refused with "6 fetches a minute", and works a
minute later. Start 3 slow fetches (`GIT_CURL_VERBOSE` on a large branch) from 3 readers against one room: the 3rd is
refused with "2 fetches ... are running already". Another room is not held back.

### @LETTER@5a. A stall gives the slot back

Start a fetch from m1mini against sg3 and suspend the git process (`kill -STOP`) mid-pack, with the connection open.
Within 30 s of its last byte moving the hub drops it and a second fetch from the same room runs (with `PerRoom` 2 you
need two suspended fetches to see the cap bite first). A fetch that is slow but keeps moving (a large branch over a
slow link) is NOT cut. Stop sg3's room mid-fetch: the reader's git fails and the next fetch answers 503.

### @LETTER@5b. A clone's default branch is not served

On sg3, in a clone whose `origin/HEAD` is `develop` (or with `init.defaultBranch` `trunk` and no origin), have a live
card work on `develop` in the clone's own checkout. `git ls-remote <url>` from m1mini does not list `develop`, and does
list the card's own `claude/*` and live branches.

### @LETTER@6. Who may ask

From the hub machine's loopback it works. Over the OpenZiti service name, and over a zrok private share, it works. Over a
zrok public share `GET /git/room/sg3/<owner>/<repo>.git/info/refs?service=git-upload-pack` answers 404, the same as a
path that is not there. On the OpenZiti service and a zrok private share all readers share one budget of 6 fetches a minute (their peer has no IP), so with two phones fetching, the 7th fetch of the two is refused. A card on a room that asks the hub over the link for `/git/room/...` gets 404.

### @LETTER@7. Automated

`env -u ATRIUM_LOCATION go test -timeout 120m -race -run 'Pass|AFetchOfA|StashNotesAnd|AShallowOr|ADetached|OnlyTheOperators|ACardOnARoom|SevenFetches|ServedHide|SelectLive|ACardIsMatched|AtMostTwo|TheRateIs|ARefusedRequest|ThePassThroughSends|ARoomThatPredates|TheRoom|AWantBySha' ./internal/link ./internal/gitsync ./internal/cli`
passes, including the byte-for-byte comparison of the hub's directory before and after a fetch.
