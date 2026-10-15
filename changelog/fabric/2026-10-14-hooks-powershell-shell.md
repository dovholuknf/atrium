# Hooks on Windows without Git Bash

On Windows with no Git Bash, installing hooks writes `"shell": "powershell"` and a PowerShell-valid command (`& "path" ...` when the path is quoted), so Claude Code no longer blocks every tool call. Where Git Bash is found nothing changes. Item u-bug-hooks-need-powershell-shell.
