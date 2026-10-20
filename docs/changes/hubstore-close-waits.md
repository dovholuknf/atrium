Docs: none, internal fix

## Test plan

## @LETTER@. The hub's store closes cleanly

### @LETTER@1. Close waits and a late call does not halt

1. Run `go test ./internal/hubstore/... -count=1`.
2. Run `TestAHumanOwnedItemCannotBeUnblockedThroughTheHub`, `TestItemSyncIsIdempotentAndQualifiesASessionOwner` and
   `TestTheReviewRouteRefusesACardAndAnotherOrigin` in `internal/link` with `-tags integration`.

**Expected:** all pass. A call running when `Close` is called finishes first, a call after `Close` answers `ErrClosed`
and the store is not halted, and a real failure still halts it.
