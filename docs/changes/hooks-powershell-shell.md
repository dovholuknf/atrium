## Test plan

## @LETTER@. Hooks on a Windows machine with no Git Bash

Claude Code on Windows runs a hook command in Git Bash, and blocks every tool call when it cannot find one unless the
entry says `"shell": "powershell"`. It looks for Git Bash in `CLAUDE_CODE_GIT_BASH_PATH`, then `git.exe` on PATH
(`..\bin\bash.exe` beside it), then `C:\Program Files\Git\bin\bash.exe`. The installer probes the same places
(`internal/claudeconf/shell.go`). A `bash.exe` that only sits on PATH does not count.

### @LETTER@1. Bash found

1. On a machine where Git Bash resolves, run `atrium hook install`.

**Expected:** no `shell` key, and each command is as before, for example `C:/x/atrium.exe hook --event tool-end`.

### @LETTER@2. No Git Bash

1. On a Windows machine where none of the three places has a `bash.exe`, run `atrium hook install`.
2. Run `atrium hook status`.

**Expected:** every entry has `"shell": "powershell"`. A path with a space is written `& "C:/Program Files/..."`, a path
without one is left bare. Status shows every hook installed and none stale. An older entry with no `shell` key shows as
stale and install repairs it in place.

### @LETTER@3. The room gate

1. Run `scripts/room-gate.ps1` against a room whose hooks are in the PowerShell form.

**Expected:** the permission gate it adds keeps the `& ` prefix and copies the `shell` key. Running it again adds no
second gate.
