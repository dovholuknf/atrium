## Design note

The stack's "last active" sort does not order the pinned group. The sort key is right: `cardActivityCmp` and the big
number both read `cardIdleSeconds`, so the shown time and the key agree. The pinned group is the problem. After
the sort, `paintStack` (stack.js, the second `list.sort`) re-sorts pinned cards by hand `rank` (`byRank`), so within
the pins the sort never applies and the times read as shuffled. Ungrouped, all 22 pins sit in that block.

Fix: pins still sit above the rest in either direction, but inside the pinned block the active sort decides, with
`byRank` only as the tie break. The same applies to the pinned rows of a hand ordered group in `stackGroupsHTML`.
The board's pinned bucket and the terminals strip keep rank order, which is their own feature. The phone and the
terminals tab are checked and reported separately.

Accepted by @rnd: pins follow the sort on the stack's flat list. The hand ordered group path in `stackGroupsHTML`
stays on rank, as do the board's pinned bucket and the terminals strip.

The phone (`m/js/home.js`, `m/js/card.js`) never pins: it sorts every card by `cardActivityCmp`, so it already agrees
with the time it shows. The terminals strip orders its pinned bucket by rank on purpose, because dragging a row in it
writes that rank, and following the sort would undo the drag. It is left as it is.
## Test plan

## @LETTER@. Stack last active sort orders pinned cards

### @LETTER@1. Pins follow the sort

1. On the Stack tab, show all, ungrouped, sort by last active.
2. The pinned group's times run from least to most idle, and reversing the sort reverses them.

Pictures: `img/bug-stack-last-active-sort/stack-1280-before.png` and `-after.png`, and the same at 390, from the
`stackPinnedSort` unit (`STACK_PINNED_SHOTS=<dir> STACK_PINNED_WHEN=before|after`). Before, the pinned times read
10h, 1m, 1h30m, 20s. After, they read 20s, 1m, 1h30m, 10h, and the unpinned rows stay below.
