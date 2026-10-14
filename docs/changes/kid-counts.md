# Child counts match what expanding shows

The count on a collapsed Terminals parent was every child, while expanding drew only the listed ones. `termKidTotal` now
counts the drawn children. Nothing about hidden ones goes on the row. The card's popup gets a children section
(`peekKidsHtml`, `termChildInfo`): children in all, shown, hidden by `paused N, dead N`, running, and the cards that need
you by name, each a button (`peekOpenKid`). No children, no section. Stack and board count rolled-up subagents with their
own rollup and are unchanged.

Images: `img/terms-paused/after-popup-parent-*.png`.

## Test plan

- Headless `kidCounts`: folded count equals drawn children, the popup text, the clickable names, no section on a leaf.
