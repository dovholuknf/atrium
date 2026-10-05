# u-new-nest-indicator

Folded parent rows now show a count chip, a running chip (spinner) and a needs-you chip, replacing the grey `▸ 3`.

## Changed
- `internal/api/web/js/terminal-list.js`: `termKidsToggle` draws the chips, new `termKidsCount` counts descendants.
- `internal/api/web/css/sharing.css`: chip styles for the folded button.
- `scripts/test-board-headless.js`: childFold asserts count 3, the waiting chip and the running chip, serves
  `/working.gif`, and `CHILDFOLD_SHOTS=<dir>` writes the PNG. `scripts/test-term-nesting.js` lifts `termKidsCount`.
- Changelog, item file design note.

## Design note
See the item file. Count is now the total under the row (was hidden only). Running and waiting count at any depth.
Cost: the parent name truncates sooner on a narrow strip when all three chips show.

## Review round
- Count chip is bold in the head colour, no longer grey. The running chip has a stronger teal fill and border and a
  12px spinner. Button padding and gaps are tighter, so the parent name shows in full in the shot ("par (par)").
- Retaken `after-folded.png` (same fixture). childFold, childUnderParent and ctxLine headless sections pass.

## Tests
`NODE_PATH=<atrium>/node_modules HEADLESS_ONLY=childFold,childUnderParent,ctxLine node scripts/test-board-headless.js`
passes. The same childFold test fails on the old code (count, waiting, running), as it should.
`node scripts/test-term-nesting.js` fails with `ctxLine is not defined`, and does so on the base commit too, so it is
not from this change. I did not run the full headless suite.

## PNGs
`docs/screens/u-new-nest-indicator/before-folded.png` (old code, same fixture) and `after-folded.png`.
