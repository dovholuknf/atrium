## Test plan

## @LETTER@. Paste spinner

### @LETTER@1. The spinner is up before the paste leaves

In a card's terminal, paste 50 KB and then 1 MB of text by Ctrl+V, by right click and by the paste box. The box
"pasting ..." is on screen at once, before the terminal shows anything of the paste. Typed keys never show it. Run
`HEADLESS_ONLY=pasteStart node scripts/test-board-headless.js`.

### @LETTER@2. It stops on the room's word

On a room that answers `in-done`, the box stays up while the runner works through the paste, however long it is
quiet, and goes within a moment of the room's `in-done` for that paste. Two pastes in a row end in the order they
left. Run `HEADLESS_ONLY=pasteDone node scripts/test-board-headless.js`.

### @LETTER@3. An older room still ends it

On a room that never answers, output after the hold or a quiet after an echo ends the box, and a paste that gets no
output is taken down by the 20 second cap. Run `HEADLESS_ONLY=pasteOldRoom node scripts/test-board-headless.js`.

### @LETTER@4. A closed socket ends it

Paste a large block and stop the room while it is landing. The box goes with the socket. Run
`HEADLESS_ONLY=pasteClose node scripts/test-board-headless.js`.
