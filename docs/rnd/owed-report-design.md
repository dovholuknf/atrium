# A card owes its launcher a report only for a prompt its launcher sent

Backlog item 41 (`docs/backlog-2.md`). Design first, as that item asks.

## The problem

A launched card owes its launcher a report when it has been given something to do since it last told the launcher
anything (`Task.OwesReport`). "Given something to do" is `prompted_at`, which `appendEventOn` stamps for EVERY
`prompted` event, whoever caused it. So when the orchestrator launched saorch, a resident merger, each report saorch's
own workers sent it made saorch owe the orchestrator a report. Both turns ended without one, and the orchestrator was
told twice, seven seconds apart, at a full turn over a large context each time.

That model fits a one-shot worker, whose only prompts are its launcher's. It does not fit a resident session, whose
prompts mostly come from sessions it launched itself.

## The rule

A card owes its launcher a report only for a prompt the LAUNCHER sent:

- its opening prompt, given at launch, and
- a message from the launcher (`atrium_say`, `atrium tell`), typed, queued, or carried in by a hook.

Nothing else creates a debt: not a message from a third session, not the operator's prompt or note or action, not
atrium's own restart wake or exit notice, and not a turn the session's own background task or monitor woke.

The board's STUCK escalation (`stuckNow` / `stoppedSilently`) follows the SAME rule, deliberately. Both read
`OwesReport`, so there is one definition of "owes" and the badge and the notice cannot disagree. Something stuck is
something the launcher is waiting on. A card nobody is waiting on is not stuck by being quiet.

## What is recorded about the sender, and where

Every door that records a prompt already writes the sender into the event payload as `from_peer` (a wire name), and
leaves it out for the operator. The launch's opening prompt now does the same, naming the session that asked for the
launch (`req.SpawnedBy`) and leaving it out when the operator did. That matters because a launch also runs onto an
existing card (a reopen or a resume with a prompt), where the recorded launcher, written once, is not necessarily who
is asking. `via: "launch"` alone therefore counts for nothing (Mercurius round 1, finding C1). Nothing else is needed
at any door, which is the point: the existing comment on `appendEventOn` asks that "every path that records a prompt stamps it, including ones
written after this", and a rule enforced by each door is a rule the next door forgets.

So the decision is made once, in the store, where the stamp is. A new column `task.owed_at` holds the last prompt
that counted. `prompted_at` keeps its meaning (the last prompt from anyone) and stays truthful for whatever reads it
later. `OwesReport` and `PromptKey` move to `owed_at`.

`appendEventOn`, for a `prompted` event, decides whether it counts:

1. `from_peer` empty: does not count. That is the operator, a note, an action, a wake, a reopen by the operator.
2. `from_peer` names the card's launcher: counts. Compared three ways, because the launcher is recorded by wire name
   (`spawned_by`, qualified by tenant), by card id (`spawned_by_id`), and for a launcher on another room as
   `name@room`: the sender qualified against `spawned_by`, the sender against the wire name of the card
   `spawned_by_id` names, and the sender against `spawned_by` case-insensitively.
3. Anything else: does not count.

Why stored, not in memory: the debt has to survive a daemon restart or a restart would forgive everything owed, and
`docs/runtime/activity-design.md` forbids storing only what a runner is doing RIGHT NOW. A debt is a fact about a
conversation, so it is stored, like `reported_at`.

Why the store rather than the daemon deciding and passing a flag: a flag is one more argument at nine call sites, and
`queueMessage` writes its own event inside the store. The store has the card's lineage in the same transaction.

## A migration: `0068_owed_at`

`ALTER TABLE task ADD COLUMN owed_at TEXT NOT NULL DEFAULT ''`, then a backfill `UPDATE task SET owed_at =
prompted_at WHERE owed_at = '' AND prompted_at != ''`. Both tolerate running twice (the runner swallows the duplicate
column, and the backfill only fills empties). Added at the END of the slice.

The backfill means a card that owes today still owes after the upgrade, on the old, wider basis. It stops owing when
it reports or when the launcher next prompts it. Without it every open debt would be silently forgiven at upgrade,
which is the wrong direction to be wrong in for a watchdog. The alternative, reusing `prompted_at` and narrowing when
it is stamped, needs no migration but leaves a column named for one thing holding another.

## Told apart: a turn woken by a background task or a monitor

No detection is needed, and none is attempted. A monitor event or a finished background task wakes the session
inside Claude Code. Atrium is told only that a turn started (UserPromptSubmit) and later ended (Stop). No door
records a `prompted` event for it, so `owed_at` does not move.

What that means in each case:

- The card owes nothing (it reported after its launcher's last prompt): the woken turn cannot make it owe. No notice,
  no STUCK. This is the sa83 case.
- The card does owe (the launcher prompted it and it has not reported): the debt is real and predates the wake. The
  notice is keyed on `PromptKey()`, which is `owed_at`, so the woken turns share one key and the launcher is told
  once, not once per wake. Item 31's hold (`backgroundWork`) still defers even that one notice while the last Stop
  named running background work.

The UserPromptSubmit hook is NOT used to decide. It cannot say who sent the prompt: a keystroke by the operator, an
atrium-typed message and a monitor wake all arrive as the same event, and guessing from timing would be exactly the
guess `promptWasPeer` already makes for a different purpose. Deriving the debt from atrium's own record of who sent
what avoids the guess.

A message from the card's own session to itself (a script relaying monitor output through `atrium tell` to its own
handle) is a message from a session that is not the launcher, so it does not count either.

## Across a daemon restart

Nothing is in memory. `owed_at`, `reported_at` and `turn_end` are stored, and the notice dedupe is the stored
`a2a_notice`. A restart neither forgives a debt nor repeats a notice.

## Resident versus one-shot

A one-shot worker: launched with a prompt, so the opening prompt, sent as its launcher, sets the debt, and every message from its launcher
after that sets it again. Behaviour is unchanged, except that an operator prompt to an agent-launched worker no longer
makes it owe its launcher, which is intended: the launcher did not ask.

A resident session: owes for the launcher's opening prompt and its launcher's `atrium_say`, and for nothing its own
workers send. Its workers' reports arrive as messages from sessions that are not its launcher.

A card with no launcher, or launched by the human, is out of scope, as before (`agentLaunched`).

## What does not change

`handleStop` is untouched. `silentStop` and `stoppedSilently` keep their shape, and `peerSaid` still marks a report
when a worker speaks to its launcher. The permission chain is not touched. Nothing here posts from a hook or adds a
retry. The seen state is not touched.

## Known limits

- A launcher's message stamps the debt when it is QUEUED, as before, not when it is delivered. `stoppedSilently`
  already requires a turn that ended after the prompt.
- A launcher relaunched or renamed after lineage was recorded no longer matches by name, only by card id. Lineage is
  written once (`SetLineage`), so this needs the launcher's card to have gone.
- A launcher's message is one door among several a human could imitate by typing `--from`. The peer bus trusts the
  sender it is told, and this inherits that. No auth, by the repository's rule.
