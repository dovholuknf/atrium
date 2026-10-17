Docs: none, a test fix.

## What and why

Runs 39052523653 and 39054604921, macos board-1 and board-2: the board step failed after every headless unit passed,
on `scripts/check-board-drawer.js`: `terms 3: it hangs under the line` and `board 5` or `stack 5: the same click
with the drawer shut does open it (fetched: nothing)`, at 1280px in one run and 390px in the next. The CI report
lists units from the sharded run, so these never reached failing.tsv and the jobs read as red with nothing named.

Step 3 clicks the filter line and reads the drawer's geometry at once. The drawer slides open, so on a slow frame the
panel is read mid-slide, above the line's bottom edge. Step 5 clicks a card with the drawer shut, waits a fixed
300 ms and expects the card's read or its pane. On a loaded runner the read had not left in 300 ms.

Both steps now wait on the state they assert, up to three seconds at 50 ms, and answer at once when it holds. No
timeout was widened: the fixed pause is gone and the bound is the same order as the unit waits around it.

## Test plan

## @LETTER@. The drawer check holds under load

### @LETTER@1. Run it busy

1. Start CPU burners.
2. `node scripts/check-board-drawer.js`.

**Expected:** `the filter drawer holds.` Steps 3 and 5 pass at 1280px and 390px on every tab.
