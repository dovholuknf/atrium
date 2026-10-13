## Test plan

## @LETTER@. Grey means not running

Before and after: `docs/changes/img/grey-before.png` and `docs/changes/img/grey-after.png`. The rows are the stack, then the
terminals tab, for the same eight fake cards.

### @LETTER@1. A running agent-launched card that waits for you is orange

1. Launch an interview from an agent (a card tagged `origin:agent`) and let it end a turn with no open question.
2. Look at it on the stack, the terminals tab and the phone page.

**Expected:** it is orange and counted as waiting on every surface, the same as a card you launched yourself.

### @LETTER@2. A card whose runner is gone is grey

1. Take a card that says needs-input or needs-permission with `supervised` false and no pid (a hub outage can leave one).

**Expected:** it is grey on the stack, the terminals tab and the phone page, it is not counted as waiting and it does not
ring the bell. A joined terminal that reports a pid stays orange.

### @LETTER@3. Headless units

Run `HEADLESS_UNITS=greyMeansDead,mAgentIdle,bootClean node scripts/test-board-headless.js`.

**Expected:** all three pass.
