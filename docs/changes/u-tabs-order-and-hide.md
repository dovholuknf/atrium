# u-tabs-order-and-hide

The top tabs have an order and a place, both chosen by the user and kept in this browser.

- **Control:** a list dialog, "arrange tabs". One row per tab with up and down buttons and a select: in the bar, in the
  `...` menu, or hidden. Opened from the gear ("top bar tabs") or the last entry of the `...` menu. Reset to default
  clears it. Drag in the bar was not chosen, since the bar is also squeezed by the overflow and a list is steadier.
- **Storage:** `atrium.tabs` in localStorage, `{ order: [...], place: { view: "menu" | "hidden" } }`. The default layout
  stores nothing. Another tab of the same browser follows through the `storage` event.
- **Hidden but waiting:** a hidden tab whose count is not zero (perms, for one) is not hidden. It is listed in the `...`
  menu, and the `...` button carries the sum of the counts of everything in the menu. When the count returns to zero the
  tab is hidden again.
- **Overflow:** the responsive overflow from the top nav change runs over the tabs left in the bar, in the custom order.
  Tabs placed in the menu are reserved the `...` button's width. The phone header ignores placement and keeps its
  scrolling nav, but follows the order.

Before and after: `img/u-tabs-order-and-hide-before.png`, `img/u-tabs-order-and-hide-after.png` (the dialog) and
`img/u-tabs-order-and-hide-after-bar.png` (the bar with the count on `...`).

## Test plan

## @LETTER@. The top tabs can be ordered, moved to the menu or hidden

### @LETTER@1. Reorder and persist

1. Open the gear, press "arrange tabs", and move usage up two places.
2. Close the dialog and reload the page.

**Expected:** usage sits two places earlier in the bar, before and after the reload.

### @LETTER@2. Menu and hidden

1. In the dialog set history to hidden and rooms to "in the ... menu".

**Expected:** history is in neither the bar nor the menu. Rooms is in the `...` menu, which now shows.

### @LETTER@3. A hidden tab that is waiting still tells you

1. Hide perms, then raise a permission request.

**Expected:** perms is listed in the `...` menu and the `...` button carries its count. Answer the request and perms
leaves the menu again.

### @LETTER@4. Narrow window with a custom order

1. With the layout above, narrow the window to 1024px.

**Expected:** no tab is cut, and the tabs that do not fit are in the `...` menu in your order.

### @LETTER@5. Reset

1. Press "reset to default" in the dialog.

**Expected:** the original order and every tab in the bar, and `atrium.tabs` is gone from localStorage.
