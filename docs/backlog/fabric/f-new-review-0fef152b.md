# Review: f-hub-receive 0fef152b

Range `607bb026..0fef152b`, 4 commits, 27 files. The hub's own store now takes pushes: from a room's card on the link
`git` kind, and from the operator on the board. Design: docs/rnd/hub-forge-design.md rev 2, 3.2 and 3.4. Change doc:
docs/changes/f-hub-receive.md.

Verdict: **HOLD** on M1. The push log orders its rows by an id taken from the clock, and nothing carries that id across
a restart. After a restart with the clock behind the last row, a release does not release. I proved it with a test.
M2 is a crash window the change doc does not cover. The identity, the rules and the hook, which were the security
questions, pass.

## How it was checked

- I read all the non-test code in the range.
  - Identity and reach: `identity.go`, `edge/reach.go`, `link/git.go`, `link/git_store.go`, `link/proxy.go`.
  - Push handling: `receive.go`, `receive_rules.go`, `pktline.go`, `refname.go`.
  - The log: `pushlog.go`, `hubstore/gitpush.go` and migration 0008.
  - The CLI and the hub wiring.
- I read three files outside the range that the brief depends on:
  - `edge/local.go`, for LocalOperator;
  - `gitsync/forward.go`, the room's forwarder today;
  - `mirrorOne`, to check lock ordering.
- At the tip, in a detached worktree:
  - `go vet` over gitsync, link, hubstore, edge and cli is clean.
  - `go test` passes for gitsync (68s), link (117s), hubstore and edge.
- I wrote a proof test for M1 in hubstore, ran it and removed it. It is below.
- I did not redo the ~50 mutation checks. The change doc lists them, and the survivors it says were fixed match tests I
  read.

## Findings

### M1 (HOLD): push ids come from the clock and reset on restart, so a release can sort before the owner it releases

`nextPushID` returns `max(now in ns, last + 1)`, and `last` exists only in process memory. After a hub restart, or a
database moved to a machine whose clock is behind, a new row can get an id smaller than rows already in the table.
`ownerOf` is "the first push with an id greater than MAX(release id)". So a release written with a smaller id does not
release.

Proof, in `internal/hubstore` at the tip:

1. Append s1's push at T0.
2. Set `pushIDs.last = 0`, which is what a restart does.
3. Write a release at T0 minus 1h through `nextPushID`.
4. `Owner` answers `sg4 s1 true`. The branch is still s1's.

Two consequences follow:

- `atrium rooms git release` reports "released" while the log keeps the old owner until the clock passes T0.
- An operator push in that window takes a row id ahead of the owner's. The operator then becomes the owner, and the
  card is locked out of its own branch.

Both fail closed, but they break the invariant the whole design leans on: the owner is the first push after the latest
release.

The fix is small. Take the id inside the Append and Release transactions as `max(now, MAX(id) + 1)` read from the
table, or seed `pushIDs.last` from `MAX(id)` when the store opens. The first also holds for two hub processes on one
database. Add a test that resets `pushIDs.last` and writes a release with an earlier clock. MemPushLog orders by its
slice, so the gitsync tests cannot see this.

### M2: a ref that moved with no row, and release rows that stay when git refuses

There are two windows here.

**1. A crash between git's ref update and `Append`.** The hub can die after receive-pack has moved the ref and before
the row is written. `undo` never runs in that case, and the ref stays with no row.

- For a new branch, the log then has no owner. The next card of any room can fast-forward it and become the owner.
- For an existing branch, the old owner stands, which is right.

The change doc's "no row means not pushed" is not true in this window.

Two ways to fix it:

- Write a `pending` row before git and confirm it after, so a pending row with a matching ref counts as the owner.
- At startup, compare each store repository's refs with the latest row of each branch, and audit or own any mismatch
  as described in the doc.

Either fix is fine. So is a paragraph in the change doc that accepts the gap for stage 1 and names the consequence.

**2. Release rows are written before git runs.** If the hook then refuses (not a fast-forward) or fsck fails, the
release stays and the pusher owns nothing.

The release was justified, because the owner's room answered that the card is gone, dead, or done for 7 days, so this
is benign. Say it in the doc's "decided: How is a taking-over written to the log?". Or write the release in the same
Append as the push row, which also makes it all-or-none.

### L1: capabilities are read only from the first line that has a NUL

