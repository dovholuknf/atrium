## Test plan

## @LETTER@. Card address and growler lows

### @LETTER@1. A pop-out that reloads onto another card is found by the board

Open a pop-out at `/alias/<a>`, then load `/alias/<b>` in that same window. In the console `window.name` reads
`atrium-term-<id of b>`. Ask the board to pop out card b: it raises that window and opens no second one. Run
`HEADLESS_ONLY=cardUrlWinName node scripts/test-board-headless.js`.

### @LETTER@2. A notification click finds a window whatever its trailing slash

Pop a card out at `/alias/<name>/` (with the slash), then click that card's notification. That window comes forward,
not the board. The same holds for a window at `/alias/<name>` and a notification path ending in a slash. Run
`HEADLESS_ONLY=cardUrlNotify node scripts/test-board-headless.js`.

### @LETTER@3. A pop-out rings its own reminder while the board has focus

Pop a card out with an open growler, then focus the board. When its reminder comes the pop-out plays the tone and the
board shows the message as a toast, and the board plays nothing for that card. Switch the pop-out's notifications off
or mute it: nothing rings for that card. Run `HEADLESS_ONLY=growlPopout node scripts/test-board-headless.js`.

### @LETTER@4. Two test flakes are gone

`phonePan` and `cacheChip` each pass 10 of 10 runs alone and 12 of 12 run at once. Run
`HEADLESS_ONLY=phonePan node scripts/test-board-headless.js` and the same with `cacheChip`. The causes are in
`docs/backlog/ui/u-new-suite-flakes-0930.md`.
