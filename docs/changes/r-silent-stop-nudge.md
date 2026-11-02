## Test plan

## @LETTER@. A silent stop is nudged twice

### @LETTER@1. Worker stops without reporting

1. Launch a worker from another card and prompt it with something that ends in a plain reply.
2. Let its turn end without atrium_done, atrium_blocked or a say, and again after the first nudge.

**Expected:** the worker is typed one continuation line each time and its card's events record both. The launcher
hears nothing until a third silent stop under the same prompt.

### @LETTER@2. Report after the first nudge

1. Let a worker stop silently once, then have it call atrium_done.

**Expected:** the launcher gets the report and no silent-stop notice.
