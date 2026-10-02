# Review: f-ps-quote-sweep d3b94cb8..9dadb315 (m1mini, 2026-10-02): OK

One commit, the follow-up named in the f-allowed-folders re-read: `scripts/atrium-autostart.ps1:203`. Unsigned.

## What holds

- **It was worse than the exe path, and both are fixed.** The old line turned the room's double-quoted `--db`,
  `--agent` and `--http` values into single-quoted literals with **no doubling at all** (`-replace '"', "'"`). So a
  db or address path holding `'` or a typographic quote broke out of the string, on top of the exe's ASCII-only
  doubling. `Get-RoomTaskCommand` now quotes every value with the typographic-quote-safe `ConvertTo-PsLiteral`, the
  same one-liner as the six helpers.
- **Plain values give the same text, byte for byte.** Checked on m1mini by running the old code (from d3b94cb8) and
  `Get-RoomTaskCommand` side by side for a plain exe and db, with and without `--agent` and `--http`: `-ceq` was
  true both times. So provision-room's read-back of an existing task still sees it as right, and no task is
  rewritten for nothing.
- **The outer layer is unchanged and safe.** `-Command "…"` escapes `"` as `\"`, and a Windows path cannot hold `"`.
  The non-room branches pass a raw argument string to the task action, not PowerShell text, as stated.
- **Tests.** `scripts/test-autostart-quote.ps1` lifts the two functions out by AST. For five odd exe paths (`’`, an
  injection string, `'`, `$x`, `‘…‘`) with a db holding `’`, each parses to exactly three statements with the exe
  whole, then runs under pwsh `-EncodedCommand` against an argv-recording fake: nothing is injected and the args
  arrive whole. It passes here ("all checks pass"), and `check-powershell.ps1` passes. The worker's mutation check
  (6+ failures with the ASCII-only literal) is consistent.

Not run: a Windows host, or schtasks itself.

Verdict: OK d3b94cb8..9dadb315, hub-ok and room-ok (ops tooling).

Quality: a good follow-up. It found the worse half of the bug, kept the task text stable for the existing check,
and tested it by parsing as well as by running.
