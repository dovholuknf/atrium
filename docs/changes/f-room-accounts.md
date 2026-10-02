## Test plan

## @LETTER@. A room is warned off running as an admin or as the operator: `account` and `account-rights`

`provision-room.ps1` (step `account-rights`), `room-toolchain.ps1` (step `account`) and `room-check.ps1` (row `account`)
read, read only and over the ssh login they already use, who the room will run as. When that is an administrator or the
operator's own everyday account they say so as a `warn` with the reason and `see docs/room-accounts.md`, and the run
goes on. `-IAcceptRunningAsMe` makes it `ok accepted by the operator`, `-RequireDedicatedAccount` makes it a `fail`. The
shared code is `scripts/room-account.ps1` (functions and two payload texts, dot-sourced), the tests are
`scripts/test-room-account.ps1`, and the page is `docs/room-accounts.md`. A clean account changes nothing but one new
`ok` line.

### @LETTER@1. Offline, on any machine with pwsh 7

1. `pwsh -NoProfile -File scripts/test-room-account.ps1` prints `all N checks pass` (126 at the time of writing). It
   needs no ssh. It runs a fake ssh against `room-toolchain.ps1` and `provision-room.ps1`, and `room-check.ps1` too when
   python3 and an atrium with the `requirements` subcommand are there (it says `skip room-check end to end` otherwise).
2. `pwsh -NoProfile -File scripts/check-powershell.ps1` ends `all powershell parses.`
3. `pwsh -NoProfile -File scripts/test-room-toolchain-c.ps1` still passes (544 checks). Run beside other heavy work, its `sim`
   checks fail on the base commit too, so rerun it alone if one does. `scripts/test-room-folders.ps1` fails at its
   `args.txt` check on a Mac on the base commit too, which this change did not touch.

**Expected:** no `FAIL` line from 1 and 2.

### @LETTER@2. A real clean room says ok, one line

1. On sg4, `pwsh -File scripts\room-toolchain.ps1 <claude-user>@sg3 -Check`.
2. Read the second line, right after `ssh ok`.

**Expected:** `room-toolchain account ok SG3\claude: not elevated, not in an admin group, not the operator's account`,
and the rest of the output is what it was before. No file on sg3 changed. Run it again with the elevated clint account
as the target to see the warn: `account warn SG3\clint is in Administrators (S-1-5-32-544) and the token is elevated. an
agent in this room ... see docs/room-accounts.md, and -IAcceptRunningAsMe says you accept it`.

### @LETTER@3. The operator's own account, the heuristic

1. `pwsh -File scripts\room-toolchain.ps1 local -Check` as yourself.
2. `pwsh -File scripts\room-toolchain.ps1 <you>@<another machine> -Check`, then again with `-OperatorAccount <you>`.

**Expected:** 1 is `account warn <you> is the account that runs this script, on this machine, so it is the operator's
own login` (a `local` target is always this). 2 is `ok` for a login with no admin rights, because a different machine
under your own name cannot be told from a dedicated account, and `warn ... is listed as an operator account` once
`-OperatorAccount` (or `ATRIUM_OPERATOR_ACCOUNT=<you>`, or `<you>@<host>`) names it.

### @LETTER@4. macOS and Linux, admin groups and sudo

1. Against m1mini (a standard `claude` user): `pwsh -File scripts/room-toolchain.ps1 claude@m1mini -Check`.
2. Against an account in the `admin` group on a Mac, or in `sudo` or `wheel` on Linux, then one with a passwordless
   sudoers line (`<user> ALL=(ALL) NOPASSWD: ALL` in a file under `/etc/sudoers.d`, put there by hand for this test).

**Expected:** 1 is `ok claude: not elevated, not in an admin group, no passwordless sudo, not the operator's account`. 2
is a `warn` naming the group, or `can run sudo with no password (sudo -n -l says NOPASSWD: ALL)`. Remove the test
sudoers line after. Nothing on the target is written by the probe. `sudo -n -l` appears in the target's sudo log like
any use.

