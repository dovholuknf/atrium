## Test plan

## @LETTER@. The pulls walk drawer

### @LETTER@1. Walk a ready review

1. In the pulls view, click `walk` on a ready row that has no walker.
2. Wait for the walker's terminal to attach.

**Expected:** a walker card is launched, its terminal opens and the walk drawer opens beside it with the review's findings in walk order.

### @LETTER@2. Edit a finding

1. In the drawer press `e`, change the comment and press `save`.
2. Press `e` and save again.

**Expected:** each save shows the new comment. The second save is not refused, since it quotes the hash the first one returned.

### @LETTER@3. A finding changed behind your back

1. Start an edit, and change the same finding file on disk before you save.
2. Save.

**Expected:** nothing is written. The drawer says the file changed while you were editing it and shows yours beside what is on disk. `keep mine` writes yours over it and `take theirs` drops yours.

### @LETTER@4. Walk marks move the row

1. Press `s` on a finding, `j`, then `d`, then `u`.
2. Switch to the pulls view.

**Expected:** the rail shows skipped, deferred and then open again, and the row says `walking N of M` with the counts the daemon answered.
