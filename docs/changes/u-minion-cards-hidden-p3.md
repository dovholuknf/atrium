## Test plan

## @LETTER@. Item minion cards hidden, phase 3 (board, stack and phone)

### @LETTER@1. The board leaves out resting minions

1. Launch an item so its minion session runs, with one of your own sessions running too.
2. Open the Board.

**Expected:** only your own session is drawn, and the column counts and the `N running` line do not include the minion.
The header working indicator still counts it.

### @LETTER@2. The stack leaves them out

1. Open the Stack with the `all` pill on.

**Expected:** the minion has no row and the `all` pill does not count it.

### @LETTER@3. The phone's home

1. Open the phone's home, then `all`.

**Expected:** the minion is not listed and `all` does not count it. When the minion asks a question or has a permission
waiting, its row appears (and in `needs you`), and goes once answered.
