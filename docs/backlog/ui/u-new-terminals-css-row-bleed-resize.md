# u-new-terminals-css-row-bleed-resize. Terminals tab: the selected row bleeds, and the resize button moves

Status: done on branch claude/u-new-terminals-css-row-bleed-resize, awaiting review (see the question below). Owned by @ui. CSS only. Filed by the orchestrator 2026-10-01, from clint.

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

## Design note

The "bleed" was not a CSS overflow. It was the tab bridge: a deliberate strip (`#tab-bridge`, `placeTabBridge`) that
paints the attached row's colour across the gutter into the terminal pane so the row and the pane read as one tab. At
the screenshot's width it looks exactly like a highlight spilling out of the list. Removing it was the only way to
meet "stays inside the list column at every width", so it is now OFF by default (`termBridge = false` in
terminal-list.js) and the attached row keeps its full border and rounded corners. The machinery is left in place and
the old headless bridge sections turn it on, so it can be restored by flipping one variable.

The width control is two small icon buttons (a corner grip, the second mirrored) in a `.tlcorner` row under the
rows, bottom left. Same one-click steps (full, mini, off), tooltips say which way. Hidden on a phone as before.

Question for clint: is dropping the tab look what you want, or do you want the row to still read as joined to the
pane some other way (for example only the pane's frame taking the row's colour)?
