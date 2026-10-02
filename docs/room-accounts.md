# Run a room as its own account

Atrium does not recommend running a room as an administrator, or as your own everyday account. Give the room an account
of its own that is a standard user, and let a separate administrator run the few commands that need rights.

That is the whole advice. The rest of this page is why, what it costs, what atrium says when you do not follow it, and
the commands to set it up on Windows, macOS and Linux.

## Why

An agent runs as whoever started it. It holds that account's ssh keys, its git identity and credentials, its browser
profile, the tokens its cloud CLIs left behind, and whatever administrator rights the account has. Whatever the agent
can be talked or tricked into doing, it does as that account.

It can be talked into things. A README in a repository it cloned, a pull request description, a web page it was asked to
read, the output of a tool: all of it is text the model reads, and any of it can carry instructions. The permission gate
does not change that. The gate is a speed bump and a record. It stops mistakes and it shows you what was asked for. It
does not contain an agent that has been talked into a command that looks like the dozen you already approved.

Here is the case with real names. Say the room on sg3 ran as clint. A worker is asked to look at a bug report, and the
report says that to reproduce the bug you run a one-line installer. The command looks ordinary and you approve it. As
clint it can read `~\.ssh`, the GitHub login in Git Credential Manager, the cloud CLI tokens in clint's profile and the
browser profile with every session clint is signed into. If clint is also an administrator, the same line can add a
Defender exclusion, open a firewall port or install a driver. One approval, and nothing about it needed a clever attack.

Now the setup this page asks for. The room on sg3 runs as `SG3\claude`, which is not elevated and is not in
Administrators, and clint is a separate administrator who runs the few lines that need one. `SG3\claude` has its own
home, its own `.ssh`, its own git identity and a GitHub login that clint gave that account and no other. The same
installer line runs, finds little worth taking, and cannot write outside the folders `claude` was granted. Atrium did
not do that. The operating system's file permissions did, which is the point: they were built to contain a user, and
atrium was not. m1mini has the same shape, a standard macOS user called `claude` that is in no admin group.

Administrator makes everything worse. An administrator is one prompt injection from changing the firewall, installing
something that survives a reboot, or reading every other user's files. There is no good reason for a room to be able to.

## What it costs

A few steps need rights the room's account does not have, so a human runs them. Giving `claude` write access to a shared
drive is one. When its own `icacls` is refused, `room-toolchain.ps1` prints the line for an administrator to paste:

```powershell
icacls V:\work\tools\msys64 /grant 'SG3\claude:(OI)(CI)M'
```

That gives `claude` Modify on the folder and everything under it, and `OI` and `CI` make new files and folders inherit
it. Excluding build output from Defender is another, and `room-defender.ps1` prints the line for an administrator, with
the paths of the account the agents run as:

```powershell
Add-MpPreference -ExclusionPath 'D:\worktrees', 'C:\Users\claude\AppData\Local\go-build', 'C:\Users\claude\go\pkg'
```

Two more happen once and need a person at the keyboard: `git credential-manager github login` at a console as the room's
user, and `ssh -t <target> claude auth login`. Installing atrium's hooks for the account is a third.

The account also needs a second home. The toolchain goes into that home (`room-toolchain.ps1` installs go, node and the
rest there with no admin), the git identity is set again, and credentials are entered again, on purpose. Files shared
with your own account need a group or an ACL, because that is what separate accounts mean. The room's autostart is a
task or unit tied to that account's logon, so decide how the room comes back after a reboot before you lock the account
out of logging on.

The first setup is the expensive one. After that the administrator is needed when a new shared folder appears.

## What atrium checks and says

`provision-room.ps1`, `room-toolchain.ps1` and `room-check.ps1` all read, over the ssh login they already use, who the
room will run as. They write nothing, and `-Check` is unchanged. Each says what it found in one line.

Atrium warns when the account is an administrator: on Windows an elevated token, or membership in Administrators, Domain
Admins or Enterprise Admins, matched by SID so a localized machine reads the same. On macOS and Linux it warns for uid
0, the groups `admin`, `sudo` or `wheel` as the platform has them, or `sudo -n -l` working without a password. That
command only lists, never prompts and never runs anything, but sudo logs it like any use.

