# r-new-review-a23a9034. Growler R1: review

Status: open. Filed by @review 2026-09-30. Read-only review of a23a9034 (merge of `claude/r-growler`, R1). Owned by
@runtime.

The migration is right: `0005_growl` is appended at the end of the slice and every statement is `IF NOT EXISTS`.
The store's transactions are scoped per room, a card missing from an announcement does not end its growler
(`GrowlSync` ends only cards that are `present`), and the reasons written always satisfy the `CHECK`, because a
report is only ever `blocked` or `question` by the time it reaches `derive`.

## 1. Low. Ending a reason overwrites a dismissal

`endGrowl` (`internal/hubstore/growl.go:131`) sets `state = 'resolved'` and `changed_via = 'hub'` on every row whose
reason ended, dismissed ones included. The `0005_growl` comment says the opposite: "a growler dismissed an hour ago
whose permission was answered just now is still `dismissed`". Who dismissed it, and from which tab, is gone. Either
keep the state and set only `ended_at`, which is why the column exists, or fix the comment.

## 2. Low. Two publishes can leave every screen one set behind

`publish` (`internal/link/growl.go:642`) builds its payload outside the lock and compares fingerprints inside it.
Two callers at once (an announcement and a tick, or a fill) can build in one order and store in the other, so the
older set is sent last and becomes `last`. The next tick fixes it, since its set differs from the stale fingerprint.
So screens are stale for up to `growlTick`, 30 seconds. Build and compare under one lock, or send a version with the
set.

## 3. Low. A fresh tab can miss the opening set

`openGrowls` (`growl.go:850`) sends the live set to a new stream without blocking, and drops it when the channel is
full. `publish` suppresses a set that has not changed, so that tab sees nothing until a growler changes, which can be
hours. Send it blocking with a short timeout, or have the board fetch `/_hub/growls` on open.

## 4. Low. An unfilled permission is asked about every tick, forever

`tick` starts `fill` for every room that has a permission growler with no subject (`growl.go:496`). When the room's
pending list never contains that card, for instance a request answered while the card's status lags, the hub asks
`GET /v1/permissions` of that room every 30 seconds until the growler ends. It is bounded by `filling` and cheap.
Stop asking after a few misses for that id.

## Tests

Same run as `r-new-review-5edc1821.md`.
