# Review: f-room-accounts 71748e0b

Range `47820430..71748e0b`, one commit. It adds `scripts/room-account.ps1`, which the three scripts below call, and
`docs/room-accounts.md`. The callers are `provision-room.ps1` (`account-rights`), `room-toolchain.ps1` (`account`) and
`room-check.ps1` (an `account` row).

Verdict: **HOLD** on M1. On Windows a non-elevated admin reads as `ok`, which is the wrong `ok` the brief ranks worst.
The other points pass, or have a Medium or Low below.

## How it was checked

- I read `room-account.ps1`, the three call-site diffs, `test-room-account.ps1` and the doc at the tip.
- I ran `pwsh -File scripts/test-room-account.ps1` from a detached worktree at 71748e0b: 94 of 126 ok.
  - All 32 failures are in the fake-ssh toolchain and provision runs, plus the real-probe `sudo.rc` check.
  - That is the sandbox class in REVIEWER-NOTES: no exec from `$TMPDIR`. For those parts I rely on @fabric's counts.
- On this Mac, `sudo -n -l </dev/null` returns rc 1 with "a password is required" and no prompt, as designed.

## Findings

### M1 (HOLD): a filtered Windows token hides Administrators, so a non-elevated admin is `ok`

`WindowsIdentity.Groups` returns only the groups whose attributes, masked with ENABLED, LOGON_ID and USE_FOR_DENY_ONLY,
equal ENABLED. Deny-only groups are skipped.

Under UAC, the filtered token of an admin carries Administrators (S-1-5-32-544) as deny-only. Domain Admins and
Enterprise Admins are deny-only as well. `IsInRole(Administrator)` is False for the same reason. So for an admin
account in a non-elevated session, the probe returns:

- `elevated=False`
- a `sids=` list with no 544, 512 or 519

`Get-AccountVerdict` then finds no reason, and the line says
`ok ...: not elevated, not in an admin group, not the operator's account`.

Where this happens:

- `room-toolchain.ps1 local` run from an ordinary desktop shell;
- `room-check.ps1` run the same way;
- a room started by its logon task without "run with highest privileges".

These are the cases where the operator is most likely the admin. Over Windows OpenSSH an admin usually gets the full
token (elevated=True), so the remote path mostly warns correctly. That is how the tests and the doc came out right
for sg3.

Three things say otherwise and are wrong:

- the script comment, which says a filtered token still lists Administrators;
- the check `win: a filtered token is still a member`, whose fixture has 544 in the list together with
  `elevated=False`, a pair .NET never produces;
- the doc, which says membership is matched by SID.

The `with a filtered token` reason branch is unreachable in practice.

Fix: read the groups including deny-only ones. Any of these works, and each is read-only:

- `whoami /groups /fo csv /nh`, taking column 3, the SID;
- a P/Invoke of `GetTokenInformation(TokenGroups)` without the filter;
- `GetTokenInformation(TokenLinkedToken)`, to look at the elevated half.

`whoami` is the smallest. It is present on every supported Windows and is locale-proof once read by SID. Also:

- change the fixture to what a filtered token really yields;
- add a check that a deny-only 544 from the new source warns;
- mutation-check that dropping the deny-only source turns the check red.

### M2: Unix false negatives, the cheap ones

The doc says "`sudo -n -l` working without a password" warns. The code warns only when that output also matches
`NOPASSWD: ALL` or `(…) ALL`.

`sudo -n -l` with rc 0 and no cached credential means some rule is NOPASSWD. A narrow-looking NOPASSWD rule is still
root when the command takes a shell escape: `vim`, `less`, `find`, `tar`, `systemctl`, `docker`, `pip`, any script the
user can write. Test 70 (`sudo that works for one named command is fine`) asserts the wrong `ok`.

Fix: warn on any `sudo.rc=0`, and keep the ALL and NOPASSWD wording as the more specific reason. The cost is a warn
for an account that has, for example, `NOPASSWD: /usr/bin/systemctl restart x`. That trade fits the brief: a wrong
`ok` is worse than a wrong warn.

On Linux, `docker`, `lxd` (and `incus-admin`) and `libvirt` are root-equivalent: `docker run -v /:/h` takes the host.
Test 66 (`docker alone is not on the list`) is a deliberate choice, and it lands on the wrong side of the same trade.
Add them, with a reason that says why, for example `is in the group docker (root on this host)`.

