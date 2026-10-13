## Test plan

## @LETTER@. A card that blinks out of the list is not announced again

### @LETTER@1. One poll gone, then back

1. Open the board and let a new card arrive. "X is on the board" rings once.
2. Make the card vanish from the list for one poll (a room dropping and reattaching) and return.

**Expected:** no second "X is on the board". A card genuinely new still rings.

Arrival keys are `a:` plus the bare id, so a room prefix does not make a new key. They are kept for the session and
pruned only after the card has been absent from the list for 24 hours, or by the shared cap of 1000 remembered keys.
Headless unit: `arrivedOnce`.
