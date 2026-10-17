## What and why

The `changeReq` unit was one 259 line function that opened 16 pages and made about 60 assertions, 40 of them after a
click. It took 42 to 71 s and failed with `page.click: Timeout 30000ms exceeded` on windows in every one of the last ten
CI runs, so no run said which click or why.

With `#/hubrepos/requests/<tab>/<id>` in the address (`f-board-deep-links`), each part of the page can be loaded by its
address and read on `data-route="ready"`. `scripts/board-tests/changereq.js` holds 23 units, one thing each: the list,
behind, diverged, the sha rule, a refused record, a record, a merged request's history, an unseen branch, withdraw,
close, a read-only board, the four error states, the ledger's rows, opening a request, text-not-markup, a state word,
the mock, the mock off, repo names, the two 403s, one list read at a time, and the 390px layout. Each takes about a
second. `changeReq` stays in the harness for the two steps that measure what the page draws, the 4.5:1 contrast check and
the screenshots, and stays on `scripts/board-visual-units.txt`.

Splitting found a product bug. After a refused Open request, `crWrite` painted the error while `cr.busy` was still true
and cleared `busy` without painting again, so the button stayed disabled until some other repaint. The old test passed
when a poll repainted in time. `crWrite` now repaints once `busy` clears.

## Test plan

## @LETTER@. The change request units

### @LETTER@1. Alone

1. `node scripts/board-tests/changereq.js`.

**Expected:** 23 units pass, each in about a second.

### @LETTER@2. A refused open can be retried

1. Open the repos page on a hub, Requests, and ask to merge a branch with a name the hub does not have.
2. Read the error, correct the branch, and press Open request.

**Expected:** the button is enabled after the error and the second press opens the request.
