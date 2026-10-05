# REPORT: u-new-terminals-css-row-bleed-resize

## What changed
- The "bleed" is the tab bridge (a strip painting the attached row's colour across the gutter into the pane). It is now off
  by default (`termBridge = false`, terminal-list.js). The attached row keeps its full border and rounded corners
  (`.bridged` class gates the squared-off look). Machinery kept, one variable restores it.
- The width buttons moved from the tray bar to a `.tlcorner` row at the list's lower left: corner grip buttons
  (widen one is mirrored). Same steps, one click. Divider drag untouched. Hidden on phone (phone.css).
- Tests: new `termRowBleed` section (150px and 520px: row inside the column, no bridge, buttons in lower left corner and
  icon only, one-click steps, divider width still set). Existing bridge/termWear tests set `termBridge = true`. termBox
  and the tray checks now look for the buttons in `.tlcorner`.
- Changelog: changelog/ui/2026-10-04-u-new-terminals-css-row-bleed-resize.md. Design note and a question for clint are in
  the item file.

## Review round 2
- The corner grip is now a 30x28 bordered button with an 18px icon, label colour on the chip fill. Hover goes teal border
  and bright icon, with a focus ring. The tooltip stays the project's `data-tip`. One click still narrows or widens.
- The tray and mini checks ran (units `core2`, `termBox`, `trayHead`, `gearTermList`, `phoneTermBar`, `termListLastRow`,
  `termWear`, `bridge`, `termRowBleed`). All pass. `phoneListFit` fails with "the cards differ in width
  [341,341,341,341,341,355]" and does the same on the parent commit, so it is not from this change.
- The bridge question for clint in the item file is unchanged and still open.

## Design note / open question
Dropping the tab bridge was a design decision made to meet the item as written. Does clint want the row still tied to the
pane some other way? See the item file.

## Tests
`HEADLESS_UNITS=core,core2,termBox,termRowBleed,termWear,bridge,termListLastRow,trayHead,gearTermList,phoneTermBar`
passes. The earlier full run aborted at the unrelated `hubRepos` section.

## PNGs
docs/screens/u-new-terminals-css-row-bleed-resize/before-150.png, before-520.png, after-150.png, after-520.png
(after retaken with the larger grip, headless rows from the test mock, before taken from the unmodified parent).
