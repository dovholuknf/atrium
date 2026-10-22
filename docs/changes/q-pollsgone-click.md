## Test plan

## @LETTER@. pollsGone clicks a walk button that is settled

### @LETTER@1. The unit

1. Run `HEADLESS_UNITS=pollsGone node scripts/test-board-headless.js`.

**Expected:** `pollsGone: ok`. The walk click happens after `dock.tenant` is set for the card, not just after the
button lost its `hidden` attribute.
