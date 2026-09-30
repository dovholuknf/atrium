# rd-new-room-deploy-hold. "Room deploy needed": every agent holds, the room redeploys, every agent resumes

Status: not started. Owned by @rnd, design today (clint, 2026-09-30). Replaces the restart-window idea, which clint
rejected, and the "wait until everything is idle for 10 seconds" rule, which almost never fires with ten agents.

## What clint wants

1. An agent (usually a director) says "room deploy needed" through atrium. One call, no chat relay to the CTO.
2. Atrium holds all work on that room. Agent-to-agent messages are held too, not dropped. Every agent stops at its
   next step and waits.
3. The deploy agent (@merge, or whoever holds the deploy) builds and redeploys the room.
4. Every agent wakes with one short message: the room was redeployed, messages are flowing again, resume the work.

## The constraint: token efficient

A held agent must burn nothing while it waits. No polling turns, no narration, no "still waiting" messages. The
wake is one message per agent, not a conversation. The request is one call, not a chat thread.

## Pieces that already exist (read before designing)

- The permission chain (`internal/daemon/daemon.go`, `onPermRequest`). A hook that blocks is the zero-token wait
  atrium already has: the model is parked inside one tool call. A "deploy hold" answer could sit early in the chain.
  Find out whether the hold belongs there, and where in the order.
- A room restart ends every pty today, so a hook blocked inside it dies with the runner. f-011 (`atrium ptyhost`,
  stage 1 built on claude/terminal) keeps runners alive across a daemon restart. Say whether this item needs f-011,
  and what it does without it (park, then `atrium_wake_after_restart`).
- The peer bus holds messages already (queued says, `held` across rooms). Say how the hold uses it.
- r-037, the board freeze and budget (`docs/rnd/freeze-budget-design.md`), is the same "all agents stop" half. Merge
  the two designs or say why not.
- `atrium_wake_after_restart`, parking, `scripts/live/deploy-batch.ps1`.

## Questions the design answers

- What does the request look like (an `atrium` command, a control MCP tool, or both), and who may make it?
- What happens to a request while a hold is already in force, and to two directors asking at once?
- What does an agent in the middle of a long tool call (a 10-minute `go test`) see?
- What if the deploy fails? Who lifts the hold, and what do the agents hear?
- Humans' own cards: held or not?
