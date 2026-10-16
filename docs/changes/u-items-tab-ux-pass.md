# Items tab UX pass

Built from `docs/ui/items-tab-ux-pass.md`, phases 1 to 3 (the board only). Phase 4, the hub closing dead assignments, is
not built: it needs the answer to open question 2 in the design.

- `js/items-data.js`: `foldItems` makes one row per backlog id from the hub's one assignment per card. `itemNeedsYou` is
  the one answer to "does this need clint", read by the header chip, the group and the tab badge. A blocked assignment
  with no question is no ask, and the string "(no reason given)" is gone.
- `js/items.js`, `css/items.css`: groups needs you, idle, working, queued, finished. A step line, a legend, a distinct
  current segment, count chips, a maximum width and two columns from 1700px. The phone page (`m/js/items.js`) gets one
  row per item and the same needs-you test, and keeps its four groups.
- Not shown on the list any more: tokens, dollars, an age column, priority and dept tags, the sort pill, empty spend and
  landed cards. The detail keeps them. Relaunch is drawn disabled until its behaviour is decided.
- Proof: `scripts/test-items-fold.js` (pure row logic), headless `itemsUx`, and the real shots in
  `docs/ui/img/items-tab-ux-pass/` at 2338 and 1280 wide.

## Test plan

## @LETTER@. The Items tab, one row per item

### @LETTER@1. Folded rows and the question

1. On a hub with several cards on one backlog item, open the Items tab.

**Expected:** one row per backlog id. Its cards sit under a "1 live, 3 ended" line. A blocked item with no question
shows no ask, and the "needs you" chip equals the rows under "needs you".

### @LETTER@2. Steps, idle and wide layout

1. End the card of an item that is at building and wait for the next poll.
2. Open the tab at 2338px and at 1280px.

**Expected:** the item moves to "idle" with "cancel item" and a disabled "relaunch". Each row reads "step N of 8 ·
<step>". At 2338px there are two columns with needs you and idle on the left, at 1280px one column, and no sideways
scroll.
