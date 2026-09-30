# Room deploy hold: one call asks for a deploy, every agent holds, the room redeploys, every agent resumes

Status: design, 2026-09-30, @rnd. Item `docs/backlog/rnd/rd-new-room-deploy-hold.md`. Built by @runtime (room and
hub), @ui draws the banner. Shares the hold step with r-037 (`docs/rnd/freeze-budget-design.md` section 1), and is
built on it: whichever is built first builds the step, and the second adds its kind. Replaces "wait until the room is
idle for 10 seconds", which almost never fires with ten agents.

## 1. The answer in six lines

1. A director calls `atrium_deploy request` once. The hub records it and tells the deploy owner (@merge) once.
2. When @merge is ready to build, it calls `atrium_deploy start`. The room sets a `deploy` hold: every gated call from
   every card but @merge's is refused with "end your turn and wait", and messages between agents are held, not dropped.
3. @merge calls `atrium_deploy wait`, one long tool call that returns when the room is quiet. It burns nothing while
   blocked, the same way the Mode A agent loop does.
4. @merge runs the deploy it runs today (`scripts/live/deploy-batch.ps1`, detached).
5. The new room finds the hold in its store, lifts it once it has reopened its cards, and types ONE wake line into
   each held card. Held messages flow after it.
6. If the deploy is called off, reverted, or never happens, the hold is lifted by @merge, by the room noticing the
   same build came back, by a human on the board, or by its expiry. Every agent hears one line saying which.

**A held agent costs one short turn: it reads the refusal and ends its turn.** Then it is idle, which costs nothing.
The wake is one line. There is no polling turn, no "still waiting", and no chat relay to the CTO.

## 2. Why the hold REFUSES and does not PARK inside the hook

The item suggested the zero-token wait atrium already has: a blocked PreToolUse hook, with the model parked inside
one tool call. It does not survive a deploy, for three reasons found in the code:

- **The permission hook fails open when atrium is unreachable** (daemon resilience rule 2). A hook parked across the
  room's own restart loses its connection when the daemon winds down, fails open, and RUNS the held call during the
  deploy, in the ten seconds `shutdown.go` gives supervised runners before they end.
- **A hook has a timeout.** f-011's decision was 30 seconds then fail open. A deploy takes minutes, so a parked hook
  would time out and fail open long before the lift.
- **Without f-011 the runner dies at the restart anyway**, and the parked call dies with it. Nothing was saved by
  parking, and a tool call interrupted mid-flight is worse than one never started.

A refusal the model reads as "stop and end your turn" puts every agent in the one state that survives both kinds of
restart: idle, turn over, nothing in flight. That state costs zero tokens for as long as it lasts, which is the
constraint. The one short turn it costs is the price of that safety, and it is paid once per agent.

## 3. Does it need f-011 (`atrium ptyhost`)?

**No.** It works on today's room, and it gets cheaper with f-011.

- **Without f-011 (today):** the restart ends every runner, and `reopenSaved` resumes them on the new daemon
  (`internal/daemon/reopen.go`), which is what every deploy does now. The hold's gain is that nothing was mid-call
  when that happened, and every agent is told what happened instead of sitting idle until a human types "we up". The
  wake is a restart wake (`internal/daemon/restartwake.go`): typed once the runner is back, the input line is empty
  and the turn is over.
- **With f-011:** the runners outlive the daemon, so nothing is resumed and no context is re-read. The wake is then an
  ordinary queued atrium message, typed when the card is idle. The lift chooses per card: a card whose runner this
  daemon reopened gets a restart wake, and a card whose runner survived gets a message. Same text either way.

## 4. What a held agent reads

The refusal, from the hold step (freeze-budget section 1), recorded as decided by `deploy-hold`:

> [atrium] room deploy: `<room>` is being redeployed by `<deployer>` (for: `<why>`). end your turn now and wait. do
> not retry this call and do not poll. one message will say when the room is back, and this call will not have run.

The wake, typed or queued once per held card after the lift:

> [atrium] room deploy done: `<room>` is on build `<new>` (was `<old>`), for: `<why>`. messages are flowing again.
> resume your work. a call the hold refused did not run: run it again if you still need it.

Other outcomes change the first sentence only (section 7). Both lines are bounded to 600 bytes, with the reasons cut
to fit, and carry the atrium label so no model reads them as the operator's words.

## 5. The surface

One MCP tool, `atrium_deploy`, full class only (workers ask their director), served by the hub:

