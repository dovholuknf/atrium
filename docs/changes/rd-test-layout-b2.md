Docs: none, test tooling only. The design is `docs/rnd/test-layout-design.md`, stage B2.

What moved: the units of ten small board pages left `scripts/test-board-headless.js` for one file each in
`scripts/board-tests/` (`chat`, `pulls`, `hubrepos`, `items`, `usage`, `rooms`, `settings`, `boot`, `deploy`, `launch`).
Each file exports its units and runs alone with `node scripts/board-tests/<bucket>.js`. The shared helpers are in
`scripts/lib/helpers.js`, `scripts/lib/results.js` and `scripts/lib/alone.js`. The harness still runs every unit, in the
same order, and no assertion changed and no unit was renamed. `scripts/test-buckets.json` names the moved files.

New: `scripts/board-tests/backlog.js` holds the first units for the backlog page.

The minion-hidden units (`boardMinionHidden`, `stackMinionHidden`, `phoneMinionHidden`) stay in the harness. The items
units `itemMinionChip`, `itemDocsLanded` and `itemOpenHiddenCard` live in `items.js`.

Pass counts, on `claude/main` at 9d749e57 with the move and the units added since it (`launchSetup`, `itemsUx`, `launchRepoUrls`, the phone and settings units and the rest) all bucketed: `check-test-buckets.js` counts 350 units in 64 buckets; the harness sequence has 353 `unit(` calls for 351 distinct names (`noResumeButton` and `trayHead` are called twice, which predates B2). Sharded (`test-board-sharded.js --local`): 349 of 352 units passed first time. `changeReq` passes now, so the earlier failure was the fixture of the time and is gone. `worn` and `clearKeepsPage` failed only in the shards and pass alone: `clearKeepsPage` reads the xterm buffer (`getLine(i)` then `translateToString`, which loads a cell) before the terminal has written, so under 16 shards the row is undefined and the page throws `reading 'loadCell'`. That unit never moved and the cause is timing, not the move. `mGrowlQuestion` fails alone too, and it is not B2's: it passes at cb6ef738~1 and fails at cb6ef738 (phone growlers keep their height), and its section never moved. The serial run does not fit in ten minutes on this machine (the units add up to 24m36s) and crashed the page in `peekEverywhere` under load, so serial was not used as the gate.

## Test plan

## @LETTER@. Board units by page

### @LETTER@1. The same units pass, serial and sharded

1. Run `FULL_SUITE=1 node scripts/test-board-headless.js`.
2. Run `FULL_SUITE=1 node scripts/test-board-sharded.js --local`.

**Expected:** both pass the units they passed before the change, plus the new backlog units.

### @LETTER@2. A bucket passes alone

1. Run `node scripts/board-tests/items.js`, then the same for each other file in `scripts/board-tests/`.

**Expected:** each run passes the units named in the file's header.
