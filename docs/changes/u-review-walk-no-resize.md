Docs: none, a layout fix with no new control, setting or verb

## Test plan

## @LETTER@. The review walk never resizes the terminal

### @LETTER@1. The walk in the list's column

1. On a desktop window wider than 900px, drag the terminal list's grip until the list is at least 320px wide.
2. Attach a card that has a `findings` folder, so the **walk** button shows on its terminal bar.
3. Note the terminal's width in columns (for Claude Code, the width of its input box).
4. Press **walk**.

**Expected:** the findings drawer covers the list's column exactly. The terminal does not move or narrow, and no
resize notice appears in it.

### @LETTER@2. The walk over the terminal's right edge

1. Drag the list back under 320px (the default is 260px), or set the list to `mini` or `off`.
2. Press **walk**.

**Expected:** the drawer lies over the terminal's right edge. The terminal's left side, its columns and its rows
are unchanged, and no resize notice appears.

### @LETTER@3. Closing it

1. With the walk open in either place, press its `×`, then press **walk** again a few times.

**Expected:** the terminal never redraws, and its scrollback gains no extra copy of the conversation.

### @LETTER@4. The headless check

1. Run `HEADLESS_UNITS=walkNoResize node scripts/test-board-headless.js`.

**Expected:** it prints `walkNoResize: ok`.
