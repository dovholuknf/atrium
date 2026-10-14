# Review of claude/main...af914e9d (f-room-bringup)

Read-only review. Nothing was run against a machine. Line numbers are of the files at af914e9d.

Verdict: HOLD (2 HIGH)

## Checked and clean

- The Windows drive grant is `(RA,REA)` only, with no `(OI)(CI)`, so it is not inherited and never names Users or Everyone. See `room-work.ps1:106` and `room-toolchain-c.ps1:316`. `New-IcaclsArgs` refuses Users and Everyone for the `attrs` mode as well.
- No token or secret is on any command line. The hub mirror URL carries no credentials, ssh carries the key, and remote scripts go over stdin or `-EncodedCommand`.
- Remote quoting of `-WorkRoot` is sound. Paths go through `ConvertTo-PsLiteral` and `ConvertTo-ShLiteral`, settings through `Quote-Ps` and `Quote-Sh`, and `Test-WorkRootArg` refuses `;`, newlines, `..`, UNC and drive roots.
- An agent-pack rerun at the same commit changes nothing. The record is rewritten only when files changed or the commit differs (`room-work.ps1:511`, `:537`). `-Remove` leaves the work root and the pack alone.
- Cache redirection stays inside the account profile (`.npmrc`, `%APPDATA%\go\env`, `pip.ini`, the user CARGO_HOME) or the work root.

## HIGH

1. **Unix cache script uses bash arrays under `sh -s`: `room-work.ps1:227`, `:302`.**
   - `dirs=(...)` and `"${dirs[@]}"` are not POSIX. `Invoke-Remote` pipes the script to `sh -s` (`provision-room.ps1:486`).
   - On dash (Debian and Ubuntu) it is a syntax error, so every Unix room fails `work-cache`. It only worked in the Windows runs.
   - Fix: use `set --` and `for d in "$@"`, or a space-separated list of quoted words.

2. **`-Check` writes on a Unix room: `room-work.ps1:290-293`, reached from `room-check.ps1:~794` and `provision-room.ps1` `-Check`.**
   - `setkv` runs `mkdir -p "$(dirname "$f")"` before `$mk` is looked at. It also creates `$f.atrium-tmp.$$` and `$t.empty` in the config dir.
   - A `-Check` or room-check run, which `room-check.ps1:~40` says "WRITES NOTHING" without `-Fix`, creates `~/.config/go` and `~/.config/pip` (and `~`-level dirs on macOS).
   - Fix: stage temp files in `${TMPDIR:-/tmp}` only, and `mkdir` only inside `apply` when `mk=1`.

## MEDIUM

3. **The work-root probe writes a file even for a read-only check: `room-work.ps1:142-145` (Windows), `:159` (Unix uses `-w` and does not write).**
   - It writes and deletes `.atrium-probe-$PID` in the work root. This is inside the work root, but `room-check` is documented as write-free.
   - Test ACLs through a no-write route, or say so in the doc.

4. **Admin lines are ordered wrongly when an intermediate parent is missing: `room-work.ps1:106-108`, `:111-112`.**
   - The `icacls` and `chmod` lines for every bad parent come before the `New-Item` or `install -d`.
   - For `V:\a\localai` with `V:\a` absent, the grant on `V:\a` fails and the printed lines do not work as given.
   - The "cannot examine" wording is also wrong for a parent that does not exist.

5. **The Unix admin line `sudo chmod o+x <parent>` grants search to every user, not just the account: `room-work.ps1:111`.**
   - This is broader than the Windows attribute-only grant. `setfacl -m u:<acct>:x` is the account-only form.
   - It is also emitted for `/` when `/` fails the probe, which is unlikely but wrong.

6. **PS 5.1 mangles non-ASCII in the user's existing files: `room-work.ps1:240`, `:253`.**
   - `Set-KeyLine` and `Set-IniKey` read with `Get-Content`. The remote is `powershell.exe` 5.1, which reads BOM-less UTF-8 as ANSI. The file is then rewritten as UTF-8, so non-ASCII in `.npmrc`, `go env` or `pip.ini` is corrupted.
   - Read with `-Encoding UTF8`, or use `[IO.File]::ReadAllLines`.

7. **Agent-pack install writes through symlinked parent folders: `room-work.ps1:499-505`, `:529-532`.**
   - Only the leaf is checked for being a link. If `~/.claude/skills` or `skills/<name>` is a link or junction (the dotfiles layout makes this likely on an operator box), `Copy-Item` or `cp` writes into the link target, which can be outside the account's `~/.claude`.
   - Check each parent below `~/.claude` with `LinkType` or `-L`, and refuse or replace the link.
   - Also, a locally edited agent with a packed name is silently overwritten and counted as `changed`. There is no warn.

8. **`room set` skips the shared validation: `internal/cli/roomsettings.go:83`.**
   - The API path uses `store.CheckContextHandoffDir`, which trims and `filepath.Clean`s (`contextcycle.go:84`). The CLI only runs `IsAbs`, so a value with `..` or surrounding spaces is stored raw.
   - `reviews_root` has no `Clean` on the API path either, so call the shared check for the handoff dir and `Clean` for reviews.
   - `IsAbs` is host-OS dependent: `/srv/x` is rejected on a Windows room. That is fine for the Windows flow, which sends `V:/...`.

## LOW

9. **The cargo line has a quoting hole: `room-work.ps1:310`.** `export CARGO_HOME='$cargo'` breaks, and could inject into the account's `.profile`, when `-WorkRoot` holds a `'`. `Test-WorkRootArg` does not refuse quotes. Use `ConvertTo-ShLiteral`. Also, an `sh -s` ssh session never reads `.profile`, so the Unix cargo setting does not reach the room's own process.

10. **`awk -v` expands backslash escapes in the value: `room-work.ps1:292`, `:295`.** A path with `\` is altered. Pass the values by `ENVIRON`.

11. **`-WorkRoot` is not checked against the target OS: `room-work.ps1:52`.**
    - `V:\x` on a Unix room becomes `mkdir -p V:/x` under `$HOME`.
    - `/srv/x` on a Windows room gives empty `Get-WorkParents`, so the message reads "the drive  is not there" with a blank drive (`:189`).

12. **`sh` pack parsing is fragile: `room-work.ps1:527-528`.** `read -r rel h` breaks on file names with spaces. `tmp` is left behind on `unpack=fail` (`:523`).

13. **`$env:GIT_TERMINAL_PROMPT = '0'` is set in the operator's session and never restored: `room-work.ps1:437`, `:457`.**

14. **`-Branch` and `-Repo` are used as git arguments unvalidated: `room-work.ps1:458`.** A branch starting with `-` becomes an option. Use `git clone -b $Branch -- $url $src`.

15. **The `-WorkRoot` account gets `(OI)(CI)F` on its own root (`room-work.ps1:108`).** That is by design, but `F` includes WRITE_DAC, so `M` would be enough.
