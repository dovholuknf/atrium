## Test plan

## @LETTER@. A close of a held worktree says so and finishes later

### @LETTER@1. Hold the folder and close (Windows)

1. Open a card, then in another shell `cd` into its worktree and keep a file in it open.
2. Close the card from the board.
3. The toast says "not cleaned up yet" and names the folder that did not remove.
4. Release the file. Within a minute or so the worktree is gone from disk and the card's inventory row is freed.
