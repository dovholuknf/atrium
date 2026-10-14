# Remove the resume button; a click on a dead card does not resume it

Scope notes from the first look (nothing built yet). The orchestrator asked for:

1. No "resume" button on any card (Terminals rows, stack, board, phone).
2. A click on a dead (not running) card only opens or selects it. It must not resume or show the
   "resuming / it is starting again on the same conversation" toast. Resume stays in the right-click menu only.
3. The hover text "click it to start it again here" changes to match.

## Where the code is (internal/api/web/js)

- `terminal-list.js` ~1230: `termresume` chip in `termRowChips` (parked, not supervised).
- `terminal-list.js` ~1095-1103: a cold row's `data-tip` ("click to resume the same conversation", "click to start it again here") and its `onclick` (`resumeParked` / `resumePinned`). Should become an open/select-only click.
- `terminal-list.js` ~2032: `endedresume` button in `termEndedRow` (the "ended" group rows).
- `terminal-list.js` ~718 and `card-menu.js` ~951: the right-click "resume" entries. KEEP these.
- `board.js` ~846-851: the shelved card's "resume"/"unshelve" chip (`unshelveCard`). Remove the chip; unshelve stays in the card menu (`card-menu.js` ~476).
- `board.js` ~1003-1012: `parkedChip` text "or use resume" and `resumeParked` (its toast is the one in the screenshot). The function stays for the menu; the toast stays for the menu path.
- `peek.js` ~235: hover popup text "this one has exited. click it to start it again here".
- `terminal-debug.js` ~215: calls `resumeParked` on a parked card; check whether it is a click path.
- Phone: `m/` has a long-press resume (`m.css` ~272, js/resume.js). Check for any visible resume button there.
- `scripts/test-board-headless.js`: units that click or look for the resume chip (grep `termresume`, `endedresume`, `resumeParked`) must change with this.

Not started: no code changed on this branch.
