## Test plan

## @LETTER@. The switcher finds the card and nothing else

### @LETTER@1. A real match hides the scattered ones

1. Press the switcher key and type the whole branch name of a card, such as `ui-for-config`.

**Expected:** that card is listed. A card whose directory only holds those letters scattered in order is not.

### @LETTER@2. No real match keeps the fallback

1. Type letters that appear in no title, tag or directory as a run, but do appear in order in one directory.

**Expected:** that card is still listed, so `atsw` style shorthand still works.

### @LETTER@3. Rows lead with the title

1. Open the switcher with two cards in the same repo.

**Expected:** each row's main text is the card's title and the dim text beside it is its short path, so the two are different at a glance.
