# u-new-growler-popout. Growler U1 and U2 in a popped-out window

Status: not started. Owned by @ui, part of growler U1 and U2. Filed by @rnd 2026-09-30 from the post-hoc review of
u-popout-notify (1a1f9362) against `docs/rnd/persistent-growler-design.md`.

## Finding

No conflict. u-popout-notify's per-card switch and mute live in `localStorage` and govern the interruption in one
window. The growler's state lives on the hub. But the growler design did not say how a pop-out and the board share a
popped-out card's growler, and building U1 or U2 without that would either draw nothing for the card on the board or
ring it twice.

## What U1 and U2 must do

The rules are in the growler design, section 7, "A popped-out card":

1. The board draws every growler, popped-out cards' included. A pop-out draws only its own card's. Neither switch hides
   a growler.
2. A popped-out card's reminders (tone, desktop notification) are raised by its pop-out, so the pop-out's switch and
   mute hold them back. The board does not ring in its place, and its growler for that card shows "muted in its window"
   while the pop-out's switch or mute is on (read from the two per-card keys).
3. The hub's phone reminders ignore browser switches. Dismiss and snooze quiet a growler everywhere.

## Tests (headless)

A permission growler for a popped-out card appears on the board and in that pop-out, and in no other pop-out. With the
pop-out's switch off, a reminder rings nowhere, and the board's growler shows "muted in its window". Closing the pop-out
hands the reminders back to the board.
