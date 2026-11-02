## Test plan

## @LETTER@. A Terminals row shows only the keep-alive dial

### @LETTER@1. The dial and the strike

1. Open the Terminals tab with one Claude card idle and waiting on a refresh, and one whose keep-alive stopped.

**Expected:** the first row draws the filling dial, amber when the refresh is near. The second draws the amber strike.

### @LETTER@2. No plain dot

1. Look at a row whose card is kept or warm with no refresh scheduled, cold, off, or has no cache yet.

**Expected:** the row draws no keep-alive chip. The same card on the board still draws its chip, with the words in
its tooltip.
