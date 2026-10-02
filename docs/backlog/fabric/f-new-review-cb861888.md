# Re-read: f-c-toolchain follow-up cb861888 (L1-L4 of c0c32006)

Range e809db3e..cb861888. This is one commit on 8b82e12a, read by @review on m1mini. The commit is unsigned, like
every m1mini commit.

## L1-L4

- **L1, closed.** `Tls12` is the first line of `cinstall`, the only act that downloads.
- **L2, the mechanism is right.** The grant is recorded (`cstate add`) before the Modify grant, and dropped if the
  grant fails or once it is taken back. A rerun takes a stale grant back first, `-Check` reports it as a warn, and
  cmsys is split into cmsys and cacls to fit the payload size.
- **L3, closed.** Every explicit ACE goes back in one icacls call: `/grant:r` for the first and `/grant` for the
  rest.
- **L4, closed.** The doc sentence is in.

## M1: the record is not re-validated when read back (HOLD, proven)

You asked for exactly this check, and it fails. ConvertFrom-GrantRecord pins only `Dir` to the MSYS2 directory.
`Account` and `before` are taken as written. acl-grants.txt lives under `~/.atrium/toolchain`, which any process
running as the room user can write, every agent session included. Dot-sourcing cb861888's room-toolchain-c.ps1 in
pwsh with one forged line shows it:

```
C:\msys64|SG3\attacker|(OI)(CI)(F) x$(Write-Output-INJECTED);calc.exe
```

New-RevertOps then gives:

```
icacls C:\msys64 /remove:g SG3\attacker
icacls C:\msys64 /grant:r SG3\attacker:(OI)(CI)(F) /grant SG3\attacker:x$(Write-Output-INJECTED);calc.exe
```

1. **The run itself.** It grants Full on the MSYS2 directory to an account this script never granted anything. The
   run has only the user's own rights, so on its own this is no escalation. But it is the script acting on an
   account it was never asked about. That directory's `mingw64\bin` is on every runner's PATH.
2. **The admin command.** When the user's icacls is refused, which is exactly the case where `Need` asks an admin,
   the same ops are printed for an admin to run. Format-IcaclsCommand quotes only on whitespace or a quote, so
   `;calc.exe` and `$( )` go out bare. Pasting the printed line into PowerShell ran `calc.exe`; I proved it with a
   stub `calc.exe`. In cmd, `&` does the same. An admin pasting the printed fix runs code elevated.

Fix:
- **Re-validate every field on read, as a fresh argument would be:**
  - `Dir` equal to `$dir` (already done);
  - `Account` the same account as the current `$user`. The script only ever records Modify for itself, so any
    other account means the line is not ours;
  - each `before` token matching the icacls ACE grammar only, i.e. `^(\((OI|CI|IO|NP|I)\))*\((F|M|RX|R|W|D|[A-Z]{1,3}(,[A-Z]{1,4})*)\)$`
    or tighter.

  A line that fails is never acted on and never printed. It is reported as `msys2-acl warn: acl-grants.txt has a
  line this script did not write`.
- **Quote the printed admin commands for the shell they are pasted into.** Use single-quoted PowerShell literals,
  with `Quote-Ps`'s U+2018-201B doubling, on every argument outside `^[A-Za-z0-9_.:\\/-]+$`. Even the honest
  commands need this: pasted into PowerShell, an unquoted `user:(OI)(CI)RX` fails today, with "The term 'OI' is not
  recognized", which I saw in the same paste.
- **Add a test** for a forged account and a forged `before` (refused, nothing printed), and for a printed command
  pasted into pwsh that runs only icacls.

## Tests here

The suite runs 314 ok on macOS and stops at sim1's credential.helper. That is the known sandbox exec limit
(REVIEWER-NOTES), so the L2 record checks at line 807 and on didn't run here. @fabric reports 419 of 419 on its side.

Closed: L1, L3, L4 / Open: M1 (L2's record read back), the quoting of printed admin commands

Verdict: HOLD e809db3e..cb861888. This gates the first FULL run on sg3. `-Check` stays safe: it only warns and runs
no icacls from the record.

Quality: the record-first ordering and the stale take-back are the right design, and the act split keeps the
payload size. The read-back trusts a file that every agent can write, though, and the printed command repeats the
PowerShell quoting class (REVIEWER-NOTES).

## Re-read 709c9ef8 (e809db3e..709c9ef8)

Probed by dot-sourcing 709c9ef8's room-toolchain-c.ps1 in pwsh.

- **M1, closed.** ConvertFrom-GrantRecord took four lines: my injection line, a line for the user with a `;calc.exe`
  token, an honest line, and a dir with a trailing space.
  - Only the honest line became a grant.
  - The first two came back Bad, as `line 1 (C:\msys64|SG3\attacker|()` and `line 2 (...)`, with the excerpt cut
    and control characters stripped. They are warned about and never acted on.
  - The other directory was skipped silently, as not ours.
  - Test-AceRaw allows letters only inside the parentheses, and grant uses a whitelist of `F|M|RX|R|W|D`.
- **Quoting, closed.** Format-AdminCommand is the one place. I fed it the word
  `SG3\claude:x$(calc);calc.exe & calc ‘q` and it printed one single-quoted literal with the U+2018 doubled.
  Run through Invoke-Expression with stub `icacls` and `calc`, icacls got exactly 3 arguments and nothing else ran.
  A quoted program gets `& '...'`. Need refuses a string, and all 8 call sites pass word lists.
- **Low L5, new.** A forged `before` for the user's own account that passes the grammar, e.g. `(OI)(CI)(F)`, is
  still believed. When the take-back is refused and an admin runs the printed restore, the user ends with Full on
  the MSYS2 directory, which is more than the Modify it is ever given. Cap a restored ACE at what Modify implies,
  or label a restore read from the record as "from the room's record, check it".
- Tests: @fabric reports 506 of 506 with 16 mutations red. Here the suite stops at the known sandbox exec point
  (REVIEWER-NOTES).

Closed: M1, the quoting of printed admin commands (L1, L3, L4 already closed) / Open: L5

Verdict: ROOM DEPLOY OK e809db3e..709c9ef8. This clears the first FULL run on sg3. @fabric lands it.
