## Test plan

## @LETTER@. The read tab's width and skin

Open a markdown file from a card with "open rendered" on a wide window, with a document that has a table of four
columns and long cells.

### @LETTER@1. Width

1. Open the rendered page. It starts on Narrow.
2. Click Wide, then Full.
3. Reload the tab.

**Expected:** the prose column widens at each step and is the whole window on Full. On Narrow the table still runs past
the column to the right and its cells do not wrap. The choice survives the reload and shows in a second read tab.

### @LETTER@2. Skin

1. Note the board's skin in Settings. Open the rendered page.
2. Pick another skin in the page's picker, then reload.
3. Pick "the board's skin".

**Expected:** the page first wears the board's skin. The picker changes this page only and the board keeps its skin. The
pick survives a reload and the last step returns to the board's skin.

### @LETTER@3. Phone

1. Open the rendered page at about 390px wide.

**Expected:** the controls wrap onto a second line, the page does not scroll sideways, and a wide table scrolls inside
itself.
