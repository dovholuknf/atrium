Docs: none, a fix that makes the hold do what its design says

## Test plan

## @LETTER@. A lifted deploy hold wakes nobody it did not refuse

### @LETTER@1. Called off with idle cards

1. Have several idle cards in a room and one working card.
2. Set a deploy hold, let the working card make a tool call (it is refused), then call the hold off.

**Expected:** the working card gets one queued line, "room deploy called off by ...", at its next tool call. No idle
card receives anything or starts a turn, and nothing is typed into any terminal.

### @LETTER@2. A deploy that will refuse never holds

1. On a room whose pty host is in the room task's job, with runners, run `deploy-batch.ps1` without `-KillRunners`.

**Expected:** it says REFUSED with the runner counts and exits 1 before any hold is set. No card's call is refused.
`scripts/live/test-deploy-batch-preflight.ps1` covers it with stubs.
