# Hub gate ignores an answered 404

`cappedFetch` trips the hub-health gate only when the read got no answer. A 404 from `/_hub/*` on a plain daemon is an
answer. See `changelog/ui/2026-10-17-hub-gate-ignores-an-answered-404.md`.

## Test plan @LETTER@

1. Open a board served by `atrium daemon` with the console open. After the first load no read waits on the gate, and
   a reload that comes back on the Terminals view still shows the stack list when you switch to it.
2. `HEADLESS_UNITS=themePreview,u016,termDebug,pullsDrawer,groupColor,walkContext,clearKeepsPage,resumeSpinner node
   scripts/test-board-headless.js` passes.