### L1: `sudo -n -l` has no time cap

`-n` stops a prompt, but sudo can still wait on the network to resolve the host or reach LDAP or SSSD. That is a known
slow path on a machine with a dead directory server. The ssh call itself has no timeout in `Invoke-AccountStep`
either.

Fix: put `timeout 10` in front where it exists (Linux coreutils; macOS has none), or cap the ssh call. If the probe is
cut off, the result should read as the unknown warn, which is already handled.

### L2: the `sudo.all` reason overstates

`can run sudo with no password prompt (sudo -n -l lists ALL)` fires on rc 0 with `(ALL) ALL`. That case is either a
cached credential or a rule with a password. Say what was seen instead, for example
`can run any command with sudo (sudo -n -l lists ALL)`.

### L3: other root-equivalent Windows groups are not checked

Backup Operators (S-1-5-32-551) can read every file. Hyper-V Administrators (S-1-5-32-578) can mount any disk. Server
Operators (549) and Account Operators (548) are more on a domain controller. This is optional, but 551 is worth a
reason line.

### L4: the doc's Linux shared-folder line sets setgid on files

`chmod -R g+rwXs /srv/work` puts `s` on every regular file as well, and an executable file that is setgid `work` runs
as that group. The intent is setgid directories, so new files inherit the group:

```sh
sudo chgrp -R work /srv/work && sudo chmod -R g+rwX /srv/work && sudo find /srv/work -type d -exec chmod g+s {} +
```

### L5: small doc points

- **Windows:** if a folder called `C:\Users\claude` already exists, the first logon via `runas` creates
  `C:\Users\claude.SG3`. One clause in the doc would cover it: "if the profile landed elsewhere, use that path".
- **Windows:** `Add-LocalGroupMember -SID S-1-5-32-545` errors when the account is already in Users. The error is
  harmless, but it is red ink in a "paste this" block.
- **macOS:** `systemsetup -setremotelogin on` needs Full Disk Access for the terminal on current macOS, or it errors.
  The doc should say so, or point to System Settings > General > Sharing > Remote Login.
- **Linux:** `sudo -n -l` leaves a line in the auth log on Linux as well. The doc says "sudo logs it like any use",
  which is right. Keep it.

## The five points

1. **Read-only probes.** Pass, apart from L1.
   - Neither probe takes an interpolated value. The tests prove there is no `$Target`, `$User` or `$Prefix`, so no
     quoting is needed.
   - The only sudo is `sudo -n -l </dev/null`, and nothing in either probe writes.
   - Output is only the matched reason. The checks that no group is dumped hold.
2. **Detection.**
   - SIDs: pass. 544 is matched exactly, and 512 and 519 are matched by their whole RID: `5120` and `1512` do not
     match. A domain group named Administrators (`-1105`) does not match.
   - uid 0 and the group lists: pass.
   - Filtered token: fail, M1.
   - False negatives on Unix: M2.
3. **Operator heuristic.** Pass. The doc says what it catches, and it says it cannot see your own account on another
   machine. Nothing under the home is read, and the probe reads only `id`, `uname -n`, the token and `sudo -l`.
4. **Exit codes and paths.** Pass.
   - Only `Refuse` exits: `provision-room.ps1` with 6, `room-toolchain.ps1` with 1.
   - `account-rights` is skipped on `-Remove`, `-Restart` and `-SmokeOnly`.
   - The `room-check.ps1` row is never unmet and never takes Require.
   - Giving both flags is an argument error.
   - A clean account adds one `ok` line. That is per @fabric's fake-ssh runs; here those runs stop at the sandbox.
5. **The doc.** M1 makes its Windows membership claim untrue until fixed, and M2 does the same for its `sudo -n -l`
   claim. Otherwise:
   - Commands: no password reaches history (`Read-Host -AsSecureString`, `sysadminctl -password -`), and no secrets.
     The `com.apple.access_ssh` block adds the operator before it can lock them out, and says why.
   - Lock-out: Deny log on applies to `claude` only, and the doc says to settle the restart first.
   - The admin-keys trap is right: for an Administrators member, `sshd`'s default `Match Group administrators` reads
     `administrators_authorized_keys`, which must be owned only by SYSTEM and Administrators.
   - The `authorized_keys` ACL by SID is right.
   - Voice: fine. The real-names scenario uses the room and account names already in the repo, and no private paths.

