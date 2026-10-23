Docs: none, no reference page describes the not-running pane

## Test plan

## @LETTER@. The not-running pane resumes a picked conversation

### @LETTER@1. The pane says resume and offers the conversations

1. Pick a card with no runner whose directory holds three or more claude conversations, one with a very long title.
2. Open its terminal.

**Expected:** the pane says Resume, never Relaunch, in its text, its button and the button's tooltip. A dropdown
lists the conversations, the one marked `current` is preselected, and the long title ends in an ellipsis inside the
pane. Resume, Launch new and Cancel are the buttons.

### @LETTER@2. Resume starts the picked conversation

1. Pick a conversation other than current in the dropdown.
2. Press Resume.

**Expected:** that conversation resumes on the same card, and no "which conversation?" dialog opens.

### @LETTER@3. One conversation, and cancel

1. Open the terminal of a card with no runner and only one conversation in its directory.
2. Press Cancel.

**Expected:** no dropdown is drawn. Cancel closes the pane and the terminal view says nothing attached, as the bar's
close does.
