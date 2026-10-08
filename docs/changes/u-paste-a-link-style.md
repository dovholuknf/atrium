## Test plan

## @LETTER@. The paste-a-link box says what it read

### @LETTER@1. Recognised live

1. Copy a pull request link, press Ctrl+Alt+R.
2. Clear the field and type an issue link, then a Zendesk ticket link.

**Expected:** under the field a PR chip, then an issue chip, then a ticket chip, each with the recogniser's label and the
title or repo, a moment after the typing stops. Nothing is started.

### @LETTER@2. Not a link, not known

1. Type some words and press Enter, then try a link no recogniser matches.

**Expected:** "that is not a link", then the reason the link is not known, in the warning colour.

### @LETTER@3. Every skin

1. Open the box in a dark skin and a light skin.

**Expected:** the field, the focus ring, the chip and the key chips are readable in both.