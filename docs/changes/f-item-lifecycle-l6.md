Stage L6 of `docs/rnd/item-lifecycle-design.md`: the items board. The mockups in `docs/rnd/img/item-lifecycle/` are the
spec, and the built views sit beside them as `list-built-1400.png`, `detail-built-1400.png`, `blocked-built-1400.png`
and the same at 2400.

Before (the old tab, a flat list with a fold per state): `docs/rnd/img/item-lifecycle/items-tab-before.png`.
After: `docs/rnd/img/item-lifecycle/list-built-1400.png`.

`js/items-data.js` is the one seam. It reads `/_hub/items` as it is today (a row with no steps shows one honest step,
`building`), and takes a row that carries `steps`, `ask`, tokens and cost as it is. Wiring the L2 to L5 routes is an edit
to that file only. `?itemsmock=1` answers from `js/items-mock.js` instead, for that load, with a corner note.

## Test plan

## @LETTER@. The items board

### @LETTER@1. List

1. Open the board on a hub with `?itemsmock=1` and open the Items tab.

**Expected:** four stat cards, filters, groups blocked, running, queued, finished, each row with an eight segment bar.
Blocked rows are ordered oldest question first. Finished is open.

### @LETTER@2. Answer on the row

1. Click `answer` on a blocked row, type, click `send`.

**Expected:** the row stops being blocked. Without `?itemsmock=1` the answer posts to the hub as `board`.

### @LETTER@3. Detail

1. Click a row. Click `Items` in the crumb to return.

**Expected:** eight step cards, the question box when blocked, the side column with `estimated` on the total, and
`cancel item` behind a confirm.

### @LETTER@4. Skin

1. Change the skin in settings while the tab is open.

**Expected:** the tab repaints. `items.css` holds no raw colour.
