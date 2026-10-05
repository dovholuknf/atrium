# Report: u-new-gate-readout-only-when-blocking

## What changed
- `internal/api/web/js/typing.js`: new `typingGateText(s, held)` returns `{ text, blocking }`, the one place the
  readout's words live (the details drawer item can reuse it). `paintTyping` now always shows a line while the
  readout is on. Before, it hid itself unless a message was held.
- `internal/api/web/css/terminal.css`: `.term-typing.calm`, dim, no stripe.
- `scripts/test-board-headless.js` (`typingSection`): the old "hidden when the gate opens" check is now "reads
  'line empty', dim". New checks for the three states and that the line's text is never repeated. Optional
  `TYPING_SHOTS` and `TYPING_SHOT_PREFIX` env vars write the PNGs.
- Item file gained a Design note. Changelog added.

## Design notes
- Nothing held: dim "line empty" or "N chars on the line". Held: "1 message from @runtime waits: N chars on your line".
- Looks empty (only blanks on the line, count above 0): the count plus the line quoted with blanks as dots, cut at 40.
- "Looks empty" is judged from atrium's model of the line, not the screen. Open question for clint if that is too
  narrow, noted in the item.
- An open gate reads as the dim line even if the card still shows a held message for a moment.

## Tests
`HEADLESS_ONLY=typing node scripts/test-board-headless.js` (playwright-core from D:/tmp/termswitch via a shim on
NODE_PATH, ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG cleared): "the sections asked for passed: typing". The full
suite was not run.

## Screens
`docs/screens/u-new-gate-readout-only-when-blocking/`: `before-` and `after-` each of `nothing-held.png`,
`message-held.png`, `looks-empty.png`. The before shots show the old code (no line when nothing is held, the same
"N chars" for the looks-empty case).