It also warns when the account looks like yours, and that part is a heuristic. It catches two cases. The login is one
you listed with `-OperatorAccount name` or the environment variable `ATRIUM_OPERATOR_ACCOUNT`, as `name`, `DOMAIN\name`
or `name@host`. Or the login is the account running the script and the target is the same machine, which is always true
for `room-toolchain.ps1 local`. It cannot tell your own account on a different machine from a dedicated one, so list
that one. Nothing in the account's files or history is read.

A warn looks like this, and a clean account says `ok`:

```
room-toolchain account warn SG3\clint is in Administrators (S-1-5-32-544) and the token is elevated. an agent in this room does whatever it is talked into with that account's rights. see docs/room-accounts.md, and -IAcceptRunningAsMe says you accept it
room-toolchain account ok SG3\claude: not elevated, not in an admin group, not the operator's account
provision account-rights ok claude: not elevated, not in an admin group, no passwordless sudo, not the operator's account
```

It is advice, not a lock. The run goes on after a warn. `-IAcceptRunningAsMe` turns the warn into an `ok accepted by the
operator` that still names the reason, so the choice shows in the log. `-RequireDedicatedAccount` turns it into a `fail`
for a fleet that wants it enforced, and so does a probe that cannot tell. `provision-room.ps1` exits 6, which is its
refusal code, and `room-toolchain.ps1` exits 1. Giving both flags is an argument error.

In `provision-room.ps1` the step is called `account-rights`, because `account` is already the `-User` check there, and
it only runs for a provision, not for `-Remove`, `-Restart` or `-SmokeOnly`. `room-check.ps1` has an `account` row that
is only ever `ok` or `warn`, never fixed and never counted as unmet. Provisioning never creates an account, so it never
makes an administrator one. Whoever creates the account decides, and the next section creates a standard one.

## Set one up

Each block says what it does, then does it. Swap in your own names, paths and key. The administrator runs everything
here except the `provision-room.ps1` and `room-toolchain.ps1` lines, which run from the hub as the new account. What was
actually run is at the end of this page.

### Windows

This makes a local user in no group but Users, with a password nobody types into a history file, and grants it its
folders. Run it in an elevated PowerShell on the room.

```powershell
$pw = Read-Host -AsSecureString 'password for the new account'
New-LocalUser -Name claude -Password $pw -FullName 'atrium room' -Description 'runs the atrium room, not an admin' -PasswordNeverExpires
Add-LocalGroupMember -SID S-1-5-32-545 -Member claude
icacls D:\worktrees /grant 'SG3\claude:(OI)(CI)M'
icacls V:\work /grant 'SG3\claude:(OI)(CI)M'
```

Now ssh. Windows OpenSSH reads a standard user's keys from that user's own `.ssh`. The profile folder does not exist
until the account has logged on once, so log it on once first, then put the hub's public key in place and lock the file
down using SIDs, not names. Add the server first if the machine has none.

```powershell
Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
Set-Service sshd -StartupType Automatic
Start-Service sshd
runas /user:SG3\claude "cmd /c exit"
New-Item -ItemType Directory -Force C:\Users\claude\.ssh | Out-Null
Set-Content C:\Users\claude\.ssh\authorized_keys 'ssh-ed25519 AAAA...the-hubs-public-key hub'
icacls C:\Users\claude\.ssh\authorized_keys /inheritance:r /grant 'SG3\claude:F' /grant '*S-1-5-18:F' /grant '*S-1-5-32-544:F'
```

This is where being a standard user saves trouble. For a member of Administrators, `sshd` ignores that file
and reads `C:\ProgramData\ssh\administrators_authorized_keys` instead, which must belong only to SYSTEM and
Administrators. People paste a key where it looks right and get a password prompt. A standard account never meets it.

To keep the account off the console, deny it Deny log on locally and Deny log on through Remote Desktop Services in
`secpol.msc`, under Local Policies and User Rights Assignment. The autostart task fires at that account's logon, as the costs section says, so settle how the room restarts first.

Then the Defender line from `room-defender.ps1`, pasted by the administrator, and from the hub:

```powershell
pwsh -File scripts\provision-room.ps1 claude@sg3 -Name sg3
pwsh -File scripts\room-toolchain.ps1 claude@sg3
```

### macOS

