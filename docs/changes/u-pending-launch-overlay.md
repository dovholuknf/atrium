Docs: none, no doc page says where the opening card is drawn

## Test plan

## @LETTER@. An opening link floats in the lower left

### @LETTER@1. A slow open does not move the list

1. With several cards on the board, paste a pull request link whose clone is not on the room yet.
2. Watch the card list while the room says "making the worktree".

**Expected:** the link's title, its URL and the step float as a card in the lower left, over the board. No card in
the list moves, not when the open starts and not when it ends.

### @LETTER@2. It goes when the open ends

1. Wait for the open to finish.

**Expected:** the floating card is gone and the new card is attached.

### @LETTER@3. A failed open

1. Paste a link the room cannot open.

**Expected:** a toast says why, and the floating card turns to the reason with edit, again and dismiss. Dismiss
removes it, and the list never moved.
