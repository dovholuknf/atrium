## Test plan

## @LETTER@. A room deploy holds every agent and wakes them after

Needs a room and a hub running this build, and two supervised cards on the room besides the owner's. Set the owner
first, from the hub's machine: `PUT /_hub/deploy-owner` with `{"owner": "<the owner's handle>"}`.

### @LETTER@1. A request with no owner is refused

1. Clear the owner (`{"owner": ""}`), then from a director call `atrium_deploy` with `action: request`.

**Expected:** refused with "no deploy owner is set: ask the human". Nothing is recorded.

### @LETTER@2. Two requests make one say to the owner

1. Set the owner again. From two directors, call `action: request` with different `why`.

**Expected:** the owner is told once. The second answer says "added to the request ... made at ...".
`action: status` lists both.

### @LETTER@3. The hold refuses the next call, and not the owner's

1. From the owner, call `action: start`.
2. On a held card, have the agent run any gated tool.
3. From the owner, run a gated tool.

**Expected:** the held call is refused with `[atrium] room deploy: <room> is being redeployed by <owner> (for: ...)`.
The permission history records it as decided by `deploy-hold`. The owner's call is not held. A worker's
`atrium_deploy` is not in its tool list.

### @LETTER@4. Messages are held and the operator's are not

1. From one held agent, `atrium_say` to another held card.
2. From the board, type a message to the same card.

**Expected:** the say answers `queued` with "held: <room> is being redeployed. it is delivered after the wake". The
board message is delivered during the hold.

### @LETTER@5. `wait`, then the deploy, then one wake each

1. From the owner, `action: wait`. It answers `busy` with a list, or `quiet`.
2. Run the deploy the owner runs today (`scripts/live/deploy-batch.ps1`).

**Expected:** after the room is back, each held card gets one line behind `[atrium] restart wake:` saying
`room deploy done: <room> is on build <new> (was <old>)`, typed once its runner is up and its turn is over. The
held say from @LETTER@4 arrives after that line, not before. A card that had queued its own restart wake gets its
own line and the hold's in one prompt.

### @LETTER@6. The same build coming back

1. Hold the room, then restart it with no binary swap (`restart_atrium`).

**Expected:** the wake says `room deploy did not take: <room> came back on build <old>`.

### @LETTER@7. Called off, and run out

1. Hold the room, then from the owner `action: cancel` with a `why`.
2. Set the room setting `deploy_hold_max` to `1`, hold the room again, and wait two minutes.

**Expected:** 1: every held card gets `room deploy called off by <owner>: <why>` as a message, and `status` shows
the requests waiting again. 2: the same with `room deploy hold ran out after 1 minutes with no redeploy`.

### @LETTER@8. A launch onto a held room

1. While held, launch from a held director, then from the owner.

**Expected:** the director's launch is refused with "<room> is being redeployed: launch after the wake". The
owner's goes ahead.

### @LETTER@9. With no hold, nothing changes

**Expected:** the chain, delivery, launches, keep-alive and idle park behave as before this build.

## For the board (@ui)

The room answers `GET /v1/hold` with `{"room", "build", "hold", "busy": [{card, title}], "quiet", "ungated"}`.
`hold` is null when the room is not held, otherwise `{kind: "deploy", id, by, whys, exempt, from_build,
started_at, expires_at, cards}`. `POST /v1/hold` with `{"action": "lift", "outcome": "operator", "by": "operator"}`
is the lift button. The room sends a `hold` event on its stream, `{"kind": "deploy", "hold": <the hold or null>}`,
whenever it is set or lifted, and republishes every card it touched. Each held card's view carries
`held: {kind, by, since}`, absent when it is not held.

Draw: an amber banner across every board view while `hold` is set, with the reasons (`whys`), who (`by`), since
when, the busy list from `GET /v1/hold`, the `ungated` cards as "not gated and were not held", and one lift button
with no confirm. A `held: deploy` badge on each card with `held`, never a column change. A "request a deploy" item
in the room's menu is not wired yet: a human request has no hub endpoint in this build.

## Where the build differs from the design

- **The startup lift runs before any runner starts**, not after `reopenSaved`. The restart wake is only typed into
  a runner that started after the wake was queued (`wakeRunnerReady`), so a wake queued after the reopen would never
  be typed. Queued first, the reopen's runners are all newer. A card's agents' messages stay held in memory until
  its wake is typed, so nothing types ahead of it.
- **No default owner.** The design names `merge` as the default. @merge is not running, so an unset owner refuses
  rather than telling a card nobody reads.
- **Not built yet:** the `atrium deploy` CLI, a human request from the board, telling the owner after the wake that
  a request is waiting (`status` shows it), and the "this commit is already in this deploy" answer for a request
  made during a hold. The expiry runs on its own 30 second tick rather than the sweep's.
- **The freeze (r-037) is not built.** The hold is stored as a list of kinds so the freeze can be added beside it.
