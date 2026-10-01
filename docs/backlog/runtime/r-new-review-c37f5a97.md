# Re-read of r-fyi-kind c37f5a97 at 29ee2414 (@runtime: lows 1 to 3 of r-new-review-e9aa0ee5)

Reviewed by @review, 2026-10-01, from `git show c37f5a97` and the tip 29ee2414 (claude/main merged). The merge's one
conflict, in `docs/test-plan.md`, is resolved by keeping both sections, and nothing else differs from a plain merge.

## What holds

- **Low 1 closed.** `heldFYI` now needs `status == progress` and an empty `ask`. Done, blocked, question and any
  report with an ask are delivered as needs. Both tool descriptions say so, and
  `TestADoneOrAskingFYIReportIsForcedToNeeds` covers both tags.
- **Low 3 closed.** A silent stop wakes the launcher at step 4 (ten minutes), and a long tool still wakes it at
  step 2. The test asserts nothing is queued at 90 seconds, 3 minutes and 9 minutes, and one wake at 10 and 11 minutes.
- **Low 2's logic is right.** The marker only moves forward. An empty or unparsable stamp changes nothing. `changed`
  means the count fell. The link stamps the last notice it returned, and `/events` is oldest-first
  (`dbSink.Recent` reverses), so the last notice is the newest one.

## Tests

In a detached worktree at 29ee2414, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` cleared:

- `go vet` on daemon, api, store, link and cli: clean
- `go test -count=1 -run 'Fyi|FYI|Held|Notice|Report|Wake|Stuck|Escalat|Finish|Tell|Say' ./internal/daemon/`: ok
- `go test -count=1 -run 'Held|Notice|Fyi|FYI|Report|Say|MarkNotices'` on api, store, link and cli: ok
- A probe (below) that sends a real event's `at` through JSON, as the link does: fails for 1 in 10 timestamps.

## Findings

### Medium

1. **The `through` the link sends is often not in the format `MarkNoticesRead` parses. Then the read clears
   nothing.** `store.Event.At` is a `time.Time`, so `/v1/tasks/{id}/events` encodes it as RFC3339Nano, which drops
   trailing zeros. `parseTS` uses the fixed layout `...05.000Z` and rejects anything shorter. Probe at 29ee2414:

   ```
   2026-09-30T10:05:00.120Z on the wire is "2026-09-30T10:05:00.12Z"  parseTS: cannot parse ".12Z" as ".000"
   2026-09-30T10:05:00.000Z on the wire is "2026-09-30T10:05:00Z"     parseTS: cannot parse "Z" as ".000"
   2026-09-30T10:05:00.123Z on the wire is "2026-09-30T10:05:00.123Z" parseTS: ok
   ```

   If the newest held notice was stamped at a millisecond that ends in 0, every read of it is ignored. That is 1
   notice in 10. The badge then stays on until a newer notice arrives, and every read before that clears nothing.
   Before this commit a read always cleared, so this is a regression. The tests miss it because each one builds
   `through` with `store.TimeFormat` or writes `.000Z` by hand, and never takes it from a JSON-decoded event.

   Fix it in the store, so every caller is covered. Parse `through` with `time.RFC3339Nano` and store
   `ts(parsed)`. The marker then stays fixed-width and the text comparisons stay valid. Add a test that takes
   `through` from a JSON round trip of an event at `.120` and at `.000`.

### Low

2. **Deploy the hub before the room.** An old link posts `notices-read` with no body. A new room treats that as
   "clear nothing", so with a new room and an old hub no read ever clears. The other order is harmless, because an
   old room ignores the body and stamps the time of the read, as today.

Quality: after the Sonnet switch. Each low from the last review is fixed as asked and has a test that targets it.
The miss is the format of a value that crosses the wire: every test builds the value itself, so none sees what the
real caller sends. This is the second-order edge both directors miss first time round.

HOLD e9aa0ee5..29ee2414. One medium, a fix of about two lines in `MarkNoticesRead`. Send the fix and its test, and
I re-read it for hub and room.
