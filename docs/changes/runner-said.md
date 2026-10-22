## Test plan

## @LETTER@. A missing pty host binary fails fast

### @LETTER@1. No such host binary

1. Turn `pty_host` on and point the host start at a binary that does not exist.
2. Launch a runner.

**Expected:** the runner lands in the room's process within a second or two, and its card says so.
