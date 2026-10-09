## Test plan

## @LETTER@. The card peek waits for the pointer to rest

### @LETTER@1. Sweeping across a card

1. Move the mouse left and right across a card, in strokes of more than 10 pixels, for two seconds or more.

**Expected:** no peek opens while the pointer keeps moving.

### @LETTER@2. Resting

1. Stop the pointer on the card.

**Expected:** the peek opens one second after the last big move.

### @LETTER@3. Hand jitter

1. Hold the pointer on a card and wiggle it by a few pixels.

**Expected:** the peek still opens after a second. Moving within the card while it is open does not close it.
