## Test plan

## @LETTER@. Background commands do not flash a console window on Windows

### @LETTER@1. gitsync's git runs hidden

1. Start the hub and a room on Windows the usual way, without a console of their own.
2. Launch a card, let it commit, and let the room sync with the hub for a few minutes.

**Expected:** no console window appears on the desktop while git runs. `go test -run TestGitCommandHasNoWindow
./internal/gitsync` passes.

### @LETTER@2. Card and PR routes run git hidden

1. Close a card with a merge, open a PR worktree from the board, and list a provider's worktrees.

**Expected:** each works as before, and no console window flashes.

### @LETTER@3. Windows somebody asked for still show

1. Launch a runner in window mode, open a file from a card in the editor, and open a terminal from a card.

**Expected:** the runner's terminal, the editor and the terminal all appear as before.
