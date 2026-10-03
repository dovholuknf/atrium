# Review: f-c-preflight aa63b3f4

Range `526337e1..aa63b3f4`, 2 commits on `claude/f-c-preflight2`, 6 files.

- `4863748e` probes the `-Msys2Dir` target before an install. It also sets an explicit git identity when it differs
  from the config.
- `aa63b3f4` makes the tests follow the `cdisk` probe, and removes the probe file explicitly.

What changes:
- `scripts/room-toolchain-c.ps1` gets `Test-Msys2Target`, `Get-GitIdentityPlan` and the `cdisk` act.
- `scripts/room-toolchain.ps1` calls them from `Invoke-CMsys2` and `Invoke-CRest`.
- The rest is tests, the changelog, `docs/changes/f-c-preflight.md` and `docs/backlog/fabric/f-new-c-preflight-rest.md`.

The parked `claude/f-c-preflight-full` (64b9670f) is not an ancestor of the tip and is not in the range. The commits
are unsigned (m1mini).

Verdict: **OK** for room deploy. Four Lows can follow.

## How it was checked

- I read the diff in full, with the code around it: `Test-WinPathArg`, `Test-GitIdentityArg`, `New-IcaclsArgs`,
  `Format-AdminCommand`, `Get-CPayload` and `ConvertFrom-KeyValue`.
- `pwsh -NoProfile -File scripts/test-room-toolchain-c.ps1`:
  - On an LF copy, all 571 checks pass at the tip, and all 544 pass at 526337e1.
  - On the CRLF checkout, the tip and 526337e1 both stop at the same place (line 598, `credential.helper` not found).
    So the CRLF failure is older than this range.
  - `check-powershell.ps1` parses everything on LF.
- Merged onto `claude/landing` 8bfdcdb1, `git merge-tree` is clean (tree b9f37cd4). On LF the merged tree passes all
  571 checks and check-powershell. Landing has changed no `.ps1` since 526337e1.
- I probed the helpers under pwsh with the inputs below. I also ran the `cdisk` act body locally against a drive that
  is not there.
- Seven mutants (one turned out to be a no-op):

  | Mutant | Result |
  |---|---|
  | drop the readable gate | killed (2 checks, and the blocked stage calls `cinstall`) |
  | free check always passes | killed |
  | `-cne` to `-ne` in the identity plan | killed |
  | `-not $tv.Ok` never blocks | killed (`called cinstall`) |
  | drop the read-back (`$stuck` never set) | **survives**, see L1 |
  | the act's `Get-ChildItem -ErrorAction Stop` to `SilentlyContinue` | survives. It can only be tested on Windows, where a drive can be unreadable. |
  | a duplicated `$rd = $true` | a no-op, so it proves nothing |

## @fabric's checks

1. **The payload fits.** Encoded, `cdisk` is 3380 characters for `C:\msys64`, 3392 for a 243-character path and 3456
   for a path full of Unicode quotes. The limit is 7800.
2. **The probe cannot be fooled.** `-Msys2Dir` goes through `Test-WinPathArg` before any act runs, and every case
   below is refused as it should be:
   - `\\srv\share\m`, `//srv/share/m`, `\\?\C:\m`, `\\.\C:\m`, `msys64`, `.\m`, `C`, `Cm`, `C:m`, `C:` and `1:\m`
     are not drive paths.
   - `C:\` and `C:/` are drive roots.
   - A newline, `"` and `|` are characters a path cannot hold.
   - Quotes: `C:\a'b`, `C:\a’;calc.exe;‘`, `C:\x‚$(calc)‛` and `C:\a'';b` each parse back from the payload as one
     `StringConstantExpressionAst`, equal to what was given.
   - Trailing dots and spaces: `C:\m.`, `C:\m ` and `C:\m. .` are accepted. Windows strips them, so the drive and the
     ancestor that get probed are the ones the install uses, and the probe is not fooled.
   - Only a Kelvin sign `K` (U+212A) gets past `Test-WinPathArg`'s case-insensitive `-match`. On the room it has no
     drive root, so the act reports `drive.exists=False` and the run is refused. It fails safe, and the function is
     older than this range.
