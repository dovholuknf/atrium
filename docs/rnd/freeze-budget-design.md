# A board freeze, and a token and tool-call budget that trips it (design)

Status: design approved by clint 2026-09-30, build later. Section 6's questions are answered (clint's decision walk,
2026-09-30) and folded in. Written by @rnd from item 1 of `docs/rnd/competitor-features.md`. Nothing is built. Off by
default. Owned by @runtime when it is built, with a board control from @ui. Item r-037.

Section 1 is now the HOLD STEP shared with the room deploy hold (`docs/rnd/room-deploy-hold-design.md`,
rd-new-room-deploy-hold). Both ask the same thing of the permission chain, "stop every agent here at its next step",
and differ only in who lifts it and what the agents hear after. One step, three kinds.

## The ask, in one paragraph

One switch that stops every agent on the board from doing anything more, and a budget that stops one card, or a
card and everything it launched, when an unattended fleet spends more than the operator meant. Gas Town has an
estop, Omnigent has `cost_budget` and `max_tool_calls_per_session`, and ruflo has a global AI budget. Atrium hid
dollars on purpose (item 37), so the budget is in tokens and gated tool calls.

## 1. The hold step (the freeze, and the deploy hold)

**A new step in the permission chain, between the shelved card (3) and the standing rule (4).** While a hold is on,
every gated tool call from a held card is refused with a reason the model reads as the operator talking. The
freeze's words:

> the operator has frozen this board: stop, and wait. do not retry this call. your next message comes from the
> operator. (reason: `<why>`, since `<time>`)

The deploy hold's words are in its own design, section 4.

Why there, and not somewhere else:

- **After a queued message (2)**, because a message is the operator reaching out, and "here is why I froze you" is
  exactly the message someone sends right after freezing.
- **After a shelved card (3)**, because a shelved card already says no, and its own reason is more specific. A card
  the budget stopped is shelved (section 2), so it answers here.
- **Before standing rules (4) and auto mode (5)**, because the whole point is that nothing the operator decided
  earlier keeps running. A hold that a standing rule could answer would be decoration. This is the same argument
  that put auto mode last, turned the other way: auto mode means "stop asking me", and a hold means "stop, whatever
  I said before".

**The kinds.** One room setting, `room_hold`, holding a list of at most one hold per kind:

| Kind | Set by | Held cards | Messages between agents | Lifted by | After the lift |
|---|---|---|---|---|---|
| `freeze` | a human, on the board | every card | delivered as usual | a human | nothing is sent. the operator says what next |
| `deploy` | the deploy owner (its design) | every card but the deployer's and any it names | held, not dropped | the room restarting, the deployer, a human, or the expiry | one wake line per held card |

A freeze and a deploy hold can both be on. The freeze is checked first and its words win, because "stop, whatever
anybody said" outranks "stop for a deploy". Lifting a deploy hold never lifts a freeze.

Where it lives: `internal/daemon/daemon.go` `onPermRequest`, a check of the `room_hold` setting read from memory (a
setting write updates the cached copy), so the hot path costs one atomic load. Recorded like the shelved step:
`DecidePermissionBy(p.ID, "block", reason, "frozen")`, or `"deploy-hold"`, so the review and the permission history
show every call a hold refused.

**What it does not stop, said plainly.**

- A tool call that is already running. It stops the NEXT one.
- Tools the gate never sees. The permission hook skips a short read-only list (`internal/cli/hook_permission.go`).
  Those still run, and they still cost tokens. So a hold stops every change, and a model can still read and think
  until its turn ends. The refusal tells it to end the turn, and in practice it does.
- A runner without atrium's gate (codex today, a card with no hooks). The board says so on the banner: "N cards are
  not gated and were not held", listing them, so nobody believes a hold covered what it did not.

**Scope.** A room's hold is its own `room_hold` setting. The hub's freeze is the same setting pushed to every attached
room (the way `PushInputLag` pushes the input lag setting, `internal/link/inputlag.go`) and again to any room that
attaches while it is on. A room that cannot be reached is listed on the banner as "not frozen, not reachable".
Unfreezing is the same push with `on: false`. A deploy hold is one room's only.

**How a person sees it.** A red banner across the top of every board view for a freeze, amber for a deploy hold,
with the reason, who set it and when, and one lift button. The freeze switch sits in the header, behind a confirm,
because one misclick stops everything. Lifting needs no confirm. Every held card shows a `frozen` or `held: deploy`
badge, never a column change: a hold is a fact about the board, not a bucket of attention (the same rule as activity,
`docs/runtime/activity-design.md`).

**Unfreeze is not a resume.** Agents blocked by the freeze have been told to wait. Unfreezing sends nothing. The
operator says what happens next, by `atrium_say` or the board's message box, because "carry on" after a freeze is a
decision, and the operator may want to give it card by card. The deploy hold is the other way round on purpose: it
is lifted by a machine event, and nobody is at the keyboard to say "carry on" to ten agents.

**Keep-alive pauses while frozen** (clint, answer 3), and for any card a budget stopped. Keep-alive is atrium
spending, not an agent, and a freeze "for money" wants it stopped too. The deploy hold's rule is in its own design.

