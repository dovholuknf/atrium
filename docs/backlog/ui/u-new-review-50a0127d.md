# Review: u-m-changes-real 50a0127d (/m changes sheet against a real /changes answer, @ui)

Range `m1mini/claude/u-m-changes-real` 50a0127d~1..50a0127d, off claude/main 8c20e14c. One product change in
`internal/api/web/m/js/changes.js` (8 lines), the mChangesReal section in the headless suite, and 4 PNGs.

## What holds

- A file with no hunks now says why: an empty added file, an empty deleted file, a rename with no content change, or
  otherwise a change of mode only. The statuses match the ones `internal/daemon/changes.go` sends (`added`,
  `deleted`, `renamed`, `modified`, `binary`).
- The new wording cannot hide a cut. `binary` returns earlier, and so does any file with `hunks_cut` or with line
  counts but no hunks ("too large to show here"). So the branch is reached only by a file that has no lines to show.
  A `modified` file with zero counts and no hunks is a mode-only change, which is what the last case says.
- Every string is fixed text set through `note()`, so no answer text reaches markup.

No findings.

Read, not run: board sections are @ui's.

Quality: after the Sonnet switch. Testing against a live answer found a real gap, and the fix keeps the cut checks
ahead of it. No drop.

HUB DEPLOY OK and ROOM DEPLOY OK 50a0127d~1..50a0127d.
