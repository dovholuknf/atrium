# Review: hub-forge stage 5, f-new-change-requests cdf17855

Range `e9be3fc3..cdf17855`, five commits on claude/f-change-requests:

- `6f2bbecd` hubstore: migration 0009_change_request, with create, list, get and a one-time end.
- `6a379c16` gitsync: the Pushed read, Reachable, and RoomBranch.
- `6bdf367a` link: GET /_hub/git/pushed, GET and POST /_hub/change-requests and /<id>, the `change-request`
  event, the owner fyi, the growler for main, and the audit.
- `7d30dbe3` docs.
- `cdf17855` hubstore: fold the source room's case on create (the answer to @runtime's Low).

Verdict: **OK for the hub**, on one landing condition (C1), with Lows L1 to L5.

## How it was checked

- **Reading.** I read the whole range: the store, the routes, `crActorOf`, `crOwner`, `crAnnounce`, the growler
  half, `RoomBranch` and `advertisedTip`. I also read `edge.LocalOperator`, `CheckPushBranch`, `ParseName` and
  the hub's git listener, to see which paths can reach /_hub.
- **The migration.** @runtime read 0009 and found nothing to hold. It is last in the list, every statement is
  IF NOT EXISTS, MAX(n)+1 is safe under one open connection, and the partial unique index matches the CRCreate
  lookup. I agree. The index is `WHERE state = 'open'`, so a closed request may be asked again, which a test pins.
- **Mutants.** 11, all killed:
  - the LocalOperator check in `crActorOf` turned off;
  - `merged` allowed to the creator card;
  - `canonicalRoom` dropped;
  - `Reachable` answering true without the ancestry walk (fails in both link and gitsync);
  - the owner taken from two matching worktrees;
  - the title in the fyi left unquoted;
  - a growler for every target, not only main;
  - the own-room check on create removed;
  - the `cdf17855` fold removed (fails the hubstore test and the link test).
- **The merge with stage 4.** I trial-merged a1a89240 and then cdf17855 onto landing 8d4d21a5. Git reports no
  conflict, but the result does not build. That is C1. With C1's rename:
  - `go build ./...` and `go vet` on hubstore, link, gitsync and cli pass;
  - `gofmt -l` is clean on every Go file either stage touches;
  - hubstore, link, gitsync and cli all pass;
  - the migration list still ends at 0009, and stage 4 adds none.

## Landing condition

### C1: both stages declare `gitsync.Store.hasCommit`, with different signatures

