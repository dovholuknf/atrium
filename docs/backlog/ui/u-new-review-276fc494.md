# Review: u-repos-pass2 276fc494

Range `9731154f..276fc494`, 2 commits.

- `dde8f24b` adds the fixture.
- `276fc494` changes `js/hubrepos.js`, `css/hubrepos.css`, `index.html` (the switcher) and
  `scripts/test-board-headless.js`.

The repos tab gets three views (Shelf, which is the default, Ledger and Feed), a per-browser switcher and one shared
empty-state hero. There are no Go changes.

Verdict: **OK** for hub and room. Two Lows can follow.

## How it was checked

- I read the hubrepos.js diff, with every HTML sink in it, plus the switcher markup, the CSS class renames and the new
  headless units. Per the standing note, I read the board units and did not run them.
- `node --check` passes on hubrepos.js, the fixture and test-board-headless.js at the tip.
- I looked at two of the 24 screenshots: shelf full at 2000 on paper, and ledger full at 390 in dark.

## Points

- **Escaping.** Every value from `/_hub/git/repos` goes through `esc()`: owner, repo, host, branch name, room, card,
  sha, url, the times and the card title from `lastTasks`. That includes attributes (`title`, `data-copy`,
  `data-repo`, `datetime`).
  - The values that are not escaped are numbers (counts, `--s` px, bar widths) and class names from a hash or a fixed
    table.
  - There is no inline handler. Clicks go through `data-act` and `data-copy` on one delegated listener, which is the
    form REVIEWER-NOTES asks for.
  - This matters more once f-hub-receive lands, because then a card chooses the branch name. Its allowlist is narrow
    anyway.
- **Storage.**
  - Every `localStorage` read and write is in try/catch, with an in-memory fallback.
  - An unknown value falls back to Shelf.
  - The `storage` listener repaints for the key, or for a clear (`key === null`).
  - Nothing else is remembered.
- **The switcher.** It is a radiogroup with `aria-checked`, a roving `tabIndex`, arrows that wrap, and Home/End. Focus
  follows the choice. A focused `data-act` control keeps focus across a repaint.
- **Refresh.** The guard is kept, and the button is disabled while a fetch is out.
- **`.empty`.** The repos tab now uses `is-empty` and `st-*`. The global `.empty` in files.css, cards.css and notify.css
  is untouched, so the collision is gone without moving anyone else's style.

## Lows

- **L1: a branch with no room reads " pushed to".** f-hub-receive gives a branch an empty room and card in two cases: a
  branch the operator pushed, and one with no log row.
  - In the feed, `<b>' + esc(b.room) + '</b> pushed to` then reads as " pushed to", with a `?` avatar.
  - The shelf and ledger who-lines go blank.
  - The fixture has no such branch.
  - Say "the operator" (or "on the hub") when `room` is empty, and add one fixture branch for it.
- **L2: two clipped edges at 390.** In the 390 ledger, the horizontal repo strip cuts the second tile ("openzi…") at
  the edge, and the top nav runs off to the right ("us…"). If the strip is meant to scroll, a fade or a partial tile
  that reads as "more" would say so. The top nav is not this change.

Atrium-Verdict: hub-ok 9731154f..276fc494
Atrium-Verdict: room-ok 9731154f..276fc494
Quality: careful board work: consistent escaping, a radiogroup with full keyboard support, and storage that fails soft.
