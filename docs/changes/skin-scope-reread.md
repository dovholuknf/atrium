Docs: none, a test fix.

## What and why

Run 39052523653, windows board-1: `skinScope` failed in the shard with `a room attaching or leaving clobbered the
settled ALL skin: it became "ember", wanted the applied vapor` and passed alone. The check moves the mock's answer
for the ALL scope to `ember` and then flips a room out and back in, so that any re-read of the skin in those five
seconds shows as `ember`. The bug it guards against is `loadHubRooms` calling `bootSkin` on a room flip after the
skin has settled.

A re-read is not only that path. Every open of the event stream, the first and each reconnect, calls
`loadGlobalAuto`, which applies the skin the settings answer carries, because the daemon's answer wins: a skin
cleared from another browser has to reach this one. On the loaded Windows runner the stream reopened inside the
window, the read was right to apply `ember`, and the check read it as the room-flip bug.

The unit now counts calls to `bootSkin` across the two flips and fails on any, which is the bug itself. The worn
skin is still judged when no `/v1/events` request was made during the window, and when one was, the unit says so
and leaves the skin alone.

## Test plan

## @LETTER@. A room flip does not re-read a settled skin

### @LETTER@1. The unit

1. `HEADLESS_UNITS=skinScope node scripts/test-board-headless.js`, under CPU load.

**Expected:** passes. If the stream reopened during the flips the unit prints that it did and still passes.
