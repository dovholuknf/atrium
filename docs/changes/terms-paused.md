# Paused is not dead

`shelved` is renamed `paused` everywhere: labels, chips, menus, MCP text, docs, and the stored status. Migration
`0101_status_paused` widens the `task.status` CHECK in place (a rebuild of `task` would fire every child cascade, see
0025) and rewrites `shelved` rows. It is safe to run twice. `store.NormalizeStatus` makes `shelved` an alias for
`paused` on PATCH, list filters and `SetStatus`.

PAUSED: no agent running, can be resumed (stopped by the operator, or parked after sitting idle). DEAD: no agent, cannot
be revived. A parked card is paused, never ready (`cardPaused`, `cardStatusLabel` in `cardrules.js`). The paused chip
lists and hides it. The popup of a paused card has a resume row, a dead card with a folder has `new agent here`.
The Terminals header reads `N shown`. Stack and board have no paused chip, only the column, so the nesting filter is
Terminals only.

Images: `img/terms-paused/{before,after}-*.png`, shot by `scripts/shoot-terms-paused.js`.

## Test plan

- Go: `TestPausedMigrationRenamesRowsAndWidensTheCheck`, `TestShelvedIsAnAliasForPaused`.
- Headless `pausedDead`, `hiddenEveryLevel` (child and grandchild hidden under a visible parent and grandparent).
- By hand: park a card under a parent with the paused chip off. It is not listed. Turn the chip on, it is.
