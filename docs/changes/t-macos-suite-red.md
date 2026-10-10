## Test plan

## @LETTER@. A pty host binds under a long state dir

### @LETTER@1. Long state dir

1. On macOS, start a room with its state dir under a path of 100 bytes or more, for example a deep TMPDIR.
2. Start a terminal card.

**Expected:** the terminal starts, and the host socket is at `/tmp/atrium-<uid>/ptyhost-<hash>.sock`, a directory at
mode 0700 owned by you.

### @LETTER@2. Short state dir

1. Start a room with a short state dir.

**Expected:** the socket stays inside the state dir as before.