3. **An unreadable drive never reports ok.**
   - With `drive.readable=False`, `Test-Msys2Target` is not ok even when the folder is writable and the drive has
     space.
   - Missing keys or an empty kv are not ok either.
   - `Invoke-CMsys2` fails the step if `drive.exists` is missing from the act's output.
   - The blocked stage is needs-human, with the icacls line in `-Check` and in a run alike. It never says "would
     install", and it calls only `cmsys,cdisk`.
4. **Free space on a missing drive does not throw.**
   - Run against `Z:\work\msys64`, the act prints all six keys and does not stop.
     `GetUnresolvedProviderPathFromPSPath` throws "Cannot find drive", and the act catches it. `Test-Path ''` and
     `DriveInfo('')` are caught too, and give `free=-1`.
   - The Split-Path loop ends with an empty `anc`.
   - The drive check refuses before space is looked at.
5. **The icacls line is quoted.**
   - Each path is one argument when the line is parsed, with no extra command: `D:\a’;calc.exe;‘ b`,
     `D:\a';calc.exe;'`, `D:\a$(calc)` and `D:\a b" & calc`.
   - For example: `icacls 'D:\a’’;calc.exe;‘‘ b' /grant 'ROOM\builder:(OI)(CI)M'`.
   - The e1090781 M1 does not come back.
6. **The git identity cannot be injected, and is never ok while the config differs.**
   - `Test-GitIdentityArg` refuses `"`, `\`, backtick, `$`, CR and LF. It also refuses an email with a space, `<>` or a
     second `@`.
   - `O'Brien;calc` is accepted. It reaches git as a single argument from a variable, and the printed command quotes
     it as `'O''Brien;calc'`.
   - A leading `-` is accepted. Git takes a value after the key literally: `git config --global user.name --unset`
     stores `--unset`, and so do `-x` and `--get`.
   - The plan compares with `-cne`, so a case difference is set.
   - `-Check` reports "would set" as a warn.
   - A run reads the config back with `cgit`. A value that still differs, or a re-read that comes back empty, is
     needs-human with the exact `git config` lines. A step can only be ok when the plan has nothing to set.

## Lows

- **L1: nothing tests the read-back.** No test drives `Invoke-CRest` through `cgitset` and the second `cgit`. With
  `$stuck` never set, the "set" mutant still passes all 571 checks. The code is right as written, but the promise
  "needs-human if it did not take" has no guard.
  - Add a flow test where the mocked `cgit` returns the old value after `cgitset`.
  - It should give needs-human with the `git config --global user.email ...` line, and not done.
- **L2: a name with trailing whitespace never matches.** `ConvertFrom-KeyValue` runs `TrimEnd()` on every value, and
  `Test-GitIdentityArg` accepts `'Bob '`.
  - Git stores `Bob `, and the read-back gives `Bob`. So the step is needs-human ("did not take") on every run, and
    the printed command sets the same value again.
  - It fails safe, but the message is wrong.
  - Fix: refuse leading or trailing whitespace in `Test-GitIdentityArg`, or compare trimmed values.
- **L3: an unreadable drive root asks for an inheriting grant on the whole drive.**
  - The fix printed for `drive.readable=False` is `icacls D:\ /grant 'acct:(OI)(CI)RX'`. That grants read to that
    account on every inheriting folder on the drive.
  - Listing the root does not need that. `icacls D:\ /grant 'acct:RX'`, with no inheritance, is enough.
  - The gate also refuses when the root cannot be listed but the target's nearest folder is writable, because
    `anc.writable` is only probed when `$rd` is true.
  - Either give the root grant without `(OI)(CI)`, or probe the ancestor first and only ask about the root when the
    ancestor is not writable.
  - This is new in this range. It is a Low because a person reads the line before running it, and the grant is to
    one account.
- **L4: `free=-1` passes without a word.** When the drive exists and can be read but `DriveInfo` throws, the 6 GB check
  is skipped and the step goes on to "would install". A test pins this on purpose ("does not block"). Say so in the
  step, for example "free space unknown, 6 GB not checked", so the changelog's "must have 6 GB free" is not read as
  checked.

Atrium-Verdict: room-ok 526337e1..aa63b3f4
Quality: a small, focused rebuild. The probe fails closed at every gate, every line meant to be pasted goes through
Format-AdminCommand, and every gate is caught by a mutant. Only the identity read-back has no test.
