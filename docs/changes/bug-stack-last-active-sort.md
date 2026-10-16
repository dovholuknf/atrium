## Design note

The stack's "last active" sort does not order the pinned group. The sort key is right: `cardActivityCmp` and the big
number both read `cardIdleSeconds`, so the shown time and the key agree. The pinned group is the problem. After
the sort, `paintStack` (stack.js, the second `list.sort`) re-sorts pinned cards by hand `rank` (`byRank`), so within
the pins the sort never applies and the times read as shuffled. Ungrouped, all 22 pins sit in that block.

Fix: pins still sit above the rest in either direction, but inside the pinned block the active sort decides, with
`byRank` only as the tie break. The same applies to the pinned rows of a hand ordered group in `stackGroupsHTML`.
The board's pinned bucket and the terminals strip keep rank order, which is their own feature. The phone and the
terminals tab are checked and reported separately.

Open question for the review: this gives up hand order of pins on the stack tab, where the column shows a duration
the pins are not ordered by. The alternative is to keep rank and stop showing the duration on pinned rows.

## Test plan

## @LETTER@. Stack last active sort orders pinned cards

### @LETTER@1. Pins follow the sort

1. On the Stack tab, show all, ungrouped, sort by last active.
2. The pinned group's times run from least to most idle, and reversing the sort reverses them.