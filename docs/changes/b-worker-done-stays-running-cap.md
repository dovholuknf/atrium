`keepScratchOpen` tagged every scratch launch `atrium:keep-open`, and `staysOpen` exempts that tag from exit-on-report
and from `atrium_cull`. So a finished scratch worker stayed running and idle, held a launch slot, and could be neither
exited nor reclaimed. The default tag is gone. A scratch card now exits on its final done report after the usual short
delay, and `atrium_cull` reclaims its BRIEF.md and directory (no branch to check). The card and the directory stay until
then, so a `wake: true` say still resumes an exited worker. A keep-open or investigation tag passed on purpose, a
resident, a blocked report and a done carrying an ask are not exited. The launch cap already skips cards in `done`.
Supersedes the default in `bug-scratch-card-exits-on-done`.

## Test plan

## @LETTER@. A finished scratch worker leaves

### @LETTER@1. Done exits it

1. Launch a worker with `atrium_launch_worker` and `scratch: true`. Confirm its card has no `atrium:keep-open` tag.
2. Have it call `atrium_done`, then wait a few seconds.

**Expected:** its runner is gone, the card is `done`, and a new launch is not refused by the cap.

### @LETTER@2. Wake resumes it

1. From the launcher, `atrium_say` the exited worker with `wake: true`.

**Expected:** the same card resumes and gets the message.

### @LETTER@3. Cull reclaims it

1. Call `atrium_cull` on the done scratch card.

**Expected:** its BRIEF.md and empty scratch directory are removed.

### @LETTER@4. Exceptions

1. Launch scratch workers with `resident: true`, with an explicit `atrium:keep-open` tag, and one that calls
   `atrium_blocked`.

**Expected:** all three keep running.