| Action | Who | What it does |
|---|---|---|
| `request {room?, why, sha?}` | any full-class card, or a human on the board | records a request for the room (default: the caller's). tells the deploy owner once |
| `start {room}` | the deploy owner, or a human | sets the hold on the room. returns at once with who is busy |
| `wait {room, max_seconds?}` | the deploy owner | blocks until the room is quiet, at most 600 seconds, then answers `quiet` or the busy list |
| `cancel {room, why}` | the deploy owner, or a human | lifts the hold with no restart |
| `status {room?}` | anyone full class | the pending request, the hold, who is busy |

`atrium deploy <action>` is the same, as a CLI for scripts and for a human at a terminal.

**The deploy owner** is a hub setting, `deploy_owner`, holding a handle (default `merge`), resolved at delivery the
way `report_to` is, because @merge's card id changes when it is relaunched. With no owner set, `request` is refused
with "no deploy owner is set: ask the human", rather than guessing. The owner's own card is never held by its own
hold, and `start` may name up to three more `exempt` cards (a card the deploy itself needs, like a build watcher).

**The request is on the hub** (a `hub_setting` row per room, `deploy_request:<room>`, holding the list of
`{by, why, sha, at}`), because the hub outlives the room and the owner may be on another room. **The hold is on the
room** (the `room_hold` setting, kind `deploy`), because the room has to find it again after it restarts, and the
permission chain that enforces it runs in the room. It holds `{id, by, whys, exempt, from_build, started_at,
expires_at, cards}`, where `cards` is the list of cards held at `start`, the list the wake goes to.

## 6. The questions the item asked

**What the request looks like, and who may make it.** Section 5: one tool call, or one CLI line, from any full-class
card or a human. A worker cannot, because a worker does not decide that a room goes down. It asks its director.

**A request while one is pending, and two directors at once.** Requests for a room coalesce. The first tells the
owner, and the answer says "requested, `<owner>` deploys it. keep working: you will be held when it starts and woken
after". A second request while one is pending is added to the same list, silently for the owner (who sees every
reason at `start`), and answered "added to the request `<first>` made at `<time>`". Two at once are two appends under
the hub's store lock, so one of them is first and the other is added.

**A request while a hold is in force.** It is recorded as the NEXT request, not folded into the running deploy,
because the build may already be made. The answer says so, and when the request carries `sha` and the hold recorded
the integration branch tip at `start`, it also says whether that commit is already in this deploy
(`git merge-base --is-ancestor`, the check item dependencies uses), so a director does not ask for a deploy it is
about to get. After the lift, the owner is told once that a request is pending.

**An agent in the middle of a long tool call (a 10-minute `go test`).** It is not interrupted. The hold stops its
NEXT gated call, so the test finishes, the model reads its result, and its next gated call is refused. `wait` counts
it as busy until then, using the room's existing busy rule (`busyRoomAgents` in `internal/cli/roomrestart.go`: a
supervised card with fresh activity that is not waiting on a human). If `wait` runs out with cards still busy, the
owner chooses: `wait` again, or go ahead, in which case the restart ends the test and the wake says the call did not
run. No new policy is invented: this is `restart_atrium`'s `force`, made explicit.

**The deploy fails. Who lifts, and what do agents hear?** Section 7.

**Humans' own cards.** Held, the same as agents' cards, with a banner on the board. Three reasons. The room going down
ends a human's supervised terminal too, so a card left working would be cut off mid-call anyway. The hold refuses
only TOOL CALLS, so a person typing into a terminal is never blocked, and what they type is delivered. And the person
running the deploy can put a card in `exempt`. Operator messages typed on the board to a held card are not held:
only messages from other AGENTS are, because the operator reaching out is exactly what must still work.

## 7. Every way a hold ends

| How | Who notices | The wake's first sentence |
|---|---|---|
| The room restarts on a new build | the new room, at startup | `room deploy done: <room> is on build <new> (was <old>)` |
| The room restarts on the SAME build (a revert, or a restart with no swap) | the new room, comparing `from_build` | `room deploy did not take: <room> came back on build <old>. the deploy may have been reverted` |
| The owner calls it off before any restart | `cancel` | `room deploy called off by <who>: <why>` |
| Nobody restarts the room in time | the room, at `expires_at` (default 60 minutes after `start`, setting `deploy_hold_max`) | `room deploy hold ran out after 60 minutes with no redeploy` |
| A human lifts it on the board | the board | `room deploy hold lifted by the operator` |

