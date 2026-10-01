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
