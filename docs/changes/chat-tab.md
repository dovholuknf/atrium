## Test plan

## @LETTER@. The chat tab

### @LETTER@1. Open it and pick a session

1. Open the board on a desktop width and click the chat tab.
2. Click a session in the list on the left.

**Expected:** the list is the terminals tab's list (same grouping, filter and pins), the row is marked, and the right side
shows that session's conversation: its replies as bubbles on the left, in a centred column no wider than about 820px.
No terminal is attached.

### @LETTER@2. Talk to it

1. Type into the composer and send.
2. Pick a session that is working.

**Expected:** your message arrives as a bubble on the right and reaches the card. A working session shows the spinner
with what it is doing. New replies arrive without a reload, and the view follows them unless you scrolled up.

### @LETTER@3. Terminal and back

1. Click "terminal" at the top of the conversation.
2. Click "chat" in the terminal's toolbar.

**Expected:** the first lands on the terminals tab with that card attached, the second returns to that card's
conversation.
