## Test plan

## @LETTER@. The pulls page

Design: `docs/rnd/pulls-redesign.md` with `docs/rnd/pulls-overhaul.md`. Pictures: `docs/changes/img/u-pulls-overhaul/`.
Headless: `HEADLESS_UNITS=bootClean,pulls,pullsPage,pullsPanelStage,prMove,pullsDrawer,pullsAbsent node
scripts/test-board-headless.js`, and `node scripts/check-pulls-closed.js`.

What was wrong and where it went: the row said failed while the work succeeded (a failed stage is now a red dot and a red
note, the pill says `failed at <stage>` and a ready review keeps its verdict), there was no verdict (a pill and finding
chips, the board's guess dashed until the panel writes `verdict`), no way to reach the card (Open card and Answer
buttons from the board's own task list) and every button weighed the same (one primary, the rest in `...`).

The daemon does not send `verdict`, `tests` or `stages` yet. The board reads them when present and otherwise derives the
verdict from the finding counts (any `high` is blocked, any finding is changes, else approve), draws no tests chip, and
derives the stage dots from the row's state and the stage named in its `run_error`. A review whose findings are all posted
or dismissed sits under In progress with Open card. The phone page is the board's own pulls tab at 390 wide: `/m` has no
pulls page.

### @LETTER@1. The page and its counters

1. Open the pulls tab with reviews in several states.

**Expected:** a header with Open and All counts, a repo select, `+ Paste a PR` and a `...` menu. Under it four counters
(need you, ready to post, reviewing, failed). Cards sit in groups in that order, newest first, and an empty group is not
drawn. Clicking a counter shows only that group and clicking it again shows everything.

### @LETTER@2. A card

1. Look at a blocked review, an approve review, a queued one and a failed one.

**Expected:** a 4px bar on the left (red, teal, grey, dim red), an uppercase tinted pill, chips (blockers red, med amber,
tests green or red), four stage dots joined by lines and one primary button bottom right: Post review, Start review,
Retry. A review that is still being walked says Continue review, and a finished one says Open card. A card waiting on a
question says Answer in orange and shows the question. `...` holds Abort the run, Show the run log, Move review and card
to another room (more than one room only), Close card and free its worktree, branch and review, Open on GitHub and Copy
PR link.

### @LETTER@3. Paste, empty and loading

1. Press `+ Paste a PR`, paste a link, press Open.
2. Close every review, then reload.
3. Reload and watch the page before the list arrives.

**Expected:** the popover takes the link and an optional why, opens the card, and closes. An empty page is a dashed panel
reading Nothing under review with the same button, and the last closed reviews under it. Loading is three skeleton cards
and an ellipsis in the counts.

### @LETTER@4. Phone

1. Open the board at 390 wide and go to pulls.

**Expected:** one column, the counters in two lines, meta bottom left and the primary and `...` bottom right at a touch
size, and `...` opens a sheet at the foot of the screen.