`parsePush` takes `req.Caps` from the first NUL line only. git's `read_head_info` parses a feature list on every command
line. So `push-options` or `object-format=sha256` on a second line gets past the hub's check and reaches git.

The hook ignores push options, and git checks the object format itself, so this does no harm today. Still, the hub's
view and git's should not differ. Either refuse a NUL on any line after the first, or apply the same checks to every
feature list.

### L2: a must-carry for f-room-forwarder, and duplicate headers

Today nothing on a room can reach `/git/hub/` on the link with headers it chose. `gitsync/forward.go` is a per-command
forwarder with a 128-bit token, used only by the daemon's own fetches.

f-room-forwarder will be a stable listener that cards' own git talks to. A card's git can set any header with
`-c http.extraHeader=X-Atrium-Card: <other card>`. The forwarder is an `httputil.ReverseProxy`, which copies incoming
headers. So it must `Del` both card headers on `pr.Out` before it sets its own, or a card can push as another card of
its room. The ownership rule depends on exactly that difference.

On the hub, `Header.Get` reads only the first value. Refuse a request where either header has more than one value, so
an `Add` instead of a `Set` in the forwarder fails loudly rather than letting the client's value win.

Put both points in the change doc's "For the next items". I will check them when f-room-forwarder comes.

### L3: what the lock is held around

- `rec.copyTo(w)` writes the response to the client before the deferred unlock. A client that stops reading the
  result, which is a report for up to 1000 refs plus git's progress, holds `store:<repo>`. The spool before the lock is
  right, and slowloris on the body is fine. Copy the response out after the unlock, which is safe because everything it
  needs is in `rec`.
- `decide` runs `git check-ref-format` once per ref under the lock. 1000 refs on sg4's Windows is tens of seconds.
  Running it before the lock, in `askOwners`' pass, gives the same answer, because the ref name does not depend on
  the lock.
- `askOwners` asks each distinct owner one after another, with 8 seconds each and no overall cap. It holds no lock, but
  one push can tie up a link connection for a long time. Cap the total, for example at 30 seconds, and count owners
  left unasked as unreachable, which `checkOwner` already turns into a refusal.

### L4: a card on the hub's own machine is the operator

`LocalOperator` is loopback, a loopback Host and no forwarding header. A card running on the hub machine meets all
three. Any room on that machine counts, including sg4 when the hub runs on sg4. So with this change such a card can push
`main`, push tags and release any branch through `http://127.0.0.1:<board>/git/hub/...`.

This is the existing trust model, and those cards could already reach `/_hub/` as the operator (stage 2 in local.go).
Stage 1, though, is what makes `main` writable that way. State it in the change doc under who can reach it, so the
operator knows a room should not run on the hub machine as the hub's own account.

### L5: small points

- **An adopted mirror's branch.** The mirror pass force-fetches `+refs/heads/<branch>` from the checkout into an
  adopted bare. An operator push to that branch through the store is overwritten on the next pass, and the log still
  says the operator pushed it. Refuse the operator's push to the mirror's own branch, or note it in the doc.
- **git's own stderr.** It reaches the client on sideband 2 for hook, fsck and unpack failures, and git can name the
  repository or quarantine path there, for example "unable to create temporary object directory". The hub's own
  sentences carry no path. This is not worth filtering for stage 1, but the brief's point (8) is true only of the hub's
  own sentences.
- **The test plan's own placeholders.** The test-plan headings are `@LETTER@`, which is fine if the release tooling
  fills them.

## The brief's nine points

1. **Identity.** Pass.
   - The room comes from the name the link certificate gave in `serveGit`, and `X-Atrium-Room` is ignored.
   - The card headers are read only for `CallerRoom`. On the board they are never read, the push is the operator's,
     and `forGit` strips them along with `Authorization`, `Cookie` and `Git-Protocol`.
   - A chain must start with the card, is capped at 8 entries, and each id is `[A-Za-z0-9_-]{1,128}`.
   - `inherits` requires `p.Room == room`.
   - Forged and duplicate headers: see L2.
   - Reach marks are set by the listener's wrapper. The zrok share is marked public or private from `bs.Mode`, and
     the ziti listener as overlay. Nothing a client sends sets a mark.
   - The main listener uses `LocalOperator`:
     - `net.ParseIP(...).IsLoopback()` covers `::1` and every 127/8 address.
     - Host spoofing is caught by the Host check.
     - `X-Forwarded-*`, `Forwarded`, `X-Real-Ip` and `X-Proxy` refuse.
     - A unix socket's RemoteAddr fails `SplitHostPort`, which refuses.
     - The known gap, a raw TCP tunnel, is documented in local.go.
   - A card on the hub machine: see L4.