This makes a standard user (no `-admin`), lets only chosen users in over ssh, and puts the key in. `sysadminctl` asks
for the password so it stays out of your history.

```sh
sudo sysadminctl -addUser claude -fullName 'atrium room' -password -
sudo dseditgroup -o create -q com.apple.access_ssh
sudo dseditgroup -o edit -a claude -t user com.apple.access_ssh
sudo dseditgroup -o edit -a "$(id -un)" -t user com.apple.access_ssh
sudo systemsetup -setremotelogin on
sudo -u claude sh -c 'umask 077 && mkdir -p /Users/claude/.ssh && echo "ssh-ed25519 AAAA...the-hubs-public-key hub" >> /Users/claude/.ssh/authorized_keys'
```

The third `dseditgroup` line adds you. Once `com.apple.access_ssh` exists only its members can log in over ssh, you
included. A folder shared with your own account gets a group:

```sh
sudo dseditgroup -o create work
sudo dseditgroup -o edit -a claude -t user work
sudo chgrp -R work /Users/Shared/work && sudo chmod -R g+rwX /Users/Shared/work
```

Then `pwsh -File scripts/provision-room.ps1 claude@m1mini -Name m1mini` from the hub. The room is a launchd user agent,
and it loads into the account's desktop session, so `claude` needs one desktop login before it does. Until then the
script leaves the agent written and says so.

One caveat. macOS keeps `~/Desktop`, `~/Documents`, `~/Downloads` and external volumes behind privacy prompts that an
ssh session cannot answer. Keep the room's work somewhere else, such as `~/work`. Granting Full Disk Access to
`/usr/libexec/sshd-keygen-wrapper` fixes it by letting every ssh login read those folders, which is the thing this page
is about avoiding, so do it only if you must.

### Linux

This makes a normal user with no sudo group and no password, keeps its user services running after logout, and puts the
key in.

```sh
sudo useradd -m -s /bin/bash claude
id claude
sudo loginctl enable-linger claude
sudo install -d -m 700 -o claude -g claude /home/claude/.ssh
echo 'ssh-ed25519 AAAA...the-hubs-public-key hub' | sudo -u claude tee -a /home/claude/.ssh/authorized_keys
sudo chmod 600 /home/claude/.ssh/authorized_keys
```

`id claude` should show no `sudo`, `wheel` or `admin`. If a distribution refuses key logins for an account with no
password, that is `sshd` treating the account as locked, and `sudo usermod -p '*' claude` fixes it. Shared folders get a
group:

```sh
sudo groupadd work
sudo usermod -aG work claude
sudo chgrp -R work /srv/work && sudo chmod -R g+rwXs /srv/work
```

Then `pwsh -File scripts/provision-room.ps1 claude@lab1 -Name lab1` from the hub installs the room as a systemd user
unit for `claude`. `-Linger` on that command does the `enable-linger` step if you skipped it.

## If you really must run as yourself

Sometimes it is one machine and one person. Then limit what the account holds, and be honest that this makes the first
mistake smaller and contains nothing.

Give the agent a browser profile that is not yours and is signed in to nothing. Do not forward your ssh agent into the
room (`ForwardAgent no`, and keep keys out of any agent the room's shell can reach). Keep cloud credentials out of
environment variables and shell profiles the room reads. Work in worktrees and set `-AllowedFolders` so atrium launches
nowhere else. Then say so with `-IAcceptRunningAsMe`, so the warn stops and the log shows it was deliberate.

## What was and was not run

The detection and its verdicts are tested offline in `scripts/test-room-account.ps1`, including a fake ssh against
`room-toolchain.ps1` and `provision-room.ps1`. Nothing here was run on a real Windows or Linux machine, so the Windows
and Linux blocks were parsed for syntax and not executed. On the Mac, the key and group-folder commands ran against a
temporary folder, and `sysadminctl` and `dseditgroup` were run for their usage text only. Account creation, the
`com.apple.access_ssh` group, `systemsetup`, `New-LocalUser`, the OpenSSH capability, `runas` and `secpol.msc` were not
run, and neither was `sysadminctl` taking its password from a prompt.

See also: `docs/release/packaging.md` (provisioning a room over ssh) and `docs/user-guide.md` (Windows Defender).
