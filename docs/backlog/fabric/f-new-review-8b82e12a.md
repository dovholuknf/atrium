# Review: f-c-toolchain e809db3e..8b82e12a (m1mini, 2026-10-02): OK, safe to run -Check on sg3

One commit, scripts only: `scripts/room-toolchain.ps1 -Profile c`, the new `room-toolchain-c.ps1` and its test. clint's
ask, and sg3 waits on it. Per @fabric, this pass means "safe to run `-Check` on sg3 via sg4 first". It is that, and the
lows below are for before the first full run. Unsigned.

## The eight asks

1. **`-Check` writes nothing: right.**
   - The probes (`cmsys`, `cgit`, `cauth`) only read. The probe opens one pacman database file for write access and
     closes it, without writing, to test writability, and `icacls` is called with the directory alone, which lists.
   - `cvcpkg` and `csdk` run with `Dry=1` and only report.
   - The ACL step prints "would grant" lines and runs no `cacl`, and the presets go through `cpread` (read only).
   - The one network touch is `git ls-remote`, as the doc says.
2. **Quoting and credentials: right.**
   - Values enter payloads through `Quote-Ps`, which now doubles `'` and U+2018 to U+201B. `Test-WinPathArg` refuses
     relative paths, UNC paths, `" < > | * ?` and control characters, newline included. `Test-AccountArg` and
     `Test-GitIdentityArg` refuse what 5.1 passes badly, so those values are refused rather than quoted.
   - The transport is `-EncodedCommand` with a gzip loader that sets `$ProgressPreference`, so no outer shell quoting.
   - Native calls go through `Start-Process -ArgumentList` with `Q()` (a double-quote wrap) only on validated paths
     and URLs, and with fixed strings for pacman.
   - No token can reach a command line: `ConvertTo-RepoUrl` refuses user info, `ls-remote` runs with
     `GIT_TERMINAL_PROMPT=0` and `GCM_INTERACTIVE=never`, only the helper's name is ever set, and the GCM login is
     printed for a person to run.
3. **icacls: right in shape.** `New-IcaclsArgs` refuses Everyone, Users and Authenticated Users itself. RX goes only
   to other runner accounts that cannot already read. Modify goes only when pacman must write and cannot. The take-back
   runs whether pacman succeeded or not, and restores the user's own explicit ACE as it was. A refused icacls is
   `needs-human`, with the admin command, and pacman is then not run. See L2 and L3.
4. **The CMakeUserPresets merge: right.**
   - A present preset is never changed, and only missing names are added.
   - Invalid JSON, a top level that is not an object, a version under 2 or a `configurePresets` that is not a list
     each leave the file alone and are `needs-human`.
   - `-Depth 64` with the depth warning caught, then a round-trip check of the preset count and the top-level keys,
     before anything is written.
   - `.atrium-bak` is written once.
   - L4: under 5.1, a merged file is rewritten in 5.1's own JSON formatting, so the content is the same but the
     layout and escapes change. Say so.
5. **Idempotence and a partial MSYS2 unpack: right.**
   - The archive unpacks into `<dir>.tmp` and is moved into place only after `pacman.exe` is found. `finally` removes
     the temporary folder and the download.
   - A non-empty target is refused unless `-Force`.
   - `pacman -S --needed` and the "fill gaps only" checks make a rerun cheap. The sha256 is checked before anything
     unpacks, and a mismatch is exit 4.
6. **Windows PowerShell 5.1: no 5.1-incompatible syntax in the payloads.** I read every payload (CCommon, CRun,
   Find, the Merge and all twelve acts) for a ternary, `?.`, `??`, `&&` or `||`, `-AsHashtable`,
   `ConvertFrom-Json -Depth`, multi-segment `Join-Path`, `-AsByteStream`, `$IsWindows` and `ForEach -Parallel`, and
   found none. `J` uses `[IO.Path]::Combine`, `::new()` is in 5.0, and `Get-FileHash`, `-Tail` and `-NotePropertyName`
   are all in 5.1. One fragile spot: `J` with a single argument would misbehave (`$args[1..0]`). Every call passes
   two or more.
7. **No `-Profile` means no change: right as stated.** `Profile` defaults to `none`. `ServerAliveInterval` and every C
   step are only under `c`. The helpers moved to the dot-sourced file unchanged, and the payload sizes are reported
   under 7800.
8. **Exit code 6 and the needs-human block: right.** `Get-CExitCode` gives 6 only when no failure code applies, and
   `Format-NeedsHuman` prints one command per line, unique.

Tests: `check-powershell.ps1` passes ("all powershell parses"). `test-room-toolchain-c.ps1` ran 306 checks ok here and
then stopped at its git-credential simulation (`credential.helper` not created). That is this session's sandbox,
which cannot run the fakes from `$TMPDIR`, as before. The worker's run was 371 with 17 mutations, all red.

## Lows, before the first full run (not for -Check)

- **L1, TLS 1.2 in the C payloads.** `winPayload` sets `[Net.ServicePointManager]::SecurityProtocol = Tls12`, but the
  C acts (`CCommon`) do not, and `cinstall`'s `Invoke-WebRequest` to repo.msys2.org runs under 5.1. Recent Windows
  with .NET 4.7 or later uses the system default and works, but an older box fails the download. Add the same line to
  `CCommon`.
- **L2, an interrupted run leaves Modify granted.** The take-back is an ordinary step after pacman, not a `finally` on
  the remote. An ssh drop, or the hub-side run killed during a 25-minute pacman update, leaves the user's Modify on the
  MSYS2 folder. A rerun then measures it writable, plans nothing, and the grant stays silently. Record the grant on the
  room (a marker file beside the record, naming the ACE), so the next run or `-Check` takes it back or reports it.
- **L3, the restore puts back only the first explicit ACE.** If the user had more than one explicit ACE on the
  folder, `/remove:g` drops them all and only `$before[0]` is restored. Restore each one.
- **L4,** the 5.1 JSON reformatting note above.

Closed: none
Open: L1, L2, L3, L4

Verdict: OK e809db3e..8b82e12a, hub-ok and room-ok (ops tooling). Safe to run `-Check` on sg3 now. Fold L1 and L2 in
before the first full run.

Quality: thorough. It refuses rather than quotes where 5.1 cannot carry a value, has one act per concern, and is honest
about what cannot be proven off Windows. The lows are robustness on a long remote run.