**The lift at startup** runs after `reopenSaved` has queued every card it reopens, not the moment the store opens,
because a restart wake needs its card's runner to come back, and the restart wake's own gate (runner up, empty line,
turn over) takes it from there. The lift is one transaction: it clears the hold, queues the wakes, and writes a
`deploy-hold-lifted` event on each held card, so a second startup never wakes anyone twice. A card whose own restart
wake is already queued (`atrium_wake_after_restart`) keeps its own line, and the hold's line is appended to it, so
the card is typed into once.

The requesters are told the outcome inside the same wake (they were held too). The hub marks the request done, or
failed with the outcome, and a request made during the hold becomes the pending one.

## 8. What else the hold does while it is on

- **Messages between agents are held.** `holdingMessages` (`internal/daemon/newcontext.go`), the predicate every
  delivery path already asks, also answers true for a held card and a message whose sender is not the operator. The
  sender's `atrium_say` answers `queued` with the note "held: `<room>` is being redeployed. it is delivered after the
  wake". Held messages go through `releaseHeld` after the wake, as a new-context cycle's do, so nothing types ahead of
  the wake line. Messages from another room to a held card reach the room and wait there, as any queued say does.
- **Launches onto the held room are refused** with "`<room>` is being redeployed: launch after the wake". The owner may
  still launch, since it is not held. A launch would otherwise start a runner that the restart kills a minute later.
- **Keep-alive pauses on held cards without f-011**, because the restart ends the runner and the resume re-reads its
  context anyway, so a refresh buys nothing. With f-011 host on, keep-alive runs as usual during the hold: the runner
  survives, and a warm cache is what makes its wake cheap.
- **The idle-park sweep and the merged-cull sweep skip held cards**, so the deploy is not what parks or culls them.

## 9. Stage 1, what @runtime builds

Room side (`internal/daemon`):

- The hold step, kind `deploy`, in `onPermRequest` after shelved (built with the freeze if r-037 lands first).
- `room_hold` in settings, read from memory on the hot path. `POST /v1/hold` (set, lift) and `GET /v1/hold` (the hold,
  plus the busy list from the same rule `busyRoomAgents` uses, moved from `internal/cli` into `internal/daemon` so
  both call one function) on the human listener, reached by the hub through its proxy.
- `holdingMessages` widened, the launch refusal, keep-alive and sweep skips (section 8).
- The lift at startup (section 7), the expiry on the existing sweep tick, and the wake choice (restart wake or queued
  message) by whether this daemon reopened the card's runner.

Hub side (`internal/link`, `internal/hubstore`):

- `atrium_deploy` on the control MCP, full class, audited like `restart_atrium` (`ctl-deploy`), and the CLI.
- `deploy_owner` and `deploy_request:<room>` hub settings. The one say to the owner uses the existing say path.
- `wait` polls the room's `GET /v1/hold` every 5 seconds on the long client, at most 600 seconds.

@ui: the amber banner with the reasons, who, when, the busy list and a lift button, a `held: deploy` badge on each held
card, and a "request a deploy" item in the room's menu for a human.

`scripts/live/deploy-batch.ps1` needs no change: it restarts the room, and the room lifts its own hold. The owner's
brief gains four lines: `start`, `wait`, queue its own wake, run the script.

## 10. Tests

- Request coalescing: two requests make one say to the owner and one list. A request during a hold is the next one,
  and one carrying a sha already in the deploy is told so.
- `request` with no owner set is refused. A worker-class caller does not see the tool.
- The hold step: a held card's gated call is refused with the deploy words and recorded `deploy-hold`. The owner's
  and an exempt card's calls pass. A freeze on top gives the freeze's words.
- Messages: an agent's say to a held card is queued with the held note and delivered after the wake. An operator's
  board message is delivered during the hold.
- `wait` answers `quiet` when nothing is busy, and the busy list at its bound. A card running a long tool call is
  busy until its next gated call is refused.
- Startup lift on a new build, on the same build, twice in a row (the second is a no-op), and a card with its own
  restart wake gets one combined line.
- Expiry lifts with its sentence. `cancel` lifts with no restart and wakes by message.
- A launch onto a held room is refused, and the owner's launch is not.
- With the hold off, nothing changes: the chain, delivery, launches and keep-alive all behave as today.

## 11. Not in this design

- Holding a room for anything but a deploy. That is the freeze, which a human sets.
- Deploying several rooms in one hold. A deploy is per room today and `deploy-batch.ps1` restarts one room. A
  fleet-wide deploy is a request per room, run in turn by the owner.
- Choosing WHEN to deploy. The owner decides when to call `start`. This design only makes that moment safe and cheap.
</content>
</invoke>
