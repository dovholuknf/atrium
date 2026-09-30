# Held-message escalation: what has waited too long says so once, and the card face shows it

Status: design, 2026-09-30, @rnd. Item `docs/backlog/runtime/r-new-held-message-escalation.md`. Built by @runtime
(three small room stages) and @ui (one board stage), after card URLs. Nothing here is built.

## 1. The answer in six lines

1. **A `when: done` message older than 15 minutes stops waiting for the turn to end.** The next tool call carries it,
   framed "this waited N minutes for your turn to end". It is delivered, so it escalates exactly once.
2. **A card past its context threshold while mid-turn is told once**, at its next tool call, to finish the step,
   commit, write its handoff and end its turn. Once more if it grows another 50k, and that second time its launcher is
   told as well. Then nothing more that turn.
3. **A turn running longer than 45 minutes is flagged on the card and its launcher is told once.** A report, never an
   action. The notice says how many tool calls arrived in the last ten minutes, so the launcher can tell a long test
   run from a runaway.
4. **One long tool call** cannot be reached by anything, because Claude Code takes nothing in during a call. The card
   face shows it from 5 minutes on, and the existing `long-tool` notice tells the launcher at 20.
5. **The card face shows all of it without hovering**: `✉ 4 · 1h29m`, `253k mid-turn`, `turn 1h32m`, `Bash 10m`.
6. **Four stages, each S.** Most of what they need is already there.

## 2. What is already there

| piece | where | what it does now |
| --- | --- | --- |
| `when: done` | `internal/daemon/saywhen.go` | a message waits for the turn to end. The permission hook skips a `WaitTurn` message and leaves it for the Stop hook (`messages.go`, the `via != "stop"` filter) |
| the typist | `messages.go` `turnHolds`, `deferPeerInjection` | types a held message when the line is empty, the keyboard quiet, and (for `done`) the turn over |
| held signal | card `activity`: `held_peer`, `held_count`, `held_seconds`, `held_for`, `held_turn` | the strip's `!` or `✉` chip. The age is only in the tooltip |
| stuck escalation | `a2a.go` `watchWorkers`, `stuckNow`, `Escalation` | `silent-stop` and `long-tool` (one call past `LongToolAfter`, 20 min), the launcher told once, the board on `EscalationBackoff` |
| context size | `contextsize.go` `ContextSize` on the card | `tokens`, `warn` at the threshold, and a `context-size` notice to the launcher the first time it passes |
| auto new-context | `autocontext.go` (r-029) | cycles a card over its limit, but only at a turn end |
| current tool | card `activity`: `what`, `since`, `seconds` | the live badge. A tool call's age is already on the card |
| held launcher notices | r-hold-notices | a launcher tagged `atrium:hold-notices` or `atrium:orchestrator` has notices held rather than typed |

So step 4 and most of step 5 need no new room state. Steps 1, 2 and 3 are one small change each.

## 3. Stage R1: a held message stops waiting after 15 minutes

In the permission chain's step 2 (a queued message), the `via != "stop"` filter keeps a `WaitTurn` message back. It
now keeps one back only while it is younger than `escalate.held_after` (default 15 minutes). An older one is carried by
the next tool call like any queued message, with one line in front of it:

> [atrium] this message waited 23 minutes for your turn to end, so it is delivered now. finish the step you are on,
> then read it.

**The typist too, but not for every runner.** For a runner that takes input mid-turn, `turnHolds` stops holding an
aged message for the turn (it still waits for an empty line and a quiet keyboard). For a runner that does not
(`midTurnInputFor` false), the typist keeps waiting for the turn, because a line typed into it mid-turn is lost, and the
hook route above is the only one used.

Recorded on the card as an event, `held message escalated after 23m`, and on the sender's say record. It fires once per
message, because a delivered message is gone.

*Acceptance test.* A `when: done` message to a card in a turn that keeps making tool calls is delivered at the first
tool call after 15 minutes, with the framing line, and not before. The same message to a runner without mid-turn input
is delivered by the hook route only. A message younger than 15 minutes at the turn's end is delivered by the Stop hook
as today.

## 4. Stage R2: over context, mid-turn, told once

At a `PreToolUse` for a card that is `running` with `context.warn` true, the chain's step 2 delivers one message from
atrium, once per turn:

> [atrium] you are at 253k context. finish the step you are on, commit, write your handoff, and end your turn. atrium
> cycles your context when the turn ends.

It blocks that one tool call with the text, the same way any queued message does, and the model reads it and carries
on. The last clause is true only where r-029 reaches the card. Elsewhere the line ends at "end your turn".

