# headless-green

The headless board suite (`node scripts/test-board-headless.js`) was red on claude/main and aborted partway, so
nothing after the abort ran. Each failure below says whether the board or the test was wrong.

## What was wrong

| failure | verdict | why |
|---|---|---|
| run aborts at `wrote()` waiting for the fixture PUT | board | `js/dragclick.js` measured every click from the last mouse-down. A key press on a focused button clicks with no pointer position, so it looked like a long drag and was swallowed. Keyboard clicks (`detail === 0`) now skip the drag test. |
| gear has no terminal list section, sort or hide did not act | board | Same cause. The test opens the gear with `gear.click()`, a click with no pointer, and the guard ate it, so the settings dialog never opened. |
| notice link did not open the settings cog | board | Same cause. `openFloorSetting` calls `gear.click()`. |
| terminated pinned terminal's menu offers no dismiss | board | The card-menu rewrite dropped `dismiss`. The interview cuts items from the menu but never asks to remove the way to take a dead pinned card out of the list, and `terminate and clean up` is the only other item that does it, which is far heavier. Restored for a pinned card with no runner, above terminate and clean up. |
| hide control has 3 segments, not 2 | test | `show stopped` joined agents and subagents on purpose. The interview (Q3) asks that stopped cards stay out of the list unless "show stopped" is toggled. The test now expects three. |
| gear controls do not show the defaults | test | Same change. The gear's hide row now reads `agents`, `subagents (2)*`, `show stopped`. |

## Test plan

## @LETTER@. Keyboard and programmatic clicks after a mouse click

### @LETTER@1. Space on a switch

1. Click anywhere on the board with the mouse.
2. On the runners page, Tab to a fixture's on/off switch and press Space.

**Expected:** the switch flips and the change is saved.

### @LETTER@2. A dead pinned terminal

1. Pin a terminal, let its runner exit, and right-click it in the terminals tab.

**Expected:** the menu offers dismiss. Dismiss unpins it and the row leaves the list.

### @LETTER@3. Selecting text

1. Drag to select text on a card.

**Expected:** no click action fires.
