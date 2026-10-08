# u-dropdowns-instant

Decision: the cause was a fetch on open. `openLaunch` awaited `/v1/harnesses` and `openPickRepo` awaited `/v1/providers`,
so the dialog came up late, after a network round trip, with its selects empty until then. The board now reads both once
at start (`bootBoard`) and the dialogs fill from what is held. A board that has nothing held yet still waits once. The
repo picker refreshes in the background and only touches its select when the list changed and the select has no focus.
The audit room filter and the usage room filter rebuilt their options on every event: they now rebuild only when the set
changed and the select does not have focus. Rooms already came from the stream. The phone page builds its selects in
place with no fetch, so it is unchanged. Not changed: `leastBusyRoom`, which adds its option when a pasted link is
recognised, which is a user action and not an open.

## Test plan

## @LETTER@. Dropdowns are already filled

### @LETTER@1. The launch dialog

1. On a hub with two rooms, slow the network (devtools throttling). Reload the board and wait a few seconds.
2. Click `new agent`.

**Expected:** the dialog appears at once. The room and runner selects hold their options, and open without any change.

### @LETTER@2. The repo picker

1. In the launch dialog press `repo`.

**Expected:** the picker appears at once with its provider select filled.

The headless section `dropdownsInstant` covers both with every API answer held 500ms.
