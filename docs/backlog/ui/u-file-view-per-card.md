# u-file-view-per-card. The open file belongs to its card, and the editor fills the panel

Status: done, ready to deploy. Asked for by clint 2026-10-05, urgent.

## What clint asked

1. With a file open over a terminal, clicking another agent in the list left the file showing. A file should belong to the
   card it was opened on.
2. The file editor filled only about half the panel height and left a dark gap under it. It should fill down to the footer
   hint line at every window height.

## What was built

- `fileViews` in `js/notes-files.js`: a Map keyed by bare card id, memory only. `fileViewLeave` stashes the open file (the
  `editing` record, text, scroll, the "not saved" mark, whether the drawer was opened by the file) and hides the editor and
  drawer. `fileViewEnter` puts it back and reloads the listing behind it. `fileViewPrune` drops entries for cards that are
  no longer in the list.
- `openTerm` calls leave before tearing the old pane down and enter once the new card is set. `reconcileAttached` prunes.
  `clearTermPane` for a real close (not a switch) drops that card's file and closes the editor.
- CSS cause of the gap: when the drawer is open `#t-screen` is hidden but its parent `.term-body` kept `flex: 1 1 auto`
  and a share of the pane (281px of 783px at 900px high). `css/rows.css` now hides `.term-body` while the drawer is open.

## How it was tested

`fileView` in `scripts/test-board-headless.js`: opens a file on A, edits and scrolls it, checks the editor's bottom edge is
within 24px of the `.term-help` footer at window heights 900 and 620 and that it scrolls inside itself, switches to B (file
hidden, terminal shown), back to A (edited text, scroll and "not saved" kept), closes on A (stays closed after a round
trip), and prunes a vanished card. Before the CSS fix the gap was 291px and 151px. Run with `HEADLESS_ONLY=fileView,bootClean`
plus `termBox,keepAlive,attachAtOnce,linkReuse,linkTip,termDebug`, all pass.

Pictures are in `docs/screens/u-file-view-per-card/`: `before-*` show the gap, `after-*` the filled editor, and
`away-on-b` and `back-on-a` the switch states. They come from the headless fixture, not a live room.
