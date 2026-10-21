## Test plan

## @LETTER@. A detached deploy keeps its switches and never ends in silence

### @LETTER@1. Switches and refusals

1. Run `pwsh -NoProfile -File scripts/live/test-deploy-detached.ps1`.

**Expected:** every check passes: the detached argv carries -NoBuild, -Immediate, -KillRunners, -Control and
-IKnowItKillsMyShell and never -WhatIf, a refused detached child exits 1 with the refusal in its log and said to the card
through a fake hub, and the installer copies `room-ssh.json` and leaves the README.

### @LETTER@2. Installed copy

1. Run `scripts/live/install-live-scripts.ps1 -WhatIf`.

**Expected:** `room-ssh.json` is listed as new or changed when the live folder lacks it or differs.