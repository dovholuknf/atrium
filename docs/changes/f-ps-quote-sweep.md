## Test plan

@LETTER@

1. `pwsh -File scripts/test-autostart-quote.ps1` passes (five odd exe paths parse as three statements, and off Windows
   the task text runs against a fake atrium with the arguments arriving whole).
2. On a Windows room with autostart, install it again with a plain path and compare `schtasks /query /tn <task> /xml`
   with what it showed before: the command text is the same.
