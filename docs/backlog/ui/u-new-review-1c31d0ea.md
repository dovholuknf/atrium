# Review: suite-units 1c31d0ea (@ui)

Range d33bd3b5..1c31d0ea, one commit on sg3/claude/suite-units. Test harness only: scripts/test-board-headless.js,
scripts/board-suite-weights.json and a changelog line. Nothing served. Unsigned (sg3 has no key), noted as asked.

## What holds

- **The 66 bare calls removed from main().** I checked each against the tip. Every one is owned by an
  `await unit(name, () => xSection(...))` call, and so is every `async function xSection` except the four in
  `notUnits`. Those four are called from inside other sections (each name appears three times). No unit name is
  registered twice. No bare call is left at main()'s indent.
- **The four new units.** noReadyChildren, childUnderParent, childFold and liveHome are registered, with weights.
  These four were the only bare calls that had no unit, so before this change they ran outside every unit in every
  shard.
- **growlStable.** `phone` is the loop variable of the section's `for (const phone of [false, true])`, so the hover
  check is now desktop only. The comment gives the reason: a touch context keeps no mouse hover.
- **cardUrlWayOut.** It now also waits for `document.body`, which closes the race after navigation.

## Findings

### MEDIUM: the guard's exit 1 never reaches the run it guards (proven)

The guard sets `process.exitCode = 1` in the harness's list mode. The only caller of list mode is `listUnits()` in
scripts/test-board-sharded.js. That function takes the last line that starts with `{`, parses it, and reads
`r.status` only when no such line exists. It also never prints the harness's stderr when the JSON is there. So a
bare or unowned section passes `--list` and the whole sharded run with no word, and the bare call then runs in every
shard. That is the hazard the guard was written for.

Proof: D:/worktrees/claude/reviews/github-dovholuknf-atrium/proof-1c31d0ea.ps1 adds one bare
`await pasteStartSection(browser, base);` after the liveHome unit in a detached worktree at 1c31d0ea:

- `HEADLESS_LIST=1 node scripts/test-board-headless.js`: `sections no unit owns: pasteStartSection ...`, exit 1.
- `node scripts/test-board-sharded.js --list`: the shard plan, exit 0, and no guard message.

Fix: in `listUnits`, when `r.status !== 0`, print `r.stderr` and exit 1 even when a JSON line is present. A run of
the sharded suite would then refuse before it starts.

### NITS

- The bare-call regex matches only `^ {4}await \w+Section\(`. A bare call inside a block in main() (6 spaces) gets
  past it. Matching any indent between main()'s start and its `catch` would close that.
- The branch is based on d33bd3b5. claude/main has moved, and test-board-sharded.js changed there (8a408fe2, sg3
  dispatch). The fix above touches the same file, so rebase before folding it.

## Verdict

HOLD d33bd3b5..1c31d0ea on the medium. Re-read d33bd3b5..tip. Harness only and nothing served, so the verdict will
be hub-ok and room-ok as for test tooling. Board checks are @ui's: I read the cases and did not run the suite.

Quality: after the Sonnet switch, the cleanup is careful and complete: every removed call is accounted for. The miss
is the second-order one. The guard was tested where it is raised and not where it is read.
