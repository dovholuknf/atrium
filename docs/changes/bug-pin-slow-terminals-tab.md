# bug-pin-slow-terminals-tab

`togglePin` waited for the PATCH and then a whole task fetch before the star, the pinned bucket or the switcher moved.
Over a remote room that is two round trips and the full list. It is now optimistic: `setPinLocal` flips `pinned` on the
held card and repaints from the held map in the click's frame, the PATCH goes after, and a refusal reverts the flip and
toasts. SSE brings the authoritative row. The Terminals drop (pin then pin-order) does the same, its two writes behind
the paint. `patchTask` is not used for this, because it swallows its own failure and a revert has to hear it.

Every pin site goes through `togglePin` (the star in the stack and the list, the card menu, the "unpin" of an unattached
row) or the drop, so all are covered. The phone page has no pin control. A card from a room that is not answering still
draws no control (`pinStar`).

Not measured against a remote room: this machine had none. The headless unit holds the PATCH for two seconds and sees
the star turn in two animation frames. Images: `img/bug-pin-slow-terminals-tab/before.png` (star still off with the
PATCH in flight) and `after.png` (star on).

## Test plan

## @LETTER@. Pinning is immediate

### @LETTER@1. Pin from the Terminals list

1. On a board with a remote room, open Terminals and click the star on an unpinned card.

**Expected:** the star fills and the row moves to the pinned bucket as you click, not after a pause.

### @LETTER@2. A refused pin

1. Pin a card while its room is unreachable or refuses the change.

**Expected:** the star fills, then goes back and a "that did not stick" toast appears.
