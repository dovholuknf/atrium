Docs: none, a fix.

## What and why

Run 39052523653, ubuntu board-1: `history` failed in the shard with `a fresh history load did not go back to one
page` and `kept the old scroll`, and passed alone. The unit scrolls two pages in, then calls `renderHistory(false)`,
a fresh load, and waits for one page of rows at the top.

`renderHistory` keeps a sequence number and the newest call wins: an older answer is thrown away when it lands. The
board's poll repaints the history view live, `renderHistory(false, true)`, and a live repaint keeps as many pages as
are shown. On the loaded runner a live repaint landed while the fresh load's request was out, took the newer
sequence, and the fresh load was dropped when it answered. The live repaint then drew the two pages again and put the
scroll back. On the board that is a search typed, or the recap filter changed, that never takes while the poll is
busy.

A live repaint is now skipped while a fresh load is in flight: the fresh load is about to paint anyway, and the next
poll repaints live. The unit fires the live repaint on the same tick as the fresh load, which fails on the old code
with the two messages from the run and passes with the fix.

## Test plan

## @LETTER@. A search is not lost to a live repaint

### @LETTER@1. Type while the board is busy

1. Open the history view on a board with more than one page, press "more" once.
2. Type a search word.

**Expected:** the list shows one page of matches at the top. It never stays at two pages or keeps the old scroll.

### @LETTER@2. The unit

1. `HEADLESS_UNITS=core,history,core2 node scripts/test-board-headless.js`.

**Expected:** passes. On the old `renderHistory` the fresh-load check fails every time.
