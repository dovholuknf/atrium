# u-drag-no-click
- New `js/dragclick.js` (capture pointerdown + capture click on document; swallows click on >4px move or a selection intersecting the target; skips input/textarea/select/contenteditable/.xterm). Loaded in `index.html` and `m/index.html`.
- Copy button `#t-copy` beside `#t-title` (sibling, so drag-select and alias repaints are unaffected); `copyTermName()` in `js/terminal.js` copies the shown name (`@alias` or label) and toasts. CSS in `css/terminal.css`.
- Headless case added in `scripts/test-board-headless.js` (alias section): drag-selected click must not open the dialog; the existing plain click still does.
- Tests: `check-board.sh` shows no parse errors; it reports the pre-existing `t-compose` duplicate id and a phone invariant (`sizeCommands`/switchView), both not from this change. `node --check` passes on touched JS. The headless harness was NOT run (cannot run one section; known hang).
