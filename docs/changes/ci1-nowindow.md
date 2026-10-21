Docs: none, a test guard rename

## Test plan

## @LETTER@. The exec-start guard

### @LETTER@1. Guard passes

1. Run `go test ./internal/nowindow`.

**Expected:** it passes, and a new process start in the launch code that does not hide its window still fails it.
