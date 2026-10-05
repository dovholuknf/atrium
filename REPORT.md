# u-new-growler-stack-fold report

## What changed
- `internal/api/web/js/growl.js`: the dashed row under the growler is now "next: <title>" and brings that growler up
  as the full one, going round the set (kept by id in `growlFront`). A second row "show all N: counts" opens the
  stacked list, only when more than one other growler waits. The list closes with "show less", and so does the phone
  strip. "fold" is gone from the stack and the growler setting is untouched.
- `internal/cli/turnquestions.go`: only top-level numbers of the Open Questions block count. A numbered line indented
  deeper than the first item is read as part of the item above it (before, three spaces of indent were accepted for
  every number, so sub-items counted).
- `scripts/test-board-headless.js`: `growlStack` and `growlPopout` updated for the new labels, with new checks for
  "next" going round, "show all", "show less", no "fold" or "+N more" text, and two growlers drawing "next" only.
  `STACKFOLD_SHOTS=dir` writes PNGs of the growler.
- `internal/cli/turnquestions_test.go`: sub-items case, and `TestOpenQuestionsCountSixNotTen` (6 questions with 4
  numbered sub-items and numbered lines outside the block counts 6).
- Docs: design note in `docs/backlog/ui/u-new-growler-stack-fold.md`, changelog
  `changelog/ui/2026-10-04-u-new-growler-stack-fold.md`.

## Design notes
In the item file under "## Design note". Short form: kept the advance and labelled it "next" (the full growler already
has "open"), kept the stacked view and closed it with "show less".

## Tests
- `go test ./internal/cli` (ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG cleared): ok.
- `go test ./internal/link`: one failure, `TestTheHubRaisesTheForgeAlertOnceAndEndsItOnSuccess`. I changed nothing in
  `internal/link`, so I did not chase it.
- `HEADLESS_ONLY=growlStack,growlActions,growlModal,growlQuiet,growlAttention,growlPhone,growlPopout,growlOff,growlRemind
  node scripts/test-board-headless.js` (NODE_PATH pointed at another worktree's node_modules, as this one has no
  playwright): all ok.
- The phone page `/m/` has its own growler (`m/js/growl.js`) and was not changed.

## PNGs (`docs/screens/u-new-growler-stack-fold/`)
Real growler, headless board, real code. Before is the old growl.js run through the same fixtures.
- `before-plus-one-row.png`, `after-plus-one-row.png`: the "+1 more" row and its replacement.
- `before-stacked-collapsed.png`, `after-stacked-collapsed.png`: the stacked view collapsed.
- `before-stacked-open.png`, `after-stacked-open.png`: open, with "fold" and with "show less".
