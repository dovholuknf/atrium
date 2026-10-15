## Test plan

## @LETTER@. The deploy waits for the store and for the reattach

### @LETTER@1. Offline checks

1. Run `pwsh -NoProfile -File scripts/live/test-deploy-waits.ps1`.

**Expected:** all passed. A temp db held by a dummy process times out with `STORE-WAIT-TIMEOUT` and exit 4, passes once
the holder exits, and a fake hub that lists the room late is accepted after "waiting for fake to reattach" lines.

### @LETTER@2. A dry run

1. Run `scripts/live/atrium-deploy.ps1 -Rooms sgg -WhatIf`.

**Expected:** the swap step reads "wait up to Ns for the port", nothing is stopped or changed.
