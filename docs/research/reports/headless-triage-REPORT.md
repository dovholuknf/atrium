# Headless triage: main vs deployed build

main = df2a1d613 (hub/claude/main at start; later commits touch no web or scripts files).
live = ce4d5b74. Each unit run alone, 3 times per commit, each commit using its own
copy of scripts/test-board-headless.js (HEADLESS_ONLY=<unit>). Playwright 1.63 with cached chromium 1243.

| unit           | main passes/3 | live passes/3 | verdict      | suspect commit |
|----------------|---------------|---------------|--------------|----------------|
| u001Audit      | 0/3 (R1, R2)  | 0/3 (R1, R2)  | pre-existing | none           |
| termLastRead   | 0/3           | 0/3           | pre-existing | none           |
| hubReposLedger | 3/3           | 0/3           | not a fail on main | none     |
| switchBackCost | 0/3           | 0/3           | harness      | none           |

Notes:
- u001Audit: identical output on both. R1 (? chip tap dismisses the questions) and R2 (approve top 476 bottom 520 H 506)
  fail the same way on the deployed build.
- termLastRead: registered on both commits, and LR_OFF = true in js/lastread.js on both (the main() comment about it is
  on both too). Fails identically on both: 3 failures per run (replay lost the divider, focus while the board tab is up
  spent the restore, alt-tab return lost the divider), not 2. Same on live, so the deploy does not change it.
- hubReposLedger: passes on main alone, 3/3. On live it fails 3/3 (branch name not cut with tooltip: null).
  This is the opposite of a regression: the Ledger view came after live, in 3ef5ac0fb (Repos tab: Shelf, Ledger and
  Feed views). If it failed on main in the full serial run but passes alone, that is run-order or state leakage in the
  suite, not the product.
- switchBackCost: throws "path argument must be of type string ... undefined" at the readFileSync(process.env.REPLAY)
  line, on both commits. REPLAY is not set; the unit is marked NOT IN THE DEFAULT RUN. A harness input, not a product
  failure. I did not supply a replay file, so the unit's real result is unknown.
- Nothing passes on live and fails on main, so no blame commit is named.

DEPLOY OK
