# r-new-context-clear-vs-restart. A restart during a context clear leaves the clear stuck

Status: not started. Owned by @runtime. Filed by the orchestrator 2026-10-01, from clint's report.

## What happened

2026-10-01 ~07:47, clint started a context clear on the orchestrator card (01a0f2da, room sg4-control). A hub
deploy ran at the same time (8033295b, built and installed by the orchestrator). The clear's progress stuck at
step 1 of 3 and never finished. clint had to clear the card a second time by hand.

## Second case, no restart: an idle card

2026-10-01 07:50:45, clint started a new context on @card-audit (sg4-control 01a0f404). The card sat idle at its
prompt, last turn done at 18:10 the day before. GET new-context showed step 1 of 3, `capture`, and the capture
prompt was not typed. A second start answered 409 "a new context is already under way on this card", with no way
on the board to see why or to cancel it. clint typed "a", the card answered it, and only after that turn ended
did the capture prompt arrive. So step 1 waits for a turn end, and an idle card has no turn left to end. This
case may also explain the first one, since the restart and the clear overlapped only by chance.

## Wanted

0. A card already idle at its prompt gets the capture prompt at once. The 409 names the step it is stuck on and
   for how long, and the board offers cancel (the DELETE that exists).


0a. While a new context runs, the card's terminal refuses typed input from the board and the phone. The terminal
   shows a strip naming the step (1 of 3, capture) with a cancel button, which is the DELETE. clint typed into
   @card-audit mid-clear twice, and his text landed in the turn the capture was waiting on. Board half goes to @ui
   once the daemon reports the state on the card, which `new_context` on the task already does.
1. Find out which half owns the three steps (hub proxy, room daemon, or the board) and why a hub restart strands
   them. A restart mid-clear must finish the clear or abandon it with the reason shown. It must never leave the
   card showing a step forever.
2. Deploys check for a card mid-clear first. `deploy-when-idle.ps1` and the hub deploy wait for in-flight
   clears to finish, the same way they wait for idle runners. Same rule for `deploy-ready`'s one-click deploy.

## Open for the design

- Is a clear stored anywhere, or only held in memory by the process that started it? If only in memory, (2) is
  the whole fix and (1) is the wording the board shows when it finds a half-done one.
