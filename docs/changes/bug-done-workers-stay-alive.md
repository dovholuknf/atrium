## Test plan

## @LETTER@. A done worker exits and a woken one can push

### @LETTER@1. Done exits the runner and says why
Launch a scratch worker, let it call atrium_done. Its answer carries a note that the runner exits and why, the card
shows an exit-on-report event, and the process is gone within a few seconds.

### @LETTER@2. A woken card can fetch
Say to the done card with wake. In the resumed session run `git fetch hub`. It succeeds before the card has made any
tool call of its own.

### @LETTER@3. Resident stays
Launch with resident true. Its done leaves the runner up.

### @LETTER@4. A hub-launched work card exits
POST /v1/launch with lifetime work and no spawning session, then a done report. The runner exits. Without a lifetime,
launched by a person from the board, it stays.