**Repeats: once more, and only once.** If the card is still mid-turn and has grown 50k past the size it was told at,
it is told again with the new size, and this time its launcher gets a `context-size` notice saying it was told twice
(held for a hold-notices launcher like every other notice). After that, nothing more this turn. The claim is kept in
memory per card and turn (the turn's `PromptKey`), and a new turn re-arms it.

*Acceptance test.* A card over its threshold mid-turn gets the line at its next tool call, once. At +50k it gets it
again and the launcher gets one notice. At +100k nothing more. A new turn over the threshold gets the line again. A card
under the threshold never gets it.

## 5. Stage R3: a turn over 45 minutes is flagged and reported

`stuckNow` gains a third source, `long-turn`: a `running` card whose turn opened more than `escalate.turn_after`
(default 45 minutes) ago. The turn opens at the prompt that started it, the same `PromptKey` the silent-stop notice
keys on. The escalation goes on the card, on the board's backoff, like the other two.

The launcher is told once per turn, through `notifyLauncher` with the turn's `PromptKey` as the key, so a hold-notices
launcher has it held:

> rnd-director has been in one turn for 47 minutes. 31 tool calls in the last 10 minutes, now in Bash for 2 minutes. a
> report, not an action. card 01a0...

**A long legitimate turn from a runaway.** Steps 1 and 2 ride tool calls, so a turn that is working pays nothing for
them. Step 3 cannot tell the two apart and does not try: it reports, once, with the tool-call rate, and the launcher
decides. A turn of 40 minutes is under the default.

**Which cards.** `watchWorkers` looks only at cards with a launcher. `long-turn` is worked out for every running card,
so the board shows it on clint's own cards too, and the launcher notice is sent only where there is a launcher.

**Priority, one escalation per card.** `Escalation` carries one source. The order is `silent-stop`, `long-tool`,
`long-turn`: a card in one long call inside a long turn shows the long call, which is the more specific fact.

*Acceptance test.* A card 45 minutes into a turn gets the `long-turn` escalation and its launcher one notice, with the
tool-call count. At 90 minutes no second notice. A card with no launcher gets the escalation and no notice. A
hold-notices launcher's notice is held. A new turn re-arms it.

## 6. Stage U1: the card face, never hover-only

On the board card and on the terminal strip row, the u-032 rule:

| chip | shown when | from |
| --- | --- | --- |
| `✉ 4 · 1h29m` (or `! 4 · 1h29m` for a line hold) | messages are held | `held_count`, `held_seconds`, already on the card. The age moves from the tooltip to the face |
| `253k mid-turn` | `context.warn` and status `running` | `context`, already on the card |
| `turn 1h32m` | the `long-turn` escalation is on the card | R3 |
| `Bash 10m` | one tool call has run 5 minutes or more | `activity.what` and `activity.seconds`, already on the card |

The tooltips keep their longer words. A chip names the fact, and its tooltip says what clears it.

*Acceptance test.* Headless Playwright on a mocked card list: each chip appears at its threshold with the right text, on
the board card and the strip row, and disappears when the field clears. `Bash 10m` does not appear at 4 minutes.

## 7. Settings

Daemon-wide, in the room's settings table, each with an environment override the way `LongToolAfter` has one:

| setting | default | env |
| --- | --- | --- |
| `escalate.held_after` | 15 minutes | `ATRIUM_ESCALATE_HELD` |
| `escalate.turn_after` | 45 minutes | `ATRIUM_ESCALATE_TURN` |
| the context threshold | the existing one | unchanged |
| `LongToolAfter` (the launcher's one-call notice) | 20 minutes, unchanged | `ATRIUM_A2A_LONG_TOOL` |

Not per card in this item. A card that legitimately runs long turns can be given its own limit later, as an override
on the card.

## 8. The build

| order | stage | owner | size | deploy |
| --- | --- | --- | --- | --- |
| 1 | R1, held message after 15 minutes | @runtime | S | room |
| 2 | R3, long turn | @runtime | S | room |
| 3 | R2, over context mid-turn | @runtime | S | room |
| 4 | U1, the card face | @ui | S | hub |

R1 first because it is what failed on 2026-09-30. U1 can start at once for its three chips that need nothing new, and
add `turn` when R3 lands. The three room stages ride one room deploy.

## 9. Questions for later

clint is busy, so these take their defaults and are also parked in the day's rnd queue file.

1. **The limits.** 15 minutes for a held message, 45 for a turn, daemon-wide. Different numbers, or per-card limits?
2. **The launcher's one-call notice at 20 minutes.** The item's second case was a 10-minute call. The card face now
   shows it from 5 minutes. Should the launcher be told at 10 instead of 20?
3. **R2 blocks one tool call to deliver its line**, the way any queued message does. The alternative is to add the line
   to the `PostToolUse` answer without blocking. That hook is installed, but as an activity hook: fire and forget, one
   second, and nothing reads its answer (the daemon's resilience rule 3). Giving it an answer to carry would change
   that contract on the hot path. Keep the block?