### @LETTER@5. The flags

1. Against an account that warns: `provision-room.ps1 <target> -Name <room> -IAcceptRunningAsMe`. It prints `provision
   account-rights ok accepted by the operator (-IAcceptRunningAsMe): <account> ...` and goes on.
2. The same with `-RequireDedicatedAccount`. It prints `account-rights fail` and ends `provision done fail 6` before the
   hub is looked for. `room-toolchain.ps1 <target> -RequireDedicatedAccount` ends `room-toolchain done fail 1`.
3. Both flags together, or `-OperatorAccount "a b"`, exit 1 with `args fail` before ssh is tried.
4. `provision-room.ps1 <target> -Name <room> -Remove -RequireDedicatedAccount` is not refused: `-Remove`, `-Restart` and
   `-SmokeOnly` never run the step.

**Expected:** as described. `room-check.ps1 <room>` prints `room-check account ...` right after its `ssh` row, and the
account row never changes its exit code.

### @LETTER@6. The page

1. Open `docs/room-accounts.md`. Run the Windows, macOS or Linux block on a throwaway machine, as the administrator,
   then `provision-room.ps1` and `room-toolchain.ps1` as the new account.

**Expected:** the account is a standard user, the room provisions, and `account-rights` is `ok`. This is the part
nothing here could run. See "Not proven" below.

### @LETTER@7. NOT proven here (macOS, pwsh 7, no Windows, no Linux target)

- Windows PowerShell 5.1 running the Windows probe. It is parsed by the pwsh 7 parser, scanned for syntax 5.1 lacks, run
  under pwsh on the Mac (where `WindowsIdentity` is unsupported and it reports `err=` and warns, which is tested), and
  measured. A real token, UAC, a filtered token, Domain Admins and Enterprise Admins are tested only as fixtures of the
  SID list. Whether `WindowsIdentity.Groups` of a filtered token lists the deny-only Administrators SID is documented
  behaviour and was not observed here.
- `sudo -n -l` on a machine with passwordless sudo. The outputs it is judged on are fixtures from the sudo manual's
  format.
- `id -Gn` on Linux. The macOS one was read on this Mac (m1mini, `claude`: staff everyone localaccounts and others, no
  admin).
- Every account-creating command in `docs/room-accounts.md` for Windows and Linux (`New-LocalUser`,
  `Add-LocalGroupMember -SID`, the OpenSSH capability, `runas`, `icacls` with `*S-1-5-32-544`, `useradd`, `loginctl
  enable-linger`, `usermod`), and on the Mac `sysadminctl -addUser ... -password -` (that the dash prompts),
  `dseditgroup` creating `com.apple.access_ssh` and `systemsetup`. They are syntax checked. The key and group-folder
  commands ran on the Mac against a temporary folder.
- The macOS LaunchAgent loading in the new account's session, and Full Disk Access for `sshd-keygen-wrapper`.

### @LETTER@8. Decided

- decided: the step is a `warn` that goes on, and `-RequireDedicatedAccount` is the opt in `fail` / the brief's call,
  and a refusal would break rooms that run as their owner today. `-IAcceptRunningAsMe` turns the warn into an `ok
  accepted by the operator` that still names the reason, so the log shows the choice.
- decided: the exit code of `-RequireDedicatedAccount` is 6 in `provision-room.ps1` (its existing "refused" class) and 1
  in `room-toolchain.ps1` / it has no refuse class, 1 is its "a local problem" code, and a new code 7 would collide with
  whatever the preflight change adds to the same script.
- decided: the step is `account-rights` in `provision-room.ps1` and `account` in the other two / provision already
  prints `account ok|fail` for the `-User` check, and two lines with one step name would be ambiguous for the board
  dialog. It is not run for `-Remove`, `-Restart` or `-SmokeOnly`, so a refusal can never stop a removal.
- decided: both flags together are an argument error (exit 1) / they say opposite things, like `-Autostart` and
  `-NoAutostart`.
