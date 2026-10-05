# u-new-quiet-subagents-not-origin. Quiet the subagents, not everything an agent launched

Status: not started. Owned by @ui. Filed by the orchestrator 2026-10-01, from clint.

## What happened

clint left the window on purpose to test an alert from the orchestrator card and got no toast, no desktop
notification, no sound. The card carried `origin:agent` because an agent had launched it (a context cycle), and the
"quiet doers" preference (`sounds.js`, `quietDoers: true`, `quietDoer` in `notify.js`) silences every card with that
tag. The orchestrator and the directors are agent-launched but they are the cards he talks to.

clint: "it should be more like 'atrium:subagent' do not notify."

## Wanted

- The quiet rule keys on `atrium:subagent` (SubagentTag, the tag the launch cap counts), not `origin:agent`. A card an
  agent launched as a resident (director, orchestrator) notifies. A worker tagged `atrium:subagent` stays quiet.
- Permission requests still notify from every card, as today. A card with a tone of its own still notifies.
- The settings text beside the checkbox (`index.html` ~1684, which names `origin:agent`) says the new rule.
- The terminals "subagents" hide toggle uses `isDoer` (origin:agent). Say in the design whether it moves to the same
  tag, so "subagent" means one thing on the board.

## Undo after it lands

The orchestrator card (sg4-control 01a0f2da) had `origin:agent` and `atrium:subagent` removed by hand on 2026-10-01
to get alerts. Put `origin:agent` back once this lands. Leave `atrium:subagent` off, it is not a worker.

## Design note

- New `isSubagent` in `js/cardrules.js` matches `atrium:subagent` (trimmed, case-insensitive). `isDoer` stays as the
  `origin:agent` test and still drives `agentIdle`, which is about who launched a card.
- `quietDoer` in `notify.js` uses `isSubagent`. Permission, blocker and own-tone exceptions are unchanged.
- The terminals "subagents" hide toggle (`sessionHiddenBy` and the counts) and the phone's hide subagents filter also
  moved to `isSubagent`, so "subagent" means one thing. The phone's director and orchestrator exclusions stay.
- The setting is still stored as `quietDoers`, so nobody's choice is lost. Only the label and hint text changed.
