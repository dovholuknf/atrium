## Test plan

## @LETTER@. No handoff pre-warning before the context limit

### @LETTER@1. A card below its limit hears nothing

1. Set a card's context limit low and let its conversation grow past 80% of it, but not past the limit.

**Expected:** no "atrium will cycle your context" line reaches the card.

### @LETTER@2. The cycle at the limit still asks for the handoff

1. Let the same conversation grow past the limit.

**Expected:** atrium asks for the handoff and cycles the context as before.

### @LETTER@3. An old board still saves settings

1. POST `/v1/settings` with `{"context_prep_pct": "80"}` beside another setting.

**Expected:** the request succeeds, the other setting is stored and `context_prep_pct` is ignored.