- decided: a probe that cannot tell (an `err=` line, no output, or a nonzero exit) is a `warn` that goes on, and a
  `fail` under `-RequireDedicatedAccount` / enforcing a rule you cannot check would be a hole.
- decided: Windows groups are matched by SID (S-1-5-32-544, and the RIDs 512 and 519 under S-1-5-21-x-y-z), from
  `WindowsIdentity.Groups`, not from `whoami /groups` / the SIDs do not change with the language, and the same call
  gives the elevation (`IsInRole(Administrator)`), so a filtered token that is still a member reads as a member. The
  brief's `whoami /groups` fixture is covered by a SID list fixture, since the payload never reads names.
- decided: the operator's own account is two rules, a list (`-OperatorAccount`, `ATRIUM_OPERATOR_ACCOUNT`, as `name`,
  `DOMAIN\name` or `name@host`) and "the login is the account running the script and the target is this machine", where
  "this machine" is a `local` target or a host name that matches (case and domain suffix ignored) / the brief's (b) plus
  the explicit list. A logged-in console session (a) was skipped as not cheap, and the home directory is never looked
  at.
- decided: group names `admin` and `wheel` count on macOS, and `sudo`, `wheel` and `admin` on Linux, as whole names. On
  Linux `docker` is not on the list. On macOS the group `sudo` is not.
- decided: `sudo -n -l` is judged by its exit code and by `NOPASSWD: ALL` (passwordless sudo) or a plain `ALL` line
  (sudo with no prompt, a cached credential). A `NOPASSWD` for one named command is not a warn / it is a legitimate
  narrow grant.
- decided: provision never creates an account, so there is no "create it non-admin" code to change.
  `docs/room-accounts.md` says so and gives the standard-user commands. The header of `provision-room.ps1` says it too.
- decided: `room-check.ps1` gets an `account` row that is only `ok` or `warn`, with no `-RequireDedicatedAccount` and no
  fix / it is check only and advice. `-IAcceptRunningAsMe` and `-OperatorAccount` are accepted so its row can match the
  other two.
- decided: the `ok` text is `<account>: not elevated, not in an admin group, no passwordless sudo, not the operator's
  account` on macOS and Linux and without the sudo clause on Windows, which has none.
- decided: no `-PayloadSizes` line for the probe. That hook's count is asserted by the C tests, and the preflight change
  edits the same area. The size is measured in `test-room-account.ps1` instead (the probe is a few hundred characters,
  far under 7800, and still under it with provision-room's longest preamble).
- decided: two existing checks in `test-room-toolchain-c.ps1` ignore the new `account` line, because `local` is always
  the operator's account on the machine running the test, so it is a `warn` there, and a Windows sim has no token.
- decided: the doc says "tested offline, not on a real Windows or Linux target" in its own last section, because some
  commands there could not be run.

### @LETTER@9. Mutation checks

Each of these was made red in a throwaway copy and put back (the test is `scripts/test-room-account.ps1`, and the number
is how many checks went red). All 32 were red.

`room-account.ps1`: the Administrators SID changed (13), the elevated reason removed (4), the Domain Admins RID (1), the
Enterprise Admins RID (1), uid 0 (2), the sudo exit code (6), the macOS admin group removed (2), group names matched by
prefix (1), the operator same-login rule inverted (6), `name@host` host rule inverted (2), `local` target not trusted
(1), `-RequireDedicatedAccount` not refusing (4), and not refusing on an unknown probe (2), `-IAcceptRunningAsMe`
ignored (3), the reason joiner (5), the `ok` text (2), `sudo -n -l` changed to `sudo -l` (1), a write added to the
Windows probe (1), `NOPASSWD` count test (2), the pointer to the doc (3). `room-toolchain.ps1`: the step removed (11),
the early skip path without it (1), the refusal ignored (2), the both-flags check removed (1), accept not passed (1).
`provision-room.ps1`: exit 1 instead of 6 (1), the step renamed (5), the step run for `-Remove` (7), the operator list
dropped (5), macOS read as Linux (5). `room-check.ps1`: the row removed (6), the operator list dropped (7).
