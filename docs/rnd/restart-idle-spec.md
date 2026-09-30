# A restart resumes only the cards that were working (backlog-2 item 38): spec, not built

Written 2026-09-29 by @runtime from item 37's `session_usage` rows, read off a COPY of the live database. Nothing here
is built. The Open Questions at the end are for clint.

## What the numbers say

The table held 526 rows over about 8.6 hours (2026-09-28 19:56Z to 2026-09-29 04:35Z), $207 in all, and two room
restarts, about 20:00Z and 00:25Z.

The question item 38 was waiting on is "what does a resume cost". The answer is that a resume costs nothing by
itself. A resumed Claude Code process spends nothing until its next turn, and that turn reads the same cached prefix
the old process wrote. The first turn after a resume is flagged `after_resume`, and those rows split cleanly on the
gap since the card's last spend (including a keep-alive refresh):

| Gap before the first turn after a resume | Cache written | Context | Reads as |
| --- | --- | --- | --- |
| 1 minute (a restart wake) | 6k to 15k | 145k to 250k | a hit |
| 13 to 46 minutes (a keep-alive refresh inside the hour) | 12k to 36k | 310k to 432k | a hit |
| 1h17m to 4h19m | 150k to 270k, most of the context | 65k to 290k | a full rewrite |

So the misses are the 1h TTL running out, not the resume. A first turn after a resume that finds a cold cache costs
about $0.40 to $2.30 at these context sizes, which is the price of the gap, and the same card would pay it if it had
never been restarted.

## What that means for item 38

Parking idle cards at a restart would save no tokens. An idle card that is resumed and never typed into spends
nothing, and one that is typed into pays the same first turn either way. What resuming every card does cost is:

- a process per card (about 21 of them), and their memory,
- the length of the restart's settle window, since `settling.go` holds until every expected card is back,
- the chance of a resume failing and filing a card as `dead` that nobody was using.

Keep-alive does not need a running process, because it forks from the transcript. So parking a card does not stop it
being kept warm, and warming does not argue for resuming.

## A shape, if it is still wanted

- At a restart, a card resumes if it was working, has a queued message, a pending restart wake, or an open
  permission. Everything else is recorded as `parked`: its resume id kept, no process, still in its column. Not a new
  status. A flag on the card or a line in its history.
- A parked card resumes on the first thing that needs it: an attach, a typed line, a say, a restart wake, an action.
  A say to a parked card resumes it and is then delivered, instead of answering `undeliverable` (item 83's refusal is
  for a card with no runner, and a parked card is exactly that, on purpose).
- The board shows a parked card as it shows an idle one, with a small mark, so the operator is not surprised by a
  resume that takes a few seconds on attach.

## Open Questions for clint

1. **This finding decides whether 38 is worth building.** Parking idle cards saves processes, memory and restart
   time, and NO tokens. Is item 38 about tokens? If yes, it can be closed with this note. If it is about process
   count, memory or how long a restart takes, say which, and it is worth building on the shape above.
2. Should a say to a parked card resume it without asking? The peer bus never types into a terminal, and resuming is
   stronger than typing. The alternative is a queued say that is delivered when the operator next resumes it.
3. Which cards count as "working" for the rule: only `running`, or also `needs-permission` and cards with a queued
   message? The shape above says all three.
4. Two rows priced at $0.00 (cards `01a0c906` and `01a0771d`, 290k and 150k written) mean a model the price table
   does not know. Worth a look under item 37, whatever is decided here.

## clint's answers, 2026-09-29

1. **It is about tokens**: keep-alive refreshes that keep a cache warm while nobody is at the terminal. A card clint
   has not looked at all day should sit idle and cold. Item 38 folds into the keep-alive policy spike with item 39
   (`docs/rnd/keepalive-policy-design.md`, reviewed with Mercurius), which decides what stays warm and what parks.
2. **A say may wake a parked card.** One design worth weighing: the say answers "parked" first, and the sender
   confirms to resume it.
3. **Yes**: `running`, `needs-permission`, and cards with a queued message or a pending restart wake count as working.
4. **Drop cost.** Stop calculating and showing dollar figures for now. It may come back one day, and probably will
   not. The price table's unknown models are moot.
