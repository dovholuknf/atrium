Docs: none, a test fix

## Test plan

## @LETTER@. A held ask survives a pause and a stalled runner

### @LETTER@1. Poll through a pause

1. Run `go test -tags integration ./internal/link -run TestAHeldAskIsPolledThroughAPause -count=1 -v`.

**Expected:** it passes, and the ask says go after the resume, never busy.
