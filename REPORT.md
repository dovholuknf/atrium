# REPORT: u-new-terminals-css-row-bleed-resize

## What changed
- The "bleed" is the tab bridge (a strip painting the attached row's colour across the gutter into the pane). It is now off
  by default (`termBridge = false`, terminal-list.js). The attached row keeps its full border and rounded corners
  (`.bridged` class gates the squared-off look). Machinery kept, one variable restores it.
- The width buttons moved from the tray bar to a `.tlcorner` row at the list's lower left: icon-only corner grip
  buttons (widen one is mirrored). Same steps, one click. Divider drag untouched. Hidden on phone (phone.css).
- Tests: new `termRowBleed` section (150px and 520px: row inside the column, no bridge, buttons in lower left corner and
  icon only, one-click steps, divider width still set). Existing bridge/termWear tests set `termBridge = true`. termBox
  and the tray checks now look for the buttons in `.tlcorner`.
- Changelog: changelog/ui/2026-10-04-u-new-terminals-css-row-bleed-resize.md. Design note and a question for clint are in
  the item file.

## Design note / open question
Dropping the tab bridge was a design decision made to meet the item as written. Does clint want the row still tied to the
pane some other way? See the item file.

## Tests
`$env:HEADLESS_ONLY="termRowBleed,termBox,bridge,termWear"; node scripts/test-board-headless.js` : all four pass.
The full suite run aborted at `hubRepos` (`#hubrepos-refresh` not visible), unrelated to this change, so the later
inline tray checks I edited (widthBtns, mini) were not reached. Not verified.

## PNGs
docs/screens/u-new-terminals-css-row-bleed-resize/before-150.png, before-520.png, after-150.png, after-520.png
(headless real rows from the test mock, before taken from the unmodified HEAD).
