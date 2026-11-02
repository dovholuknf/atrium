## A room hears the hub's paths in order

A room handed the hub's list of paths to `OnPaths` in a new goroutine each time, so the list from the welcome could
land after a newer pushed list and leave the room with the stale one. It now hands them over one at a time, in the
order they arrived.
