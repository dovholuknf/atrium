# controls-used: the "controls used" view records, opens at once and sorts

- WHY EVERY ROW SAID "never": the counts were posted with a beacon and read with a plain fetch. In the ALL view with
  more than one room known, a beacon cannot carry `X-Atrium-Room`, so the hub refused the post with its "pick a room"
  409, and the read was refused the same way. The view swallowed the failure and listed only the controls found in the
  DOM, all "never". Fixed in two places: the board posts with a keepalive fetch, which carries the room header, and the
  hub sends `POST /v1/usage` and `GET /v1/usage/board` to the first room when none is named (`boardUsagePath` in
  `internal/link/proxy.go`). Counts therefore live on the room being looked at, or on the first room in the ALL view.
- Snapshot: the room keeps the totals in memory and rebuilds them only after a batch that changed something. The board
  keeps the last totals and the last look at the DOM and draws from them the moment the dialog opens, then reads again.
  A first snapshot is taken a few seconds after the page loads.
- Sorting: the "control" and "used" headers sort the rows, most used first by default, a second click flips. Controls
  never used stay at the bottom. The headers are not counted as controls.
- Daemon side: the snapshot is in the room, and the routing is in the hub, so both need a deploy.

## Test plan

## @LETTER@. Controls used

### @LETTER@1. Counted in the ALL view

1. With two rooms attached, stay in the ALL view and click the `usage` tab twice and the `...` button once.
2. Choose "controls used" in the `...` menu.

**Expected:** the `usage` tab is listed with 2, not "never".

### @LETTER@2. Opens at once

1. Close the dialog and open it again.

**Expected:** the rows are there as it opens, with no empty moment.

### @LETTER@3. Sorting

1. Click "control", then "control" again, then "used", then "used" again.

**Expected:** A to Z, Z to A, most used first, fewest used first. Never rows stay at the bottom throughout.
