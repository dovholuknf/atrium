# Run a room as its own account

Atrium does not recommend running a room as an administrator, or as your own everyday account. Give the room an account
of its own that is a standard user, called `localai`, and let a separate administrator run the few commands that need
rights. You drive the agents from your own account through the board, which carries their terminals, files and
permission requests to your browser.

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

Atrium warns when the account is an administrator. On Windows that is an elevated token, or membership in
Administrators, Domain Admins, Enterprise Admins, Backup Operators or Hyper-V Administrators, or the group
`docker-users` (Docker Desktop's, which reaches host files, matched by name because its RID differs on every machine).
The groups are read from `whoami /groups` and matched by SID, so a localized machine reads the same. An administrator in
an ordinary, non-elevated session holds a UAC filtered token, which carries Administrators as a deny-only group.
`whoami` lists it and .NET's own group list leaves it out, so that case warns too, and says `with a filtered token`.

On macOS and Linux it warns for uid 0 and for the groups `admin`, `sudo` or `wheel` as the platform has them. On Linux
it also warns for `docker`, `lxd`, `incus-admin` and `libvirt`, each of which is root on the host. And it warns when
`sudo -n -l` works at all without a password, even for one named command, because a rule for `vim`, `find` or
`systemctl` is usually a way to a root shell. That command only lists, never prompts and never runs anything, but sudo
logs it like any use.

A probe that cannot answer is a warn that goes on, never a wait. `sudo -n -l` is usually cut off after 5 seconds (a
directory server that does not reply will do it), which reads `may have sudo`. A sudo that has made itself root cannot
be signalled by the user, so that cut-off can fail, and then the whole probe is cut off after 45 seconds, never more,
which reads `could not tell who the room runs as`.

The answer is not taken on trust either. ssh runs the account's login shell, so its rc files can print before the probe
and an exit trap can print after it, and either could say `uid=1000` to make an admin read as clean. Atrium makes up a
marker for each run, the probe prints it around its facts, and only the lines between the pair are read. A fact said
twice with two values, a missing marker or a repeated one is a warn, `could not tell ... tampered with or noisy`, never
an `ok`. This guards against noise and a naive profile, and not against a hostile account. On Windows the marker travels
in the command line, which a process of the same account can read. On Unix it arrives on stdin, and the login shell
starts before `sh -s` does, so `~/.bashrc` runs first and can read stdin, which means a determined account can learn the
marker too.

It also warns when the account looks like yours, and that part is a heuristic. It catches two cases. The login is one
you listed with `-OperatorAccount name` or the environment variable `ATRIUM_OPERATOR_ACCOUNT`, as `name`, `DOMAIN\name`
or `name@host`. Or the login is the account running the script and the target is the same machine, which is always true
for `room-toolchain.ps1 local`. It cannot tell your own account on a different machine from a dedicated one, so list
that one. Nothing in the account's files or history is read.

A warn looks like this, and a clean account says `ok`:

```
room-toolchain account warn SG3\clint is in Administrators (S-1-5-32-544) and the token is elevated. an agent in this room does whatever it is talked into with that account's rights. see docs/room-accounts.md, and -IAcceptRunningAsMe says you accept it
room-toolchain account warn SG3\clint is in Administrators (S-1-5-32-544) with a filtered token. an agent in this room does whatever it is talked into with that account's rights. see docs/room-accounts.md, and -IAcceptRunningAsMe says you accept it
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

## New machines: localai and the shared folder

A machine added from now on is provisioned as `localai`, with no flag. `provision-room.ps1` checks for the account on a
new machine (no manifest, no room) and never creates it. When it is missing the `account` line prints the one command
(`net user localai /add`, `sysadminctl -addUser localai` or `sudo useradd -m localai`) and the run stops with exit 11.
A machine that already is a room is left alone, whatever account it runs under, so sg3 stays on `claude`.
`-KeepAccount` is the explicit opt-out for a new machine that should run under its own login.

The `shared-folder` step then makes `C:\Users\Public\atrium`, `/Users/Shared/atrium` or `/srv/atrium` for the repository
clones, which `room-git.ps1 init -GitRoot` places under it. When the login may not make it (usually `/srv`) the line
prints the command an administrator runs and the run stops with exit 12. `-NoSharedFolder` keeps clones in `~/git`.
`provision-room.ps1 -Check` runs these steps read only and changes nothing. After the join and before the room
starts, provisioning runs `atrium room set git_root <folder>` on the remote, so the room looks for its clones there. By
hand, with the room stopped: `atrium room set git_root /srv/atrium`, and `atrium room get git_root` to read it.

## Set one up

Each block says what it does, then does it. Swap in your own names, paths and key. The administrator runs everything
here except the `provision-room.ps1` and `room-toolchain.ps1` lines, which run from the hub as the new account. What was
actually run is at the end of this page.

### Windows

This makes a local user in no group but Users, with a password nobody types into a history file, and grants it its
folders. Run it in an elevated PowerShell on the room.

```powershell
$pw = Read-Host -AsSecureString 'password for the new account'
New-LocalUser -Name localai -Password $pw -FullName 'atrium room' -Description 'runs the atrium room, not an admin' -PasswordNeverExpires
if (-not (Get-LocalGroupMember -SID S-1-5-32-545 | Where-Object Name -like '*\localai')) { Add-LocalGroupMember -SID S-1-5-32-545 -Member localai }
icacls D:\worktrees /grant 'SG3\localai:(OI)(CI)M'
```

Then the work drive, if the machine has one. Do not grant on the drive's top folder the way you grant on `D:\worktrees`.
Claude Code examines every folder on the way to a path it writes, so the account must be able to read the attributes of
each parent, and must still be unable to list the drive or create anything at its root. See
[Keep agent files off the system drive](#keep-agent-files-off-the-system-drive) for why. The first line can take a long
time on a big drive, and should be left to finish.

```powershell
# localai may read V:\'s attributes, cannot list it, cannot create in it, and nothing is inherited
icacls V:\ /grant 'SG3\localai:(RA,REA)'
# the agent folder, full control, inherited below
New-Item -ItemType Directory V:\localai
icacls V:\localai /grant 'SG3\localai:(OI)(CI)F'
```

Now ssh. Windows OpenSSH reads a standard user's keys from that user's own `.ssh`. The profile folder does not exist
until the account has logged on once, so log it on once first, then put the hub's public key in place and lock the file
down using SIDs, not names. Add the server first if the machine has none.

```powershell
Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
Set-Service sshd -StartupType Automatic
Start-Service sshd
runas /user:SG3\localai "cmd /c exit"
New-Item -ItemType Directory -Force C:\Users\localai\.ssh | Out-Null
Set-Content C:\Users\localai\.ssh\authorized_keys 'ssh-ed25519 AAAA...the-hubs-public-key hub'
icacls C:\Users\localai\.ssh\authorized_keys /inheritance:r /grant 'SG3\localai:F' /grant '*S-1-5-18:F' /grant '*S-1-5-32-544:F'
```

If a folder called `C:\Users\localai` already existed, Windows made the profile as `C:\Users\localai.SG3` instead. Use
whatever path the profile landed at in the two lines that name it.

This is where being a standard user saves trouble. For a member of Administrators, `sshd` ignores that file
and reads `C:\ProgramData\ssh\administrators_authorized_keys` instead, which must belong only to SYSTEM and
Administrators. People paste a key where it looks right and get a password prompt. A standard account never meets it.

To keep the account off the console, deny it Deny log on locally and Deny log on through Remote Desktop Services in
`secpol.msc`, under Local Policies and User Rights Assignment. The autostart task fires at that account's logon, as the
costs section says, so settle how the room restarts first.

Then the Defender line from `room-defender.ps1`, pasted by the administrator, and from the hub:

```powershell
pwsh -File scripts\provision-room.ps1 localai@sg3 -Name sg3 -WorkRoot V:\localai
pwsh -File scripts\room-toolchain.ps1 localai@sg3
```

Drop `-WorkRoot` on a machine with one disk. With it, exit 13 prints the administrator's lines if the grants above were
not made.

### macOS

This makes a standard user (no `-admin`), lets only chosen users in over ssh, and puts the key in. `sysadminctl` asks
for the password so it stays out of your history.

```sh
sudo sysadminctl -addUser localai -fullName 'atrium room' -password -
sudo dseditgroup -o create -q com.apple.access_ssh
sudo dseditgroup -o edit -a localai -t user com.apple.access_ssh
sudo dseditgroup -o edit -a "$(id -un)" -t user com.apple.access_ssh
sudo systemsetup -setremotelogin on
sudo -u localai sh -c 'umask 077 && mkdir -p /Users/localai/.ssh && echo "ssh-ed25519 AAAA...the-hubs-public-key hub" >> /Users/localai/.ssh/authorized_keys'
```

The third `dseditgroup` line adds you. Once `com.apple.access_ssh` exists only its members can log in over ssh, you
included. On current macOS `systemsetup -setremotelogin on` needs Full Disk Access for the terminal you run it in and
errors without it. Turn Remote Login on in System Settings, General, Sharing instead if that happens. A folder shared
with your own account gets a group:

```sh
sudo dseditgroup -o create work
sudo dseditgroup -o edit -a localai -t user work
sudo chgrp -R work /Users/Shared/work && sudo chmod -R g+rwX /Users/Shared/work
```

Then `pwsh -File scripts/provision-room.ps1 localai@m1mini -Name m1mini` from the hub. The room is a launchd user agent,
and it loads into the account's desktop session, so `localai` needs one desktop login before it does. Until then the
script leaves the agent written and says so.

One caveat. macOS keeps `~/Desktop`, `~/Documents`, `~/Downloads` and external volumes behind privacy prompts that an
ssh session cannot answer. Keep the room's work somewhere else, such as `~/work`. Granting Full Disk Access to
`/usr/libexec/sshd-keygen-wrapper` fixes it by letting every ssh login read those folders, which is the thing this page
is about avoiding, so do it only if you must.

### Linux

This makes a normal user with no sudo group and no password, keeps its user services running after logout, and puts the
key in.

```sh
sudo useradd -m -s /bin/bash localai
id localai
sudo loginctl enable-linger localai
sudo install -d -m 700 -o localai -g localai /home/localai/.ssh
echo 'ssh-ed25519 AAAA...the-hubs-public-key hub' | sudo -u localai tee -a /home/localai/.ssh/authorized_keys
sudo chmod 600 /home/localai/.ssh/authorized_keys
```

`id localai` should show no `sudo`, `wheel` or `admin`. If a distribution refuses key logins for an account with no
password, that is `sshd` treating the account as locked, and `sudo usermod -p '*' localai` fixes it. Shared folders get
a group:

```sh
sudo groupadd work
sudo usermod -aG work localai
sudo chgrp -R work /srv/work && sudo chmod -R g+rwX /srv/work
sudo find /srv/work -type d -exec chmod g+s {} +
```

The setgid bit goes on the directories only, so new files inherit the group. `chmod -R g+s` would put it on every file,
and an executable that is setgid `work` runs as that group.

Then `pwsh -File scripts/provision-room.ps1 localai@lab1 -Name lab1` from the hub installs the room as a systemd user
unit for `localai`. `-Linger` on that command does the `enable-linger` step if you skipped it.

## Keep agent files off the system drive

An agent room fills a disk. Every pull request is a worktree, every worktree has a `node_modules` (0.41 GB for
ziti-console), and the npm cache grows beside them (0.40 GB on sg3). On sg3 all of it landed on C: because the room's
`git.scm_root` was `C:/Users/claude/git` and the caches sat in `claude`'s profile. The rule is that everything an agent
writes goes on the work volume, and nothing of it on the system drive.

### The Claude Code prompt

Putting the work on another drive has a trap. Claude Code examines every folder on the way to a path before it writes
there. When it cannot, it raises a prompt of its own:

```
Where V:/work/localai/... leads on disk could not be determined (a link or directory on the way could not be examined,
or the links do not resolve)
```

That prompt is not atrium's. The permission hook had already answered allow on sg3 (board auto mode, recorded four
times) and the prompt still showed, so the card sat blocked in its terminal until a person answered it. Neither atrium
rules nor auto mode can answer it. The cause on sg3 was that `claude` had full control of `V:\work\localai` and no access
at all to `V:\` or `V:\work`. Every parent folder of the agent folder has to be examinable by the room's account, which
is the `(RA,REA)` grant in the Windows block above. It gives the account the attributes of `V:\` and nothing else: no
listing, no creating, and nothing inherited. Check it as the room's account. Creating `V:\probe` must be refused,
creating a folder under `V:\localai` must work, and a Claude Code session must write there with no prompt.

### Let provisioning do it: `-WorkRoot`

`provision-room.ps1 localai@sg3 -Name sg3 -WorkRoot V:\localai` sets up everything below as the room's own account, with
no admin. It takes the place of the shared folder, and it lays out one folder:

| Folder | What goes there | How it is set |
| --- | --- | --- |
| `V:\localai\git` | the clones, and their `<clone>-worktrees` folders | `atrium room set git_root` and `scm_root`, and `room-git.ps1 init -GitRoot` |
| `V:\localai\reviews` | review run folders | `atrium room set reviews_root` |
| `V:\localai\handoff` | context hand-off files | `atrium room set context_handoff_dir` |
| `V:\localai\cache\npm` | npm's cache | `cache=` in the account's `~/.npmrc` |
| `V:\localai\cache\go-mod`, `go-build` | `GOMODCACHE` and `GOCACHE` | the file `go env -w` writes, under `%APPDATA%\go\env` |
| `V:\localai\cache\pip` | pip's cache | `cache-dir` under `[global]` in `%APPDATA%\pip\pip.ini` |
| `V:\localai\cache\cargo` | `CARGO_HOME` | a user environment variable, or a profile line on Unix |

None of that is done by the script itself. `provision-room.ps1` writes a `room.yaml` for the machine
(`scripts/room-spec.ps1`). It first asks the atrium on the machine it runs on whether the work root is one the
room's operating system accepts (`atrium room setup --validate --os <os> --work-root <dir> --account <login>`,
strings only: no ssh, no disk). A relative folder, a drive root, a UNC path, a root of the wrong kind for the OS, a
system folder, another user's home (or root's, or `C:\Users\Public`) is exit 1 with its reason before anything on
the room changes. Then it runs the room's own `atrium room setup --spec - --plan` over ssh, and `--apply` after the
join. What only the room's disk can tell (a link, junction or 8.3 short name that leads into another home, the
account's real profile folder when it is not named like the login) is judged there, by the plan or the apply. A
machine with no atrium yet (or one too old to have `room setup`) has its plan skipped, with a warn: the first run
installs an atrium and then asks, so a root that only the room's disk refuses is found after the account and the
binary were put there. That is the trade: `-Check` and `room-check` cannot read the work root of a machine with no
current atrium, where the old probe could, and so give a warn or a skip and not the early 13.

With no `-WorkRoot` the room gets a `room.yaml` of the agent pack alone: the shared folder stays as it was and the
pack is installed and read as before. The agent pack never stops a run: a pack that cannot be fetched or installed
is a `warn` and not an exit code. A mirror that cannot be fetched is a warn too, and the run goes on. `-NoAgentPack`
is recorded in the manifest (`agentpack: none`), so `room-check` reads a room with no pack record against the
default repository and shows a missing pack as a row, unless that choice was made.

The caches are written as each tool's own file, so they work before the tool is installed, and a rerun edits the file in
place and writes nothing when it is already right. When the tool is there it is asked afterwards, and an answer that
differs (`GOENV`, a project `.npmrc`) is a `warn`. A running room cannot be set, so a rerun reads the settings it serves
from `GET /v1/settings` and says `ok` when they match. The root is recorded as `workroot` in the manifest, so a rerun
needs no flag. `-Remove` leaves the folder and the cache settings alone and says so.

The `work-root` step reads every parent of the root as the room's account first. When one cannot be examined, or the root
cannot be made or written, the step fails with exit 13, nothing else has changed, and the lines for an administrator are
printed under it. For a root of `V:\localai` they are `icacls V:\ /grant 'SG3\localai:(RA,REA)'` and the two lines that
make the folder and give the account full control of it, and never anything that lets the account list the drive or
create at its root. The drive line is the attributes only and is not inherited: do not widen it to `(OI)(CI)RX`, which
would let the account list the drive and read all of it. A mapped drive belongs to one logon session and is not there over
ssh, so name a drive letter that is a real disk. On a Windows machine with a second fixed drive and no `-WorkRoot`,
`provision-room.ps1` and `-Check` say so as a `warn` naming the roomiest drive.

`room-check.ps1` has the same read, the rows of `atrium room setup --plan` for a `room.yaml` rebuilt from the manifest:
`work-root` (with `work-dirs` and `work-cache`), the four settings, and `agent-pack`. An account that cannot examine a
parent is `human` there, and anything else is a `warn`.

By hand, as the room's account, with the room stopped for the first:

- `git.scm_root`: `atrium room set scm_root V:/localai/git`, and `atrium room get scm_root` to read it back.
- The npm cache: `npm config set cache V:\localai\cache\npm`.
- The other tool caches the same way: Go's `GOMODCACHE` and `GOCACHE`, pip's cache folder, and cargo's `CARGO_HOME`.

Do not use a symlink or a junction to make a short path lead to the work drive, or to get past the prompt above. A link
hides where the files really are and adds one more path for Claude Code to examine. On sg3 the link was not the cause,
the unreadable parents were. The rule is clint's choice: point the settings at the real folder.

Agents must never be able to create a folder at the root of the work drive, so do not grant the account anything on it
beyond the attributes. Run a dependency install once, from one session. Two concurrent `npm ci` in one `node_modules`
failed with `ENOTEMPTY`.

### macOS and Linux

The same idea applies and the commands are the ones above. Every parent folder of the work folder must be readable and
searchable by the room's user. Provision prints the line for each parent that is not: `setfacl -m u:<account>:x <parent>`
(`chmod +a` on a Mac), which opens it to that account alone and never to every user, after `install -d` for one that is
missing. The filesystem root is never touched. A check (`-Check`, room-check) writes nothing on the room, and the caches and
`scm_root` belong on the work volume, not in the home folder on the boot disk.

## The agent pack

A room is only as useful as the agents it can name. A new provision installs the operator's agents and skills, the
`claude/agents/*.md` files and every `claude/skills/<name>/` folder that has a `SKILL.md` in the `dovholuknf/dotfiles`
repository (`-AgentPackRepo` and `-AgentPackBranch` change it, `-NoAgentPack` skips it), into the account's `~/.claude`
as real files. The commit they came from is recorded in `~/.claude/atrium-agent-pack.json` beside a SHA-256 for each file.
A rerun at the same commit changes nothing, a file edited on the room is put back, and an agent that is not in the repo is
left alone. A skill that is a link into another repository (two on sg3, `debug-ziti-desktop-edge-win` and
`debug-ziti-edge-tunnel-log`) is not fetched and is named in a `skip` line.

A `room.yaml` may also list a `codex` and a `gemini` pack, each taken from its own folder of the repository (`codex/`,
`gemini/`, or `from:`) and put where that runner reads it: gemini's `agents/*.md` and `skills/` in `~/.gemini`, and codex's
`skills/` alone in `~/.codex`, since codex reads no markdown agents. Each has its own `atrium-agent-pack.json` and its own
`agent-pack-<runner>` row. A variable that moves a runner's folder (`GEMINI_CLI_HOME`, `CODEX_HOME`) is followed, read from
the account's user environment where the OS keeps one (the Windows registry) and else from the environment the process was
started with, not from a login profile, which an ssh command does not read. A `%NAME%` in a registry value is expanded first. The moved
folder is used only when it is absolute for the OS, has no `..` and no `%` or `$` left, is not the folder of another runner's
pack (or inside or around it), and lies under the account's home or the work root; otherwise the row is a `fail` naming the
variable and its value, and nothing is installed. The provision script needs no `room.yaml` for
this: it installs the claude pack always, and the codex and gemini packs when the repository has a `codex/` or `gemini/`
folder with `agents` or `skills` in it. The runners are recorded in the manifest, so `room-check.ps1` shows an
`agent-pack-codex` or `agent-pack-gemini` row for each. `-NoAgentPack` still means none at all.

The source is the hub's own git mirror of the repository, never the checkout on the operator's machine. The room does not
fetch it itself: the room's `/git/hub` forwarder is tokenized per card and no card exists when a room is provisioned. So
the hub machine clones the mirror, whole, since the hub serves whole fetches only, tars the checkout with its `.git`, and
sends it over scp, and the room's `atrium room setup --apply --pack-dir <folder>` installs from that. A mirror that cannot
be fetched is a `warn`, not a failure, and the run goes on. `room-check.ps1` and `provision-room.ps1 -Check` read the
record and `warn` when the pack is missing, older than the mirror's commit (the scripts compare it, since the room is not
told the hub's address), or lacks an agent the review panel names by default (`c-systems-reviewer`, `go-security-reviewer`, `functional-tester` and
`nonfunctional-tester`).

## If you really must run as yourself

Sometimes it is one machine and one person. Then limit what the account holds, and be honest that this makes the first
mistake smaller and contains nothing.

Give the agent a browser profile that is not yours and is signed in to nothing. Do not forward your ssh agent into the
room (`ForwardAgent no`, and keep keys out of any agent the room's shell can reach). Keep cloud credentials out of
environment variables and shell profiles the room reads. Work in worktrees and set `-AllowedFolders` so atrium launches
nowhere else. Then say so with `-IAcceptRunningAsMe`, so the warn stops and the log shows it was deliberate.

## What was and was not run

The detection and its verdicts are tested offline in `scripts/test-room-account.ps1`, including a fake ssh against
`room-toolchain.ps1` and `provision-room.ps1`. The Windows group list is tested over `whoami /groups /fo csv /nh` output
written in its documented format, a filtered token and a German one among it, and none of it was captured on a Windows
machine. `sudo -n -l` is real on the Mac (it needs a password there) and a fixture elsewhere. Nothing here was run on a
real Windows or Linux machine, so the Windows and Linux blocks were parsed for syntax and not executed. On the Mac, the
key and group-folder commands ran against a temporary folder, and `sysadminctl` and `dseditgroup` were run for their
usage text only. Account creation, the `com.apple.access_ssh` group, `systemsetup`, `New-LocalUser`, the OpenSSH
capability, `runas` and `secpol.msc` were not run, and neither was `sysadminctl` taking its password from a prompt.

See also: `docs/release/packaging.md` (provisioning a room over ssh) and `docs/user-guide.md` (Windows Defender).
