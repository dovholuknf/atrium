## Test plan

## @LETTER@. A prepare command runs under Windows PowerShell 5.1

### @LETTER@1. Launch with a prepare command

1. On a room whose shell is Windows PowerShell 5.1, set a prepare command that exports one variable.
2. Launch a card.

**Expected:** the card starts and sees the variable. The launch does not fail on -AsArray.
