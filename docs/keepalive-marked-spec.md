# Keep-alive warms the cards you mark, not every idle card (backlog-2 item 39): spec, not built

Written 2026-09-29 by @runtime from item 37's `session_usage` rows, read off a COPY of the live database. Nothing here
is built. The Open Questions at the end are for clint.

## What already exists

Item 23's keep-alive is on by default for new Claude cards (the gear), and every card has its own switch in its menu.
`decide` in `internal/daemon/keepalive.go` already skips a card that is not `needs-input`, is lean, is under 50k of
context, is not on the 1h cache, or has local hooks, and it stops a card by itself once its refreshes since it went
idle cost an eighth of one full 1h rewrite. So "the cards you mark" is already possible. Item 39 is really about the
DEFAULT: on for every card, or on only for the cards someone picks.

## What the numbers say

Over about 8.6 hours (2026-09-28 19:56Z to 2026-09-29 04:35Z):

- Keep-alive made 42 refreshes on 13 of 57 cards, for $4.27, about 2% of the $207 spent. About $0.10 a refresh.
- 8 of those 13 cards had no turn of their own after any refresh, so what was spent on them bought nothing yet. Some
  of them may still be returned to.
- Where a card did come back inside the hour it paid off: a 310k context wrote 12k on its first turn after a refresh
  46 minutes before, and a 432k one wrote 36k after 13 minutes. A full rewrite at those sizes costs $2 to $4, so one
  saved rewrite pays for 20 to 40 refreshes.
- Two refreshes were full writes themselves: 128k written for $1.02 on `01a0e960`, and 94k for $0.76 on `01a0e8f0`,
  each the only refresh on its card. $1.78 of the $4.27. Filed and diagnosed as item 87: both are lean cards, whose
  fork cannot rebuild their prompt, and item 70 (fa2b2cc) already skips lean cards. So the largest keep-alive waste in
  this window is already gone, and what is left to save is smaller still.

So the money item 39 could save today is at most a few dollars a day, and less than the two cold refreshes above cost.
It grows with the number of idle cards, and the break-even stop already bounds it per card.

## A shape, if it is still wanted

- Keep the per-card switch. Change the default for new cards from on to a rule: on for a card that is pinned, or in a
  column the operator chose in the gear, and off otherwise. The gear keeps "on for every card" as one choice.
- A card turned on by hand stays on. A card turned off by hand stays off. The rule only decides for cards nobody has
  touched, so no one's choice is undone by a move between columns.
- The card's details already say why keep-alive skipped it. A card off by the default rule says so there, with the
  rule's name, so "why is this one cold" has an answer.

## Open Questions for clint

1. Is it worth building now? At about 2% of spend, and with the break-even stop, the saving is small, and the two
   cold refreshes that were the bigger waste are already fixed (item 87, by item 70). The suggestion is to leave item
   39 until there are more cards.
2. If it is built, what should decide the default: pinned cards, a column, a tag, or only the switch? The shape above
   says pinned or a chosen column.
3. Should a card that has been idle past some age (a day, say) stop being warmed whatever its switch says? The
   break-even stop already ends each idle stretch, so this would only matter for a card that is woken and left again
   and again.
