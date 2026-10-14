## Test plan

## @LETTER@. The live scripts refuse a supervised shell

### @LETTER@1. Refusal and opt-in

1. Run `pwsh -NoProfile -File scripts/live/test-refuse-supervised.ps1`. It fakes every process.
2. From a terminal inside a room, run `stop-atrium.ps1 -WhatIf`.

**Expected:** the test prints `all passed`, and the script refuses with exit 1 and names wt.exe. `-Control` without
`-IKnowItKillsMyShell` (or the reverse) is refused, and `-Rooms local` or `all` never include sg4-control.
