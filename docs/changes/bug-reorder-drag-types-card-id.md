Docs: none, a fix that makes the board match what it already says

## Test plan

## @LETTER@. Reordering the list never types into a terminal

### @LETTER@1. Release a reorder over the terminal

1. Open a terminal and the Terminals list beside it.
2. Drag a card in the list and let go over the terminal screen. Repeat with a group heading.

**Expected:** nothing is typed into the session, and the terminal shows no drop highlight while the drag is over it.

### @LETTER@2. Reorder still works

1. Drag a card to another place in the list and let go on the list.

**Expected:** the order is written as before.

### @LETTER@3. Outside text still pastes

1. Drag selected text from another window onto the terminal screen.

**Expected:** the text is pasted into the session.
