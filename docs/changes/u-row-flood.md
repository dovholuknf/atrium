## Test plan

## @LETTER@. Context on a terminals row is a thin line, not a flood

### @LETTER@1. No badge, no tint

Open the terminals list with cards at about 10%, 80% and 130% of the land line, and one with no context figure. None
shows a `LAND` badge. Every row has the same background, border and left accent bar as the plain one. Each row with a
figure has a 3px line along its bottom edge: neutral, amber from the warn line, danger from the land line. Hover the
line for its tooltip. Run `HEADLESS_ONLY=ctxLine node scripts/test-board-headless.js`.
