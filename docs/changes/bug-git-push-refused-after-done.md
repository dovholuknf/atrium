Docs: none, a fix that makes push and progress do what they already say

## Test plan

## @LETTER@. A resident card pushes after its done

### @LETTER@1. Push after done

1. Launch a worker, have it call `atrium_done` with a sha, then say it a second task without ending its runner.
2. Have it commit and call `atrium_git_push` on its branch.

**Expected:** the push goes through. It is not refused with "that card is not running".

### @LETTER@2. Progress after done

1. On the same card, call `atrium_report` with status `progress`.

**Expected:** the answer's status is not `done`, and the card is back in running.
