## Test plan

## @LETTER@. The terminals tab card menu

Pictures: `docs/changes/card-menu/before.png`, `after.png` (running card) and `after-stopped.png` (stopped card).

### @LETTER@1. The menu order

1. Open the terminals tab and right-click a running card.

**Expected:** restart, new context, a divider, settings, a divider, stop, terminate and clean up (orange). Each item
except settings has a (?) that shows its help. No item has a grey line under it.

### @LETTER@2. Stopped cards

1. Right-click a running card and pick stop.
2. Turn on the show stopped pill in the list controls.
3. Right-click the stopped card.

**Expected:** after step 1 the card leaves the list with no question. After step 2 it is back. In step 3 the menu
offers resume, and stop is not offered.

### @LETTER@3. Groups

1. With custom grouping on and named groups, right-click a card.

**Expected:** add to group and remove from group sit after new context. Remove from group only shows for a card
that is in a group. The board's own menu is unchanged.

### @LETTER@4. Terminate and clean up

1. Pick terminate and clean up on a card whose folder is clean.
2. Pick it on a card whose folder holds changes that exist nowhere else.

**Expected:** step 1 closes with no question. Step 2 asks, with a "never show me this again" tick. Ticking it makes
the next such card close without asking.

### @LETTER@5. Alias and rename

1. Click a card's title in the list.

**Expected:** the alias dialog opens.

### @LETTER@6. Phone

1. On a phone width, long press a card.

**Expected:** the same menu in the same order, as a sheet. Tapping a (?) shows its help.
