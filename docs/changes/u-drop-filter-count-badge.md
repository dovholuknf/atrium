# u-drop-filter-count-badge

The green count on the filter drawer's handle is gone, with its CSS (`.fcount`), the `changed` field `fdStatus` took,
`host.dataset.changed`, and the three painters of it (`boardChanged`, `stackChanged`, `groupChanged`). Nothing else read
them. The summary line and the `12 of 40` match text are unchanged.

The headless units `phoneBoardCompact` and the terminals show-chip unit, and `check-board-drawer.js`, no longer assert
the count.

## Test plan

1. Open the board, stack and terminals tabs and change a chip, a sort or the grouping. The filters handle shows no
   number, and the summary line reflects the change.
2. Headless units `phoneBoardCompact` and `bootClean` pass.
