# mCompact flake: asserts send states it never waited for

Board unit mCompact failed in the shard and on its retry on macOS board-1 (runs 42070041145 and 42109272357).

## Cause (INFERRED)

- After the Bash activity arrived, the unit waited only for `Bash` in `#m-working`, then read the row and wanted
  `sent · running Bash`. `sentOf` in `card.js` says `sending` until the send settles, so a slow runner read
  `sending·running Bash`.
- The "sent line leaves" wait was a `until(...).catch(() => {})` poll of 5000ms that swallowed a timeout, so the
  next line asserted a state the wait had not reached.

## Fix

- The note wait is a `waitForFunction` on the `.mc-note` having no height, with `slow(5000)`.
- A `waitForFunction` for `.wk-sent` reading `sent` sits before the row is read.
- No timeout widened, no flat wait added.

## Proof

- INFERRED. Playwright is not installed on this room and `ssh claudevm` timed out, so the unit was not run.
