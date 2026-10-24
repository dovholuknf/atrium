Docs: none, a fix that makes the cache chip and keep-alive match what the cold handoff already promises

## Test plan

## @LETTER@. A card going cold is handed off once and then shows cold

### @LETTER@1. The handoff

1. Turn on Settings, hand off a card whose cache goes cold. Keep-alive may be on or off for the card.
2. Leave an idle Claude card of over 50k context for 56 minutes after its last reply.

**Expected:** the card is typed the context limit prompt, writes its handoff, is cleared and reads the handoff. A
frost toast says it was handed off.

### @LETTER@2. Cold, and not kept warm

1. Leave the card idle for another hour after the handoff.

**Expected:** its cache chip shows cold since the handoff, the tooltip says it was handed off as its cache went cold,
and no keep-alive refresh runs on it.

### @LETTER@3. Once per cold

1. Leave it idle for several more hours.
2. Type a turn into it yourself and leave it idle for an hour.

**Expected:** no second handoff in step 1. After step 2 it is handed off once more.
