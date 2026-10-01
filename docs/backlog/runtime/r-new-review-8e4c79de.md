# Review of r-statusline 8e4c79de (@runtime, every room gets clint's status line)

Range `b36e0390..8e4c79de`, one commit on claude/r-statusline. Order: D:\tmp\runtime-order-statusline.txt, steps 2
and 3. Files: `scripts/provision-room.ps1` (a `statusline` step), `scripts/statusline-command.sh` (new, a portable
copy), `scripts/room-check.ps1` (a `statusline` row), `atrium.requirements.yaml` and `internal/requirements` (a
`statusline: required` runner key), a changelog file and test-plan HU.

The base b36e0390 is on the claude/main from before it was rewritten. claude/main now rests on 7b069c66 with new
SHAs for the same patches, so the branch needs a rebase before it lands. The verdict matches by patch-id, so the
range here still holds after the rebase.

Not verified live, as @runtime says: no provision or room-check run on a room. I did not run one either.

## Against the order

- **Only the `statusLine` key is merged.** The step reads the remote settings.json as base64, parses it in
  PowerShell (no jq needed on the remote), sets `statusLine` with `Add-Member -Force`, writes a
  `settings.json.statusline-<stamp>.bak` first, and writes nothing when the key already holds the wanted command.
  A file that is not JSON is left alone with a warn. No settings.json means one is created holding only the key.
- **Idempotent.** The script is copied only when its LF-normalised SHA-256 differs from the remote's, and the
  settings file only when the key differs. A rerun is `ok/ok`, no new backup.
- **Never a failure.** Every branch is a `Step ... ok|done|warn`, which matches the hooks and mcp steps.
- **The portable script.** The diff against C:\Users\claude\.claude\statusline-command.sh is the three changes it
  names: a `date` fallback for `printf '%()T'`, a `tr` lower-case and a `/cygdrive` prefix in place of `${x,,}`, and
  `stat -f %z` after `stat -c %s`. Nothing else in it needs bash 4 (`[[ =~ ]]`, `<<<`, `< <(...)`, `printf -v` all
  exist in 3.2). No secret or PII in it.
- **The requirement.** `statusline` is an enum with one value, so a project cannot name a command there, which is
  the same rule as `hooks`. The parser test covers both. `go vet` and `go test ./internal/requirements/` pass at
  8e4c79de.
- **room-check** reads the key on the account and says `human` with the provision command when it is missing.

## Findings

1. **Medium. The command breaks when bash is under a path with a space.** `$slCmd = "$($slk.bash) $slPath"` is
   unquoted. When no `bash.exe` is on PATH, the first fallback is `C:/Program Files/Git/bin/bash.exe`, so the command
   becomes `C:/Program Files/Git/bin/bash.exe C:/Users/x/.claude/statusline-command.sh`. Claude Code runs the command
   through a shell, which splits at the space and runs `C:/Program`. A home with a space breaks it the same way. The
   result is no status line, and room-check says `ok` because the key is there. The Git for Windows installer's
   default puts only `Git\cmd` on PATH, which holds `git.exe` and no `bash.exe`, so this fallback is the common case.
   Fix: quote both parts (`"<bash>" "<script>"`), which bash and cmd both read, and compare the quoted form for the
   idempotent check. Add a line to HU: provision a Windows room whose bash is only under Program Files.
2. **Medium. The script needs jq, and nothing checks for it.** Every percentage, the context size, the transcript
   size and the usage log come from the one `jq` pass (statusline-command.sh:19). Without jq the line still prints,
   but as the folder, the branch and the clock only, which is not the status line the order asks for. git-bash ships
   no jq, and nothing in provision or `atrium.requirements.yaml` installs or requires it. Fix: the step probes
   `command -v jq` with the bash it chose and warns by name when it is missing (or installs it where provision
   installs other tools), and room-check's row says the same.
3. **Low. room-check checks the key, not that it works.** A `statusLine` naming a missing script or an unusable bash
   (finding 1) reads `ok`. Cheap fix: run the command once with `echo '{}' |` and require exit 0 and output, and
   that the script path exists.
4. **Low. The backup result is ignored.** `$null = Invoke-Remote $bak` discards `bak=1`, then the file is written and
   the step says "settings.json backed up first" either way. When the file existed and `bak=1` is missing, warn and
   write nothing.
