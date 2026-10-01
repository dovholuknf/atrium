## Test plan

## @LETTER@. The terminals list sorts by started

### @LETTER@1. Third sort button
Open the gear's terminal list section, or unroll the tray above the rows. SORT offers `name`, `activity` and
`started`. Press `started`: the cards run newest first, and the tray's summary reads `sorted by started`.

### @LETTER@2. Held under grouping and across reloads
With grouping on `by project`, each project's cards run newest first. Reload the board: `started` is still lit. Two
cards started at the same moment keep the same order on every refresh.
