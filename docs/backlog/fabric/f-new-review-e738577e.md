# Review of e738577e (f-026, a legacy overlay dial cannot take a proven name)

Reviewed by @review, 2026-09-30. First read as 4333ff0c. `git range-diff` shows e738577e is the same patch rebased
onto two queue-file commits.

## What holds

- **Every kind is covered.** The check sits in `Hub.take` after the name is settled and before the kinds split
  (`internal/link/hub.go:310-323`), so control, data, announce, upgrade, relay and git all go through it. The relay
  hole named in the commit (serveRelay only asks `Has(name)`) is closed.
- **A proven room cannot lock itself out mid-upgrade.** The room wraps its `Dialer`, not each caller, in `Proven`
  (`internal/link/overlaytls.go:176-199`), and `roomDialer` returns that one dialer for every kind
  (`internal/cli/transport.go:217`). A room holding a certificate never dials the old path for upgrade or git. The
  only rooms that do are an old-form join string or a missing `room.crt`, and those are the ones meant to be told to
  re-join. The refusal says how.
- **Fail closed on a store error is right.** `h.Enrolled` answers true on an error (`internal/cli/atrium_run.go`),
  for the one name asked about. The store's guard halts on a real failure, so the error case is short-lived.
- **Migration `0006_room_enrolled`** is appended last, is an `ADD COLUMN` (duplicate swallowed by the runner,
  `internal/hubstore/schema.go:306`) plus an `UPDATE ... WHERE enrolled_at = ''`, so a second run changes nothing.
  The correlated subquery matches the `room_audit` columns (`room_id`, `kind`, `at`). `joined` is written in one
  place only, `Spend` (`internal/hubstore/secrets.go:175`), so the backfill never marks a legacy-only room.
- **Name folding agrees.** `Enrolled` uses `fold` and `provenName` uses `keyOf`. Both are ASCII lowercase, and
  `fold` also trims, which only makes a padded name more likely to be refused.
- **Rate limit** of the refusal line is bounded by the set of proven names, not by what a caller puts in its hello.
- **`Hub.control`** now refuses a keyless attach over a keyed one (`hub.go:410`). On an overlay listener `take`
  already refused it, so this is the second fence, for a transport with no `legacyConn` type.

## Live probe (read-only GETs on the hub at 127.0.0.1:7778, 16:59)

- `/_hub/rooms`: claude-sg4, m1mini, sg3 and sg4-control attached, all four `proven:true`.
- `/_hub/audit?kind=joined`: one line, sg4-control. `/_hub/audit?kind=room-unproven`: none.
- So deploying this refuses no room that attaches today.

## Findings

### Low

1. **A proven room with no `joined` line is unenrolled until its next certificate attach.** claude-sg4, m1mini and
   sg3 are proven but have no `joined` line left, so the backfill leaves them empty and `Proved` fills them on their
   next attach. While one of them is detached after the hub restarts, a legacy dial under its name is still
   accepted. The window closes on that room's first reattach, usually seconds. No change needed, but the hub
   deploy should come up with the rooms reattaching, not while one is known to be down.
2. **`Spend` marks the name enrolled before the certificate is signed** (`secrets.go:165`). If signing or the write
   back to the room fails after that, the name is proven with no certificate anywhere, and its old path is closed.
   The secret is already spent in that case, so the room needs a new join string whatever this does. The refusal
   sentence tells it that. Noted, not a change.
3. **The kinds loop in `TestOldPathCannotTakeAnAttachedProvenName` omits `upgradeKind` and `gitKind`.** They are
   covered by construction (the check is before the switch), but a later move of the check into the switch arms
   would drop them silently. Add both to the loop.
4. **The `Hub.control` keyless-over-keyed change has no test of its own.** Reaching it needs a transport that is not
   a `MixedListener`, which is why it is hard to test here. Worth one test against a plain listener with a keyed room
   attached.

## Tests

At 4333ff0c, in a detached worktree, `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` cleared:

- `go vet ./internal/link/ ./internal/hubstore/ ./internal/cli/`: clean.
- `go test ./internal/hubstore/ ./internal/cli/`: ok (41s, 26s).
- `go test -run 'Proven|OldPath|Legacy|Overlay|Takeover|Relay|Enrol' -v ./internal/link/`: 21 pass, including the
  three new ones.
- `go test ./internal/link/` (whole package): ok (204s).

Not run: internal/daemon and internal/api, which the diff does not touch.

## Follow-up 53a6f8f0 (landed with 3bea1447)

`git range-diff` shows 3bea1447 is the same patch as e738577e. 53a6f8f0 closes lows 3 and 4: the kinds loop adds
`upgradeKind` and `gitKind`, and `TestAKeylessAttachCannotReplaceAKeyedRoom` tests the `Hub.control` comparison over
a pipe. At 53a6f8f0: vet clean on link, the five old-path and keyless tests pass 3 of 3. With the pre-f-026
comparison put back, the keyless test fails, so it pins the fix. Lows 1 and 2 stay as noted.

## Verdict

**PASS e738577e.** Four lows, none blocking. Hub side: **ROOM DEPLOY OK e738577e** on review grounds.
