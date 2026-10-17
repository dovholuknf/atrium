Docs: none, a fix. Nothing a reader can name changed.

## What and why

Run 39042049947, ubuntu board: `groupBridge` failed in the shard with `getComputedStyle ... parameter 1 is not of
type 'Element'` and again alone with `the row is not drawn`. The handoff counted it the fourth run with that red.
Under six CPU burners in a Linux container the unit failed 2 of 15 times, and a trace of every call to `switchView`,
`renderTermList` and the pane teardown showed the row's `on` class going at the moment the boot's `restoreWhereYouWere`
called `switchView("terms")`, half a second to a second after load.

`switchView` marked the view's tab by toggling `on` on every element with class `tab`. The terminals list's rows are
`.card.tab`, so the switch took the attached row's mark off too. The repaint that follows did not put it back:
`setHTML` skips a paint whose markup is the string it last painted, and the markup had not changed. The mark came back
only on the next change to the list. In the test the restore landed between two reads of the attached row, and the
second found none. On the board, a view switch while the bridge spans a row leaves the bridge with no row to span
until something else changes.

The switch and the click wiring now address `.tab[data-view]`, the nav's tabs, and `revealChips` reads the view the
same way. `groupBridge` switches to the terminals view right after attaching and checks the row still carries its
mark, which fails deterministically on the old code and passes 15 of 15 under load in the container.

## Test plan

## @LETTER@. A view switch leaves the attached row marked

### @LETTER@1. Switch away and back

1. Attach a terminal, so a row in the terminals list is highlighted and the bridge spans it.
2. Click the board tab, then the terminals tab.

**Expected:** the row is highlighted and bridged as soon as the view is back, with no beat where the bridge spans
nothing.

### @LETTER@2. The unit

1. `HEADLESS_UNITS=groupBridge node scripts/test-board-headless.js`, under CPU load.

**Expected:** passes every time. Before the fix it failed 2 of 15 under six busy loops on Linux.
