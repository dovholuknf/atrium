Docs: none, test tooling only. The design is `docs/rnd/test-layout-design.md`, stage B2.

What moved: the units of ten small board pages left `scripts/test-board-headless.js` for one file each in
`scripts/board-tests/` (`chat`, `pulls`, `hubrepos`, `items`, `usage`, `rooms`, `settings`, `boot`, `deploy`, `launch`).
Each file exports its units and runs alone with `node scripts/board-tests/<bucket>.js`. The shared helpers are in
`scripts/lib/helpers.js`, `scripts/lib/results.js` and `scripts/lib/alone.js`. The harness still runs every unit, in the
same order, and no assertion changed and no unit was renamed. `scripts/test-buckets.json` names the moved files.

New: `scripts/board-tests/backlog.js` holds the first units for the backlog page.

The minion-hidden units (`boardMinionHidden`, `stackMinionHidden`, `phoneMinionHidden`) stay in the harness. The items
units `itemMinionChip`, `itemDocsLanded` and `itemOpenHiddenCard` live in `items.js`.

Pass counts, before and after: serial (test-board-headless.js, units that ran): 328 before, 334 after (333 in the full run, which lacked itemDocsLanded because the move had dropped its call; restored and run alone). Sharded (test-board-sharded.js --local): 331 units before, 337 after. New units, all in the backlog bucket: backlogAbsent, backlogRows, backlogSort, backlogPrios, backlogPriority, backlogFilters. itemDocsLanded, boardMinionHidden, stackMinionHidden and phoneMinionHidden pass in both. Failing in both before and after: changeReq. Flaky and passing alone: phoneNudge, clearKeepsPage (loadCell), bridge, resumeSpinner. Each bucket passes alone.

## Test plan

## @LETTER@. Board units by page

### @LETTER@1. The same units pass, serial and sharded

1. Run `FULL_SUITE=1 node scripts/test-board-headless.js`.
2. Run `FULL_SUITE=1 node scripts/test-board-sharded.js --local`.

**Expected:** both pass the units they passed before the change, plus the new backlog units.

### @LETTER@2. A bucket passes alone

1. Run `node scripts/board-tests/items.js`, then the same for each other file in `scripts/board-tests/`.

**Expected:** each run passes the units named in the file's header.