5. **Low. The WSL launcher has a second home.** The PATH probe skips `\Windows\System32\` but not
   `%LOCALAPPDATA%\Microsoft\WindowsApps\`, where Store installs put app execution aliases. A WSL `bash.exe` found
   there runs the script inside Linux, where `C:/Users/...` does not exist. Skip `\WindowsApps\` too.
6. **Nit.** A cygwin `bash.exe` named by full path is not a login shell, so `/usr/bin` may be missing from PATH and
   `cat`, `git` and `jq` are not found. `bash -l` or a PATH line for that case would cover it. Only matters if a
   room uses the cygwin fallback.
7. **Nit.** The round trip through `ConvertFrom-Json` and `ConvertTo-Json` reformats the whole file (indent, key
   spacing) and turns ISO date strings into DateTime and back. The backup covers it, and settings.json rarely holds
   dates. Say it in the step's comment so nobody is surprised by the diff.

Quality: after the Sonnet switch, the same pattern: the merge rule the order named is careful (only the key, backup,
idempotent, no jq on the remote), and the step one level out, whether the line it writes actually runs on that
machine, is missed.

**HOLD b36e0390..8e4c79de.** Mediums 1 and 2. A re-read of `b36e0390..<tip>` after the rebase (patch-id keeps the
range valid). Verdict will be ROOM DEPLOY OK only: no board or hub code is touched.

## Re-read f7298fc0

Rebased onto the re-signed claude/main: 2fb82b25 is 8e4c79de with the same patch-id (checked), on 0da8c949. The fix
is f7298fc0 on top. The verdict range is `0da8c949..f7298fc0`, which holds both, on the new history.

- **Medium 1 closed.** `$slCmd` is `"<bash>" "<script>"`, and the idempotent check compares that quoted form. An
  unquoted command from 8e4c79de reads as different and is rewritten once, with a backup. HU 3 covers a room whose
  only bash is under Program Files.
- **Medium 2 closed.** Provision probes `command -v jq` with the bash it chose (Windows) or `/bin/bash` (Unix), under
  `ErrorActionPreference = 'Continue'` so a native stderr line cannot throw on Windows PowerShell, and warns by name
  with where jq.exe goes for git-bash. room-check says the same as `human`.
- **Low 3 closed.** room-check parses `statusLine.command` from the JSON, splits it into a bash and a script (quoted
  or bare), runs it once with `{}` on stdin, and requires exit 0 and output. The script ends in `printf`, so a good
  run exits 0, and an empty `session_id` makes `log_usage` return before it writes, so the check leaves no usage-log
  line. A command it cannot split is `human`, which is right for a hand-written one.
- **Low 4 closed.** An existing file with no `bak=1` is not rewritten, and the step says so.
- **Low 5 closed.** The PATH probe skips `\WindowsApps\` as well as `\Windows\System32\`.
- **Nit 7 closed.** The round-trip reformatting is said in the step's comment. Nit 6 (cygwin non-login PATH) is
  partly answered: the jq probe runs through that bash, so a cygwin bash missing `/usr/bin` now warns by name.

## New findings

1. **Low. A bash found on PATH is written by name, not by path.** When `Get-Command bash.exe -All` finds a
   non-WSL bash, the probe says `bash=bash.exe`, so the command is `"bash.exe" "<script>"`. The probe skipped the
   System32 one, but the written command does not: whatever runs it resolves `bash.exe` by its own PATH order, and
   System32 usually comes before Git. room-check's `& 'bash.exe'` from PowerShell does the same, so on a room with WSL
   installed it can run WSL bash with a `C:/Users/...` path and report a good line as broken. If Claude Code runs
   the command inside git-bash, `/usr/bin` comes first and it resolves right. I have not confirmed which shell it
   uses, which is why this is low. Fix:
   write `bash=$($b.Source -replace '\\', '/')`, the path the probe actually chose.
2. **Low. Both probes see the ssh session's PATH, not the runner's.** jq on a PATH that only the room's logon task
   or a shell profile sets (for example Homebrew's `/opt/homebrew/bin` on a Mac before macOS 15, which has no
   `/usr/bin/jq`) reads as missing, and the reverse is possible. This is true of every room-check row that probes over
   ssh, so it is a note, not a fix here.
3. **Nit.** The Windows probe ends with `$ErrorActionPreference = 'Stop'`, which may not be what it started as. Save
   the old value and restore it.

Quality: after the Sonnet switch, no drop on this pass. Every finding is fixed in the shape asked, with a test-plan
line, and the runtime check goes one step further than asked (it parses the real command, not a pattern).

**ROOM DEPLOY OK 0da8c949..f7298fc0.** Not run live by @runtime or by me: HU 1 to 3 on a throwaway room before
relying on it on sg3 and m1mini.