2. **The rules before git.** Pass, apart from L1.
   - The pkt length is 4 to 65520 and refused past the buffer. `0001` to `0003` refuse.
   - The command list is read from the first 1 MB of the decompressed spool. git reads the same bytes, because the
     spooled body is what git is handed and Content-Encoding is dropped.
   - The other line checks:
     - each sha must be 40 lowercase hex;
     - more than 1000 refs refuses;
     - shallow, push-cert, push-options and sha256 refuse;
     - a NUL in a ref fails the allowlist.
   - The ref allowlist runs before `check-ref-format`. Case and directory/file clashes are checked against the
     repository and within the push, and a ref named twice is refused.
   - Delete, tags (operator only, never moved), non-heads, and `main` and `claude/main` for cards are all refused.
   - The old sha is checked against `cur`, which is read under the lock.
3. **The hook and env.**
   - `core.hooksPath` rides `GIT_CONFIG_COUNT`. That is command scope, which outranks repository config.
   - A store repository is made with `--template=` and has no hooks. Its config is written only by the hub, and a push
     cannot write config.
   - `GIT_CONFIG_NOSYSTEM` and `GIT_CONFIG_GLOBAL=/dev/null` are set.
   - `cgi.Handler` passes only `inherited`, and request headers arrive as `HTTP_*`, never as `GIT_*`.
   - A refused push moves nothing. The hub's own refusal never runs git. The hook's refusal fails the whole push, and
     git drops the quarantine. The spool is removed, and a repository made for a push is removed again if it took
     nothing.
   - The undo-on-log-failure is compare-and-swap (`update-ref ref old new`), which is right. The crash window is M2.
4. **Ownership.** Pass, apart from M1.
   - The lazy release is answered by `p.ctl.relay` to the room named in the owner's row. The pusher's room is never
     asked about another room's card, so a pusher cannot forge the answer.
   - The answer is used only for the owner it was asked about, which is re-read under the lock. A changed owner gets
     "try again".
   - 7 days is `DoneRelease`.
   - An unreachable room, or any error other than ErrCardGone, keeps the branch owned.
   - The release route sits behind `LocalOperator`, and a POST body is limited to 4 KB.
5. **Mirrors and locks.**
   - Adopted mirrors are refused to cards at both the advertisement and the push.
   - The lock order is `store:` then `mirror:` then `store:*`. `mirrorOne` takes only `mirror:`, so there is no
     inversion.
   - Two pushes of one new branch: the second sees the ref there with an old sha of zero and is refused.
6. **Size and DoS.**
   - A declared Content-Length over 500 MB is refused before reading, and a chunked body is capped while spooling.
   - gzip is capped after it is decompressed.
   - `receive.maxInputSize` matches the cap, and `receive.fsckObjects` is on.
   - The spool happens before the lock, so a slow body holds no lock.
   - L3 covers the response written under the lock and the owner questions with no total cap.
7. **Migration 0008.**
   - It tolerates an existing table (`IF NOT EXISTS`), indexes `(repo, ref, id)`, and has a kind CHECK.
   - The docs_test pin now pins 0007 at position 7, which is the right invariant.
   - The defect is in the id scheme in `gitpush.go` (M1), not in the DDL.
8. **No disk paths in refusals.** Pass for the hub's sentences. See L5 for git's own.
9. **A hub with no PushLog takes no pushes.** Pass. Both the advertisement and the push check it, and
   `ReleaseBranch` errors. The hub sets `PushLog` before it sets the handler.

## Does @runtime need to see 0008 separately?

No for the DDL. It is one additive table and one index, and both tolerate being there. The fix for M1 is in
`hubstore/gitpush.go`, and I will re-read it. If @runtime owns hubstore's id conventions, a one-line look at that fix is
enough.

## To close the hold

- M1 with its test.
- M2 fixed, or written into the change doc as accepted for stage 1.
- L2's two lines added to "For the next items".
- The Lows can follow.

Atrium-Verdict: hold 607bb026..0fef152b
Quality: the security core is careful: the reach is set by the listener, the rules run before git, the hook belongs to
the hub, and every rule is mutation-checked. The hold is a clock id in the log that a restart can reorder.
