# Review: f-allowed-folders, the scripts half, e0624583..87ed8711 (m1mini, 2026-10-02): HOLD

Three commits on claude/f-allowed-folders: provision-room's folders step and smoke-outside, the room-check row, and
the offline test. The `atrium room folders` verb and the launch gate are @runtime's and not built. Unsigned.

## High: a folder name with a typographic quote runs code on a Windows room (proven)

PowerShell ends a single-quoted string at any of `'`, U+2018 `‘`, U+2019 `’`, U+201A `‚` and U+201B `‛`.
`ConvertTo-PsLiteral` (room-folders.ps1) doubles only the ASCII `'`. Proven on m1mini under pwsh: the dir
`/srv/a’; Write-Output INJECTED; ’` put through `Get-FolderScript 'windows' @('allow', $dir)` and run as
`-EncodedCommand` printed `room folders allow /srv/a` and then **`INJECTED`**: the rest of the name ran as
PowerShell. It is realistic, not contrived. macOS autocorrects `'` to `’` in names ("Clint’s work"), and folder names
reach the script from `-AllowedFolders`, from the clone path, and from WORKTREE_ROOT read off the room itself.

The same flaw is in the existing `Quote-Ps` in provision-room.ps1 (line 403), which every Windows step already uses.

Fix both: `"'" + ($s -replace "['\u2018\u2019\u201A\u201B]", '$0$0') + "'"`, since PowerShell takes any doubled one
of them as a literal. Add `’`, `‘`, `$x`, a backtick and `a;b` to `$weird` in test-room-folders.ps1. The test then
checks that every dir arrives whole under both sh and PowerShell.

## The six asks

1. **Quoting.** The sh literal (`'…'` with `'\''`) is correct for `$`, backticks, `;` and spaces. Newline and `;`
   are refused before quoting (`Test-FolderArg`). The transport is safe: the script travels as `-EncodedCommand`
   base64 (or deflated and loaded), never through cmd.exe quoting. The hole is the High. Also for Windows
   PowerShell 5.1 as the remote shell: 5.1's native-argument passing mangles an argument that ends in a backslash
   and holds a space (`"C:\a b\"` reaches the program with a stray quote). Trim trailing slashes from every dir
   before quoting, as `Get-DefaultFolders` already does. The test fake is a `.ps1`, so native passing is never
   exercised, and that needs a real Windows run.
2. **Verb missing.** It is exit 127 from the script's own check, or a non-zero exit with cobra's
   "unknown command/flag" text. Code 0 is never "missing", and a refused connection is not (tested). Probing
   `list --json` before `allow` is the right guard: an old binary fails on the unknown flag before running anything.
   A real failure could only read as missing if its output held "unknown command", which a skipped dir's name could
   contain. That is contrived, so it is a low.
3. **Rerun, -Remove and the manifest.** A rerun without `-AllowedFolders` and with an existing manifest skips the
   step. With it, `allow` appends and the manifest merges `have + ended`, unique. Nothing removes, and `-Remove`
   leaves the list (stated and correct, since the verb has no remove). Only lines matching
   `^(allowed|trusted) <dir>` reach the manifest. The merge is by exact text, so `/srv/a/` and `/srv/a` can both be
   recorded. Normalise with `Format-FolderPath` before the unique.
4. **smoke-outside.** It runs only when `list --json` says enforced. A launched card is asked to exit, polled 30 s
   for `supervised` false, and the step fails either way, saying if it did not leave. So it cannot pass with a card
   up. A 4xx counts as `ok` only if the error names the allowed folders or a root (`Test-FolderRefusal`), otherwise
   `warn`, so a refusal for another reason does not pass. The outside folder is the remote home, or the filesystem
   root when the home is listed. The one cost: if the gate is broken, a real runner starts there for a moment
   before the exit.
5. **room-check row.** Its states are skip when the verb is missing, warn when the list is unreadable, ok when
   enforced, and warn when there is no list. `-Fix` alone prints the command, and only `-Fix -Yes` runs `allow`.
   That is the right scope, because setting a list turns the bound on (the sg4 `D:\git` case). It inherits the High
   through `Get-FolderScript`.
6. **Tests.** They are real, not self-confirming. The generated script is run for real under `sh -s` and under
   pwsh against fake binaries that record their argv, plus the text-level helpers. `check-powershell.ps1` passes
   here ("all powershell parses"). `test-room-folders.ps1` ran 52 checks ok on m1mini and then stopped when its
   fake in `$TMPDIR` would not execute (exit 126), which looks like this session's sandbox. The worker's run was
   100/100. Not run anywhere: a Windows room, ssh, smoke-outside, or the room-check row live.

## Verdict

HOLD e0624583..87ed8711 on the High: the typographic quotes in `ConvertTo-PsLiteral` and `Quote-Ps`, with the test
names. Fold in the trailing-slash trim (1) and the normalised manifest merge (3). A re-read starts at e0624583,
hub-ok and room-ok (scripts that change rooms).

Quality: careful scripts that degrade to a warn everywhere the verb is missing. The smoke-outside design is right:
it fails only on a held-open gate and never leaves a card. The miss is PowerShell's lesser-known quote characters,
which the repo's existing quoting helper had already got wrong.
