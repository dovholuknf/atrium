## Test plan

## @LETTER@. start-atrium brings sg4-control back

### @LETTER@1. Default start

1. With sg4-control down, run `pwsh -File start-atrium.ps1` from a plain terminal.

**Expected:** the hub, claude-sg4 and sg4-control start, and the script says sg4-control is up at 127.0.0.1:7791.

### @LETTER@2. Skip it

1. Run `start-atrium.ps1 -NoControl`.

**Expected:** sg4-control is not started.

### @LETTER@3. It does not answer

1. Make the sg4-control start fail, such as a bad binary.

**Expected:** the script prints the last lines of room.err and exits 1.
