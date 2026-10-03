A card can ask the hub where to fetch code it does not have in its cwd, instead of asking for a paste (hub forge stage 4).
`GET /_hub/git/url?repo=<host/owner/repo or a name>&branch=<b>` answers with the URL to `git fetch`, and the control tool
`atrium_git_url` (args `repo` required, `branch` and `room` optional) returns the same JSON plus one line a model reads:
`fetch it with: git fetch <url> <branch>`. Item f-new-git-url.

What it answers, per branch, from one of two SOURCES:
- `hub`: finished work, pushed to the hub's store, at `/git/hub/<host>/<owner>/<repo>.git`.
- `room`: work in progress on a room, passed through at `/git/room/<room>/<host>/<owner>/<repo>.git`, with `online` for
  that room.
- A branch that is both pushed AND still on its room answers BOTH, each with its sha, and `ahead` says whether the room
  has commits the hub does not. `ahead` is worked out without copying anything: the room's sha is looked up in the hub's
  store (`git cat-file -e <sha>^{commit}`). There is no `collected_at`, because there is no copy.
- No `branch` lists the repo's branches the hub knows.
- A repo the hub does not know answers `not found` with the closest repos and branches (prefix, contains and a small
  edit distance, at most 5 of each). A room that is not attached answers `offline` before anyone tries a fetch; the hub's
  own copy of a branch is still shown.
- The repo may be given as `<host>/<owner>/<repo>`, `<owner>/<repo>`, a URL, or a short name; an ambiguous name is
  answered with the candidates and not guessed.
- URLs use the host the caller reached the hub by (the request's `Host`, with `https` when the request came in over TLS).
  A `Host` that is not a plain host[:port] is refused with 400, so a URL is never invented from it.

What a room lists is only what a fetch would be allowed to get. The hub asks each attached room for its protocol v0
`info/refs?service=git-upload-pack` advertisement through the same link git kind the pass-through uses, so the room's own
served-set rule (`ServedHide`: no `refs/stash`, no notes, no unserved branch, no `claude/main`, no clone default branch
checked out by the operator) decides, and the hub keeps only `refs/heads/*` with a 40-hex sha. Stage 3's rule that the
room serves only repos the hub synced is kept. Bounds: 5 s to ask a room, a 4 MiB advertisement, 500 branches per room,
a 10 s cache per room and repo (at most 256 entries), a 300 character input. An unknown repo asks no room at all.

Who may ask is the pass-through's rule, now one function (`gitReach`, used by the store, the pass-through and this
lookup): the operator's loopback, the OpenZiti service and a zrok private share may ask. A zrok public share answers 404,
the same as a path that is not there. Anything else is 403. The control tool is a worker tool (a card on any room has
it), and it asks the hub over the board address it already uses.

Decisions:
- The hub path in the brief, `/git/<host>/<owner>/<repo>.git`, is `/git/hub/<host>/<owner>/<repo>.git` in the code (stage 3
  put the store under `hub/` so a room name can never shadow a host). The answer carries the real one.
- When a branch is on both, the URL `atrium_git_url` text names is the room's if the room is ahead and online, otherwise
  the hub's, otherwise the room's.
- No CLI door: the other git tools (`atrium_git_sync`, `atrium_git_collect`, `atrium_git_push`) have none either. A card
  reaches this through the hub control MCP like they do.

After review (hold a1a89240, M1 and the lows):
- A CARD IS GIVEN A URL IT CAN FETCH. The first build put the hub's loopback address (the board the control tool asks by)
  in every URL, which is no address at all to a card on another room. Now, when the caller is a card (the tool call
  carries its agent), the tool asks the card's own room where its hub forwarder is (`GET /v1/hub-remote` on the room, new,
  answering `http://127.0.0.1:<agent port>/git/`) and rewrites each `hub` URL onto it: the path after `/git/` is the same
  (`<forwarder>hub/<host>/<owner>/<repo>.git`), and the card's own environment carries the token to that base and
  nowhere else. A base the room gives that is not `http://<loopback>:<port>/git/` is not used. A room that does not say
  (older than this, or not answering) leaves its card with NO url and the sentence why, never a wrong one. The operator's
  call (no agent) keeps the hub's own address.
- A ROOM'S WORK IN PROGRESS HAS NO URL FOR A CARD. The room route (`/git/room/...`) has no forwarder yet (the link's git
  kind does not route it), so a `room` source for a card has `url` empty and a `note`, and the line says a card cannot
  fetch it and to ask the card on that room to `atrium_git_push` it, then ask again. `pick` never chooses a source with no
  URL, so a branch on both gives the hub's URL (with the room's newer work named) and a branch only on a room gives the
  way on and no `git fetch`. The operator is still given the room's URL. This holds until a `/git/room/` forwarder exists.
- The fetch is tested end to end: a real forwarder in front of the hub's board, the URL the tool gave a card on another
  room, and a real `git fetch` of it with the card's token, which gets the branch out of the store. The same URL with no
  token is refused.
- The lows: the 4 MiB advertisement cap, the 500-branch cap on an answer (two rooms of 500), the early exit in
  `editDistance` and the 300-character cap of `resolveRepo` each have a test that goes red without them (the advert cap
  is a read limit AND a length check, so the test is red with both gone and the check alone is redundant); the
  bad-Host test now calls `serveGitURL` on the overlay, where the hosts guard does not stand in front, so it reaches
  the regex; an ambiguous short name is answered by `Lookup` with the candidates; a `down` room is asked again and an
  answering room is held; only an `ok` or `behind` sync counts a room as having a repository; and the `room`, and a branch
  that is not there, are cut like every other echo of what the caller typed. The test plan is section IS of
  `docs/test-plan.md`.

For @runtime, to go in the launch brief / CLAUDE-side card text (not edited here, `internal/daemon` is theirs): "To read
code that is not in your cwd, call atrium_git_url, then fetch it from the URL it gives. Never ask for a paste."

Tests: hub-only, room-only (and a real `git fetch` of the URL it gives), both with `ahead` equal, true and behind,
unknown repo with closest and no room asked, an offline room, a branch the room does not serve never listed, the URL
from the request host (loopback, overlay, zrok private, a bad `Host`), every reach, the cache, the tool returning the
same JSON plus the text line, and a branch pushed by a room that is gone naming that room as away. 12 mutations, each red:
base host, reach rule, `ahead` inverted, `ahead` as "differs", the never-served filter, all refs listed, an unknown repo
asked, no offline check, no cache hold, an offline pusher not named, closest unbounded, the tool dropping `branch`.
