# A board freeze, and a token and tool-call budget that trips it (design)

Status: design only, clint ranks. Written 2026-09-30 by @rnd from item 1 of `docs/rnd/competitor-features.md`.
Nothing is built. Off by default. Owned by @runtime when it is built, with a board control from @ui.

## The ask, in one paragraph

One switch that stops every agent on the board from doing anything more, and a budget that throws the switch, or
shelves one card, when an unattended fleet spends more than the operator meant. Gas Town has an estop, Omnigent has
`cost_budget` and `max_tool_calls_per_session`, and ruflo has a global AI budget. Atrium hid dollars on purpose (item
37), so the budget is in tokens and gated tool calls.

## 1. The freeze

**A new step in the permission chain, between the shelved card (3) and the standing rule (4).** While the freeze is
on, every gated tool call from every card is refused with a reason the model reads as the operator talking:

> the operator has frozen this board: stop, and wait. do not retry this call. your next message comes from the
> operator. (reason: `<why>`, since `<time>`)

Why there, and not somewhere else:

- **After a queued message (2)**, because a message is the operator reaching out, and "here is why I froze you" is
  exactly the message someone sends right after freezing.
- **After a shelved card (3)**, because a shelved card already says no, and its own reason is more specific.
- **Before standing rules (4) and auto mode (5)**, because the whole point is that nothing the operator decided
  earlier keeps running. A freeze that a standing rule could answer would be decoration. This is the same argument
  that put auto mode last, turned the other way: auto mode means "stop asking me", and a freeze means "stop, whatever
  I said before".

Where it lives: `internal/daemon/daemon.go` `onPermRequest`, a check of a daemon setting read once per call from
memory (a setting write updates the cached copy), so the hot path costs one atomic load. Recorded like the shelved
step: `DecidePermissionBy(p.ID, "block", reason, "frozen")`, so the review and the permission history show every call
the freeze refused.

**What it does not stop, said plainly.**

- A tool call that is already running. It stops the NEXT one.
- Tools the gate never sees. The permission hook skips a short read-only list (`internal/cli/hook_permission.go:31-36`,
  skip at `:72`). Those still run, and they still cost tokens. So the freeze stops every change, and a model can
  still read and think until its turn ends. That is acceptable for "stop the fleet doing damage" and is why the
  budget (section 2) also shelves.
- A runner without atrium's gate (codex today, a card with no hooks). The board says so on the freeze banner: "N cards
  are not gated and were not frozen", listing them, so nobody believes a freeze covered what it did not.

**Scope.** One room's freeze is a room setting, `board_frozen`, holding `{on, why, by, at}`. The hub's freeze is the
same setting pushed to every attached room (the way `PushInputLag` pushes the input lag setting,
`internal/link/inputlag.go`) and again to any room that attaches while it is on. A room that cannot be reached is
listed on the banner as "not frozen, not reachable". Unfreezing is the same push with `on: false`.

**How a person sees it.** A red banner across the top of every board view, with the reason, who set it and when, and
one "unfreeze" button. The switch itself sits in the header, behind a confirm, because one misclick stops everything.
Unfreeze needs no confirm. Every frozen card shows a "frozen" badge, never a column change: the freeze is a fact about
the board, not a bucket of attention (the same rule as activity, `docs/runtime/activity-design.md`).

**Unfreeze is not a resume.** Agents blocked by the freeze have been told to wait. Unfreezing sends nothing. The
operator says what happens next, by `atrium_say` or the board's message box, because "carry on" after a freeze is a
decision, and the operator may want to give it card by card.

## 2. The budget

**What is counted.** Two figures per card and per room, over a window.

- **Tokens**: input plus output plus cache writes, from the `session_usage` rows atrium already writes once per turn
  (`internal/daemon/usage.go`, a row at each Stop, `internal/store/usage.go` `AddSessionUsage`). Cache reads are left
  out by default, because they are a tenth of the price and dominate the count (the usage tab hides them by default
  for the same reason, `docs/rnd/usage-tab-design.md`). A setting can include them.
- **Gated tool calls**: the count of permission rows for the card in the window, from the permission table the chain
  already writes. Not every tool call, because the read-only ones never reach atrium (section 1).

**Windows.** A card's budget is per session (from its current SessionStart) and a room's is per rolling 24 hours. Both
are what a person means by "tonight", without a calendar.

**Settings**, all empty (off) by default:

- `budget_card_tokens`, `budget_card_calls`: per card, per session.
- `budget_room_tokens`, `budget_room_calls`: per room, rolling 24 hours.
- `budget_include_cache_reads`: false.
- `budget_action`: `shelve-card` (default) or `freeze`. A card over its own budget is shelved. A room over its budget
  freezes the room, or shelves the card that crossed the line, whichever is set.

**When it is checked.** At two points, both already on a path that runs:

- When a usage row is written (`daemon.go:526-532`, the stamp and emit callback), the card's and the room's sums are
  compared. A turn is the unit, so a card can overshoot by one turn, which the doc for the setting says.
- When a permission row is recorded in `onPermRequest`, the call counts are compared before the chain answers, so the
  call that crosses the line is the one refused.

**What it does at the line.** Shelving a card is a status change the board already has, and the shelved step in the
chain already turns every later call into a standing no with a reason. The reason names the budget: "this card spent
its budget of 2,000,000 tokens this session (2,014,338). the operator has to raise it or unshelve the card." The
launcher gets one notice (claimed in the store per session, the pattern `NoticeContext` uses), so a director learns
its worker stopped for money and not for a bug.

**Near the line.** At 80 percent, one launcher notice and a chip on the card, "80% of budget". No action.

## 3. What is not built

- **Dollars.** Item 37 hid them, and a token budget is what the operator can reason about without a price table that
  goes stale. The usage rows already carry a cost, so a dollar budget is a later setting, not a redesign.
- **A per-card override in the launch dialog.** Tags could carry one later (`atrium:budget-5m`). Not needed to start.
- **Killing a running turn.** The freeze and the budget both stop the next gated call. Ending a turn mid-flight means
  typing into a terminal someone may be using, and atrium does not do that for a policy.

## 4. Schema and settings

No migration. The freeze and the budget are rows in the key-value settings table (`internal/store/settings.go`), and
the counts are sums over tables that exist (`session_usage`, `permission`). If the sums prove slow on a large store,
an index on `(task_id, ended_at)` is the fix, and that would be a migration at the END of the slice.

## 5. Tests

- The freeze step sits after shelved and before rules: a matching standing rule, a card in auto mode and global auto
  mode all still get the freeze refusal. A queued message is still delivered first.
- The hub's freeze reaches every attached room, and a room attaching while it is on is frozen at attach.
- An ungated card is listed on the banner and is not claimed as frozen.
- A card crossing `budget_card_calls` has that call refused and is shelved. The reason names the budget.
- A usage row that crosses `budget_card_tokens` shelves the card, and the next call gets the shelved answer.
- `budget_action: freeze` at the room line freezes the room.
- Cache reads are excluded unless the setting says otherwise.
- The 80 percent notice is sent once per session and survives a restart without repeating.
- Everything off by default: an empty setting never shelves or freezes.

## 6. Open questions for clint

1. The default action when a card crosses its budget: shelve the card (this design), or freeze the room?
2. Are cache reads in or out of the token count by default? This design says out.
3. Should the freeze also stop keep-alive refreshes? Keep-alive is atrium spending, not an agent, and a freeze "for
   money" would want it stopped too. This design says yes, it pauses keep-alive while frozen.