## 2. The budget

### 2.1 What is counted

Two figures, each with its own optional limit (clint, answer 2). Either one tripping starts the soft stop.

- **Spend**: input plus output plus cache writes, from the `session_usage` rows atrium already writes once per turn
  (`internal/daemon/usage.go`, a row at each Stop, `internal/store/usage.go` `AddSessionUsage`), plus the keep-alive
  rows (`keepalive.go`, cause `keepalive`), since that is spend on the card's behalf.
- **Cache reads**: counted on their own. They are a tenth of the price and dominate the count, which is why they are
  not folded into spend. Whether to limit them at all is chosen when the limit is set.
- **Gated tool calls**: the count of permission rows for the card in the window, from the permission table the chain
  already writes. Not every tool call, because the read-only ones never reach atrium (section 1).

### 2.2 Tree spend (clint, answer 1b)

**Any agent that spawns sub-agents carries their spend on its tally.** Two kinds of child, and both are already on
record, so nothing new is read from disk:

- **Claude's own subagents** (the Agent and Task tools, inside one session). `usage.go` already reads them at the
  parent's Stop, from `<session>/subagents/agent-<id>.jsonl` on current Claude Code and from `isSidechain` records in
  the main transcript on older builds, each file with its own cursor, and writes them as rows of their own with cause
  `subagent`. So a card's own figure is the sum of ALL its rows, `subagent` included. This is the part clint asked the
  design to locate: it is `usage.go` lines 40 to 47 and the reader at `:340`, and it counts each reply once.
- **Atrium launches** (a card started with `atrium_launch`). The edge is `task.spawned_by_id` (migration
  `0055_task_lineage`). A card's TREE is the card plus every card whose `spawned_by_id` chain reaches it, recursively.
  Tree spend is the sum over the tree. A cycle cannot exist, because a card's parent is written once at launch and
  is older than the card, but the walk still stops at 64 levels and at a card seen twice.

The tree is walked in one room's store. A child launched onto ANOTHER room (`atrium_launch` with `room`) records its
parent as `room~id`, and its spend lives in that room's store. Cross-room tree spend is summed by the hub from each
room's per-card figure, in the room-stats push that already carries per-card numbers (r-010), and is stage 2. Until
then the card shows "tree spend (this room)" so nobody reads it as the whole.

**Shown as a `tree spend` line on the card** (details, beside the usage figures), with the card's own figure next to
it. A card with no children shows one line. The 80 percent notice applies to the tree.

### 2.3 Limits and windows

A limit is set per card at launch, or as a room default for cards launched without one:

- `atrium_launch` gains `budget`: `{spend, cache_reads, calls, tree_spend, tree_cache_reads}`, each a token or call
  count, each optional. The launch dialog gets the same fields (@ui). A card keeps what it was launched with.
- Room defaults, all empty (off) by default: `budget_card_spend`, `budget_card_cache_reads`, `budget_card_calls`,
  `budget_tree_spend`, `budget_tree_cache_reads`, and `budget_room_spend`, `budget_room_cache_reads`,
  `budget_room_calls` for the whole room.

**Windows.** A card's own figure is per session (from its current SessionStart). A tree's figure is from the root
card's launch, because a tree is one piece of delegated work and it has no session of its own. A room's is a rolling
24 hours. All three are what a person means by "tonight", without a calendar.

### 2.4 When it is checked

At two points, both already on a path that runs:

- When a usage row is written (the stamp and emit callback in `daemon.go`), the card's, its tree's and the room's
  sums are compared. The tree check walks up from the card to each ancestor that has a tree limit, so a row costs a
  handful of indexed sums. A turn is the unit, so a card can overshoot by one turn, which the setting's text says.
- When a permission row is recorded in `onPermRequest`, the call counts are compared before the chain answers, so the
  call that crosses the line is the one refused.

### 2.5 What happens at the line

**A card over its OWN budget: that card only is stopped** (clint, answer 1). It is shelved, a status change the board
already has, and the shelved step in the chain turns every later call into a standing no with a reason that names
the budget: "this card spent its budget of 2,000,000 tokens this session (2,014,338). the operator has to raise it or
unshelve the card." The launcher gets one notice (claimed in the store per session, the pattern `NoticeContext`
uses), so a director learns its worker stopped for money and not for a bug.

**A tree over ITS budget: the whole tree stops, in two stages** (clint, answer 1c).

- **Soft stop, at the limit.** Every card in the tree gets ONE message, queued the ordinary way, so it rides the
  next tool call or is typed when the card is idle: "[atrium] budget: the work tree under `<root>` spent `<n>` of its
  `<limit>` `<spend|cache reads>` budget. pause work now: finish the step you are in, do not start another, and wait.
  the operator decides what happens next." Nothing is killed and nothing is shelved. The agents are expected to stop.
  The tree is marked `soft-stopped at <time>` with the figure it stood at.
