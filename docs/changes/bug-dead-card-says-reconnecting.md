Docs: none, a fix that makes the pane say what is true

## Test plan

## @LETTER@. A card with no runner offers resume

### @LETTER@1. Open a card whose runner is gone

1. Stop a runner so its card stays on the board with a stale pid, then open the card in the Terminals tab.

**Expected:** at once, with no spinner, a centered dialog says the session is not running and offers Relaunch and
Launch new.

### @LETTER@2. Relaunch and Launch new

1. Press Relaunch.
2. Stop the runner again, open the card and press Launch new.

**Expected:** Relaunch resumes the same conversation on the same card. Launch new starts a fresh session in the same
directory. Relaunch is disabled, with the reason as its tooltip, on a card that has no resume id.

### @LETTER@3. A dropped socket still says reconnecting

1. Open a live card, then restart the hub.

**Expected:** the pane says it is reconnecting and the dead-card dialog never appears.
