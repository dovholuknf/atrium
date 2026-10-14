# resume-keeps-waiting

A deploy resumes every card. Each resumed session comes up, runs a turn and ends it, and the room reset `waiting_since`
to now at each step, so the phone and the board rang "X is waiting for you" for cards already waiting before the deploy.

## What changed

- Room store: a card that goes `dead` or `done` while waiting records its `waiting_since` in `waiting_carry` (migration
  0100). Entering a waiting state with no clock of its own spends the carried value instead of now, both when the
  session comes up and when the resume turn ends.
- Room daemon: an operator prompt clears the carry (`ClearCarriedWait`), so the next wait after a person spoke is new.
- Board and phone: no change. `bell.js` and `notify.js` already key the alert on `id + waiting_since`, so an unchanged
  `waiting_since` does not ring. No headless unit needed.
- `scripts/live/atrium-deploy.ps1`: `-ReportTo` with no `@room` now sends `X-Atrium-Room: sg4-control`, the local
  room, which fixes "could not say the result to minion: 409". Copying it to the Windows hub's
  `C:\Users\claude\.atrium2\scripts\` is left to the minion.

## Decisions

- "Nothing new happened" is decided by input, not by output: only an operator prompt clears the carry. A peer message
  or autonomous work after the resume does not, which is the cheaper and quieter error.
- A card that went down while running drops any stale carry, so its next wait is new.
- A separate table rather than a task column keeps `taskColumns` and every scan untouched.

## Test plan

## @LETTER@. A resumed card does not ring again

### @LETTER@1. Deploy while a card waits

1. Leave a card waiting on you. Note its waiting time.
2. Deploy, so the card is resumed and its resume turn ends.

**Expected:** the card keeps the earlier waiting time and the phone does not ring for it.

### @LETTER@2. Answer after the resume

1. Answer the resumed card and let it finish.

**Expected:** it waits again with a new time, and that one rings.
