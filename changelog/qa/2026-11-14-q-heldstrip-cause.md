# heldStrip answered row stayed: the room list cache

The board kept a room's answered request drawn on the Terminals strip. `loadRooms` caches the room list for 2s, so the
pass that followed the answer was served the old list and wrote the answered request back into `remoteLocal`. No later
pass came to correct it. `decide` now clears that cache. The heldStrip unit's mock `/v1/permissions` read takes 250ms, so
the window is hit every run instead of only on a slow CI machine.
