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

## Re-read d33bd3b5..466cf478 (merge of claude/main 566ea29c, fix 466cf478): OK hub and room

The merge is clean: `range-diff` shows 1c31d0ea `=`. The fix commit is unsigned (sg3 has no key), noted.

- **Medium closed.** `listUnits()` now exits 1 with the harness's stderr when the JSON line is there and the exit is
  not zero. Proven with proof-466cf478.ps1 (beside land-review.ps1) in a scratch worktree at 466cf478: a clean tip
  lists with exit 0, a bare `pasteStartSection` after the liveHome unit makes `sharded --list --local` exit 1 with
  "sections no unit owns: pasteStartSection".
- **Nit 1 closed.** The regex is `^\s*await \w+Section\(`. The same bare call inside an `if` block at 8 spaces also
  fails `--list` (proven, same script). It now reads the whole file, not only main(), so a section awaited inside a
  helper would also count as bare. None does today: the clean tip lists clean.
- **Nit 2 closed** by the merge.

### LOW (new, proven): check-suite-units.js fails everywhere except on the suite room

`check-suite-units.js` runs `test-board-sharded.js --list` without `--local`. Since 2dd365ba `--list` dispatches
to sg3 (`dispatchRemote`, test-board-sharded.js:187), and `BOARD_HEADLESS_FILE` does not travel there, so sg3 lists
the real harness, exits 0, and the check reports "a bare section in main() did not fail --list" (exit 1). From my
worktree at 466cf478 (remotes claudevm, m1mini, origin, sg3): as shipped, exit 1. With `ATRIUM_SUITE_LOCAL=1`,
"check-suite-units ok", exit 0. On sg3 itself it passes, which is why @ui saw ok. It fails safe (never a false pass)
and nothing calls it yet, so it does not hold. Fix: pass `--local` in `list()`. The probe file is removed either way.

### NIT

- With the bare call planted, `--list` also prints "FAIL: the headless run threw: Cannot read properties of null
  (reading 'newContext')" before the guard's line. It reads like a crash. The exit and the guard message are right.

Verdict: OK hub and room d33bd3b5..466cf478. Test tooling, nothing served. I ran the list paths only, not the suite.

Quality: after the Sonnet switch, the fold is complete and came with its own proof. The miss is the same shape as
round 1: the proof was run only on the machine where the dispatch stays local.

## Follow-up 52bbefff (466cf478..52bbefff, merge b01c4f70 of claude/main, then the fix): OK hub and room

- **Low closed.** `check-suite-units.js` lists with `--local`. Proven with proof-52bbefff.ps1 (beside
  land-review.ps1) from my worktree, `ATRIUM_SUITE_LOCAL` unset: "check-suite-units ok", exit 0.
- **Nit closed.** List mode no longer prints "the headless run threw" for a planted bare call.

### LOW (proven, older than this patch, made quieter by it): a throw in list mode truncates the list, exit 0

Any throw inside main()'s `try` in list mode stops the `unit()` calls after it, and list mode returns before
`if (bad) process.exit(1)`, so the exit stays 0 unless the section guard also fires. Proven: a bare
`await browser.newPage();` (not a section, so the guard does not see it) planted before the oneTooltip unit gives
170 units instead of 175, harness exit 0, and `sharded --list --local` exit 0. A sharded run then never schedules
those 5 units and goes green. This was already so at 466cf478 (the FAIL line printed, the exit was still 0).
52bbefff removes that line, which was the only sign. Fix: in list mode, a throw sets `process.exitCode = 1` and
says where the listing stopped, whatever the guard finds. Not a hold: it needs a non-section browser call in
main(), and none is there today.

Verdict: OK hub and room d33bd3b5..52bbefff. Test tooling, nothing served. Unsigned (sg3), noted.
