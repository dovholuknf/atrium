# u-new-terminals-css-row-bleed-resize. Terminals tab: the selected row bleeds, and the resize button moves

Status: not started. Owned by @ui. CSS only. Filed by the orchestrator 2026-10-01, from clint.

Screenshot: `D:\git\github\dovholuknf\atrium\.atrium\incoming\20261001-100246-pasted.png`.

## What is wrong

- The attached card's row in the terminals list (orchestrator, both in PINNED and in ORCHESTRATORS) draws its
  green highlight past the list's right edge, across the divider, up to the terminal pane's border.
- The list's width button (the « in the box above PINNED) sits top right, beside the controls. clint: "the
  'resize' button should be different and towards a corner, like the lower left?"

## Wanted

- The selected row's highlight stays inside the list column at every list width.
- The width control moves to the list's lower left corner and looks like a resize affordance (a grip or a
  corner handle), not a text button. Collapse and expand stay one click. Dragging the divider keeps working.
- Headless check at the 150px minimum and at a wide list.
