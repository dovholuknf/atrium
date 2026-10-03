A fetch can be passed through the hub to the room that has the work (hub forge stage 3). `git fetch
http://<hub>/git/room/<room>/<repo>.git <branch>` is passed over the data connections the room dialled to that room's
link-only git route, and the answer is streamed back. The hub stores nothing: no directory, no object, no file, and the
request body is only read, checked and sent on. `<repo>` is `<owner>/<repo>` (host `github`) or
`<host>/<owner>/<repo>`. Item f-hub-fetch-pass.

What the hub adds: protocol v0 (the `Git-Protocol` header is never sent on), a request with `shallow`, `deepen` or
`filter` lines refused before it is passed on, `503 <room> is not connected` when the room is not attached or does not
answer, 6 fetches a minute per reader (a fetch is announced by its `info/refs`, upload-pack rounds have a wider 60 a
minute) and at most 2 running at once per room (429 with `Retry-After` past either), and each fetch logged with the room,
the repo and the caller's reach (loopback, overlay or zrok private). The operator's loopback, the OpenZiti service and
a zrok private share may ask. A zrok public share answers 404. A card on a room asking over the link gets 404: that
reach is stage 1's forwarder and is not served here.

What the room serves is now its own configuration, so a want outside it is refused by the room's git: `hide refs`
and `HEAD`, then `claude/*` back, `claude/main` out again, then one branch per LIVE card, written on every request.
Upload-pack runs on environment-only config with `getanyfile`, `allowFilter` and every sha-in-want setting off. Tests
fetch `refs/stash`, a `refs/notes/*` ref, an unserved branch, `claude/main` and `hub-main`, and each is refused, by name
and by sha.

Decisions on the review's lows (the room half, 453e754a):
- A card working in the clone's own checkout on `main`, `master`, `hub-main` or `claude/main` is NOT served that branch:
  the operator's unpushed work may be there, and the card's own work is reached on a branch of its own.
- A card is live by its status (running, needs-input, needs-permission), not by whether a process runs. A PARKED card
  keeps its status and keeps being served. A done, shelved, dead or backlog card is not served.
- A card is matched to a repository by its folder name, plus its org and host when the card recorded them. A card that
  recorded neither matches on the folder name alone, so two repositories with the same folder name can each serve a
  branch NAME of the other's live card. Stage 2's scm path records the full name and closes it.

Not done: `atrium git setup` (the `insteadOf` lines) was left for its own item, because it needs the room list, the
operator token and a prompt. The URL form works without it: `git fetch http://<hub>/git/room/sg3/<owner>/<repo>.git
<branch>`. A fetch that reaches the room through an overlay shares one rate bucket per peer address.