Stage 4's `lookup.go` has `hasCommit(ctx, dir, sha) bool`. It uses `cat-file -e`, takes 40-hex shas only, and
swallows errors, which suits its "ahead" guess. Stage 5's `pushed.go` has `hasCommit(ctx, dir, sha) (bool,
error)`. It tells "absent" (exit 1) from "failed", which Pushed and Reachable need, and it takes 64-hex shas too.
The text merges cleanly, and the build fails with "method Store.hasCommit already declared".

Fix: whichever stage lands second renames stage 5's helper, for example to `commitKnown`. That means the
declaration and its comment in pushed.go, the two calls (pushed.go, in Pushed and Reachable), and the call in
pushed_test.go (also update the "hasCommit" in that test's Fatalf message). I built and tested exactly that rename.
It is better done now on claude/f-change-requests, so the second landing needs no fix-up.

## Points

1. **The card headers: sound, and the same trust as today.**
   - A request with `X-Atrium-Card` or `X-Atrium-Card-Room` is believed only when `edge.LocalOperator` holds:
     a loopback peer, a loopback Host, and no forwarding header. From any other path, including a zrok share,
     it is refused with 403, not ignored. A mutant that drops the check fails the test.
   - Rooms cannot reach these routes. A room's git connection is served only `GitStore` and `Git` (git.go),
     never the Proxy, so a card on another room cannot forge the headers over the fabric.
   - A local process can name any card, or send no header and so act as the operator. That is the trust the
     documents and snooze routes already give the hub's own machine, so this adds nothing new. The same holds
     for the zrok share: a request with no header there is the operator, behind the share's own login, as for
     documents.
2. **`state=closed` lists merged and withdrawn too. Agreed.** "closed" reads as "finished" on a board, and
   `merged` and `withdrawn` can still be asked for alone. The one lost case is "closed only", which nobody has
   asked for. The 400 message says "open, closed or all", but merged and withdrawn are accepted too. Name them
   (L4).
3. **The owner of a room branch, by worktree folder. Acceptable for now.**
   - Only cards on the source room are searched, so a card cannot claim another room's branch.
   - Inside one room, a card is the owner only if its worktree's last folder equals the branch tail. A card does
     not choose its own worktree folder, so it cannot take that name on purpose.
   - Two matches give no owner: nobody is told and nobody may close as owner. That is the safe answer, and a
     mutant that takes the first of two fails the test.
   - The fold is many-to-one (see L2).
   - The owner is fixed at create time, so a card that later takes the worktree gets nothing. That is fine.
4. **No growler for an operator's main request on an ownerless hub branch. Agreed.** No room is involved, and
   the operator who asked is the one who would answer. A card's main request always has a room, so it raises
   one.
5. **The requester's words.**
   - The title is 200 characters at most, the why 4000 and the note 1000. Control characters are refused, and so
     are U+2028/2029 and the bidi overrides (U+202A-202E, U+2066-2069). Only the why and the note may hold a
     newline or a tab. `change` is capped at 100.
   - The fyi quotes the title, the why (clipped to 1000) and the note (clipped to 500) with `strconv.Quote`, so
     a newline in the why arrives as `\n` inside quotes.
   - The growler body is the clipped title. The event carries the title, and escaping it is the board's job.
     u-scm5 already runs every value through `esc()`.
   - The audit line is the id only, which a test pins.
6. **Who may write.**
   - Close: the operator, the creator card, or the owner card.
   - Withdraw: the operator or the creator card.
   - Merged: only the operator, and a card named in the headers is never the operator. Merged also needs a full
     sha that the hub's own store reaches from the target's tip. A finished request answers 409 with the row,
     and nothing is written.
   - A card may only make a request for its own room's branch or a hub branch.
7. **Validation.**
   - repo goes through `ParseName`, branches through `CheckPushBranch` (no leading dash, no refs/ or tags/, no
     `..`, no `:`), and rooms through an ASCII allowlist.
   - An id must match `^cr_[1-9][0-9]{0,17}$` before it reaches the store.
   - The body is capped at 64K, with unknown fields refused and nothing allowed after the object.
   - Every git call passes no shell and only a checked name or sha, within a 15 s bound. `advertisedTip` reads
     at most 4 MB and stops at the second flush.
8. **The fold, cdf17855.**
   - `source_room` is now folded ASCII-lower on create, the same fold as `ByName`, so the open lookup and the
     index agree. The list compares with `lower()`, and `actor.is`, the own-room check and `RoomBranch` use
     `EqualFold`. The growler finds its room through `ByName`, which folds too.
   - So every compare on the server agrees. The other room columns (`owner_room`, `created_room`, `closed_room`)
     keep their case. They are only compared case-blind, so that is consistent, if mixed (L1).

## Lows

- **L1: `source.room` now reads back lowercase, while `owner.room` keeps the attached case.**
  - Nothing on the server breaks. On the board (u-scm5's changereq.js and m/js/changereq.js), `sourceRoom(r)`
    feeds `hubReposAccent("room" + room)` and `hubReposAvatar(room)`. The repos tab feeds the same functions
    the push log's room in its attached case.
  - So a room whose name has a capital gets a different accent, and possibly a different avatar, on a change
    request than on its repos tile, and its name shows in lowercase.
  - Either keep `source_room` as given and fold only a key column, or have the board hash a lowercased name.
    It is cosmetic. Tell @ui.
- **L2: the owner match folds `/` to `-`.** `claude/a/b`, `claude/a-b` and `a-b` all look for the worktree
  `a-b`. Two live cards can't both hold it, so at worst the owner is a card whose branch has the same tail.
  Matching on the card's recorded branch, when the room reports one, would be exact.
- **L3: the repo's case is not folded.** `github/O/R` and `github/o/r` are two open requests for one source.
  `ParseName` lowers only the host. On a case-sensitive disk, `github/O/R` reads as not-pushed, and on a
  case-blind disk it reads the same repository. Fold the owner and repo, or resolve the name to the store's
  own repository before storing it.
- **L4: small tidy-ups.**
  - The state 400 message should name merged and withdrawn.
  - `var _ = bytes.MinRead` keeps an unused import alive; remove both.
  - The third case in `crDo`'s error switch tests `ErrCRNotFound` again, which is dead code.
  - `RecordAudit("", ...)` records the id but not who acted, operator or room/card. The row already says who,
    but the audit line alone can't. Adding `actor.party()` as the room is safe: it holds no words.
- **L5: no test-plan section.** The range has no `docs/test-plan.md` entry. If this stage wants one, it takes the
  next free letter after IR. Ask me for it.

Atrium-Verdict: hub-ok e9be3fc3..cdf17855
Quality: a careful, well-bounded API. The words are quoted as data, writes are tightly gated, merged is checked
against the hub's own store, and the tests catch every mutant I tried. The one real catch is C1, which only
shows once stage 4 is merged.

## C1 met: 1ea71a3c

`cdf17855..1ea71a3c` renames stage 5's helper `hasCommit` to `commitKnown`: the declaration, its comment, the two
calls and the test, with nothing else changed. I merged stage 4 (a1a89240) and then 1ea71a3c onto landing 7ad42ff2.
The result builds. `go vet` on gitsync, link and hubstore passes, `gofmt -l` is clean, and gitsync and hubstore
pass. The verdict above holds for the range to 1ea71a3c.

Atrium-Verdict: hub-ok e9be3fc3..1ea71a3c

## Lows: 5ef9f08c

Range `1ea71a3c..5ef9f08c`. Stage 5 itself is landed at 514afab8. This takes L2 to L5.

Verdict: **OK** for the hub.

- **L2.** The owner match no longer folds `/` to `-`. A branch tail with a slash in it gives no owner.
  `TestASlashInABranchIsNotFoldedIntoAFolderName` pins it. My mutant that drops the early return survives, but it is
  equivalent: with no fold, `f/x` can never equal a folder name. So the return is only belt and braces.
- **L3: nothing slips past the index.**
  - The open lookup in `CRCreate` now compares `lower(repo)`. The lookup and the insert are in one `tx`, on a store
    with one open connection (`SetMaxOpenConns(1)`), so two creates run one after the other. The second sees the
    first, whatever its case.
  - `CRCreate` is the only place a row is inserted. The case-sensitive unique index stays as a second guard for the
    same spelling.
  - The row keeps the first ask's spelling, which is right on a case-sensitive disk.
  - SQLite's `lower()` folds only ASCII, which is all a repo name can hold after `ParseName`.
  - My mutant that puts back `repo = ?` fails the hubstore test.
- **L4.** The 400 message names merged and withdrawn. The `bytes` keepalive and the dead `ErrCRNotFound` case are
  gone. The audit line carries the acting card's room, which is an allowlisted name and so carries no words. My
  mutant that blanks it fails `TestTheAuditLineOfACardsActionNamesItsRoom`.
- **L5.** The test plan has section IT, plus a change doc and a changelog.
- **Gates.** It builds, vet passes on hubstore and link, gofmt is clean, hubstore passes, and link passes in full.

Atrium-Verdict: hub-ok 1ea71a3c..5ef9f08c
