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

1. `pwsh -NoProfile -File scripts/test-room-account.ps1` prints `all N checks pass` (189 at the time of writing). It
   needs no ssh. It runs a fake ssh against `room-toolchain.ps1` and `provision-room.ps1`, and `room-check.ps1` too when
   python3 and an atrium with the `requirements` subcommand are there (it says `skip room-check end to end` otherwise).
2. `pwsh -NoProfile -File scripts/check-powershell.ps1` ends `all powershell parses.`
3. `pwsh -NoProfile -File scripts/test-room-toolchain-c.ps1` still passes (544 checks). Run beside other heavy work, its
   `sim` checks fail on the base commit too, so rerun it alone if one does. `scripts/test-room-folders.ps1` fails at its
   `args.txt` check on a Mac on the base commit too, which this change did not touch.

**Expected:** no `FAIL` line from 1 and 2.

### @LETTER@2. A real clean room says ok, one line

1. On sg4, `pwsh -File scripts\room-toolchain.ps1 <claude-user>@sg3 -Check`.
2. Read the second line, right after `ssh ok`.

**Expected:** `room-toolchain account ok SG3\claude: not elevated, not in an admin group, not the operator's account`,
and the rest of the output is what it was before. No file on sg3 changed. Run it again with the elevated clint account
as the target to see the warn (and from a NON-elevated desktop shell as clint, `room-toolchain.ps1 local -Check` must
say `is in Administrators (S-1-5-32-544) with a filtered token`, which is the case a `WindowsIdentity.Groups` read
missed): `account warn SG3\clint is in Administrators (S-1-5-32-544) and the token is elevated. an agent in this room
... see docs/room-accounts.md, and -IAcceptRunningAsMe says you accept it`.

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
   sudoers line (`<user> ALL=(ALL) NOPASSWD: ALL` in a file under `/etc/sudoers.d`, put there by hand for this test),
   then one with a narrow line (`<user> ALL=(ALL) NOPASSWD: /usr/bin/vim`), then a Linux account in `docker`.

**Expected:** 1 is `ok claude: not elevated, not in an admin group, no passwordless sudo, not the operator's account`. 2
is a `warn` naming the group, or `can run any command with sudo and no password (sudo -n -l says NOPASSWD: ALL)`, or for
the narrow line `can use sudo with no password for some command (sudo -n -l works), and one command is usually a way to
root`, or `is in the group docker (root on this host)`. Remove the test sudoers lines after. Nothing on the target is
written by the probe. `sudo -n -l` appears in the target's sudo log like any use.

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
  SID list. The `whoami /groups /fo csv /nh` outputs the probe's own parse line is run over (a filtered token, an
  elevated one, a German one, a domain admin with a comma in a group name) are written in the documented format and were
  NOT captured on a Windows machine, and the German attribute text is from memory. The probe never reads that column.
  WHAT IS REAL: `scripts/fixtures/whoami-groups-sg4-claude-nonadmin.csv`, a capture from sg4 of `SG4\claude` in a
  non-admin shell, 13 rows (a plain account, NOT a filtered admin token, which is still to come from clint, and neither
  is an elevated one). It is in the group docker-users, so its verdict is a WARN by name, and the "ordinary non-admin
  account reads ok" test uses the capture with that one row dropped (derived in the test, the file is untouched). It
  holds well-known SIDs and two local groups under a machine SID, and the capture is REAL APART
  FROM ONE SUBSTITUTION: sg4's machine SID was replaced by the placeholder S-1-5-21-1111111111-2222222222-3333333333,
  since a real machine SID is an identifier that is not published. The test does not depend on the value. It confirmed
  the format and showed the integrity label row has an EMPTY attribute column, which the other fixtures now copy. It is
  the "ordinary non-admin account reads ok" case of the parse test. That `whoami` lists a deny-only Administrators on a
  filtered token, and `WindowsIdentity.Groups` does not, is the review's finding and documented behaviour, and was not
  observed here.
