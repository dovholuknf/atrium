Docs: none, a regression fix in the terminal input path

## Test plan

## @LETTER@. Typing and pasting scroll the terminal to the bottom

### @LETTER@1. Paste while scrolled up

1. Scroll a terminal up into its history.
2. Paste several lines.

**Expected:** the view jumps to the bottom and stays there as the runner echoes.

### @LETTER@2. Type while scrolled up

1. Scroll a terminal up, then type a character.

**Expected:** the view jumps to the bottom.
