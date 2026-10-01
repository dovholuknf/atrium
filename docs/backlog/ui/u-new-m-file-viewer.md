# u-new-m-file-viewer: read a card's files on the phone

From clint by the orchestrator, 2026-09-30 evening. Its own item, after `u-new-m-compact.md`.

## What

When a message names a file inside the card's folder, /m renders the name as a link that opens a viewer. The name can
be absolute (`D:\git\github\dovholuknf\atrium\notes\BRANCH-TRIAGE-2026-09-30.md`) or relative.

- Path detection reuses ab758324's, including the drive-path fix in 94774247.
- The viewer renders markdown with the same safe renderer the thread uses, other text as monospace, and images inline.
- It reads through the existing card files endpoint. `internal/safepath` stays the boundary: outside the card is
  `403`, and the viewer shows that as "not in this card's folder", never as a missing file.
- Phone first: a readable width, the same text-size pinch as the thread (`u-new-m-compact.md`), and back returns to
  the thread at the same scroll position.
- Read only. No editing for now.

## Open for the design

Every design goes to @rnd before the build. Points for it: how back works (history entry or an in-page sheet), a size
cap for text files, and what a binary that is not an image shows.

## Tests

Headless: a path in a message becomes a link, markdown and text and image each render, a path outside the card gets
the 403 line, and back restores the scroll.

@review before the hub deploy.
