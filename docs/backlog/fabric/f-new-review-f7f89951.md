# Review: f u-suite-shard f7f89951 (the headless board suite, sharded)

Range `bdbe44eb..f7f89951`, one commit, branch claude/u-suite-shard. Item docs/backlog/ui/u-new-board-suite-faster.md
part 1, brief D:/tmp/fabric/shard-brief.md. Test tooling only: scripts/test-board-sharded.js (new), the unit
wrappers in scripts/test-board-headless.js, the weight and flaky JSON, an opt-in branch in scripts/check-board.sh, the
test plan section HV and a changelog entry. Nothing the board serves changes.

**Verdict: HOLD `bdbe44eb..f7f89951`.** One medium: the runner passes a run in which a unit off the flaky list
failed. Everything else is sound.

## What was checked

- The plain serial run is unchanged. With no `HEADLESS_UNITS`, `unit()` runs every block in the old order, rethrows
  a throw so one throw still ends the run, and never resets the mock. The shared page is made lazily by `needPage()`,
  which only `core` and `core2` call, and `gauto` and `switches` open their own contexts, so they do not need it.
  `HEADLESS_ONLY` returns before any of this.
- The unit list matches the old call list one for one, in the same order, with `termSortStarted` last.
- The pin group `core`, `history`, `core2` is right: core and core2 drive main's one page, and the page-error check
  on it sits inside core2. The planner keeps a group on one shard in suite order, `--units` pulls in a whole group,
  and a retry reruns the whole group.
- The reset between units in a shard reassigns every module-level `let` from a `structuredClone` taken before the
  first unit. Every one of them is plain data (no sockets or functions), so the clone cannot throw. `--list` checks
  the list against the file's `let` lines.
- `fail()` records against `currentUnit`, a unit's verdict is `bad === before`, and a shard writes its results before
  `process.exit(1)`.
- A shard that dies before it reports turns its units into NOT RUN, which fails the run. Playwright not installed
  still exits 0 through `listUnits`, as check-board.sh treats it.
- check-board.sh gains `HEADLESS_SHARDED=1` and keeps the serial run as the default.

## Medium

1. **A unit that is not on the flaky list, fails, and passes its retry does not fail the run.** `LOAD (passed on
   retry)` is left out of `hard`, so the exit code is 0 (test-board-sharded.js, the `hard` filter in main, and
   docs/test-plan.md HV step 4 says so on purpose). The brief asks for "the exit code nonzero if any non-flaky section
   failed". The flaky list exists so that a pass on a second try is a decision somebody wrote down with a reason. A new
   race in a new section is exactly what fails once under load and passes alone, and with this rule it lands green on
   the post-landing whole run, which is now the only whole run there is. Fix: put `LOAD` in `hard`, keep its own block
   with "fix the wait or list it", and change HV step 4 and the file's header comment to match. Ask the orchestrator
   if the softer rule was meant. As written it is not what the brief says.

## Lows

1. **A shard's exit code is ignored once it has written results.** A `fail()` that lands while no unit is running
   (a page's late `pageerror` listener firing after its unit returned, so `currentUnit` is `""`) counts in the shard's
   `bad` and exits 1, but is in no unit, so the merged report says PASS. The results file already carries `bad`: when
   `r.code !== 0` or `bad` exceeds the sum of the unit failures, report the shard as a hard failure "outside any unit".
   The same late listener firing while the NEXT unit runs blames the wrong unit, which the report cannot catch, so a
   note in the header is enough there.
2. **`--no-retry` lets a listed flaky unit's one failure pass** (`FLAKY (failed, not retried)` is soft). The rule is
   that a flaky unit fails the run when it fails every try. With no retry its one try is every try, so it should be
   hard.
3. **The reset covers the `let`s and not the module-level objects that are mutated in place**: `qDismissed`, `LAND`,
   `walkDirs`, `gateCalls`, `HON`/`FON`/`HOFF`/`FOFF`. Every section that reads one sets it first today
   (`restartGate` zeroes `gateCalls`, the dismiss sections clear `qDismissed`, `resetSwitches` exists), so nothing
   fails. But the comment's "every module-level `let` ... A new `let` of this kind goes in here too" reads as the whole
   mock state, and `HEADLESS_LEAKS` cannot see these. Say in the comment that a mutated `const` is not reset and its
   section must set it.

## Note for @ui

The worker reports `phoneListFit` and `termDebug` failing alone at the base, and `boardDocs` is listed flaky with a
test and panel disagreement about back from a document. All three are @ui's sections. termDebug is the details debug
drawer that landed today (26ffba86).

Quality: after the Sonnet switch, no drop seen. The harness work is careful (the lazy page, the reset, the listing
check, the pin reasoning), the flaky reasons are diagnosed rather than guessed, and the one miss is the second-order
gate rule, which matches the pattern noted before.

## Re-read 7c735647..74f1bc16: OK hub and room

49522c29 is f7f89951 rebased onto 7c735647 (`git range-diff` shows `=`). 74f1bc16 is the fix.

- **Medium, fixed.** `LOAD (passed on retry)` is in `hard`, so the run exits 1. The block still prints, now saying it
  fails the run, and `failed_` keeps LOAD rows out of the FAILED list so they are not shown twice. test-plan.md step 4
  and the runner header say the same.
- **Low 1, fixed.** `strays()` compares the shard's `bad` (written by `writeUnitResults`, line 17516) with the sum of
  unit failures, and flags a nonzero exit with `bad` zero. It runs on the first shards and on the retries. A shard
  that throws in `finally` before writing results has `wrote` false, so its units read NOT RUN, which was already
  hard.
- **Low 2, fixed.** `FLAKY (failed, not retried)` is hard and labelled in the FAILED list. An unlisted failure under
  `--no-retry` was already `FAIL`.
- **Low 3, fixed.** The comment names the mutated consts.

Nit: the new comment line in test-board-headless.js (~17478) runs past 120 characters, and it names `HON and FON`
but not `HOFF`/`FOFF`.

Quality: after the Sonnet switch, no drop seen. Each finding is fixed at the point named, with nothing extra.
