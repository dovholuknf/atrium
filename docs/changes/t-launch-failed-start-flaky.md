## Test plan

## @LETTER@. A runner that dies at once is a failed start

### @LETTER@1. Launch a runner that exits immediately

1. Launch a harness whose command exits with an error at once, for example a bad flag.
2. Repeat the launch several times while the machine is busy.

**Expected:** every launch fails with "exited as soon as it started" and the runner's output. The card is dead with
one ended row, and the launcher gets one notice.
