## Test plan

## @LETTER@. The idle park timer is on the room's settings cog

Pictures: `docs/changes/img/idle-park-cog-before.png` and `docs/changes/img/idle-park-cog-after.png`.

### @LETTER@1. The control shows the room's answer

1. Open the board on a hub and click the cog on a room's row.
2. Scroll to the end of the pane, under "after atrium goes away mid-turn".

**Expected:** a "park a card that sits idle" row with a checkbox and a number of minutes. A room on the default shows
120. The line under it names the default (120 minutes) and the floor (30).

### @LETTER@2. Set minutes

1. Change the minutes to 45 and leave the box.

**Expected:** a "saved" toast. `GET /v1/settings` for that room reads `idle_park_after_now` 2700. Another room is
unchanged.

### @LETTER@3. Turn it off

1. Uncheck "park after".

**Expected:** the minutes box greys out and a toast says idle cards are not parked. `idle_park_after_now` reads `off`.
Checking it again goes back to the default.

### @LETTER@4. A value under the floor

1. Type 10 minutes and leave the box.

**Expected:** a toast says the least is 30 minutes. Nothing is sent, and the box goes back to the stored value.

Covered by the `idlePark` section of `scripts/test-board-headless.js`.
