# Review: child-fold 87d8aed6 (@ui)

Branch claude/child-fold, one feature commit, 87d8aed6. @ui sent `34f2648b..87d8aed6`, which also holds 13 commits
already on claude/main (pulls-view, live412, my reviews), so the verdict range is `87d8aed6~1..87d8aed6`. Board
only: js/terminal-list.js, js/sharing.js, css/sharing.css, plus the headless `childFold` section and
scripts/test-term-nesting.js. Read against docs/changes/child-fold.md.

## What holds

- **The chevron** is a real `<button>` with `aria-expanded`, drawn only on a row with children, and its click stops
  propagation, so pressing it never opens the parent's terminal.
- **A waiting child is never hidden.** A folded parent still draws every child for which `termKidWaits` is true, and
  that walks grandchildren too, so a question two levels down stays findable. `termNest` already refuses lineage
  loops, so the recursion ends. The count shows only the children actually hidden.
- **Remembered and shared.** The fold is an entry `kids:<bare id>` in `atrium.folded`, so a room-set change that
  retags ids keeps it. A new `storage` listener repaints the list in every other window. `foldedColumns` now refuses
  a stored value that is not an array, which is what the "bogus" headless case checks, and which also protects the
  board's own column folds.
- **No key collision.** The other entries in `atrium.folded` are column ids, `group:` and `offline:`, and none
  starts with `kids:`. The board's `toggle` handler only reads `data-fold`, which the chevron does not carry.
- The headless section covers expanded by default, no chevron on a childless card, fold, folded count, reload,
  second window, every sort, and bogus storage.

## Findings

### 1. LOW: the fold lives in the board's list, not the terminal list's

The terminal list's own group folds are in `atrium.termfolded` (`termFolded`), and the comment says "the same list as
the group folds", but this fold uses `atrium.folded`, the BOARD's. It works. The cost is that every board group
toggled in one window now repaints the terminal list in every other window, since the new listener fires on
`atrium.folded`. `atrium.termfolded` is the natural home, and the listener would then fire only for terminal-list
folds. Or fix the comment to say which list.

### 2. NIT: entries for gone parents are never pruned

A `kids:` entry outlives its card. Each one is a few dozen bytes, so this only matters after months. The board's
column entries behave the same way.

## Verdict

OK, hub-ok and room-ok 87d8aed6~1..87d8aed6. One low, one nit, no hold.

Quality: after the Sonnet switch, no drop seen. The waiting-child rule covers grandchildren, which the spec did not
ask for and is right. The bogus-storage hardening protects code the change did not have to touch.
