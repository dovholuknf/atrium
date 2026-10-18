## Test plan

## @LETTER@. Overlapping rooms redraws

### @LETTER@1. Newest draw wins

1. Open the rooms tab on a hub with a room whose login is revoked.
2. Restart the hub so the rooms event and the tab's own fetch land close together.

**Expected:** the list settles on every room, with alpha's `login needed` chip, and never falls back to `this machine`.
