## Test plan

## @LETTER@. A re-pasted issue or branch link goes to the room that holds it

### @LETTER@1. Re-paste an issue link on a hub with two rooms

1. Paste an issue link into the launch dialog with no room named. Note the room its card lands on.
2. Make the other room the less busy one.
3. Paste the same link again.

**Expected:** the answer is the same card on the same room, and no second card or worktree is made.

### @LETTER@2. Re-paste a branch link, and a pull request link

1. Repeat the steps above with a branch link, then with a pull request link.

**Expected:** each goes back to the room that holds it. The pull request behaves as it did before.

### @LETTER@3. A claim whose card is gone

1. Close the card of an issue link. Paste the link again.

**Expected:** the paste goes to the room that held the claim and opens a new card there, as a pull request does.