- **Hard stop, for a card that keeps going.** After its soft-stop message was DELIVERED (the message row says when), a
  card that does any of these is hard stopped:
  - makes more than 3 further gated tool calls,
  - ends more than 2 further turns with usage rows (a turn it was already in when the message landed is not counted),
  - or takes the tree past 110 percent of the limit by its own rows.

  The margin is one setting, `budget_hard_margin`, holding those three numbers, so an operator who finds 3 calls too
  tight changes it without a build. A hard stop is three steps, each only if the last did not end it: shelve the card
  (the chain refuses every later call, with the budget reason), ask the runner to leave (the exit action,
  `internal/daemon/actions.go`), and 60 seconds later end its process the way stopping a supervised runner already does
  (`internal/daemon/shutdown.go`). An unsupervised runner cannot be ended by atrium, and the report says so.
- **Every hard stop is reported** to that card's owner, its launcher, and so on up the `spawned_by_id` chain to the
  human: "I had to stop `<card>`, it kept going after the budget warning (`<what it did>`)". The human's copy is a board
  notification and an event on the root card, at once. The copies to AGENTS in the chain are queued and held until the
  tree is resumed, because every one of them is soft-stopped too, and a message typed into an idle soft-stopped agent
  would buy it a turn to read it, which is the spend the budget exists to stop. The operator sees everything now.
  The agents read it when the operator lets them go on.

**Resuming a tree** is the operator raising the limit (or clearing it) on the root card, or unshelving cards one at a
time. Raising the limit clears `soft-stopped` and releases the held reports. Like the freeze, it sends no "carry on":
the operator says that.

**Near the line.** At 80 percent of the tree limit (or a card's own), one notice to the root's launcher and a chip on
the root card, "80% of budget". No action.

**Keep-alive** is off for a shelved card already, and a soft-stopped tree has keep-alive paused on every card in it
(clint, answer 3).

## 3. What is not built

- **Dollars.** Item 37 hid them, and a token budget is what the operator can reason about without a price table that
  goes stale. The usage rows already carry a cost, so a dollar budget is a later setting, not a redesign.
- **Killing a running turn for the soft stop.** The soft stop is a message and the freeze stops the next gated call.
  Only the hard stop ends a process, and only after the card was told and kept going.
- **Cross-room tree spend** (section 2.2), stage 2 through room-stats.

## 4. Schema and settings

One migration, at the END of the slice: `task` gains `budget TEXT NOT NULL DEFAULT ''` (the launch figures as JSON)
and `budget_state TEXT NOT NULL DEFAULT ''` (`soft-stopped` or `hard-stopped`, with when and the figure, as JSON), and
an index `session_usage (task_id, ended_at)` for the sums. The freeze, the deploy hold and the room defaults are rows in
the key-value settings table (`internal/store/settings.go`). Counts are sums over tables that exist (`session_usage`,
`permission`). Soft-stop messages and hard-stop reports are ordinary `message` rows, so the "delivered at" the hard
stop keys on is already recorded.

## 5. Tests

- The hold step sits after shelved and before rules: a matching standing rule, a card in auto mode and global auto
  mode all still get the freeze refusal. A queued message is still delivered first.
- A freeze and a deploy hold both on: the freeze's words, and lifting the deploy hold leaves the freeze on.
- The hub's freeze reaches every attached room, and a room attaching while it is on is frozen at attach.
- An ungated card is listed on the banner and is not claimed as held.
- A card crossing its own `calls` has that call refused and is shelved. The reason names the budget. Its siblings
  and parent are untouched.
- A usage row that crosses a card's own `spend` shelves the card, and the next call gets the shelved answer.
- Cache reads and spend trip separately: a card under its spend limit and over its cache-read limit is stopped.
- Tree spend includes the card's `subagent` rows and every descendant's rows, three levels down, and stops at a card
  seen twice.
- A tree over its limit: every card gets exactly one soft-stop message. None is shelved.
- Hard stop: a card that makes 4 gated calls after its soft-stop message was delivered is shelved, asked to leave, and
  ended after 60s. A card that stopped is not. Calls made before delivery do not count.
- The hard-stop report reaches the human's board at once, and the agents in the chain only after the tree is resumed.
- Raising the root's limit clears `soft-stopped` and releases the held reports, and sends no other message.
- Keep-alive does not refresh a frozen card or any card in a soft-stopped tree.
- The 80 percent notice is sent once per session (or once per tree) and survives a restart without repeating.
- Everything off by default: an empty setting and a launch with no `budget` never shelve, soft stop or freeze.

## 6. Clint's answers (2026-09-30), and where each went

1. A card over its own budget: stop THAT card only, shelve it. Section 2.5.
   - 1b. Any agent that spawns sub-agents carries their spend, both atrium launches and Claude's own subagents, shown
     as a `tree spend` line, with the 80 percent notice on the tree. Section 2.2.
   - 1c. Over the tree budget the whole tree stops, soft then hard, and every hard stop is reported up the chain.
     Section 2.5. The margin the design sets is 3 calls, 2 turns or 110 percent, in one setting.
2. Cache reads: a choice made when the limit is set, as two separate optional limits. Section 2.1 and 2.3.
3. Keep-alive pauses while frozen and for a card a budget stopped. Sections 1 and 2.5.
</content>
</invoke>
<invoke name="Bash">
<parameter name="command">ls docs/rnd | head -50