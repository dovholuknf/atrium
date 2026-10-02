## Test plan

## @LETTER@. Pushing to the hub's own git store

Setup for all of these: the hub runs, `atrium rooms git init https://github.com/netfoundry/omnigent` has made
`github/netfoundry/omnigent`, and room sg4 is attached with card `s1`. The room's forwarder (item f-room-forwarder) sends
`X-Atrium-Card: s1` and `X-Atrium-Card-Chain: s1`. Until it exists, push through the hub's board address as the operator
and with `-c http.extraHeader` through the link.

### @LETTER@1. The operator pushes main from the hub machine

1. In a clone, `git push http://127.0.0.1:7778/git/hub/github/netfoundry/omnigent.git main:refs/heads/main`.
2. Commit again and push again. Then `git push --force` a rewritten main.

**Expected:** the first two land and `atrium rooms git store` shows the new `main` sha and time. The forced push is
refused with `remote: atrium: ...` and `! [remote rejected] main -> main`, and main is where it was.

### @LETTER@2. A card pushes a branch as its room and card

1. From sg4, on `fix/x`, `git push hub fix/x` (the hub remote is the room forwarder's URL for this repo).
2. `atrium rooms git store`.

**Expected:** the push lands. The list shows `fix/x` owned by `sg4 s1` with the push time.

### @LETTER@3. A card cannot move main, delete, or tag

1. From the same card: `git push hub fix/x:main`, `git push hub :fix/x`, `git push hub v1`.

**Expected:** each is refused with a sentence after `remote: atrium: ` (`only the operator moves main. push your work
under another branch name`, `deleting fix/x is not done by a push`, `a card cannot push a tag`), and `nothing was
pushed`. Nothing moved.

### @LETTER@4. Another card cannot push to an owned branch

1. As sg4's card `s2`, commit on top of `fix/x` and push it. Then as m1mini's card, the same.

**Expected:** refused with `fix/x is owned by sg4's s1, fetch it and push under another name`. A push of the same work
as a new name lands.

### @LETTER@5. The owner's successor takes the branch over

1. Make a new card on sg4 that `moved_to`-chains from `s1` (the forwarder sends `X-Atrium-Card-Chain: s9,s1`). Push
   `fix/x` as `s9`.

**Expected:** it lands, `store` now shows `fix/x` owned by `sg4 s9`. The same chain sent from m1mini is refused, because
card ids belong to a room and the hub honours a chain only on the room it came from.

### @LETTER@6. A branch of a finished card is released

1. Cull `s1` on sg4 (or let it be done for more than 7 days), then push to its branch as `s2`.
2. Take sg4's link down and push to a branch owned by one of its cards as m1mini's card.

**Expected:** 1. lands, owner is now `sg4 s2`, and the log has a release row `card gone`. 2. is refused with `... (the hub
cannot reach sg4 to ask whether that card is finished)`, and the branch is still owned.

### @LETTER@7. The operator lets a branch go

1. `atrium rooms git release netfoundry/omnigent fix/x`, then `atrium rooms git release netfoundry/omnigent fix/x` again,
   then with `main`.

**Expected:** `fix/x was owned by sg4's s9 and is released. the next card to push a fast-forward owns it`, then `fix/x has
no owner, so there is nothing to release`, then `that is not a branch that can be released`. The operator can
fast-forward any branch without releasing it, and its owner stays.

### @LETTER@8. No rewriting, no case twins

1. `git push --force` a rewritten `fix/x` as `s1`. Push `Fix/x` while `fix/x` exists. Push `fix/x/y` while `fix/x` exists.
   Push two refs in one command where the second breaks a rule.

**Expected:** each is refused (not a fast-forward, `differs only in case`, `clashes`, and `nothing was pushed` for both
refs of the last one), and no ref of any of them moved.

### @LETTER@9. A repository the hub lacks

1. Push `fix/x` as a card to `github/netfoundry/new-one` with `git.create_on_push` off, then on. Then as the operator.

**Expected:** off: refused, with the sentence that says to ask the operator to run `atrium rooms git init`. On: the
repository is made and the push lands. The operator's push to a repository that is not there is refused either way, and
a push that is refused after the repository was made leaves no repository behind.

### @LETTER@10. What a room can fetch

1. From sg4, `git clone` the repository, and `git clone --depth 1`, and `git clone --filter=blob:none`.

**Expected:** the plain clone works. The other two fail with `fatal: remote error: atrium: this hub serves whole fetches
only, with no filter or depth`. A push from a shallow clone is refused too.

### @LETTER@11. Who can reach it

1. Push as the operator through the zrok public share's address. Push from another machine on the main listener. Push
   with a card header on the board's address.

**Expected:** 404 on the public share for reads and pushes. 403 on the main listener from anywhere but the hub machine,
and 403 with a `Host` that is not loopback. Over the overlay or a private share it is the operator. A card header on
the board is ignored and the push is logged as the operator's. A room cannot send `X-Atrium-Room`: the room is the one
on the link certificate. **A card running on the hub's own machine is the operator here**: it is loopback with a
loopback `Host` and no forwarding header, which is what `LocalOperator` asks, so by the existing model it can push
`main`, push tags and release branches through `http://127.0.0.1:<board>/git/hub/...`. Do not run a room on the hub
machine as the hub's own account.

### @LETTER@12. The mirrors are not reachable here

1. `git clone .../git/hub/github/dovholuknf/atrium.git` for a `git_repos` mirror that was never `init`ed. Then run
   `atrium rooms git init` on it, and try a push as a card and as the operator.

**Expected:** 404 for the first. After init the room and the operator can fetch it, and every push, the operator's
included, is refused with `... is a mirror the hub keeps in step with a checkout, so a push cannot land there`, because
the mirror pass force-fetches the checkout over it.

### @LETTER@13. A hub that dies in the middle of a push

1. Push as a card, and kill the hub (`kill -9`) after git moved the branch and before the push is settled. Start it
   again. Then, with the hub down, make the same state by writing a `pending` row by hand for a branch that is not there.

**Expected:** on start `atrium rooms git store` shows the branch owned by the card that pushed it, and a push from
another card to it is refused as owned. The pending row whose branch is not there is gone, and nothing is owned for it.

### @LETTER@14. A card asked about too many owners

1. Push 12 new branch names in one push, each of which is owned by a different card of one room, from a card of
   another room, while the first room is not answering.

**Expected:** refused after at most 10 seconds, saying the hub cannot reach the room to ask. At most 8 cards were asked.

### @LETTER@15. A forwarder that adds to the card headers

1. `git -c http.extraHeader='X-Atrium-Card: s2' push ...` from a card, through a forwarder that adds its own
   `X-Atrium-Card` and does not replace the client's.

**Expected:** refused with `the request names its card more than once`, not pushed as either card.

### @LETTER@16. Size

1. Push more than 500 MB, once with a `Content-Length` and once chunked.

**Expected:** `413`, and no file is left in the hub's temp directory.

## Decisions

- decided: Where does the hub check a push, given that git's own `pre-receive` hook cannot be given the identity? / The
  hub reads the push's command list itself and applies every rule that needs no objects in Go, before git is run. The one
  rule that needs objects, fast-forward, is a hub-owned `pre-receive` script (`<hub dir>/git-hooks`, set through
  `core.hooksPath` in the environment, never in a repository) that runs `git merge-base --is-ancestor old new`. git's own
  `receive.denyNonFastForwards` and `denyDeletes` stay on behind both. / It is the design's rules unchanged, and a repo's
  own hooks and config are never read.
- decided: How does the user's git see a refusal? / A report-status body with `unpack ok`, an `ng <ref> <sentence>` per
  ref, and the sentence on sideband 2 as `atrium: ...`, so git prints `remote: atrium: ...` and `! [remote rejected]`. A
  refusal before any push body (no identity, a refused repo) is an `ERR atrium: ...` pkt-line in the advertisement, which
  git prints as `fatal: remote error: atrium: ...`. / Both are what git already prints for a server's refusal.
- decided: What does a push with no card header from a room do? / It is refused, `named none`: a room's name alone is
  not an identity. / Design 3.2: a card pushes, a room does not.
- decided: Which headers are trusted? / `X-Atrium-Card` and `X-Atrium-Card-Chain`, only when the caller is a room on the
  link `git` kind. The chain must start with the card, has at most 8 entries and each is `[A-Za-z0-9_-]{1,128}`. They
  are stripped before git, along with `Authorization`, `Cookie` and `Git-Protocol`. / Everything else reaches the hub
  through a reach the network already vouches for.
- decided: The hub trusts what a room's forwarder says about its own cards. / Yes, by design: the hub has no way to
  check a card id, and a room is trusted for its own. Two cards of one room are told apart only by this header, so a
  room that lies is the room's own problem. / Stated here and tested (a forged `X-Atrium-Room` is ignored, a forged card
  is the room's own).
- decided: Does the chain work across rooms? / No. A successor inherits only an owner on the pusher's own room, because
  card ids are per room. A card that moves to another room pushes under a new branch name. / A cross-room chain is a
  claim the hub cannot check.
- decided: How is a reach told apart? / The listeners mark their requests (`edge.MarkReach`): a zrok public share is 404
  for the whole `/git/` tree, an overlay or private share is the operator, and the main listener is the operator only from
  loopback with a loopback `Host` and no forwarding header. Anything else is 403. / The classification is made where the
  listener is made, not inferred from headers a client sends.
- decided: What are the ref name rules? / `refs/heads/` or `refs/tags/` and nothing else, ASCII letters, digits and
  `. _ - / + = , @`, at most 200 bytes, no `..`, `//`, `@{`, leading dot part, `.lock` part, or Windows device name, and
  git's own `check-ref-format` as well. / The name lands as a file name on sg4's NTFS and goes into logs and a board.
- decided: Tags. / Only the operator creates one, and nobody moves one. / Design 3.2 says tags are the operator's.
- decided: An operator-owned branch. / A branch the operator creates is owned by the operator (room and card empty), so a
  card cannot push to it, and `release` lets it go. The operator may fast-forward any branch and ownership stays. / The
  operator never takes a card's branch by pushing to it.
- decided: Shallow pushes. / Refused, with the sentence to fetch the rest first. The hub takes whole fetches only too. /
  Stage 1 has no depth or filter handling.
- decided: Who asks the owner's room, and when? / The hub, before the repository lock, only for a branch another card
  owns, with an 8 second bound. The answer is used under the lock after the owner is read again, and a branch whose owner
  changed in between is refused with `try again`. / No network call under the lock.
- decided: What frees a branch? / The owner's room says the card is gone (the room has no such card), `dead`, or `done` for
  more than 7 days. An unreachable room keeps the branch owned and says so. / Design 3.2.
- decided: How is a taking-over written to the log? / The new owner's push row is written pending with what released the
  branch, and when the push lands one transaction writes the release row `moved to <card>` (or the room's reason) with the
  old owner's room and card, then makes the push row done with a later id. A push that git refuses writes no release. /
  The owner is always the first push row after the latest release row, and a refused push must release nothing (review M2).
- decided: `LastOperatorPush` takes a ref, `(ctx, repo, ref)`. / The brief gave `(repo)`. A board shows the time of
  `main`, and the log holds the operator's pushes to other branches. / The list asks for `refs/heads/main`.
- decided: When is a push written to the log, given that the hub can die between git moving a ref and the row? / BEFORE git
  runs, as `pending` rows under the repository's lock, with `PushLog.Begin`. After git, `Settle` makes the rows of the
  refs that now hold the pushed sha `done` and deletes the others. A pending row counts as a push, so it owns its branch.
  What a crash (or a failed `Settle`) leaves is settled from the refs: a pending row whose ref holds the sha it was to
  take is done, anything else is dropped. That runs when the hub starts (`Hub.Reconcile`, from `atrium_run`, before it
  serves) and again under the lock at the start of every push to a repository. If `Begin` fails the push is refused and
  nothing moved, which replaces the earlier `update-ref` undo. If `Settle` fails the push still stands, stays owned and
  is settled by the next one. / Review M2: "no row means not pushed" was not true in the window, and a new branch with no
  row could be taken by any card. A repository a push made and that a crash left empty stays, listed `(empty)`.
- decided: A repository is made on a card's first push only. / When `git.create_on_push` is on, under the store-wide
  lock and the same case check as init, and removed again if the push took nothing (and the empty owner directories
  with it). The operator never creates a repository by a push. / Brief; init is the operator's way.
- decided: The advertisement for a repo that is missing. / Made from a throwaway empty repository, so a GET creates
  nothing. / Reads must not write.
- decided: Which locks, in which order? / `store:<lower name>`, then `mirror:<name>` if the directory is a configured mirror,
  then `store:*` for the case check and the owner directory only, released before the repository is made. Held from the
  rules through receive-pack. / Part 1's review.
- decided: An adopted mirror. / A room and the operator can fetch it, and NO push is taken, the operator's included, refused
  both at the advertisement and at the push. / The mirror pass force-fetches `+refs/heads/<branch>` from the checkout into
  it, so a push would be overwritten while the log still said the operator pushed it (review L5; it reverses the earlier
  rule that let the operator push).
- decided: Hub deploy. / Needs migration 0008 (applies itself on start) and nothing new in settings beyond
  `git.create_on_push` from part 1. A hub that cannot read the table takes no pushes. 0008 was edited in place after the
  first review (it had not shipped, and it gained `state` and `batch`), so a database that applied the first form, which
  only a hub run from the branch has, needs `DROP TABLE git_push` and `DELETE FROM schema_migration WHERE name =
  '0008_git_push'` before it starts. / No file to edit by hand otherwise.
- decided: What are the push log's ids? / Text that sorts in write order, each taken INSIDE the transaction as the larger of
  the clock and the table's highest id plus one, so no restart and no clock that is behind can put a release row ahead of
  the owner it releases. The in-memory counter is gone. `MemPushLog` orders by its slice, which is the same order, and the
  shared tests run both with a clock an hour behind. / Review M1: after a restart with the clock behind, a release did not
  release and an operator push took a card's branch.
- decided: Where is a feature list read? / On every command line that has a NUL, as git's `read_head_info` does, and the
  push-options and object-format checks are made on all of them. / The hub's view of a push must be git's (review L1).
- decided: What if a request has the card headers more than once? / It is refused with a sentence, for the advertisement
  and the push. / A forwarder that adds to a client's header instead of setting it must fail loudly, not have the client's
  value win (review L2).
- decided: What is held across the lock? / The verdict on every ref name (a `git check-ref-format` process each) is made
  before it. The answer is built in a buffer and written to the client after it is released, so a client that stops reading
  holds nothing. The owner questions are asked before it and capped: at most 8 rooms and 10 seconds in all, owners not
  asked count as unreachable, which keeps their branches owned and says so. / Review L3.
- decided: The release route. / `POST /_hub/git/release {repo, branch}` from the operator on the hub machine, the same gate as
  init. / It was missing from the proxy's route list until a link test failed (found by the test, not by a mutation).

- decided: git's own words to the client. / Not filtered: hook, fsck and unpack failures reach the client on sideband 2 as git
  wrote them, and git can name a quarantine or repository path there. The hub's own sentences name no path. / Review L5:
  the brief's "no disk paths" holds for the hub's sentences only, and filtering git's stderr is not worth it for stage 1.

## Mutation checks

Every change below was made to the code alone, the package tests were run, and a test failed (a build error, or a
crash that was the mutation's own doing, does not count).

Rules (gitsync): the hook's fast-forward check and git's `denyNonFastForwards` together (either one alone is held by the
other, which is the point of two); main operator-only; delete refused; tags operator only; a tag never moved; a stale old
sha accepted; case collision; directory/file clash; two refs in one push that differ in case; a ref named twice;
ownership refusal; a chain honoured on another room; the chain inheritance; an unreachable room releasing a branch; a
gone card not releasing; a done card released before 7 days; an adopted mirror taking a card's push (the advertisement
and the push, each alone); the declared size cap (the body is not read) and the chunked one; the `Git-Protocol` header
passed on; the card headers passed to git; `create_on_push` ignored; a repository made by a refused push left behind; a
push whose log write failed left in place; no store-wide lock (two case twins made at once); the ref allowlist; refs
outside heads and tags; a shallow push taken; a shallow or filtered fetch served.

Hub store (hubstore): the owner ignoring release rows; the owner being the latest push and not the first; the last operator
push counting a card, and being the first; a release writing no row; `Append` not in a transaction (the suite hangs, which
is a failure); branches listing tags; a released branch not showing released; ids not growing when the clock repeats;
the migration moved, and not tolerating being there; the `kind` check removed.

Found by looking at what survived, and fixed with a test: `parsePush` lost a push's capabilities when `shallow` lines came
first (a shallow push was then refused without the sentence reaching git); the delete refusal was held only by git's own
rule, so the hub's sentence was missing; the card headers were not asserted to be stripped; the size check on a declared
length had no test that the body was not read; the adopted mirror's advertisement had no test.

Not killable and left: none beyond the pairs above, where each half is held by the other (git's non-fast-forward rule
and the hub's hook; the size cap's two places).

### The second review (M1 to L5)

Each change below was made on its own and a test of the new set failed (a hang counts, a build error does not).
hubstore: the ids not read from the table (so a release with the clock behind did not release); a pending row not owning
its branch; a refused batch left pending; a settled push keeping its old id (ahead of the release marker); a takeover
that writes no release; `LastOperatorPush` counting a pending push; `Pending` ignoring the repository; the `state` check
dropped. gitsync: leftovers not settled by the next push; `Reconcile` doing nothing; leftovers all taken as landed, and
all dropped; a failed `Begin` ignored; every ref settled as landed after git took none; the operator allowed into an
adopted mirror at the advertisement and, alone, at the push; the feature list read only from the first line; a repeated
card header and a repeated chain header allowed; no cap on how many owners are asked, none on the time, and no total
deadline; the ref names checked under the lock; the answer written under the lock.

One parameter that no state could make matter, an owner query that skipped the settling batch's own rows, was found to
be dead by a mutation that survived, and was removed rather than tested.

## For the next items

- Item f-room-forwarder sends, on the link `git` kind, to `/git/hub/<host>/<owner>/<repo>.git/...` with the canonical host
  (`github`): `info/refs?service=git-upload-pack|git-receive-pack`, `POST git-upload-pack` and `git-receive-pack`, with
  `application/x-git-*-request`. A push must carry `X-Atrium-Card: <card id>` and `X-Atrium-Card-Chain: <card>,<previous>,
  <before that>` (the first entry the card, at most 8). The hub refuses a push with no card, so the forwarder should refuse
  first.
- THE FORWARDER MUST NOT LET A CARD CHOOSE ITS OWN IDENTITY. A card's git can send any header with
  `-c http.extraHeader=X-Atrium-Card: <another card of the room>`, and an `httputil.ReverseProxy` copies the incoming
  headers. So it must `Del` both `X-Atrium-Card` and `X-Atrium-Card-Chain` on the outgoing request (`pr.Out`) and then
  `Set` its own, one value each. The hub refuses a request that names either more than once, so an `Add` in place of a
  `Set` fails loudly, but a `Set` that comes after the client's value still lets the client's through only if the `Del`
  was forgotten. The ownership rule is exactly the difference between two cards of one room.
- The push row is `PushRow{Repo, Ref, Old, New, Room, Card, At, ReleasedBy, Release, Pending, Batch}`. A push is written with
  `Begin` (pending), and `Settle` makes it done. Room and card are empty for the
  operator, and on a release row they are the owner that was released.