- `sudo -n -l` on a machine with passwordless sudo. The outputs it is judged on are fixtures from the sudo manual's
  format. The 5 second cut-off is run for real on this Mac, with a fake `sudo` that never answers, and the whole-call
  cap with a fake ssh that never answers.
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
- decided: Windows groups are matched by SID (S-1-5-32-544, 551 and 578, and the RIDs 512 and 519 under S-1-5-21-x-y-z),
  read from `whoami /groups /fo csv /nh` (column 3), NOT from `WindowsIdentity.Groups` / review M1: under UAC an admin's
  filtered token holds Administrators as deny-only, .NET's `Groups` drops those and `IsInRole` is false, so a filtered
  admin read as a clean `ok`. whoami lists them. Elevation still comes from `IsInRole(Administrator)`, and a member
  with `elevated=False` is said as `with a filtered token`. The attribute column ("Group used for deny only") is
  localized, so it is not read: a SID in the list is a member. An empty list is a probe failure (every token has
  Everyone), never a clean account.
- decided: Backup Operators (S-1-5-32-551) and Hyper-V Administrators (S-1-5-32-578) are admin-equivalent by SID, each
  with its own reason / review L3, they read or mount anything. Server and Account Operators are left out (domain
  controller only).
- decided: the operator's own account is two rules, a list (`-OperatorAccount`, `ATRIUM_OPERATOR_ACCOUNT`, as `name`,
  `DOMAIN\name` or `name@host`) and "the login is the account running the script and the target is this machine", where
  "this machine" is a `local` target or a host name that matches (case and domain suffix ignored) / the brief's (b) plus
  the explicit list. A logged-in console session (a) was skipped as not cheap, and the home directory is never looked
  at.
- decided: group names `admin` and `wheel` count on macOS, and `sudo`, `wheel` and `admin` on Linux, as whole names. On
  Linux `docker`, `lxd`, `incus-admin` and `libvirt` count too, with the reason `(root on this host)` / review M2,
  `docker run -v /:/h` takes the host, and a wrong ok is worse than a wrong warn. On macOS nothing new, and the group
  `sudo` is not an admin group there.
- decided: ANY `sudo -n -l` that exits 0 is a warn / review M2, a narrow NOPASSWD rule is usually a shell escape (vim,
  less, find, tar, systemctl, docker, pip, a writable script). `NOPASSWD: ALL` and a plain `ALL` line stay as the more
  specific reasons, and a rule for one command says `can use sudo with no password for some command`. The cost is a warn
  for an account with, for example, `NOPASSWD: /usr/bin/systemctl restart x`, which `-IAcceptRunningAsMe` answers. The
  `ALL` wording says what was seen (`can run any command with sudo`), not that no password was asked / review L2.
- decided: `sudo -n -l` is cut off after 5 seconds by a background watcher (macOS has no `timeout(1)`, so one code path
  for both) and reads `sudo.rc=timeout`, which is a warn `may have sudo, since sudo -n -l did not answer in 5 seconds`
  and not a clean ok / review L1, the cut-off leaves the other facts standing.
- decided: the 5 second watcher is "usually", not a bound / review N1, a sudo that made itself root (real uid 0) cannot
  be signalled by the user, and a process group kill is refused the same way, so the capture waits. What bounds it is
  the 45 second cap on the whole call (`could not tell`), and the comment and the doc say so.
- decided: the probe output is delimited by a marker pair made up on THIS side for each run (`ATRIUM-ACCT-<guid>`), the
  probe prints it around its facts and only the text between is read / review N2, ssh runs the login shell, whose rc
  files print before the probe and whose EXIT trap prints after it, which could print `uid=`/`groups=` and read an admin
  as clean. Exactly one begin and one end in order, else `could not tell`. A fact said twice with two values (inside the
  pair, or in the fake) is `could not tell ... tampered with or noisy`, never ok, and `fail` under
  `-RequireDedicatedAccount`. The first value is what is taken when a repeat agrees. Only the probe's own keys are
  compared. The shell is not made to skip its rc files, since ssh chooses it. On Windows the marker is in the command
  line, where a process of the same account could read it, so there it guards against a profile and noise only.
