# A child nests under a launcher on another room

A child launched from another room carries only the launcher's handle, `spawned_by` = `name@room`. `launcherOf` in
`cardrules.js` now resolves it to the launcher card on that room (wire name or alias, id tagged `room~`), and
Terminals, stack, board, reveal, the usage groups and `hasRunningChild` all ask it, so the child nests like a
same-room one.
