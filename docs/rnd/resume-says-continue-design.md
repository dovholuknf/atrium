# Resume says continue: a card the restart interrupted is told to carry on, and an idle card is left alone

Status: built. A card the restart interrupted is told to carry on, and the card shows it is opening
(`internal/api/web/js/resume-opening.js`).

Origin: design, 2026-09-30, @rnd. Item `docs/backlog/runtime/r-new-resume-says-continue.md`. Built by @runtime with
the escalation stages (`docs/rnd/held-message-escalation-design.md`). Nothing here is built.

## 1. Most of this is built, and the 13:22 deploy used none of it

**A card mid-turn at a plain restart is already told to continue.** The unexpected-exit notice (995ca76c,
`internal/daemon/unexpectedexit.go`) reads each supervised card's status in the wind-down, before any runner is
stopped. `running` or `needs-permission` means mid-turn. It queues a restart-wake row, and the new room types it once
the runner is at its prompt. A card in `needs-input` gets nothing. It is on by default, `unexpected_exit` is the off
switch, and it has fired on every deploy that had a mid-turn card: 09-28 11:19, 09-29 17:05 and 21:11, 09-30 09:13
(`room.err.*`).

**At 13:21:47 no card was mid-turn.** The pre-deploy binary (0b4e3f0) contains the notice, and its log has no "was
mid-turn" line. Every director and worker had already ended its turn. That fits the deploy routine: workers are told by
a say to commit and wait (the "park workers before a deploy" practice), they end their turns, and then
`deploy-batch.ps1` restarts the room. Those cards were idle by status, so the notice rightly skipped them, and nothing
else was going to wake them.

**The deploy hold covers exactly that case, and was not used.** `atrium_deploy start` sets a `deploy` hold. Every card's
next gated call is refused with "end your turn and wait", and the new room lifts the hold at startup and types one wake
line into each held card (`liftAtStartup`, `internal/daemon/roomhold.go`). The 13:22 log has no hold line.
`deploy-batch.ps1` ran without one.

So the gap is small: the deploy script can restart a room with no hold, and the hold wakes every card in it,
including one that was idle waiting for clint.

## 2. How the room knows a card was mid-turn

**By its status at the moment the stop began**, which is the rule the notice already uses. `running` is a prompt with
no Stop yet, and `needs-permission` is a turn blocked on a dialog the restart takes away. "When the stop began" means:

- **a plain restart**: the first step of the wind-down (`noteStopMidTurn`), as today.
- **a held deploy**: the moment the hold is SET, not the wind-down. By the wind-down every held card has ended its turn
  because the hold told it to, so reading it then would find everyone idle. That is the 13:22 failure in another shape.

## 3. The change, two stages

**R5, the hold remembers who was working.** When a `deploy` hold is set, it records, per card in the hold, whether that
card was `running` or `needs-permission` at that moment (a `working` set on the hold row, beside `Cards`).
`liftAtStartup` types its wake line only into cards in that set. The others were waiting for a human, and they still
are. The wake text ends with "check `git status` first". The switch is the existing `unexpected_exit` setting, so one
switch turns off both ways a restart hands a turn back. Each card woken gets a `resume-continue` event on its timeline,
which is what the board shows ("resumed, told to continue"). A card still in the hold's refusal path, whose last gated
call was refused, counts as working: the hold is what ended its turn.

*Acceptance test.* Set a deploy hold with one card `running` and one in `needs-input`. The running card ends its turn
on the refusal. Restart the room on a new build. Only the first card gets the wake line, once, with the
`resume-continue` event, and a second startup types nothing. With `unexpected_exit` off, neither gets a line. The plain-restart test the
item asks for (one card mid-turn, one idle, no hold) already passes on the unexpected-exit notice and is kept as a
regression.

**R6, the deploy script takes the hold.** `scripts/live/deploy-batch.ps1` sets a `deploy` hold on the room through
`POST /v1/hold` before it builds the restart, and waits for the room to go quiet the way `atrium_deploy wait` does, up
to a bound. `-NoHold` skips it, and says in the log that nothing will be woken. The hold's own wake then replaces the
"commit and wait" say. The orchestrator's deploy routine stops sending that say, because a card told to wait by a say is
idle at the hold and correctly not woken.

*Acceptance test.* A `deploy-batch.ps1` run with one card mid-turn leaves a hold row, and after the restart that card is
woken. The same run with `-NoHold` logs the warning and wakes nobody but a card mid-turn at the wind-down.

## 4. Does it share a stage with escalation R1?

**No shared code, one shared rule.** R1 changes which held messages the permission chain carries mid-turn. R5 changes
which cards the hold's startup lift wakes. They ride the same room deploy. The one coupling: after a startup lift, a
woken card's agents' messages wait until its wake has been typed (`awaitingWake`, bounded at 30 minutes). R1's
15-minute rule must not carry an old held message ahead of that wake. So R1's filter also checks `awaitingWake` and
keeps waiting while it is true. That is one line in R1, and it is written into R1's test.

## 5. The build

| order | stage | owner | size | deploy |
| --- | --- | --- | --- | --- |
| with R1 | R5, the hold's `working` set, the lift wakes only those, the event | @runtime | S | room |
| after R5 | R6, `deploy-batch.ps1` takes the hold, `-NoHold` | @runtime | S | none, a script |

## 6. Questions for later

Parked in the day's rnd queue file. The build takes the defaults.

1. **One off switch for both.** `unexpected_exit` turns off the plain-restart notice and the hold's continue line
   together. Does clint want them separately?
2. **The "commit and wait" say before a deploy goes.** The hold replaces it. That changes how the orchestrator runs a
   deploy, which is the orchestrator's routine to change, not atrium's.