## To close the hold

- M1 with its fixture and the mutation check.
- M2, or a reply giving the reason to keep test 70 and test 66 as they are, which I would then note as accepted.
- The Lows can ride along or follow.

Atrium-Verdict: hold 47820430..71748e0b
Quality: careful work and honest testing. The hold is one .NET fact that the fixture assumed the wrong way.

## Re-read: 4699c740

Range `47820430..4699c740`, adding one commit to 71748e0b.

Closed:
- **M1.** The groups now come from `whoami /groups /fo csv /nh`. Only the SID column is read, through `ConvertFrom-Csv`
  with named headers, so a comma in a quoted group name is safe. The localized attribute column is never read.
  - A filtered token is a member with the filtered note.
  - An empty list, or no `S-1-*` at all, is a throw and then `Known = $false`. That is a warn, or a fail under
    Require, and never ok.
  - `.Groups` is gone from the probe.
  - The fixtures are in the documented format: filtered, elevated and German. The doc and the test header both say
    they were not captured on a real machine. Getting one capture from sg4 after landing is the right follow-up.
- **M2.**
  - Any `sudo.rc=0` warns, with three wordings.
  - `docker`, `lxd`, `incus-admin` and `libvirt` warn as root on Linux, and the test that called docker clean is gone.
  - A timeout reads as "may have sudo".
- **L1.** It is closed in effect, with one remark, N1 below. `Invoke-AccountProbe` caps the whole call at 45 seconds,
  kills the process tree, and turns a cap into the could-not-tell warn, or a refusal under Require.
- **L2.** The wording is fixed.
- **L3.** 551 and 578 are matched by SID.
- **L4.** The doc now uses `g+rwX` and sets `g+s` on directories only, with the reason given.
- **L5.**
  - the `claude.SG3` note;
  - `Add-LocalGroupMember` guarded by a membership check;
  - Full Disk Access for `systemsetup`.

How it was checked:
- I read the diff of room-account.ps1, the doc and the fixtures.
- `test-room-account.ps1` at the tip gives 123 of 159 ok here. All 36 failures use a fake ssh or a fake sudo from
  `$TMPDIR`, which is the sandbox class, so for those I rely on @fabric's 159/159.
- I ran the Unix probe by hand twice: once against the real sudo, which gives `sudo.rc=1` at once, and once against a
  fake sudo that hangs. That second run gave N1.

Open, Low, none of which holds:
- **N1: the 5 second sudo cut-off is not always 5 seconds.** I used a fake `sudo`, a shell script that runs
  `sleep 60`. The watcher kills it at 5 seconds and the probe prints `sudo.rc=timeout`, but the probe took 60 seconds.
  That is because the killed process's child still holds the `$( )` pipe.

  A real sudo has no such child. But a sudo that has set its real uid to root cannot be signalled by the user, so
  `kill` fails quietly. In both cases the 45 second cap is what actually bounds the probe.

  The doc's "cut off after 5 seconds" should say "usually, and the whole probe never waits more than 45 seconds", or
  the watcher should kill the process group. The comment in room-account.ps1 should say the same.
- **N2: an agent can change the probe's answer.** The probe's output is parsed with the last line winning. An
  account's `~/.bashrc` (bash reads it for an ssh command) or an EXIT trap set there can print `uid=` or `groups=`
  lines after the probe. So an agent on that account can make it read clean. Nothing new: 71748e0b had the same parse.
  Taking the first value of each key, and refusing a key seen twice, would close it cheaply.

Verdict: room-ok and hub-ok (re-read, 47820430..4699c740)
Quality: a thorough fix with real-format fixtures and a 45-second cap that holds. N1 is wording.

Atrium-Verdict: room-ok 47820430..4699c740
Atrium-Verdict: hub-ok 47820430..4699c740

## Re-read: 0ac1ff3e (follow-up)

Range `47820430..0ac1ff3e`. Two commits on 4699c740, which is already on landing: 2c4ddfb8 and 0ac1ff3e. @fabric
counted three, but `4699c740..0ac1ff3e` holds these two.

Verdict: **HOLD, on the history only.** The code and docs are OK.

