## Test plan

## @LETTER@. The backlog tab's view bar

### @LETTER@1. Default view

1. Open the backlog tab on a hub with items in several departments and statuses.

**Expected:** the bar's summary reads `open + in-progress N`, and items sit under department headings with counts.
Done and dropped items are not listed.

### @LETTER@2. Drawer chips

1. Open the drawer. Click `all`, then `held`, then a PRIORITY chip and a DEPT chip.
2. Click SORT BY `id`, then a column header, then GROUP `by status` and `off`.

**Expected:** the list, the chip counts and the `N of M` match count follow every click. The header and the SORT BY chips
stay in step. Reload and the choices are still there.

### @LETTER@3. Search

1. Type a word from a title, then `#runtime`, then a word and `#ui` together.

**Expected:** only items matching every word and the department show. Clearing the box restores the view.

### @LETTER@4. Phone width

1. Repeat 1 and 2 at about 390px.

**Expected:** the bar and drawer fit without sideways scrolling, and the list stays readable.

Known limit: the hub's list omits item bodies, so search does not match body text until the hub sends it.

Screenshots: `docs/changes/img/f-backlog-tab-view-bar/` (before and after, wide and phone, drawer open).
