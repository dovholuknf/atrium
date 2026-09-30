# r-new-new-context-mid-turn: a new context on a card that never leaves running

Status: TOP PRIORITY for @runtime (clint, 2026-09-30 16:35). Build, then @review.

## What happened

clint pressed new context on three directors at about 16:31. @rnd cleared at once. @ui sat at the capture step for
several minutes before it found a gap. @runtime, at 681k context, never left `running`, so the capture prompt was
never typed. It would have failed only after `captureEnd` (15 minutes) with nothing done.

Two things make that a deadlock, not just a wait:

1. `ncType` types only between turns: no turn in progress, card status not `running`, `turnSettle` quiet (r-022). A
   director driving workers, watchers and background tasks can stay in one turn, or in back-to-back turns with no
   2 s gap, for as long as its work lasts. The bigger the context, the more likely, and those are the cards that
   most need the cycle.
2. `holdingMessages` holds every delivery path from `begin` until the wake. So a message telling the card "end your
   turn so the cycle can run" is held by the cycle it is trying to unblock. The orchestrator hit exactly this at
   16:34 and had to DELETE the chip to get the message through.

## Wanted

- **A stop request when the gap does not come.** If the capture has not been typed after about 60 s because the card
  is running, type a short labelled line through the same path an immediate `atrium_say` uses (it already types
  mid-turn once the input line is empty): "atrium: a new context is waiting. Finish the step you are on, commit,
  and end your turn." The runner reads it at its next step. The capture prompt itself is still typed only between
  turns, so r-022's merge hazard does not come back.
- **The stop request is not held.** It is the cycle's own typing (`ncType` path), like the capture, so
  `holdingMessages` does not apply. Other messages stay held as now.
- **Say it on the chip.** "capture: waiting for the turn to end, asked the card to stop at HH:MM", so the board
  shows why it is waiting, and that it is not stuck.
- **A second nudge, not a loop.** If the card is still running at half of `captureEnd`, type it once more. Then wait
  out `captureEnd` and fail as now, with the reason naming both nudges.
- **The same for the wake** when the new session starts a turn before the wake is typed, and for the automatic
  threshold cycle and idle parking, which share `ncType`.

## How to check

A test daemon with a fake runner that stays `running`: the stop line is typed after the delay, it is not held, the
capture is typed only after the fake turn ends, and a card that never stops fails at `captureEnd` with both nudges in
the reason. Then by hand on a director mid-turn.

## Related

- r-new-lean-context-cycle (parked): skip the capture turn when the handoff is fresh, identity in the wake.
- r-022: why the capture is never typed mid-turn.
