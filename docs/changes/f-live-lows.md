## Test plan

@LETTER@

1. On sg4, run `pwsh -File scripts\live\start-atrium-control.ps1 -WhatIf` and check it still prints the start line with
   `<join>` hidden when `-Join x` is given.
2. Open `scripts/live/README.md` and check the packaging link resolves to `docs/release/packaging.md`.
