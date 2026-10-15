This changes `docs/changes/clean-copy.md`: a copy is no longer cleaned on its own. `copySelection` in `js/terminal.js`
now copies `getSelection()` and says nothing. Once a selection settles (the mouseup that ends a press, or ctrl-a),
`offerCleanCopy` runs the same `cleanCopy` lookup, and only on a match shows a `#cc-btn` "copy clean" button a row below
the end of the selection. Clicking it copies the reply's raw markdown. `hideCleanCopy` removes it when the selection
clears, on scroll, on Escape, and when the terminal closes. A lookup that is superseded shows nothing. The matching
rules in `clean-copy.md` are unchanged.

Two toasts are removed: "copied as written", and "in the working directory" (or "in the scratch folder") after pasting
an image, since the path is already typed into the terminal. A refused clipboard still toasts.

Headless unit: `cleanCopy`.

## Test plan

## @LETTER@. Copy clean button

### @LETTER@1. A reply

1. Have Claude write a reply holding a quoted block that the terminal wraps over two rows.
2. Select both rows and press ctrl-c, then paste into an editor.

**Expected:** the paste is what the terminal showed. A "copy clean" button sits beside the selection and no toast
appeared.

### @LETTER@2. Click it

1. With the button showing, click it and paste.

**Expected:** the paste is the source text, with the `>` and no indent or `▎`. The button is gone and no toast appeared.

### @LETTER@3. It goes away

1. Select a reply again, then clear the selection, or scroll, or press Escape.

**Expected:** the button disappears each time.

### @LETTER@4. Not a reply

1. Select some `ls -la` output.

**Expected:** no button.

### @LETTER@5. Pasting an image

1. Paste a screenshot into a terminal.

**Expected:** the path is typed into the terminal and no "in the working directory" toast appears.
