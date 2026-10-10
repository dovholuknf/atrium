## Test plan

## @LETTER@. Child cards inherit their launcher's colour

### @LETTER@1. A child outside any repo

1. Turn card colours on. Launch a card from a card in a coloured repo, with its directory a scratch folder that is
   no repo, for example `C:/temp/review/critic`.

**Expected:** the child wears its launcher's repo colour in the terminals list and on the board.

### @LETTER@2. A child in its own repo, and an explicit theme

1. Launch a card from the same launcher with its directory in a different repo that has a colour.
2. Launch another with `theme` set.

**Expected:** the first wears its own repo's colour, the second wears the theme it was given.

### @LETTER@3. A card nobody launched

1. Open a card by hand in a scratch folder.

**Expected:** it stays plain, or wears the default colour when one is set.
