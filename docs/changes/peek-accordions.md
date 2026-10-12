## Test plan

## @LETTER@. The peek's drawers

### @LETTER@1. Defaults

1. Hover a card that asks a question and has a long recap for a second.

**Expected:** NEEDS YOU is open. RECAP shows its first line and CONTEXT shows "212k of 200k", both shut. The header and
the open, say, exit, restart row are not drawers.

### @LETTER@2. Toggle

1. Click the RECAP header, then focus CONTEXT and press Enter, then Space.

**Expected:** the recap opens with its "more" link, the context opens on Enter and shuts on Space. Selecting text with
a drag in a header, or in an open drawer, toggles nothing.

### @LETTER@3. State is kept per section

1. Shut NEEDS YOU and open RECAP. Open the peek of another card.

**Expected:** on the other card NEEDS YOU is shut and RECAP is open. The choice survives a reload.
