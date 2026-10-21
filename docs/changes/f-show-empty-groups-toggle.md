## Test plan

## @LETTER@. Empty groups chip

### @LETTER@1. The chip on each tab

1. Open the Terminals tab with at least one card, group by project, and open the filter drawer.
2. Find "empty groups" in the SHOW row, then click it.
3. Repeat on the Stack and Board tabs.

**Expected:** on, groups with 0 cards are listed with their 0. Off, they are gone. Each tab keeps its own setting,
so turning it off on Terminals leaves Stack and Board as they were. Reload and the choice holds.

### @LETTER@2. A group emptying and filling live

1. With the chip off, move the last card out of a group.
2. Move a card into a group that was hidden.

**Expected:** the emptied group disappears, and the hidden group returns when the card lands in it. Pinned does not change.

### @LETTER@3. The SHOW row on a wide window

1. Widen the window until the SHOW row sits on the summary line.

**Expected:** the outline and top edge of every chip are whole, none is clipped.

Screenshots: `docs/changes/img/f-show-empty-groups-toggle/`, before and after.
