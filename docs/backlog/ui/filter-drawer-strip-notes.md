# Filter drawer strip: ship note

The filter drawer is restyled as part of the header. Shots are in `img/filter-drawer-strip/`, made with
`scripts/shoot-filter-drawer-strip.js` (`PREFIX=before|after`); before is 8a62bab0.

- **Strip.** A thin full-width strip right under the tab row, the header's own background, the summary left-aligned and a
  chevron after it (down closed, up open). The search box and match count sit at the right. The `filters N` button is gone: the
  chevron is the button and carries the changed-count badge when it is not zero.
- **Stack and Board are an accordion.** Opening pushes the list or board below it down; it no longer floats over it.
  The phone pull-down drags the same row open.
- **Terminals** wears the same strip, but the panel stays an overlay (by clint's choice, 2026-10-12): the strip is one row in
  flow, and the open panel covers the terminal's top rows. The terminal keeps its row count while the drawer is open (no fit,
  no resize to the pty), stays anchored at the bottom so the input box stays visible, and nothing changes size on close.
- **Chevrons.** The drawer chevron is a 20px icon (a 32x28 hit area, 44x40 on a phone). The card's subagent-rollup chevron
  (`.tkidfold .tcaret`, terminals list) goes from 11px to 20px with `line-height: 1`, so the row does not grow.
- Kept: attached card never filtered, phone terminals line, swallow guards, keyboard close.
- **The attached card shows even when its column chip is off,** so neither the column's count nor the `filters N` count
  reflects it. A column holding the attached card is drawn whatever its chip says.

| | before | after |
|---|---|---|
| stack closed 1280 | ![](img/filter-drawer-strip/before-stack-closed-1280.png) | ![](img/filter-drawer-strip/after-stack-closed-1280.png) |
| stack closed 390 | ![](img/filter-drawer-strip/before-stack-closed-390.png) | ![](img/filter-drawer-strip/after-stack-closed-390.png) |
| stack open 1280 | ![](img/filter-drawer-strip/before-stack-open-1280.png) | ![](img/filter-drawer-strip/after-stack-open-1280.png) |
| stack open 390 | ![](img/filter-drawer-strip/before-stack-open-390.png) | ![](img/filter-drawer-strip/after-stack-open-390.png) |
| board closed 1280 | ![](img/filter-drawer-strip/before-board-closed-1280.png) | ![](img/filter-drawer-strip/after-board-closed-1280.png) |
| board closed 390 | ![](img/filter-drawer-strip/before-board-closed-390.png) | ![](img/filter-drawer-strip/after-board-closed-390.png) |
| board open 1280 | ![](img/filter-drawer-strip/before-board-open-1280.png) | ![](img/filter-drawer-strip/after-board-open-1280.png) |
| board open 390 | ![](img/filter-drawer-strip/before-board-open-390.png) | ![](img/filter-drawer-strip/after-board-open-390.png) |
| terms closed 1280 | ![](img/filter-drawer-strip/before-terms-closed-1280.png) | ![](img/filter-drawer-strip/after-terms-closed-1280.png) |
| terms closed 390 | ![](img/filter-drawer-strip/before-terms-closed-390.png) | ![](img/filter-drawer-strip/after-terms-closed-390.png) |
| terms open 1280 | ![](img/filter-drawer-strip/before-terms-open-1280.png) | ![](img/filter-drawer-strip/after-terms-open-1280.png) |
| terms open 390 | ![](img/filter-drawer-strip/before-terms-open-390.png) | ![](img/filter-drawer-strip/after-terms-open-390.png) |
| rollup chevron 1280 | ![](img/filter-drawer-strip/before-rollup-chevron-1280.png) | ![](img/filter-drawer-strip/after-rollup-chevron-1280.png) |
| rollup chevron 390 | ![](img/filter-drawer-strip/before-rollup-chevron-390.png) | ![](img/filter-drawer-strip/after-rollup-chevron-390.png) |