- decided: on Windows `docker-users` (Docker Desktop's group, which reaches host files) is matched by NAME as well as
  the SID list, with the reason `is in the group docker-users (Docker Desktop reaches host files)` / review N4, its RID
  is different on every machine, and the installer names it itself so the name is not localized. The probe prints only
  a count (`docker=N`), from the same whoami rows.
- decided: the marker caveat is said for Unix as well as Windows / review N3, the login shell runs `~/.bashrc` before
  `sh -s` and can read stdin, which holds the marker, so the marker defends against noise and a naive profile and not
  against a hostile account. The comments and the doc say so.
- decided: the whole probe call is capped at 45 seconds by `Invoke-AccountProbe` in `room-account.ps1`, which runs the
  ssh (or `sh -s`, or `powershell.exe` for `local`) as a process and kills it, and a cap, a failed start or a missing
  group list is `warn could not tell`, `fail` under `-RequireDedicatedAccount` / review L1, an unresponsive SSSD or ssh
  must not hang. 45 is above the 25 second `ConnectTimeout`. It replaces `Invoke-Remote` for this one call in all three
  scripts, since `Invoke-Remote` has no cap and the probe needs no PATH wrapper. The Windows probe goes as plain
  `-EncodedCommand`, which the test measures.
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
is how many checks went red). All 32 were red, and then 15 more for the review fix.

`room-account.ps1`: the Administrators SID changed (13), the elevated reason removed (4), the Domain Admins RID (1), the
Enterprise Admins RID (1), uid 0 (2), the sudo exit code (6), the macOS admin group removed (2), group names matched by
prefix (1), the operator same-login rule inverted (6), `name@host` host rule inverted (2), `local` target not trusted
(1), `-RequireDedicatedAccount` not refusing (4), and not refusing on an unknown probe (2), `-IAcceptRunningAsMe`
ignored (3), the reason joiner (5), the `ok` text (2), `sudo -n -l` changed to `sudo -l` (1), a write added to the
Windows probe (1), `NOPASSWD` count test (2), the pointer to the doc (3). `room-toolchain.ps1`: the step removed (11),
the early skip path without it (1), the refusal ignored (2), the both-flags check removed (1), accept not passed (1).
`provision-room.ps1`: exit 1 instead of 6 (1), the step renamed (5), the step run for `-Remove` (7), the operator list
dropped (5), macOS read as Linux (5). `room-check.ps1`: the row removed (6), the operator list dropped (7).

Review fix, made red the same way. The number is the checks red beyond the 7 room-check end to end ones that cannot run
from a copy outside the repo. The groups read from `.Groups` instead of whoami (6), whoami column 1 for 3 (7), an empty
group list read as clean (2), Backup Operators dropped (1), Hyper-V Administrators dropped (1), the `with a filtered
token` note dropped (6), the Linux root groups emptied (3) and applied to macOS (1), a narrow sudo rule clean again (2),
`sudo.rc=timeout` not a reason (1), the `ALL` wording put back (1), exit 137 not mapped to `timeout` (1), the watcher's
kill removed (2), the cap's err text changed (2), the no-groups text removed (1).

Follow-up for N2, same way (net of the 7 room-check ones). The Unix begin marker not sent (23), the Unix end marker not
sent (22), the Windows begin marker not sent (1), the markers ignored so every line is taken (7), a repeated fact with
another value ignored (4), the marker count not checked as once each (3), end before begin allowed (1), no marker with
exit 0 read as an empty answer (2). "Last value wins instead of first" is an equivalent mutant (0), since any two
different values are already `could not tell`.

N4, same way (net of the 7 room-check ones). The docker-users name match loosened to a substring (1), the docker-users
reason removed (3), the name read from the SID column (4), and the probe's `docker=` line not printed (the test run
aborts, since the helper finds no line).
