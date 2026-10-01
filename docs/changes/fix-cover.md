## Test plan

## @LETTER@. The restart cover clears when the hub is back

### @LETTER@1. A fast hub restart takes the cover down

Deploy a hub-only change with a board open. The "atrium is restarting" cover appears and clears within a few seconds
of the hub coming back, with no reload. It must never sit until the 10 minute stale timer. Run
`HEADLESS_ONLY=coverPoll node scripts/test-board-headless.js`.

### @LETTER@2. A cover that stays up says so

Hold the hub away (headless: `HEADLESS_ONLY=coverSteps`). At 10 seconds the line under the headline says something is
wrong, and the ring and the bar stop moving. At 30 seconds a "reload this page" button appears. After the hub is back,
the button reloads onto a page whose cover clears.
