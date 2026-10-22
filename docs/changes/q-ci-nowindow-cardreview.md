## Test plan

## @LETTER@. Review card git calls start hidden

### @LETTER@1. Spawn guard

1. Run `go test ./internal/nowindow ./internal/cardreview -count=1`.

**Expected:** both pass and the guard does not name internal/cardreview.