**What holds it.** 2c4ddfb8 adds `scripts/fixtures/whoami-groups-sg4-claude-nonadmin.csv` with sg4's real machine SID
in four rows. 0ac1ff3e only replaces it on top, so the real SID stays in 2c4ddfb8 for anyone who reads the history.

The repo is public, and the orchestrator is scrubbing that same SID out of the daemon testdata as a pause exception
(r-scrub-sid). So landing it here in a new commit would undo that work. The fix is to squash 2c4ddfb8 and 0ac1ff3e into
one commit, or to rebuild the branch without the real value, and send the new sha. It is on no shared branch yet:
`git branch -a --contains 2c4ddfb8` lists only `claude/f-room-accounts`.

Closed:
- **N2.**
  - `Select-ProbeLines` takes only what sits between exactly one begin and one end of a per-run marker. No marker, a
    repeated marker, or markers out of order is `err=`, so could-not-tell.
  - `Get-AccountResult` keeps the first value of each key. The same key said again with a different value is
    could-not-tell, which refuses under Require.
- **N1.** The comment and the doc say "usually 5 seconds, bounded by the 45 second cap".

How it was checked:
- I dot-sourced room-account.ps1 at the tip and called the functions directly.
  - Noise before and after the markers saying `uid=1000` did not hide `groups=...,sudo`. The answer was still the
    sudo warn.
  - `uid=0` then `uid=1000` inside the markers gave could-not-tell.
  - A doubled begin gave the tampered `err=`.
  - Lines with no markers gave the noisy `err=`.
- `test-room-account.ps1` gives 139 of 184 here. All 45 failures are the fake-ssh or fake-sudo `$TMPDIR` class,
  including the new noise checks, so for those I rely on @fabric's 184/184.
- The fixture after 0ac1ff3e has a placeholder machine SID. The only host name in it is the `SG4\` group prefix, which
  is already all over the repo.

Open, Low:
- **N3: a determined account can learn the Unix marker too.** The marker reaches the target on stdin, but the
  account's own `~/.bashrc` runs before `sh -s` and can read stdin. It can then take the marker and print a full fake
  answer between real markers. That is the same limit the doc already states for Windows' command line. Say it for
  Unix as well: the marker defeats noise and a careless profile, not the account itself.
- **N4: `docker-users` on Windows.** The real capture shows `SG4\docker-users`. Docker Desktop's group can drive the
  engine, and through its host mounts that can reach the host's files. It is not a fixed SID, since the RID varies, but
  the name is not localized. Consider a warn on that name. It is optional.

Atrium-Verdict: hold 47820430..0ac1ff3e
Quality: the marker and the first-value parse are right and well tested. The hold is a real SID left in one commit.

## Re-read: 14422eb9 (the follow-up, rewritten)

Range `47820430..14422eb9`. One commit on 4699c740, replacing 2c4ddfb8 and 0ac1ff3e.

Closed:
- **The history hold.** `git log -p 4699c740..14422eb9` holds none of the three parts of sg4's machine SID, and the
  fixture has the placeholder from its first commit.
  - The tip's tree still holds the SID in `internal/daemon/testdata/frame-settled-statusline.bin`. That is because
    the branch is based on 4699c740, before r-scrub-sid. The branch does not touch that file, so a merge onto landing,
    which has the scrub, keeps the scrubbed copy. Rebase onto landing to be sure.
- **N3.** The Unix caveat sits beside the Windows one, in the doc and in both comments: the marker guards against noise
  and a naive profile, not a hostile account.
- **N4.** `docker-users` is matched by name, and only a `docker=<count>` line is printed.
  - Called directly, `docker=1` gives the warn "is in the group docker-users (Docker Desktop reaches host files)", and
    `docker=0` gives ok.
  - The real capture now warns, and the ok test takes that row out of a copy of the capture.

The marker and first-value work is unchanged from 0ac1ff3e, which I had already passed as code.

One Low: `docker` is not in `$probeKeys`, so a second `docker=` with another value is not flagged as a conflict. The
first value still wins, so nothing reads clean because of it. Add it when the file is next touched.

Verdict: room-ok and hub-ok (re-read, 47820430..14422eb9)

Atrium-Verdict: room-ok 47820430..14422eb9
Atrium-Verdict: hub-ok 47820430..14422eb9
