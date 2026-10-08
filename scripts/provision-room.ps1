# Make a machine an atrium room of THIS hub, over ssh, with no admin or sudo.
#
#   pwsh -File scripts\provision-room.ps1 user@host
#   pwsh -File scripts\provision-room.ps1 user@host -Name lab1 -Runners claude,codex
#   pwsh -File scripts\provision-room.ps1 user@host -Install claude
#   pwsh -File scripts\provision-room.ps1 user@host -Remove
#
# Run it on the machine that runs the hub. It finds the hub from the running
# `atrium run` process, detects the remote OS and arch, puts a matching atrium in
# the remote home folder, joins that machine's room to the hub over whatever the
# hub links over (direct, ziti or zrok), starts the room, and checks the runners
# asked for. Run it again and it changes only what is not already right.
# `-Remove` undoes what it did.
#
# ONE ROOM PER MACHINE. A machine that is already a room, of this hub or any
# other, or that already runs an atrium this script did not put there, is
# refused. `-Remove` first.
#
# WHERE THE BINARY COMES FROM. By default the release on GitHub
# (dovholuknf/atrium) that matches the remote, checked against the release's
# checksums.txt. When there is no release (today there is none), no -Version was
# given and the script sits in a checkout, it prints a `fetch warn` line and
# builds from the checkout instead, so the one command needs no flags.
# `-FromCheckout` is the explicit form. A named -Version that is missing fails.
#
# -RESTART [-Yes] [-Force] stops the room and starts it again the way it was
# started: `room --detach` for a detached room, the platform's own verb for a
# logon task, systemd user unit or LaunchAgent. It prints the plan and changes
# nothing unless -Yes. It refuses (exit 9) while a card is mid-turn or waiting on
# a permission, and names it, unless -Force. Idle live cards are listed as parked
# and resumed and do not block. It refuses (exit 10) when the supervisor has the
# room switched off. The stop is `atrium stop` and a wait of up to 40s for the
# room's ports (default 7781 and 7777, or `ports` in the manifest) to close, never
# a kill, and a failed wait is a failure. The start is pinned to the state
# directory the join used (`statedir` in the manifest, backfilled by looking for
# room.json), so it comes back as the same room. Nothing is registered, enabled
# or disabled. Step lines: restart-mode, restart-cards, stop, start, attach.
#
# THE LAST TWO STEPS. `auth` runs `claude auth status` on the remote (through a
# login shell on Unix) and reads its JSON `loggedIn`. Not signed in is a `warn`
# carrying the command the operator runs once, `ssh -t <target> claude auth
# login`, which prints a URL and so works over ssh. It never reads or carries a
# credential, and it is not a failure. `smoke` then launches a small claude
# worker on the room through the hub (`-SmokeCwd`, `-SmokeTo`, `-SmokeTimeout`
# 180), waits for its report to hold a nonce, exits the card and confirms it
# left. It is skipped when auth warned, or with `-NoSmoke`. `-SmokeOnly` runs
# just these two against a room already provisioned, and changes nothing on it.
#
# AUTOSTART IS THE DEFAULT for a new provision: the logon task, systemd user unit
# or LaunchAgent is installed, so the room comes back after a reboot. On Windows
# the task's action is `room --detach` (after dot-sourcing
# ~\.atrium\toolchain\room-env.ps1 when it exists), so a room started by hand, by
# provision and by a logon is one path. `-NoAutostart` starts the room in the
# background with `atrium room --detach` and registers nothing, and it then runs
# until the machine restarts or the user logs out. `-Autostart` is accepted: a
# no-op on a new provision, and on a rerun it registers autostart for a room
# provisioned without. A RERUN KEEPS THE MODE THE MANIFEST RECORDS, so nothing on a
# machine already provisioned changes. `-Linger` (Linux, with autostart) keeps the
# room running after logout.
#
# NOBODY LOGGED IN AT A WINDOWS MACHINE. The logon task is Interactive, so `schtasks /Run` does nothing until someone is at
# the machine (last result 267011). Then the room is started with `room --detach`, which outlives the ssh session, and the
# step is `start warn <reason>`: the task starts it at the next logon. The run goes on to the git step, auth and the smoke.
# Only a failed `room --detach` too is `start fail` and exit 3. See scripts/room-start.ps1.
#
# GIT ON A BARE WINDOWS BOX. The git step is room-git.ps1 init. A Windows remote with no git (no winget, no admin) gets
# MinGit: the latest release or `-GitVersion 2.56.0`, SHA256 checked against what the release publishes, unpacked to
# ~\.local\git with its cmd folder on the user Path. macOS and Linux print the install command instead. See
# scripts/room-git.ps1 and scripts/room-mingit.ps1.
#
# A SMOKE THAT DOES NOT REPORT prints the card's own last screen lines under the fail line, and names the fix it knows. The
# usual one on a machine whose provision stopped early is that claude has no atrium-control tools, because the `mcp` step
# never ran: rerun without -SmokeOnly.
#
# THE ACCOUNT. `-User localai` says the account the room must run as. It is
# checked over the ssh login and never created: making an account needs admin, and
# provisioning runs without it. A missing account prints the one command an
# administrator runs (`net user localai /add`, `sysadminctl -addUser localai` or
# `sudo useradd -m localai`) and stops with exit 11. An account that exists but is
# not the ssh login is exit 1: target `localai@host`.
#
# THE LOCALAI ACCOUNT AND THE SHARED FOLDER (backlog 75), for a NEW machine, one with no manifest and no room.json.
# With no -User, the account must be localai, checked exactly as -User is: missing prints the command and stops with
# exit 11, and an ssh login that is not localai is exit 1. A machine that already is a room is left alone and the
# `account` line says `skip ... already provisioned under <login>` (sg3 stays on claude). `-KeepAccount` is the explicit
# opt-out for a new machine that runs under its own login. Then `shared-folder` makes C:\Users\Public\atrium,
# /Users/Shared/atrium or /srv/atrium, or when the login may not (usually /srv) fails with exit 12 and the one command an
# administrator runs, having changed nothing. Clones go under it (room-git.ps1 init -GitRoot). `-NoSharedFolder` keeps
# them in ~/git. After the join, before any start, `atrium room set git_root` on the remote
# points the room at it (`git-root`, a `warn` with the command when the room is already running).
# `-Check` runs the steps up to here read only and ends, so it changes nothing and needs no admin. A rerun skips both.
#
# THE WORK ROOT. `-WorkRoot V:\localai` keeps everything an agent writes on the work drive, nothing of it on the system
# drive, and takes the place of the shared folder: <work>\git (git_root, scm_root and the clones with their -worktrees
# folders), <work>\reviews (reviews_root), <work>\handoff (context_handoff_dir) and <work>\cache for npm, go (GOMODCACHE and
# GOCACHE), pip and cargo, which are written as each tool's own user file (.npmrc, go env, pip.ini, CARGO_HOME), never a
# link. ALL OF IT IS DONE BY THE ROOM'S OWN atrium: this script writes a room.yaml (scripts/room-spec.ps1) and runs
# `atrium room setup --spec - --plan` on the remote before anything is changed (a machine that has an atrium) and `--apply`
# after the join (internal/roomspec has the rules). Every parent of the root must be EXAMINABLE by the account, or Claude Code
# raises a prompt nothing can answer. When one is not, or the root cannot be made or written, the step is `work-root fail`
# with the lines an administrator runs (the drive gets attributes only, `icacls V:\ /grant <acct>:(RA,REA)`, no listing and
# no creating) and the exit is 13. A rerun keeps the recorded root and says `ok`, and settings of a running room are read
# from it. -Check reports the same read only, and on a Windows box with a second fixed drive and no -WorkRoot it advises one.
# THIS machine's atrium refuses a bad root for the room's OS first (`room setup --validate`, no ssh), so that refusal changes
# nothing. A machine with no atrium yet is checked by the first run. With no work root the room.yaml is the agent pack alone:
# the old shared folder behaviour stays and the pack is installed. A failed pack is a warn and never an exit code.
#
# THE AGENT PACK. Unless -NoAgentPack, the agents and skills of -AgentPackRepo (dovholuknf/dotfiles, branch
# -AgentPackBranch main), claude/agents/*.md and claude/skills/<name>/, are installed as real files in the account's
# ~/.claude by `atrium room setup --apply`, and the commit is recorded in ~/.claude/atrium-agent-pack.json. They come from
# the hub's own mirror of the repository, cloned WHOLE here and sent as a tarball (`--pack-dir`), because the room's
# /git/hub forwarder is tokenized per card and no card exists at provision time. A rerun at the same commit changes
# nothing. -Remove leaves the work root and the pack.
#
# THE ACCOUNT'S RIGHTS. The ssh login IS the account the room runs as, since everything here runs as that login. Right
# after the account check, `account-rights` (a provision run only, not -Remove, -Restart or -SmokeOnly) reads who that
# login is, read only, and says so loudly when it is an administrator (Windows: an elevated token, Administrators,
# Domain Admins. macOS and Linux: root, admin, sudo or wheel, or passwordless sudo) or the operator's own everyday
# account (best effort, see scripts/room-account.ps1: -OperatorAccount, or the login that runs this script when the
# target is this same machine). It is a `warn` naming the reason and docs/room-accounts.md, and the run goes on.
# -IAcceptRunningAsMe makes it `ok accepted by the operator`, still naming the reason. -RequireDedicatedAccount makes it
# a `fail` and exit 6, and so does a probe that cannot tell. This script never creates an account (see above), so it
# never makes an administrator one: the account is whatever the operator made, and the page says how to make it a
# standard one. The step is not called `account` because that is the -User check's.
#
# DEFENDER. On a Windows room, scripts/room-defender.ps1 runs after the clone: it reads the Go caches, the clone's
# build.claude and the worktree root as the ssh login, sets GOTMPDIR inside the Go cache, and excludes those paths
# from real-time protection when the login is elevated. Provisioning runs without admin, so usually it prints the
# one line an administrator pastes into an elevated shell, and writes nothing for them to run.
# `-NoDefender` skips it. -Remove does not undo it.
#
# ALLOWED FOLDERS. A room has a list of folders atrium may launch in (its `browse_roots` setting), and claude's folder
# trust is written for each, so no launch sits at "do you trust this folder" with nobody to answer. The `folders` step
# runs `atrium room folders allow <dir>...` on the remote (sh on macOS and Linux, PowerShell on Windows), after the
# clone and before `auth`. A NEW provision allows the room's clone, its `<clone>-worktrees` folder and WORKTREE_ROOT
# when the room has one. `-AllowedFolders /srv/work,C:/work` allows those instead, absolute paths only, `~/` accepted.
# A RERUN CHANGES NOTHING unless -AllowedFolders is given (as for autostart), and then adds to the list. The list is
# recorded as `allowed_folders` in the manifest. An atrium without the verb is a `warn` carrying the command to run
# later, and a folder the room skips (home, a filesystem root, missing) is a `warn` naming it. -Remove leaves the
# setting alone: it lives in the room's database and the trust in the account's ~/.claude.json, both of which the
# operator may have added to, and `atrium room folders` has no verb to take a folder out. After the smoke card,
# `smoke-outside` launches one outside the list and expects the room to refuse it, when `folders list --json` says the
# list is enforced, and says `skip` when it is not or the verb is missing.
#
# THE SMOKE FOLDER is the room's clone, else the path from this repository's git
# remote for the room, else `~/.atrium/smoke`, made when missing. Never the home. When the room enforces a list and
# that folder is outside it, the smoke runs in the first allowed folder and says so. A -SmokeCwd outside the list
# is a `warn` and no smoke card, since the room would only refuse it.
#
# ONE LINE PER STEP, for a person and for the board dialog that will call this
# later (backlog-2 item 46, stage 2). Every step line is
#
#   provision <step> <status> <detail>
#
# where status is `ok` (already right, nothing changed), `done` (changed now),
# `skip`, `warn` or `fail`. The last line is `provision done ok` or
# `provision done fail <code>`. Anything else on stdout is not a step line.
#
# EXIT CODES
#   0  provisioned, attached, every runner starts
#   1  a local problem: bad arguments, no hub found, no release, the build failed
#   2  ssh could not reach the target, or its OS is not one this covers
#   3  a remote install step failed: binary, autostart or start
#   4  the join failed, or the room did not attach to the hub
#   5  installed and attached, but a runner is missing, does not start, or would not install
#   6  refused: the remote is already a room, or runs an atrium this script did not install, or -RequireDedicatedAccount
#      and the room's account is an administrator or the operator's own
#   7  the overlay needs a credential only the operator can give: see the fail line
#   8  installed and attached, but the smoke card did not report
#   9  -Restart refused: a card is mid-turn or waiting on a permission, or the
#      cards could not be read. -Force goes ahead. Nothing was stopped
#  10  -Restart refused: the room's supervisor has it switched off (a sticky
#      stop), which a start would not undo. `service start` is the fix. Nothing
#      was stopped
#  11  -User names an account that does not exist on the remote. The line prints the
#      command to create it, which this never runs. Nothing was changed
#  12  the shared folder is missing or not writable for the ssh login. The line prints the command an administrator
#      runs. Nothing was changed
#  13  the work root is not usable by the room's account: a parent folder it cannot examine, or a root it cannot make or
#      write. The lines an administrator runs are printed. Nothing else was changed
#  (-Restart also uses 3 for a stop or start that did not work, 4 for a room
#  that did not come back attached, and 6 for a machine this did not provision)
#
# CREDENTIALS FOLLOW THE OVERLAYS RULE: atrium names the command that holds a
# credential and never holds somebody else's.
#   ssh    this names the ssh command and holds no key. ssh runs with BatchMode, so
#          a target that wants a password fails at once rather than at a prompt.
#   direct the room's single-use join string, minted for this run, good for an hour.
#   ziti   an enrollment JWT for the remote, from the operator's own network: a file
#          (-ZitiJwt) or a command the operator names (-ZitiJwtCommand). It goes to
#          the remote, is enrolled there with the key made there, and is deleted.
#   zrok   the remote needs its own `zrok2 enable`, which takes the operator's
#          account token. This never carries that token. It says what to run.
#
# WHAT GOES WHERE ON THE REMOTE
#   Windows  ~\.atrium\bin\atrium.exe
#   Linux    ~/.local/bin/atrium
#   macOS    ~/.local/bin/atrium
#   all      ~/.atrium/room (key, certificate or ziti identity, room.log),
#            ~/.atrium/atrium.db, ~/.atrium/provision/manifest.json
#   autostart (the default) adds a logon task `atrium` (RunLevel Limited), a systemd user unit
#            atrium.service, or a LaunchAgent io.github.dovholuknf.atrium, and
#            their scripts under ~/.atrium/provision
#   -Install  adds the runner where its own installer puts it, usually ~/.local.
#            CODEX is installed whole, because it needs codex-code-mode-host and
#            codex-resources beside it. With node and npm on the login PATH:
#            `npm install -g --prefix ~/.local @openai/codex`. Without: the whole
#            codex-package release in ~/.local/share/codex/<version>, with a
#            `codex` wrapper (a .cmd on Windows) in ~/.local/bin. -Remove deletes
#            only what the `installed=` lines named.
#
# A BARE -SmokeTo (no @) gets `@<this side's room>` from $env:ATRIUM_ROOM, since
# the remote room cannot find a handle without one. A handle with @ is left alone.
#
# The manifest records what was already there before the first run, so -Remove
# deletes only what this script created and leaves anything older alone.

param(
    # The ssh destination: user@host, or a Host alias from ssh config.
    [string] $Target,
    # What the hub calls the room. Default: the remote machine's hostname.
    [string] $Name,
    # The runners that must be present on the remote and answer --version.
    [string[]] $Runners = @('claude'),
    # Runners to fetch from their vendor and install on the remote, if missing.
    # Opt in only: see the trust warning it prints. Knows claude and codex.
    [string[]] $Install = @(),
    # Undo everything a previous run did, on the remote and on the hub.
    [switch] $Remove,
    # Stop the room tidily and start it again, the way it was started. Prints the
    # plan and changes nothing unless -Yes. -Force goes ahead over live cards.
    [switch] $Restart,
    [switch] $Yes,
    [switch] $Force,
    # The account the room must run as, for example localai. Checked, never created: see "the account".
    [string] $User,
    # A NEW machine must be provisioned as localai: see "the localai account". -KeepAccount is the explicit opt-out, for a
    # machine that runs under another account (sg3 on claude), and -NoSharedFolder keeps the clones in ~/git.
    [switch] $KeepAccount,
    [switch] $NoSharedFolder,
    # An absolute folder on the work drive, such as V:\localai: the clones, reviews, hand-offs and the tool caches all go under
    # it, nothing of them on the system drive. See "the work root". Takes the place of the shared folder.
    [string] $WorkRoot,
    # The operator's agents and skills are installed from the hub's mirror of -AgentPackRepo into the account's ~/.claude,
    # unless -NoAgentPack. See "the agent pack".
    [switch] $NoAgentPack,
    [string] $AgentPackRepo = 'dovholuknf/dotfiles',
    [string] $AgentPackBranch = 'main',
    # Read only: the account and shared folder steps say what they find and what they would do, then the run ends.
    [switch] $Check,
    # The account's rights, see "the account's rights". The operator's own accounts: name, DOMAIN\name or name@host.
    [string[]] $OperatorAccount = @(),
    [switch] $IAcceptRunningAsMe,
    [switch] $RequireDedicatedAccount,

    # Build the binary from this checkout instead of fetching a release.
    [switch] $FromCheckout,
    # The release to fetch. Default: the latest.
    [string] $Version,
    # A prebuilt atrium for the remote's OS and arch, instead of either.
    [string] $Binary,

    # Autostart is on by default for a new provision. -NoAutostart starts the room in the background and registers
    # nothing. -Autostart is accepted and is a no-op on a new provision, and on a rerun it registers autostart for a
    # room that was provisioned without it.
    [switch] $Autostart,
    [switch] $NoAutostart,
    # Linux with autostart: turn on lingering so the room survives logout.
    [switch] $Linger,

    # Ziti hubs: the remote's enrollment JWT, as a file, or as a command that
    # prints one. `{name}` in the command becomes the room's name.
    [string] $ZitiJwt,
    [string] $ZitiJwtCommand,

    # The ssh and scp commands and any extra options for both (-i, -J, -o ...).
    # A port goes as `-o Port=2222`, because scp reads -p as something else.
    [string] $Ssh = 'ssh',
    [string] $Scp = 'scp',
    [string[]] $SshOption = @(),

    # The hub. Default: read from the running `atrium run` process. -HubAddr
    # picks one hub when more than one is running.
    [string] $HubExe,
    [string] $HubDir,
    [string] $Link,
    [string] $LinkAdvertise,
    [string] $HubAddr,

    # 'none' skips the git clone that room-git.ps1 makes by push. Anything else makes it.
    [string] $Repo = 'atrium',
    # A Windows room with no git gets MinGit, with no admin: the latest release, or this one (2.56.0). See "git on a bare Windows box".
    [string] $GitVersion,
    # Skip the Defender step on a Windows room: no exclusions, and GOTMPDIR left alone. See room-defender.ps1.
    [switch] $NoDefender,
    # The folders atrium may launch in on the room, absolute paths on the remote. Default for a new provision: the clone,
    # its -worktrees folder and WORKTREE_ROOT. A rerun changes nothing unless this is given. See "allowed folders".
    [string[]] $AllowedFolders = @(),

    # How long to wait for the room to show as attached on the hub, in seconds. 60 was too short on sg3 and sg4-wsl
    # (2026-09-29), even apart from the attach race the hub now fixes.
    [int] $AttachTimeout = 120,

    # The smoke card, last: a small claude worker on the room that reports back.
    # -SmokeTo is who it atrium_says "smoke ok <room> <nonce>" to, default the
    # card running this script when there is one. -SmokeCwd is where on the
    # remote it runs, default the clone room-git.ps1 made, else the remote home.
    # -SmokeOnly runs only auth and smoke against a room already provisioned,
    # and changes nothing on it, so it is safe against a room in use.
    [switch] $NoSmoke,
    [switch] $SmokeOnly,
    [string] $SmokeTo,
    [string] $SmokeCwd,
    [int] $SmokeTimeout = 180,
    # Which runners get a smoke card: claude and codex have a case. Default, every
    # runner in -Runners and -Install.
    [string[]] $SmokeRunners = @()
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
# UTF-8 WITHOUT A BOM for what is piped to ssh. A profile that sets $OutputEncoding with one puts EF BB BF in front of
# the sh script, and its first line then fails (`sh: 1: A=...: not found`).
$OutputEncoding = [Text.UTF8Encoding]::new($false)
if ($SmokeOnly -and ($Remove -or $NoSmoke)) { Write-Host 'provision args fail -SmokeOnly goes with neither -Remove nor -NoSmoke'; exit 1 }
if ($Autostart -and $NoAutostart) { Write-Host 'provision args fail -Autostart and -NoAutostart say opposite things'; exit 1 }
if ($Restart -and ($Remove -or $SmokeOnly -or $Autostart -or $NoAutostart -or $Install.Count -or $Binary -or $FromCheckout -or $Version)) {
    Write-Host 'provision args fail -Restart changes nothing but the running room, so it goes with none of -Remove, -SmokeOnly, -Autostart, -NoAutostart, -Install, -Binary, -FromCheckout, -Version'; exit 1
}
if (($Yes -or $Force) -and -not $Restart) { Write-Host 'provision args fail -Yes and -Force belong to -Restart'; exit 1 }
if ($IAcceptRunningAsMe -and $RequireDedicatedAccount) { Write-Host 'provision args fail -IAcceptRunningAsMe and -RequireDedicatedAccount say opposite things'; exit 1 }
if ($KeepAccount -and $User) { Write-Host 'provision args fail -KeepAccount and -User say opposite things'; exit 1 }
if (($Check -or $KeepAccount -or $NoSharedFolder) -and ($Remove -or $Restart -or $SmokeOnly)) {
    Write-Host 'provision args fail -Check, -KeepAccount and -NoSharedFolder belong to a provision run, not -Remove, -Restart or -SmokeOnly'; exit 1
}
if (($WorkRoot -or $NoAgentPack) -and ($Remove -or $Restart -or $SmokeOnly)) {
    Write-Host 'provision args fail -WorkRoot and -NoAgentPack belong to a provision run, not -Remove, -Restart or -SmokeOnly'; exit 1
}
if ($WorkRoot -and $NoSharedFolder) { Write-Host 'provision args fail -WorkRoot takes the place of the shared folder, so it goes with no -NoSharedFolder'; exit 1 }
if ($AllowedFolders.Count -and ($Remove -or $Restart -or $SmokeOnly)) {
    Write-Host 'provision args fail -AllowedFolders changes the room, so it goes with none of -Remove, -Restart, -SmokeOnly'; exit 1
}
$checkout = Split-Path -Parent $PSScriptRoot
$inCheckout = Test-Path (Join-Path $checkout 'go.mod')
$work = if ($inCheckout) { Join-Path $checkout 'build.claude/provision' } else { Join-Path ([IO.Path]::GetTempPath()) 'atrium-provision' }

# `pwsh -File` hands `-Runners claude,codex` over as one string, so commas split.
function Split-List { param($v) @($v | ForEach-Object { "$_" -split ',' } | ForEach-Object { $_.Trim() } | Where-Object { $_ }) }
$Runners = Split-List $Runners
$Install = Split-List $Install
$AllowedFolders = Split-List $AllowedFolders
. (Join-Path $PSScriptRoot 'room-folders.ps1')
. (Join-Path $PSScriptRoot 'room-spec.ps1')
. (Join-Path $PSScriptRoot 'room-account.ps1')
. (Join-Path $PSScriptRoot 'room-start.ps1')
# A -WorkRoot is judged by the room's own `atrium room setup` (exit 1, with its reason, before anything is changed), so only
# what cannot be written into a spec is refused here.
if ($PSBoundParameters.ContainsKey('WorkRoot') -and (-not $WorkRoot -or $WorkRoot -match '[\x00-\x1f\x7f]')) {
    Write-Host "provision args fail -WorkRoot '$WorkRoot' is empty or holds a control character"; exit 1
}
if (-not $NoAgentPack) {
    $why = Test-PackArg $AgentPackRepo $AgentPackBranch
    if ($why) { Write-Host "provision args fail $why"; exit 1 }
}
$operators = @(Get-OperatorList (Split-List $OperatorAccount))
foreach ($o in $operators) { $why = Test-OperatorArg $o; if ($why) { Write-Host "provision args fail $why"; exit 1 } }
foreach ($f in $AllowedFolders) {
    $why = Test-FolderArg $f
    if ($why) { Write-Host "provision args fail -AllowedFolders '$f' $why"; exit 1 }
}

# ── output ──────────────────────────────────────────────────────────────────

function Step {
    param([string] $step, [string] $status, [string] $detail = '')
    $line = "provision $step $status"
    if ($detail) { $line += " $detail" }
    Write-Host $line
}

# Finish ends the run with the last line and the exit code.
function Finish {
    param([int] $code)
    if ($code -eq 0) { Step 'done' 'ok' } else { Step 'done' 'fail' "$code" }
    exit $code
}

# Fail prints the step's fail line, any output worth seeing indented under it,
# and ends the run.
function Fail {
    param([string] $step, [int] $code, [string] $detail, $output)
    Step $step 'fail' $detail
    if ($output) { $output | ForEach-Object { Write-Host "    $_" } }
    Finish $code
}

if (-not $Target) {
    Write-Host 'usage: provision-room.ps1 <user@host> [-Name room] [-Runners claude,codex] [-Install claude] [-NoAutostart] [-User name] [-Remove]'
    exit 1
}

# ── talking to the remote ───────────────────────────────────────────────────

$sshBase = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=25') + $SshOption

# SCP FROM BESIDE SSH, when it was not named. On Windows the first scp on PATH
# is often Git's, which runs its own /usr/bin/ssh rather than the ssh this
# script was told to use, with a different config and agent.
if (-not $PSBoundParameters.ContainsKey('Scp')) {
    $sshPath = (Get-Command $Ssh -CommandType Application -ErrorAction SilentlyContinue |
        Select-Object -First 1).Source
    if ($sshPath) {
        $sib = Get-ChildItem -LiteralPath (Split-Path -Parent $sshPath) -Filter 'scp*' -ErrorAction SilentlyContinue |
            Where-Object { $_.BaseName -eq 'scp' } | Select-Object -First 1
        if ($sib) { $Scp = $sib.FullName }
    }
}

# Remote results come back as key=value lines, so they are read the same way
# on every OS. A key that repeats keeps its last value.
function ConvertFrom-KeyValue {
    param($lines)
    $h = @{}
    foreach ($l in $lines) {
        $s = "$l"
        $i = $s.IndexOf('=')
        if ($i -gt 0) { $h[$s.Substring(0, $i).Trim()] = $s.Substring($i + 1).TrimEnd() }
    }
    $h
}

# Invoke-Remote runs one script on the remote and returns its output and exit
# code.
#
# WINDOWS GETS -EncodedCommand, because the ssh server's default shell may be
# cmd, Windows PowerShell or pwsh, and each quotes differently. A base64 command
# is one token all three pass through untouched. Progress is silenced inside,
# or Windows PowerShell writes CLIXML progress records to a redirected stderr.
#
# UNIX GETS THE SCRIPT ON STDIN to `sh -s`, so nothing in it passes through the
# login shell's quoting either, and nothing in it is on a command line.
#
# WINDOWS SCRIPTS GET Sch AND Get-AT, because the ScheduledTask cmdlets go
# through CIM and a session that arrived over ssh is denied it ("Cannot connect
# to CIM server. Access denied", seen on sg3). schtasks.exe does not. Get-AT is
# what the atrium logon task runs, or nothing.
$winHelpers = @'
function Sch { $ErrorActionPreference = 'Continue'; & schtasks.exe @args 2>&1 }
function Get-AT { $x = Sch /Query /TN atrium /XML; if ($LASTEXITCODE -eq 0) { $e = ([xml](($x | ForEach-Object { "$_" }) -join "`n")).Task.Actions.Exec; "$($e.Command) $($e.Arguments)".Trim() } }
'@
#
# $StopUrl IS THE ONE ANSWER TO "WHERE DOES atrium stop GO", in every script that stops the room. Get-StopUrl reads
# it from the manifest's `ports` (the first is the room's own board), and falls back to the default 7781 when there
# is no manifest or it records none. It is read afresh on every call, since the manifest is loaded after the first.
function Get-StopUrl {
    $p = if ($script:manifest -and $script:manifest.ports) { @($script:manifest.ports)[0] } else { $null }
    if ("$p" -notmatch '^\d{1,5}$') { $p = 7781 }
    "http://127.0.0.1:$p"
}
function Invoke-Remote {
    param([string] $script)
    if ($script:remoteOS -eq 'windows') {
        $full = "`$ErrorActionPreference='Stop'; `$ProgressPreference='SilentlyContinue'`n$(if ($script -match '\b(Sch|Get-AT)\b') { $winHelpers })`n" +
            "`$A = Join-Path `$HOME '.atrium'; `$Bin = Join-Path `$A 'bin\atrium.exe'; `$StopUrl = '$(Get-StopUrl)'`n" +
            "`$P = Join-Path `$A 'provision'; `$M = Join-Path `$P 'manifest.json'`n" +
            "`$L = Join-Path `$env:LOCALAPPDATA 'atrium'`n" +
            # PATH FROM THE REGISTRY, so a Path entry this run added is seen by the
            # room it starts and the runner check, whatever the ssh server's
            # session inherited.
            "`$env:Path = (@([Environment]::GetEnvironmentVariable('Path', 'Machine'), " +
            "[Environment]::GetEnvironmentVariable('Path', 'User')) | Where-Object { `$_ }) -join ';'`n" + $script
        $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($full))
        if ($enc.Length -gt 7800) {
            # TOO LONG FOR cmd.exe'S LINE, so the script goes deflated and a short
            # loader inflates it and runs it. Base64 of UTF-8 deflate is a fraction
            # of base64 of UTF-16, so the codex installer fits.
            $ms = New-Object IO.MemoryStream
            $ds = New-Object IO.Compression.DeflateStream($ms, [IO.Compression.CompressionMode]::Compress, $true)
            $bytes = [Text.Encoding]::UTF8.GetBytes($full)
            $ds.Write($bytes, 0, $bytes.Length); $ds.Dispose()
            $loader = "`$m = New-Object IO.MemoryStream(,[Convert]::FromBase64String('$([Convert]::ToBase64String($ms.ToArray()))')); " +
                "`$r = New-Object IO.StreamReader((New-Object IO.Compression.DeflateStream(`$m, [IO.Compression.CompressionMode]::Decompress)), [Text.Encoding]::UTF8); " +
                "Invoke-Expression `$r.ReadToEnd()"
            $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($loader))
            if ($enc.Length -gt 7800) { throw "remote script too long for cmd.exe ($($enc.Length))" }
        }
        $out = & $Ssh @sshBase $Target "powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand $enc" 2>&1
    } else {
        $full = "A=`"`$HOME/.atrium`"; Bin=`"`$HOME/.local/bin/atrium`"; StopUrl='$(Get-StopUrl)'`n" +
            "P=`"`$A/provision`"; M=`"`$P/manifest.json`"`n" +
            # Where the room writes its address and lastdb.json. internal/daemon/whereami.go.
            "if [ `"`$(uname -s)`" = Darwin ]; then L=`"`$HOME/Library/Caches/atrium`"; " +
            "else L=`"`${XDG_RUNTIME_DIR:-`${XDG_STATE_HOME:-`$HOME/.local/state}}/atrium`"; fi`n" + $script
        # A COMMENT LAST, because PowerShell ends what it pipes to a native
        # command with CRLF, and `fi` followed by a carriage return is not `fi`.
        $full = ($full -replace "`r", '') + "`n#"
        $out = $full | & $Ssh @sshBase $Target 'sh -s' 2>&1
    }
    [pscustomobject]@{ Out = @($out | ForEach-Object { "$_" }); Code = $LASTEXITCODE }
}

function Copy-ToRemote {
    param([string] $local, [string] $remote)
    $out = & $Scp @sshBase -q $local "${Target}:$remote" 2>&1
    [pscustomobject]@{ Out = @($out | ForEach-Object { "$_" }); Code = $LASTEXITCODE }
}

# Quote-Ps and Quote-Sh put a value inside a remote script as a literal.
function Quote-Ps { param([string] $s) "'" + ($s -replace "['\u2018\u2019\u201A\u201B]", '$0$0') + "'" }
function Quote-Sh { param([string] $s) "'" + ($s -replace "'", "'\''") + "'" }

# ── the hub, which is this machine ──────────────────────────────────────────

# Find-Hub reads the running hub's own command line, so the join string names the
# address rooms really dial and the store the hub really uses. Any of it can be
# overridden by a parameter.
function Find-Hub {
    $lines = @()
    if ($IsWindows -or $env:OS -eq 'Windows_NT') {
        $lines = @(Get-CimInstance Win32_Process -Filter "Name='atrium.exe'" |
            Where-Object { $_.CommandLine -match '\s+run(\s|$)' } |
            ForEach-Object { [pscustomobject]@{ Cmd = $_.CommandLine; Exe = $_.ExecutablePath } })
    } else {
        $lines = @(& ps -eo args 2>$null | Where-Object { $_ -match '(^|/)atrium\s+run(\s|$)' } |
            ForEach-Object { [pscustomobject]@{ Cmd = $_; Exe = ($_ -split '\s+')[0] } })
    }
    $flag = {
        param($cmd, $n)
        # A quoted value may hold spaces. An unquoted one ends at the first.
        if ($cmd -and $cmd -match "--$n[ =](?:`"([^`"]*)`"|(\S+))") {
            if ($Matches[1]) { $Matches[1] } else { $Matches[2] }
        } else { $null }
    }
    $hubs = @($lines | ForEach-Object {
        [pscustomobject]@{
            Exe       = $_.Exe
            Dir       = & $flag $_.Cmd 'atrium-dir'
            Link      = & $flag $_.Cmd 'link'
            Advertise = & $flag $_.Cmd 'link-advertise'
            Addr      = & $flag $_.Cmd 'addr'
            Transport = & $flag $_.Cmd 'transport'
            Service   = & $flag $_.Cmd 'atrium-service'
        }
    })
    if ($HubAddr) { $hubs = @($hubs | Where-Object { $_.Addr -eq $HubAddr }) }
    $hubs | Select-Object -First 1
}

function Invoke-Hub {
    param([string[]] $hubArgs)
    $all = $hubArgs
    if ($HubDir) { $all += @('--atrium-dir', $HubDir) }
    $out = & $HubExe @all 2>&1
    [pscustomobject]@{ Out = @($out | ForEach-Object { "$_" }); Code = $LASTEXITCODE }
}

# Get-HubRoom is this hub's line for one room, or $null.
function Get-HubRoom {
    param([string] $room)
    $r = Invoke-Hub @('rooms', 'ls')
    $r.Out | Where-Object { $_ -match "^$([regex]::Escape($room))\s" } | Select-Object -First 1
}

# Get-HubLog names where the hub's log is, for an attach that did not happen. The hub's stderr goes wherever whoever
# started it sent it, so this is the live layout's `hub.err` beside the hub dir when there is one.
function Get-HubLog {
    if ($HubDir) {
        $f = Join-Path (Split-Path -Parent $HubDir) 'hub.err'
        if (Test-Path $f) { return "the hub's log is $f" }
    }
    "the hub's log is wherever its stderr goes"
}

# ── 1. reach the target and learn what it is ────────────────────────────────

# THE PROBE HAS TO WORK BEFORE WE KNOW THE SHELL. `uname` answers on Linux and
# macOS, and on Windows only when Git for Windows put one on PATH, which says
# MINGW or MSYS. Anything else is tried as Windows.
$script:remoteOS = $null
$probe = & $Ssh @sshBase $Target 'uname -sm' 2>&1
$probeCode = $LASTEXITCODE
if ($probeCode -eq 255) {
    Fail 'ssh' 2 "cannot reach $Target over ssh" $probe
}
Step 'ssh' 'ok' $Target

$arch = $null
$os = $null
$probeText = ($probe | ForEach-Object { "$_" }) -join ' '
if ($probeCode -eq 0 -and $probeText -match '^(Linux|Darwin)\s+(\S+)') {
    $os = if ($Matches[1] -eq 'Linux') { 'linux' } else { 'darwin' }
    $arch = $Matches[2]
    $script:remoteOS = $os
    $hn = Invoke-Remote 'uname -n'
    $remoteHost = ($hn.Out | Select-Object -First 1)
} else {
    $script:remoteOS = 'windows'
    $os = 'windows'
    $r = Invoke-Remote @'
"arch=$(if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE })"
"host=$env:COMPUTERNAME"
'@
    if ($r.Code -ne 0) { Fail 'os' 2 'not Linux, macOS or Windows PowerShell' ($probe + $r.Out) }
    $kv = ConvertFrom-KeyValue $r.Out
    $arch = $kv.arch
    $remoteHost = $kv.host
}
$goarch = switch -Regex ($arch) {
    '^(x86_64|amd64|AMD64)$' { 'amd64' }
    '^(aarch64|arm64|ARM64)$' { 'arm64' }
    default { $null }
}
if (-not $goarch) { Fail 'os' 2 "$os on $arch, which atrium is not built for" }
Step 'os' 'ok' "$os $goarch $remoteHost"

# ── the account, when one was asked for ─────────────────────────────────────

# NEVER CREATES AN ACCOUNT. Making one needs admin, which provisioning runs without on purpose. -User names the
# account the room must run as. Asked over the login this run already has, and when the account is not there it
# prints the one command the operator runs and stops with 11. Provisioning runs AS the ssh login, so an account that
# exists but is not that login is a mistake in the target, not something this can fix.
function Get-CreateAccountCommand {
    param([string] $os, [string] $user)
    switch ($os) {
        'windows' { "net user $user /add" }
        'darwin'  { "sysadminctl -addUser $user" }
        default   { "sudo useradd -m $user" }
    }
}
function Test-Account {
    param([string] $User, [bool] $byDefault)
    $q = switch ($os) {
        'windows' { "net user $(Quote-Ps $User) 2>&1 | Out-Null; if (`$LASTEXITCODE -eq 0) { 'exists=True' } else { 'exists=False' }`n'login=' + `$env:USERNAME" }
        'darwin'  { "if dscl . -read /Users/$(Quote-Sh $User) >/dev/null 2>&1; then echo exists=True; else echo exists=False; fi; echo login=`$(id -un)" }
        default   { "if id $(Quote-Sh $User) >/dev/null 2>&1; then echo exists=True; else echo exists=False; fi; echo login=`$(id -un)" }
    }
    $acct = ConvertFrom-KeyValue (Invoke-Remote $q).Out
    if ($acct.exists -ne 'True') {
        Step 'account' 'fail' "$User does not exist on $remoteHost. this never creates an account. an administrator runs: $(Get-CreateAccountCommand $os $User)"
        Finish 11
    }
    if ($acct.login -and $acct.login -ne $User) {
        $or = if ($byDefault) { ", or pass -KeepAccount to run the room as $($acct.login)" } else { '' }
        Fail 'account' 1 "$User exists, and $Target logs in as $($acct.login). target $User@<host> so the room runs as $User$or"
    }
    Step 'account' 'ok' "$User, the account this room runs as"
}
if ($User) { Test-Account $User $false }

# THE LOCALAI ACCOUNT, for a machine added from now on (backlog 75). Once the state is read, a NEW machine, one with no
# manifest and no room.json, must be provisioned as localai: the same check as -User, so a missing account prints the
# command and stops with 11. A machine that already is a room is left alone, whatever account it runs under (sg3 on
# claude), and -KeepAccount is the explicit opt-out for a new machine that is meant to run under its own login.
# Returns $true for a new machine that is to get the shared folder.
function Invoke-LocalAiCheck {
    if ($User) { return -not ($manifest -or $state.joinedroom) }
    if ($KeepAccount) { Step 'account' 'skip' '-KeepAccount, so the room runs as the ssh login and localai is not checked'; return $false }
    if ($manifest -or $state.joinedroom) {
        $who = (ConvertFrom-KeyValue (Invoke-Remote $(if ($os -eq 'windows') { "'login=' + `$env:USERNAME" } else { 'echo login=$(id -un)' })).Out).login
        Step 'account' 'skip' "already provisioned$(if ($who) { " under $who" }), left alone. localai applies to new machines only"
        return $false
    }
    Test-Account 'localai' $true
    $true
}

# The rights of the account the room runs as, see "the account's rights". Read only.
if (-not ($Remove -or $Restart -or $SmokeOnly)) {
    $acctKind = switch ($os) { 'windows' { 'windows' } 'darwin' { 'mac' } default { 'linux' } }
    $ar = Invoke-AccountProbe $os $Ssh $sshBase $Target
    $av = Get-AccountResult $ar.Out $ar.Code $acctKind $remoteHost (Get-LocalIdentity) $operators $false ([bool]$IAcceptRunningAsMe) ([bool]$RequireDedicatedAccount)
    Step 'account-rights' $av.Status $av.Detail
    if ($av.Refuse) { Finish 6 }
}

# ── 2. the hub ──────────────────────────────────────────────────────────────

$hub = Find-Hub
if (-not $hub) { $hub = [pscustomobject]@{} }
if (-not $HubExe) { $HubExe = if ($hub.Exe) { $hub.Exe } else { (Get-Command atrium -ErrorAction SilentlyContinue).Source } }
if (-not $HubDir) { $HubDir = $hub.Dir }
if (-not $HubAddr) { $HubAddr = if ($hub.Addr) { $hub.Addr } else { '127.0.0.1:7778' } }
if (-not $HubExe) { Fail 'hub' 1 'no running hub and no atrium on PATH. pass -HubExe' }
$transport = if ($hub.Transport) { $hub.Transport } else { 'direct' }
$zitiService = if ($hub.Service) { $hub.Service } else { 'atrium-hub' }
if (-not $Link) { $Link = if ($hub.Link) { $hub.Link } else { '127.0.0.1:7779' } }
if (-not $LinkAdvertise) { $LinkAdvertise = $hub.Advertise }

switch ($transport) {
    'direct' {
        if (-not $LinkAdvertise) {
            if ($Link -match '^(127\.|localhost|\[::1\])') {
                Fail 'hub' 1 "the hub's link is on loopback ($Link), which no other machine can dial. start it with --link and --link-advertise"
            }
            $LinkAdvertise = $Link
        }
        $hubId = "direct:$LinkAdvertise"
        $says = "direct, rooms dial $LinkAdvertise"
    }
    'ziti' { $hubId = "ziti:$zitiService"; $says = "ziti, rooms dial the service $zitiService" }
    'zrok' { $hubId = 'zrok'; $says = 'zrok, rooms dial its private share' }
    default { Fail 'hub' 1 "the hub links over $transport, which this script does not know" }
}
# zrok takes most of a minute to let a new access dial a share, measured
# 2026-09-28, so its default wait is longer.
if ($transport -eq 'zrok' -and -not $PSBoundParameters.ContainsKey('AttachTimeout')) { $AttachTimeout = 180 }
try {
    $h = Invoke-RestMethod -Uri "http://$HubAddr/_hub/health" -TimeoutSec 5
    Step 'hub' 'ok' "$says, $($h.rooms) attached now"
} catch {
    Step 'hub' 'warn' "$says, but http://$HubAddr/_hub/health did not answer"
}

# ── 3. what the remote already has ──────────────────────────────────────────

$stateScript = if ($os -eq 'windows') {
@'
if ($A) { 'preamble=ok' }
"atriumdir=$(Test-Path $A)"
"bindir=$(Test-Path (Split-Path -Parent $Bin))"
"locdir=$(Test-Path $L)"
"bin=$(Test-Path $Bin)"
if (Test-Path $Bin) { "binsha=$((Get-FileHash $Bin -Algorithm SHA256).Hash.ToLower())" }
"db=$(Test-Path (Join-Path $A 'atrium.db'))"
"roomdir=$(Test-Path (Join-Path $A 'room'))"
$rj = Join-Path $A 'room\room.json'
if (Test-Path $rj) {
    $j = Get-Content $rj -Raw | ConvertFrom-Json
    "joinedroom=$($j.room)"; "joinedhub=$($j.hub)"; "joinedtransport=$($j.transport)"; "joinedservice=$($j.service)"
}
$svc = Get-AT
if ($svc) { "service=$svc" }
if (Test-Path $M) { "manifest=$((Get-Content $M -Raw) -replace '\r?\n', ' ')" }
"zrokenv=$(Test-Path (Join-Path $HOME '.zrok2\environment.json'))"
foreach ($port in 7781, 7778) {
    try { $null = Invoke-RestMethod "http://127.0.0.1:$port/v1/health" -TimeoutSec 3; "up$port=True" } catch { "up$port=False" }
}
'@
} else {
@'
tf() { if [ -e "$1" ]; then echo True; else echo False; fi; }
if [ -n "$A" ]; then echo "preamble=ok"; fi
echo "atriumdir=$(tf "$A")"
echo "bindir=$(tf "$(dirname "$Bin")")"
echo "locdir=$(tf "$L")"
echo "bin=$(tf "$Bin")"
if [ -f "$Bin" ]; then
  if command -v sha256sum >/dev/null 2>&1; then s=$(sha256sum "$Bin"); else s=$(shasum -a 256 "$Bin"); fi
  echo "binsha=${s%% *}"
fi
echo "db=$(tf "$A/atrium.db")"
echo "roomdir=$(tf "$A/room")"
rj="$A/room/room.json"
if [ -f "$rj" ]; then
  for k in room hub transport service; do
    v=$(sed -n 's/.*"'"$k"'": *"\([^"]*\)".*/\1/p' "$rj")
    echo "joined$k=$v"
  done
fi
u="$HOME/.config/systemd/user/atrium.service"
p="$HOME/Library/LaunchAgents/io.github.dovholuknf.atrium.plist"
if [ -f "$u" ]; then echo "service=$(grep '^ExecStart=' "$u")"; fi
if [ -f "$p" ]; then echo "service=$(grep 'exec ' "$p" | head -1)"; fi
if [ -f "$M" ]; then echo "manifest=$(tr '\n' ' ' < "$M")"; fi
echo "zrokenv=$(tf "$HOME/.zrok2/environment.json")"
for port in 7781 7778; do
  if command -v curl >/dev/null 2>&1 && curl -fsS -m 3 "http://127.0.0.1:$port/v1/health" >/dev/null 2>&1; then
    echo "up$port=True"; else echo "up$port=False"; fi
done
'@
}
$st = Invoke-Remote $stateScript
if ($st.Code -ne 0) { Fail 'state' 3 'could not read what the remote has' $st.Out }
$state = ConvertFrom-KeyValue $st.Out
# THE PREAMBLE HAS TO HAVE RUN, or nothing below can be trusted. With a BOM in front of the piped sh script its first
# line (`A=...`) failed, the manifest was never found, and a room this script made read as one it did not put there.
if ($state.preamble -ne 'ok') {
    Fail 'state' 3 'the remote script lost its first lines, so what it read cannot be trusted. a BOM or a remote profile in the way? try pwsh -NoProfile' $st.Out
}
$manifest = if ($state.manifest) { $state.manifest | ConvertFrom-Json } else { $null }
# What the manifest records as already there before the first run.
$preKeys = @('atriumdir', 'bindir', 'bin', 'db', 'roomdir', 'locdir', 'service')

# What the remote's room.json says it joined, in the same form as $hubId.
$joinedId = $null
if ($state.joinedroom) {
    $jt = if ($state.joinedtransport) { $state.joinedtransport } else { 'direct' }
    $joinedId = switch ($jt) { 'direct' { "direct:$($state.joinedhub)" } 'ziti' { "ziti:$($state.joinedservice)" } default { $jt } }
}

# Save-Manifest writes the manifest as it now stands, and makes the folders
# every later step writes into.
function Save-Manifest {
    $json = $script:manifest | ConvertTo-Json -Compress -Depth 5
    $s = if ($os -eq 'windows') {
        "New-Item -ItemType Directory -Force -Path (Split-Path -Parent `$Bin), `$P | Out-Null`n" +
        "[IO.File]::WriteAllText(`$M, $(Quote-Ps $json))"
    } else {
        "mkdir -p `"`$(dirname `"`$Bin`")`" `"`$P`"`ncat > `"`$M`" <<'EOF'`n$json`nEOF"
    }
    $r = Invoke-Remote $s
    if ($r.Code -ne 0) { Fail 'state' 3 'could not write the manifest' $r.Out }
}

# WHERE THE ROOM KEEPS ITS STATE, which is where `room join` wrote room.json:
# $WORKTREE_ROOT/hub when WORKTREE_ROOT is set, else ~/.atrium (StateDir in
# internal/daemon/daemon.go). A start whose environment differs from the join's
# finds no room.json and does not come back as that room, so a restart has to
# say which one to use. Get-StateDirs asks the remote which candidates hold a
# room.json, and what WORKTREE_ROOT a login shell there would give.
function Get-StateDirs {
    $ps = if ($os -eq 'windows') {
@'
$w = $env:WORKTREE_ROOT
if (-not $w) { $w = [Environment]::GetEnvironmentVariable('WORKTREE_ROOT', 'User') }
if (-not $w) { $w = [Environment]::GetEnvironmentVariable('WORKTREE_ROOT', 'Machine') }
$c = @{ a = $A }; if ($w) { $c.w = Join-Path $w 'hub' }
"wtr=$w"
foreach ($k in $c.Keys) {
    $j = Join-Path $c[$k] 'room\room.json'
    "dir_$k=$($c[$k])"; "has_$k=$(Test-Path $j)"
    if (Test-Path $j) { "room_$k=$((Get-Content $j -Raw | ConvertFrom-Json).room)" }
}
'@
    } else {
@'
w=$("${SHELL:-/bin/sh}" -lc 'printf %s "$WORKTREE_ROOT"' </dev/null 2>/dev/null)
echo "wtr=$w"
probe() {
  echo "dir_$1=$2"
  if [ -f "$2/room/room.json" ]; then
    echo "has_$1=True"; echo "room_$1=$(sed -n 's/.*"room": *"\([^"]*\)".*/\1/p' "$2/room/room.json" | head -1)"
  else echo "has_$1=False"; fi
}
probe a "$A"
if [ -n "$w" ]; then probe w "$w/hub"; fi
'@
    }
    ConvertFrom-KeyValue (Invoke-Remote $ps).Out
}

# Resolve-StateDir picks the one to pin. The manifest's own record wins when it
# still holds room.json. Otherwise the one directory that does. Two, or none,
# is not guessed at.
function Resolve-StateDir {
    param($sd)
    $found = @('a', 'w' | Where-Object { $sd["has_$_"] -eq 'True' } | ForEach-Object { $sd["dir_$_"] })
    $rec = $script:manifest.statedir
    if ($rec -and ($found | Where-Object { $_ -eq $rec })) { return [pscustomobject]@{ Dir = $rec; Why = $null } }
    if ($found.Count -eq 1) { return [pscustomobject]@{ Dir = $found[0]; Why = $null } }
    if ($found.Count -eq 0) { return [pscustomobject]@{ Dir = $null; Why = 'no room.json under ~/.atrium or under WORKTREE_ROOT/hub' } }
    [pscustomobject]@{ Dir = $null; Why = "room.json is under both $($found -join ' and '), so which one the room uses is not known" }
}

# Set-ManifestStateDir records the resolved state directory at join time, and
# backfills a manifest written before it existed. Never a failure.
function Set-ManifestStateDir {
    if ($script:manifest.statedir) { return }
    $r = Resolve-StateDir (Get-StateDirs)
    if ($r.Dir) {
        $script:manifest | Add-Member -NotePropertyName statedir -NotePropertyValue $r.Dir -Force
        Save-Manifest
    }
}

# THE SHARED FOLDER, for a new machine (backlog 75): a local path that holds the repository clones, readable by the
# operator's account and by localai. C:\Users\Public\atrium, /Users/Shared/atrium, /srv/atrium. Made when the ssh login
# may, and when it may not (usually /srv on Linux) the step fails with the one command an administrator runs, exit 12,
# having changed nothing. -Check only reports. Returns the path, or $null when there is to be none.
function Invoke-SharedFolder {
    param([bool] $newMachine, [string] $account)
    if (-not $newMachine -or $NoSharedFolder) {
        Step 'shared-folder' 'skip' $(if ($NoSharedFolder) { '-NoSharedFolder, so clones stay under ~/git' } else { 'already provisioned or -KeepAccount, so its clones stay where they are' })
        return $null
    }
    $dir = switch ($os) { 'windows' { 'C:\Users\Public\atrium' } 'darwin' { '/Users/Shared/atrium' } default { '/srv/atrium' } }
    $make = -not $Check
    $q = if ($os -eq 'windows') {
        "`$d = $(Quote-Ps $dir); `$mk = `$$make`n" + @'
if (-not (Test-Path -LiteralPath $d)) {
    'exists=False'
    if ($mk) { try { New-Item -ItemType Directory -Force -Path $d | Out-Null; 'made=True' } catch { 'made=False' } }
}
if (Test-Path -LiteralPath $d) {
    $t = Join-Path $d ".atrium-probe-$PID"
    try { [IO.File]::WriteAllText($t, 'x'); Remove-Item -LiteralPath $t -Force; 'writable=True' } catch { 'writable=False' }
}
'@
    } else {
        "d=$(Quote-Sh $dir); mk=$(if ($make) { 1 } else { 0 })`n" + @'
if [ ! -d "$d" ]; then
  echo exists=False
  if [ "$mk" = 1 ]; then if mkdir -p "$d" 2>/dev/null; then chmod 2775 "$d" 2>/dev/null; echo made=True; else echo made=False; fi; fi
fi
if [ -d "$d" ]; then if [ -w "$d" ]; then echo writable=True; else echo writable=False; fi; fi
'@
    }
    $k = ConvertFrom-KeyValue (Invoke-Remote $q).Out
    $fix = switch ($os) {
        'windows' { "mkdir $dir" }
        'darwin'  { "sudo mkdir -p $dir && sudo chown ${account}:staff $dir && sudo chmod 2775 $dir" }
        default   { "sudo install -d -o $account -g $account -m 2775 $dir" }
    }
    if ($k.exists -eq 'False' -and $Check) {
        Step 'shared-folder' 'warn' "$dir is missing on $remoteHost. a run without -Check makes it, and prints this when it may not: $fix"
        return $dir
    }
    if ($k.made -eq 'False') {
        Step 'shared-folder' 'fail' "$dir is missing and $Target may not make it. this never needs admin itself. an administrator runs: $fix"
        Finish 12
    }
    if ($k.writable -ne 'True') {
        Step 'shared-folder' 'fail' "$dir exists and $Target cannot write to it. an administrator runs: $fix"
        Finish 12
    }
    if ($k.made -eq 'True') { Step 'shared-folder' 'done' "$dir made, the folder the room's clones go under" }
    else { Step 'shared-folder' 'ok' "$dir, the folder the room's clones go under" }
    $dir
}

# THE ROOM'S OWN SETUP (`atrium room setup`, internal/roomspec): the work root, the tool caches, the room's folder settings and
# the agent pack are made and judged by the room's atrium from a room.yaml built here (scripts/room-spec.ps1). Get-SetupYaml is
# that spec, Invoke-RoomSetup runs it on the remote with --plan (reads, cannot write) or --apply, and Show-SetupRows prints what
# it said as step lines, `todo` as a warn and `human` as a fail, followed by the lines an administrator runs.
function Get-SetupYaml {
    $n = if ($Name) { $Name } elseif ($manifest) { "$($manifest.name)" } else { ($remoteHost.ToLower() -replace '[^a-z0-9._-]', '-') -replace '^[^a-z0-9]+', '' }
    if (-not $n) { $n = 'room' }
    # every runner the room has a pack for: what this run found in the repository, else what the manifest recorded, else claude
    $runners = if ($script:packRunners) { @($script:packRunners) } elseif ($manifest -and $manifest.agentpack -and $manifest.agentpack.runners) { @($manifest.agentpack.runners) } else { @('claude') }
    try { New-RoomSpecYaml -Name $n -Os $os -WorkRoot $workRoot -PackRepo $(if ($NoAgentPack) { '' } else { $AgentPackRepo }) -PackBranch $AgentPackBranch -PackRunners $runners }
    catch { Fail 'work-root' 1 "$_" }
}
function Invoke-RoomSetup {
    param([string] $Mode, [switch] $UsePackDir)
    $r = Invoke-Remote (Get-RoomSetupScript -Os $os -Yaml (Get-SetupYaml) -Mode $Mode -UsePackDir:$UsePackDir)
    Limit-PackFailures (ConvertFrom-RoomSetup $r.Out $r.Code)
}
function Show-SetupRows {
    param($res)
    $rows = @($res.Rows)
    if ($Check -and -not $NoAgentPack) { $rows = @(Set-PackRowLatest $rows (Get-HubMirrorCommit $HubAddr $AgentPackRepo $AgentPackBranch)) }
    foreach ($r in $rows) { Step $r.Step (ConvertTo-StepStatus $r.Status) $r.Detail }
    foreach ($l in $res.AdminLines) { Write-Host "    $l" }
}

# Right after the state is read, on a provision run. A refusal here has changed nothing.
$sharedDir = $null
if (-not ($Remove -or $Restart -or $SmokeOnly)) {
    $newMachine = Invoke-LocalAiCheck
    $acctName = if ($User) { $User } else { 'localai' }
    # THE WORK ROOT, from -WorkRoot, else the one a first run recorded. With one, it takes the place of the shared folder: the
    # clones, reviews, hand-offs and caches all go under it, so there is no shared folder to make.
    $workRoot = if ($WorkRoot) { $WorkRoot } elseif ($manifest -and $manifest.workroot) { "$($manifest.workroot)" } else { $null }
    if ($workRoot) {
        # THIS machine's atrium judges the root for the room's operating system first, with no ssh and no disk, so a root that
        # is relative, a drive root, a network path, the wrong kind for the OS or another user's home is refused here, before
        # anything on the room is changed. The room's own atrium judges what only its disk can tell (links, short names).
        $vAcct = if ($User) { $User } elseif ($Target -match '^([^@]+)@') { $Matches[1] } else { '' }
        # a Windows profile of a local account carries the machine's name (al.SG3), so the login is given with it
        if ($os -eq 'windows' -and $vAcct -and $vAcct -notmatch '\\') { $vAcct = "$remoteHost\$vAcct" }
        $why = Test-WorkRootLocal $HubExe $os $workRoot $vAcct
        if ($why) { Fail 'work-root' 1 "$workRoot is refused: $why" }
        Step 'shared-folder' 'skip' "the work root $workRoot holds the clones, so there is no shared folder"
        # THE ROOM'S OWN atrium judges the work root, so a machine that has one is asked first, with --plan, which cannot write:
        # a parent folder the account cannot examine, or a root it cannot make, ends the run here with the lines an
        # administrator runs and exit 13, before anything is changed. A machine with no atrium yet (or an older one with no
        # `room setup`) is checked by the first run, which installs it and then asks.
        $pl = if ($state.binsha) { Invoke-RoomSetup 'plan' } else { $null }
        if ($pl -and $pl.Kind -eq 'ok') {
            if ($Check -or $pl.Code -ne 0) { Show-SetupRows $pl }
            if ($pl.Code -ne 0) { Finish $pl.Code }
        } elseif ($pl -and $pl.Kind -eq 'error') {
            Step 'work-root' 'fail' "atrium room setup refused: $(($pl.Other | Select-Object -First 3) -join ' | ')"
            Finish $(if ($pl.Code -eq 1) { 1 } else { 3 })
        } else {
            Step 'work-root' 'warn' "$remoteHost has no atrium that runs 'room setup' yet, so $workRoot and the agent pack are checked by the first run, which installs one and then asks"
        }
        $sharedDir = (Format-WorkPath $workRoot) + '/git'
        if ($Check) { Step 'git-root' 'ok' "a run without -Check sets git_root and scm_root to $sharedDir, reviews_root and context_handoff_dir under $workRoot (atrium room setup)" }
    } else {
        # no work root: the agent pack is still installed and read, by a spec of the pack alone
        if (-not $NoAgentPack -and $Check) {
            $pl = if ($state.binsha) { Invoke-RoomSetup 'plan' } else { $null }
            if ($pl -and $pl.Kind -eq 'ok') { Show-SetupRows $pl }
            else { Step 'agent-pack' 'warn' "$remoteHost has no atrium that runs 'room setup' yet, so the agent pack is installed by the first run" }
        }
        $sharedDir = Invoke-SharedFolder $newMachine $acctName
        # THE ROOM'S git_root SETTING is written after the join, by `room set git_root` on the remote, while no room runs.
        if ($sharedDir -and $Check) { Step 'git-root' 'ok' "a run without -Check sets the room's git_root to $sharedDir (atrium room set git_root)" }
        if ($os -eq 'windows') {
            $hd = Get-WorkHintDetail (Invoke-Remote (Get-WorkHintScript)).Out
            if ($hd) { Step 'work-root' 'warn' $hd }
        }
    }
    if ($Check) { Step 'check' 'ok' 'read only: nothing was changed'; Finish 0 }
}

# ── -Remove ─────────────────────────────────────────────────────────────────

if ($Remove) {
    if (-not $manifest) {
        Step 'state' 'skip' 'this script never provisioned this machine. nothing to undo'
        Finish 0
    }
    $room = if ($Name) { $Name } else { $manifest.name }
    # THE HUB IT JOINED, NOT JUST THE FIRST ONE FOUND, checked before anything
    # is removed. With two hubs running the wrong one could have a room of the
    # same name, and the right one would keep a row nobody can clear.
    if ($manifest.hub -and $manifest.hub -ne $hubId) {
        Fail 'hub' 1 "$room is a room of $($manifest.hub), and the hub found is $hubId. rerun with -HubAddr for that hub"
    }
    Step 'state' 'ok' "provisioned as $room"

    # WHAT WAS THERE BEFORE IS KEPT. A flag the manifest does not have, from an
    # older run, reads as "was there", so the doubt falls on keeping.
    $pre = $manifest.pre
    $was = @{}
    foreach ($k in $preKeys) { $was[$k] = if ($null -eq $pre.$k) { $true } else { [bool] $pre.$k } }
    # A manifest from before autostart was optional had it on.
    $hadAutostart = if ($null -eq $manifest.autostart) { $true } else { [bool] $manifest.autostart }
    $installed = @($manifest.installed | Where-Object { $_ })

    $rmScript = if ($os -eq 'windows') {
        (($preKeys | ForEach-Object { "`$pre_$_ = `$$($was[$_])" }) -join "`n") + "`n" +
        "`$auto = `$$hadAutostart`n" +
        "`$installed = @($((@($installed | ForEach-Object { Quote-Ps $_ })) -join ', '))`n" +
        "`$pathadded = $(Quote-Ps "$($manifest.pathadded)")`n" + @'
if ($pathadded -eq 'registry') {
    $d = Join-Path $HOME '.local\bin'
    $cur = [Environment]::GetEnvironmentVariable('Path', 'User')
    [Environment]::SetEnvironmentVariable('Path', ((@($cur -split ';') | Where-Object { $_ -and $_ -ne $d }) -join ';'), 'User')
    "path=removed ~\.local\bin from the user's Path"
}
if (Test-Path $Bin) { try { & $Bin stop --url $StopUrl 2>&1 | Out-Null } catch {} }
$deadline = (Get-Date).AddSeconds(20)
while ((Get-Process atrium -ErrorAction SilentlyContinue | Where-Object Path -eq $Bin) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 500 }
Get-Process atrium -ErrorAction SilentlyContinue | Where-Object Path -eq $Bin | Stop-Process -Force
Start-Sleep -Milliseconds 500
foreach ($i in $installed) {
    if ($i.StartsWith($HOME) -and (Test-Path -LiteralPath $i)) { Remove-Item -LiteralPath $i -Recurse -Force; "runner=removed $i" }
}
if (-not $pre_locdir) { Remove-Item -LiteralPath $L -Recurse -Force -ErrorAction SilentlyContinue }
if (-not $pre_atriumdir) { Remove-Item -LiteralPath $A -Recurse -Force; "files=removed $A" }
else {
    if (-not $pre_bin) { Remove-Item -LiteralPath $Bin -Force -ErrorAction SilentlyContinue }
    if (-not $pre_bindir) { Remove-Item -LiteralPath (Split-Path -Parent $Bin) -Recurse -Force -ErrorAction SilentlyContinue }
    # The ledger is a projection of the database, written beside it.
    if (-not $pre_db) { Get-ChildItem $A -Filter 'atrium.db*' | Remove-Item -Force; Remove-Item (Join-Path $A 'work-ledger.md') -Force -ErrorAction SilentlyContinue }
    if (-not $pre_roomdir) { Remove-Item -LiteralPath (Join-Path $A 'room') -Recurse -Force -ErrorAction SilentlyContinue }
    Remove-Item -LiteralPath $P -Recurse -Force -ErrorAction SilentlyContinue
    "files=removed what this script added under $A"
}
'@
    } else {
        (($preKeys | ForEach-Object { "pre_$_=$($was[$_])" }) -join "`n") + "`n" +
        "auto=$hadAutostart`n" +
        "installed=$(Quote-Sh (($installed) -join "`n"))`n" +
        "pathadded=$(Quote-Sh "$($manifest.pathadded)")`n" + @'
case "$pathadded" in "$HOME"/*)
  if [ -f "$pathadded" ]; then
    grep -v '# added by atrium provision-room$' "$pathadded" > "$pathadded.atrium-tmp" || true
    cat "$pathadded.atrium-tmp" > "$pathadded"; rm -f "$pathadded.atrium-tmp"
    # A profile holding nothing else was made by the line this added.
    if [ -z "$(tr -d ' \t\n' < "$pathadded")" ]; then rm -f "$pathadded"; fi
    echo "path=removed the line this added to $pathadded"
  fi;;
esac
if [ -x "$Bin" ]; then "$Bin" stop --url $StopUrl >/dev/null 2>&1 || true; fi
if [ "$auto" = True ] && [ "$pre_service" = False ] && [ -f "$P/scripts/atrium-service.sh" ]; then
  ATRIUM_EXE="$Bin" ATRIUM_SERVICE_VERB=room bash "$P/scripts/atrium-service.sh" uninstall >/dev/null 2>&1 || true
  echo "service=removed"
fi
n=0
while pgrep -f "$Bin room" >/dev/null 2>&1 && [ $n -lt 40 ]; do sleep 0.5; n=$((n+1)); done
pkill -9 -f "$Bin room" 2>/dev/null || true
printf '%s\n' "$installed" | while IFS= read -r i; do
  case "$i" in "$HOME"/*) if [ -e "$i" ] || [ -L "$i" ]; then rm -rf "$i"; echo "runner=removed $i"; fi;; esac
done
if [ "$pre_locdir" = False ]; then rm -rf "$L"; fi
if [ "$pre_atriumdir" = False ]; then rm -rf "$A"; echo "files=removed $A"
else
  if [ "$pre_db" = False ]; then rm -f "$A"/atrium.db* "$A/work-ledger.md"; fi
  if [ "$pre_roomdir" = False ]; then rm -rf "$A/room"; fi
  rm -rf "$P"
  echo "files=removed what this script added under $A"
fi
if [ "$pre_bin" = False ]; then rm -f "$Bin"; fi
if [ "$pre_bindir" = False ]; then rmdir "$(dirname "$Bin")" 2>/dev/null || true; fi
'@
    }
    # THE CONTROL SERVER THIS SCRIPT REGISTERED, first, while claude and the
    # binary are both still there. Its own call: the clean-up script is near
    # the length a Windows command line can carry.
    if ($manifest.mcpadded) {
        $mr = if ($os -eq 'windows') {
            "`$ErrorActionPreference = 'Continue'`nif (Get-Command claude -ErrorAction SilentlyContinue) { & claude mcp remove --scope user atrium-control 2>&1 | Out-Null; 'mcp=removed' }"
        } else {
            "lp=`$(`"`${SHELL:-/bin/sh}`" -lc 'printf %s `"`$PATH`"' 2>/dev/null); [ -n `"`$lp`" ] && PATH=`"`$lp:`$PATH`"`n" +
            "if command -v claude >/dev/null 2>&1; then claude mcp remove --scope user atrium-control >/dev/null 2>&1; echo mcp=removed; fi"
        }
        $mk = ConvertFrom-KeyValue (Invoke-Remote $mr).Out
        if ($mk.mcp) { Step 'mcp' 'done' 'removed atrium-control from claude' }
        else { Step 'mcp' 'skip' 'claude is not there to remove it from' }
    }
    # THE MCP CONFIG FILE this script wrote, when there was none before it.
    if ($manifest.mcpfile) {
        $mf = if ($os -eq 'windows') { "Remove-Item -Force (Join-Path `$A 'mcp.json') -ErrorAction SilentlyContinue`n'mcp=removed'" }
              else { "rm -f `"`$A/mcp.json`"`necho mcp=removed" }
        $mk = ConvertFrom-KeyValue (Invoke-Remote $mf).Out
        if ($mk.mcp) { Step 'mcp' 'done' 'removed the mcp.json this script wrote' }
    }
    # THE LOGON TASK, its own call, because the clean-up script below is near the
    # length a Windows command line can carry.
    $svcGone = $false
    if ($os -eq 'windows' -and $hadAutostart -and -not $was['service']) {
        $sr = Invoke-Remote "if (Get-AT) { `$null = Sch /End /TN atrium; `$null = Sch /Delete /TN atrium /F; 'service=removed' }"
        $svcGone = [bool] (ConvertFrom-KeyValue $sr.Out).service
    }
    $rm = Invoke-Remote $rmScript
    if ($rm.Code -ne 0) { Fail 'remove' 3 'the remote clean-up failed' $rm.Out }
    if ($svcGone) { $rm.Out += 'service=removed' }
    $kv = ConvertFrom-KeyValue $rm.Out
    if ($kv.service) { Step 'autostart' 'done' 'removed' }
    elseif ($hadAutostart) { Step 'autostart' 'skip' 'it was there before this script' }
    else { Step 'autostart' 'skip' 'none was installed' }
    $gone = @($rm.Out | Where-Object { $_ -like 'runner=removed *' } | ForEach-Object { $_.Substring(15) })
    if ($gone) { Step 'runners' 'done' "removed $($gone -join ', ')" }
    if ($kv.path) { Step 'path' 'done' ($kv.path -replace '^removed', 'removed') }
    Step 'files' 'done' $kv.files
    # THE FOLDER LIST IS NOT UNDONE. It is a setting in the room's database, which goes with the database when this script
    # made it and is the operator's own when it did not, and the trust is claude's, in the account's ~/.claude.json.
    # `atrium room folders` has no verb to take a folder out, and removing a folder's trust could hide a dialog the
    # operator had answered themselves.
    if (@($manifest.allowed_folders | Where-Object { $_ }).Count) {
        Step 'folders' 'skip' "left alone: $(@($manifest.allowed_folders) -join ', ') stay in the room's list if its database stays, and claude's trust for them stays in ~/.claude.json"
    }

    # WHAT room-git.ps1 init MADE, undone by room-git.ps1 rather than by knowing its layout: the clone on the remote,
    # ~\.room-git, and the git remote here. It keeps the clone, and says why, when it holds work this machine lacks.
    if ($Repo -ne 'none') {
        & pwsh -NoProfile -File (Join-Path $PSScriptRoot 'room-git.ps1') remove $room -Target $Target -Ssh $Ssh @(if ($SshOption) { '-SshOption'; $SshOption -join ',' }) *>&1 | ForEach-Object { Write-Host $_ }
        if ($LASTEXITCODE -ne 0) { Step 'git' 'warn' "room-git remove exited $LASTEXITCODE. rerun: room-git.ps1 remove $room -Target $Target" }
    }
    if ($manifest.workroot) { Step 'work-root' 'skip' "left alone: $($manifest.workroot) and the cache settings (.npmrc, go env, pip, CARGO_HOME) stay, they hold the account's work" }
    if ($manifest.agentpack) { Step 'agent-pack' 'skip' 'left alone: the agents and skills stay in each runner folder (~/.claude, ~/.codex, ~/.gemini), where the account may have added to them' }

    # A ROOM HEARD FROM IN THE LAST TWENTY SECONDS IS NOT REMOVED, even forced,
    # so wait for the hub to stop hearing from it.
    if (Get-HubRoom $room) {
        $deadline = (Get-Date).AddSeconds(45)
        do {
            $r = Invoke-Hub @('rooms', 'rm', $room, '--force')
            if ($r.Code -eq 0) { break }
            Start-Sleep -Seconds 3
        } while ((Get-Date) -lt $deadline)
        if ($r.Code -ne 0) { Fail 'hub' 4 "the hub would not let go of $room" $r.Out }
        Step 'hub' 'done' "removed $room"
    } else {
        Step 'hub' 'skip' "the hub has no room called $room"
    }
    # THE NETWORK'S HALF IS THE OPERATOR'S, the same way it was given.
    if ($manifest.transport -eq 'ziti') {
        Step 'overlay' 'warn' "the ziti identity for $room is still on your network. delete it there, for example: ziti edge delete identity $room"
    }
    Finish 0
}

# ── 12 and 13, the last two steps ───────────────────────────────────────────

# DEFINED HERE, AHEAD OF STEP 4, so -SmokeOnly can run them against a room that
# is already provisioned without reaching the steps between, any of which can
# restart it. They are called at the end of the file in a full run.

# ALLOWED FOLDERS, the helpers. The step itself (Invoke-AllowedFolders) runs in a full run only, and the smoke below
# reads the room's list too, so these are defined here for -SmokeOnly.
#
# Get-FolderScript (room-folders.ps1) builds the text and Invoke-Remote runs it, so the verb goes through the same path
# as every other remote step, with sh on macOS and Linux.
function Invoke-RoomFolders {
    param([string[]] $verbArgs)
    Invoke-Remote (Get-FolderScript $os $verbArgs)
}

# What the room says its list is, read once and cached. Missing is an atrium without the verb, which is a thing to say
# and never a failure. Ok is false for any other trouble reading it, with the first line it said in Text.
function Get-RoomFolderList {
    if ($script:roomFolders) { return $script:roomFolders }
    $r = Invoke-RoomFolders @('list', '--json')
    $p = ConvertFrom-FolderList $r.Out
    $script:roomFolders = [pscustomobject]@{
        Missing  = [bool] (Test-FolderVerbMissing $r.Code $r.Out)
        Ok       = ($r.Code -eq 0 -and $p.Ok)
        Roots    = $p.Roots
        Enforced = $p.Enforced
        Text     = (@($r.Out | Where-Object { "$_".Trim() }) | Select-Object -First 1) -join ''
    }
    $script:roomFolders
}

# The command that sets the list later, from this side, for a warn line. The binary is named by its path, since the
# ssh login's PATH may not hold it.
function Get-FoldersCommand {
    param([string[]] $dirs)
    $sshCmd = (@($Ssh) + $SshOption + @($Target)) -join ' '
    $exe = if ($os -eq 'windows') { '.\.atrium\bin\atrium.exe' } else { '~/.local/bin/atrium' }
    $list = if ($dirs.Count) { ($dirs | ForEach-Object { if ($_ -match '\s') { "`"$_`"" } else { $_ } }) -join ' ' } else { '<dir>...' }
    "$sshCmd $exe room folders allow $list"
}

# Remote home as an absolute path with forward slashes, for expanding a leading ~ in -AllowedFolders.
function Get-FolderRemoteHome {
    $s = if ($os -eq 'windows') { '"home=$($HOME -replace ''\\'', ''/'')"' } else { 'echo "home=$HOME"' }
    $h = (ConvertFrom-KeyValue (Invoke-Remote $s).Out).home
    if ($h) { $h.TrimEnd('/') } else { $null }
}

# 11c. folders: the folders atrium may launch in
#
# THE ROOM'S LIST AND CLAUDE'S TRUST FOR EACH, set by `atrium room folders allow`, so a launch never meets the "do you
# trust this folder" dialog nobody answers. A card on sg3 sat at one. See docs/backlog/fabric/f-new-allowed-folders.md.
#
# NEVER A FAILURE. A room with no list works as it did, it just does not bound its launches, so every way this goes
# wrong is a `warn` that carries the command to run by hand: an atrium without the verb, a folder the room skips (home,
# a filesystem root, missing), a room that did not answer.
#
# A RERUN CHANGES NOTHING unless -AllowedFolders is given, the way a rerun keeps the autostart mode. The default list is
# for a new provision only, and an explicit one adds to what the manifest records.
function Invoke-AllowedFolders {
    $explicit = $AllowedFolders.Count -gt 0
    if (-not $explicit -and -not $freshManifest) {
        $had = @($script:manifest.allowed_folders | Where-Object { $_ })
        $says = if ($had.Count) { "the manifest records $($had -join ', ')" } else { 'the manifest records none, so the room is not bounded' }
        Step 'folders' 'skip' "a rerun keeps the list as it is ($says). -AllowedFolders <dir,...> adds to it"
        return
    }
    $remoteHome = Get-FolderRemoteHome
    $dirs = @()
    if ($explicit) {
        $dirs = @($AllowedFolders | ForEach-Object { Expand-FolderArg $_ $remoteHome })
    } else {
        $wtr = $null
        try { $wtr = (Get-StateDirs).wtr } catch { }
        if ($wtr -and $os -eq 'windows') { $wtr = $wtr -replace '\\', '/' }
        $dirs = @(Get-DefaultFolders $clonePath $wtr)
        # THE WORKTREES FOLDER IS MADE HERE, since nothing has made a worktree yet and the room skips a folder that is
        # not there.
        if ($clonePath) {
            $wt = (Get-DefaultFolders $clonePath $null)[1]
            $mk = if ($os -eq 'windows') { "New-Item -ItemType Directory -Force -Path $(Quote-Ps ($wt -replace '/', '\')) | Out-Null" } else { "mkdir -p $(Quote-Sh $wt)" }
            $null = Invoke-Remote $mk
        }
    }
    if (-not $dirs.Count) {
        Step 'folders' 'warn' "no clone and no WORKTREE_ROOT on $Name, so there is no default list and the room is not bounded. set one: $(Get-FoldersCommand @())"
        return
    }

    $pre = Get-RoomFolderList
    if ($pre.Missing) {
        Step 'folders' 'warn' "this atrium has no room folders verb yet, so $Name has no allowed folders and launches are not bounded. when it has one, run: $(Get-FoldersCommand $dirs)"
        return
    }
    if (-not $pre.Ok) {
        Step 'folders' 'warn' "could not read $Name's folder list ($($pre.Text)). is the room answering on loopback? then run: $(Get-FoldersCommand $dirs)"
        return
    }

    $r = Invoke-RoomFolders (@('allow') + $dirs)
    $script:roomFolders = $null
    foreach ($l in $r.Out) { if ("$l".Trim()) { Write-Host "    $l" } }
    if (Test-FolderVerbMissing $r.Code $r.Out) {
        Step 'folders' 'warn' "this atrium has no room folders allow verb yet. when it has one, run: $(Get-FoldersCommand $dirs)"
        return
    }
    $res = @(ConvertFrom-FolderAllow $r.Out)
    $ended = @($res | Where-Object { $_.Kind -in 'allowed', 'trusted' } | ForEach-Object { $_.Dir } | Select-Object -Unique)
    $skipped = @($res | Where-Object { $_.Kind -eq 'skipped' })
    if (-not $res.Count) {
        Step 'folders' 'warn' "folders allow exited $($r.Code) and said nothing a dir line could be read from. run: $(Get-FoldersCommand $dirs)"
        return
    }
    if ($ended.Count) {
        $have = @($script:manifest.allowed_folders | Where-Object { $_ })
        # Compared as Format-FolderPath spells a folder, so /srv/a/ and /srv/a are one entry, and the first spelling stays.
        $seen = @{}
        $now = @(foreach ($d in @($have + $ended)) {
            $k = Format-FolderPath "$d"
            if ($k -and -not $seen.ContainsKey($k)) { $seen[$k] = $true; $d }
        })
        if (($now -join "`n") -ne ($have -join "`n")) {
            $script:manifest | Add-Member -NotePropertyName allowed_folders -NotePropertyValue $now -Force
            Save-Manifest
        }
    }
    foreach ($s in $skipped) {
        Step 'folders' 'warn' "$($s.Dir) was skipped: $($s.Reason). the room does not launch there until it is allowed: $(Get-FoldersCommand @($s.Dir))"
    }
    if ($r.Code -ne 0 -and -not $skipped.Count) {
        Step 'folders' 'warn' "folders allow exited $($r.Code), and only $($ended.Count) of $($dirs.Count) folders ended allowed. the lines above say why"
    }
    if ($ended.Count) {
        $was = @($pre.Roots | ForEach-Object { Format-FolderPath $_ })
        $new = @($ended | Where-Object { $was -notcontains (Format-FolderPath $_) })
        $word = if ($new.Count) { 'done' } else { 'ok' }
        $trusted = @($res | Where-Object { $_.Kind -eq 'trusted' } | ForEach-Object { $_.Dir } | Select-Object -Unique).Count
        Step 'folders' $word "$($ended -join ', ') allowed on $Name, claude's folder trust written for $trusted of $($ended.Count)$(if (-not $new.Count) { ' (all were already in the list)' })"
    }
}

# 12. is claude signed in
#
# `claude auth status` ANSWERS WITHOUT A PROMPT, in JSON, and says loggedIn. It
# is run the way the room runs claude: through a login shell on Unix.
#
# NEVER A FAILURE. A room that is not signed in still works, it only needs a
# person. So this says the exact command and goes on. It never carries, copies
# or reads a credential: the answer is read from the CLI, and the sign-in is
# done by clint, at a terminal, with their own browser.
function Test-ClaudeAuth {
    $authState = 'na'
    if (@($Runners + $Install) -contains 'claude') {
        $as = if ($os -eq 'windows') {
@'
$c = Get-Command claude -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $c) { 'auth=missing'; exit 0 }
$job = Start-Job { param($n) & $n auth status 2>&1 | Out-String } -ArgumentList $c.Source
if (Wait-Job $job -Timeout 30) { 'json=' + [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes((Receive-Job $job | Out-String))) }
else { Stop-Job $job; 'auth=hung' }
'@
        } else {
@'
sh_=${SHELL:-/bin/sh}
if [ -z "$("$sh_" -lc 'command -v claude' 2>/dev/null)" ]; then echo auth=missing; exit 0; fi
o=$("$sh_" -lc 'claude auth status' </dev/null 2>&1)
echo "json=$(printf '%s' "$o" | base64 | tr -d '\n')"
'@
        }
        $kv = ConvertFrom-KeyValue (Invoke-Remote $as).Out
        $text = if ($kv.json) { try { [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($kv.json)) } catch { '' } } else { '' }
        $j = try { $text | ConvertFrom-Json } catch { $null }
        if ($kv.auth -eq 'missing') {
            $authState = 'unknown'
            Step 'auth' 'skip' 'claude is not on PATH there, so there is nothing to check'
        } elseif ($kv.auth -eq 'hung') {
            $authState = 'unknown'
            Step 'auth' 'warn' 'claude auth status did not answer in 30s, so whether it is signed in is not known'
        } elseif ($j -and $null -ne $j.loggedIn) {
            if ($j.loggedIn) {
                $authState = 'ok'
                $who = @($j.email, $j.orgName | Where-Object { $_ }) -join ', '
                Step 'auth' 'ok' "signed in with $($j.authMethod)$(if ($who) { ", $who" })"
            } else {
                $authState = 'no'
                $sshCmd = (@($Ssh) + $SshOption + @('-t', $Target)) -join ' '
                Step 'auth' 'warn' "claude on $Name is not signed in. the room works and needs you once: run `"$sshCmd claude auth login`" and follow the URL it prints"
                Write-Host "    why that one: it is the CLI's own sign-in. it prints a URL to open in any browser, here, and takes the code back,"
                Write-Host "    so it needs no browser on $Name and this never handles the credential. 'claude setup-token' would hand you a token to store, which is a credential to carry."
            }
        } else {
            $authState = 'unknown'
            $first = ($text -split "`n" | Where-Object { $_.Trim() } | Select-Object -First 1)
            Step 'auth' 'warn' "claude auth status did not answer with JSON, so whether it is signed in is not known. it said: $first"
        }
    }
    $authState
}

# 13. smoke: a claude worker on the room that talks back
#
# THE PROOF THE ROOM CAN DO ITS JOB. It starts a small, lean claude card on the
# new room through the hub, gives it a nonce, and reads the card back from the
# hub until its report holds that nonce. The script decides pass or fail from
# what it reads itself, never from what anybody says. The card is then exited
# and checked to have left.
#
# ATRIUM-CONTROL CALLS ONLY. A permission prompt on the smoke card would go to
# the human board and stall it, so the prompt asks for no gated tool.
#
# Skipped when there is nothing it could prove: -NoSmoke, claude not a runner
# here, or claude not signed in (which auth already said, with the command).
function Invoke-Smoke {
    param([string] $authState)
    if ($NoSmoke) { Step 'smoke' 'skip' '-NoSmoke'; return }
    $want = @(@($Runners + $Install) | Where-Object { $_ } | Select-Object -Unique)
    # `-File x -SmokeRunners claude,codex` arrives as one string, so split it.
    $chosen = @($SmokeRunners | ForEach-Object { $_ -split ',' } | Where-Object { $_ })
    if ($chosen.Count) { $want = @($chosen | Select-Object -Unique) }
    $ran = 0
    $anyFail = $false
    foreach ($r in $want) {
        if (@($Runners + $Install) -notcontains $r) { Step "smoke:$r" 'skip' "$r is not a runner for this room"; continue }
        if ($r -eq 'claude') {
            $why = $null
            if ($authState -eq 'no') { $why = "claude is not signed in on $Name, so a worker there cannot answer. sign in, then rerun" }
            elseif ($authState -ne 'ok') { $why = 'whether claude is signed in is not known, so the smoke card would only guess' }
            if ($why) { Step 'smoke:claude' 'skip' $why; continue }
        } elseif ($r -eq 'codex') {
            $login = Test-CodexLogin
            if ($login -eq 'missing') { Step 'smoke:codex' 'skip' "codex is not on PATH on $Name, so there is nothing to run"; continue }
            if ($login -eq 'no') {
                $sshCmd = (@($Ssh) + $SshOption + @('-t', $Target)) -join ' '
                Step 'smoke:codex' 'warn' "codex on $Name is installed and not signed in. the room works and needs you once: run `"$sshCmd codex login --device-auth`""
                continue
            }
        } else {
            Step "smoke:$r" 'skip' 'no smoke case'
            continue
        }
        $ran++
        if (-not (Invoke-SmokeCase $r)) { $anyFail = $true }
    }
    if ($anyFail) { Finish 8 }
}

# One smoke card for one runner. True when it passed, false when a `fail` step
# was written. The card is exited whatever happened.
function Invoke-SmokeCase {
    param([string] $runner)
    $step = "smoke:$runner"
    function Get-SmokeCwd {
        if ($SmokeCwd) { return $SmokeCwd }
        if ($script:clonePath) { return $script:clonePath }
        # -SMOKEONLY MAKES NO CLONE, but room-git init left this repository a remote
        # named for the room, and its url is the clone's path there. NEVER THE
        # HOME: item 67 refuses to start a card in it, so the last resort is
        # ~/.atrium/smoke, which is made on the remote when it is missing.
        $url = & git -C (Split-Path $PSScriptRoot) remote get-url $Name 2>$null
        if ($LASTEXITCODE -eq 0 -and $url) {
            $url = "$url".Trim()
            if ($url -match '^[^/:]+:([A-Za-z]:/.*)$') { return $Matches[1] }
            if ($url -match '^ssh://[^/]+/([A-Za-z]:/.*)$') { return $Matches[1] }
            if ($url -match '^ssh://[^/]+(/.*)$') { return $Matches[1] }
        }
        $hs = if ($os -eq 'windows') { "`$d = Join-Path `$A 'smoke'; New-Item -ItemType Directory -Force -Path `$d | Out-Null; `"dir=`$(`$d -replace '\\', '/')`"" }
              else { 'd="$A/smoke"; mkdir -p "$d" && echo "dir=$d"' }
        (ConvertFrom-KeyValue (Invoke-Remote $hs).Out).dir
    }

    $nonce = -join ((1..8) | ForEach-Object { '{0:x}' -f (Get-Random -Maximum 16) })
    $me = $env:ATRIUM_AGENT_NAME; $myRoom = $env:ATRIUM_ROOM; $myCard = $env:ATRIUM_TASK_ID
    $to = $SmokeTo
    # A BARE HANDLE IS NOT FOUND FROM THE OTHER ROOM: the smoke worker runs on the
    # remote, and only `name@room` reaches back across the hub to this side.
    if ($to -and $to -notmatch '@' -and $myRoom) { $to = "$to@$myRoom" }
    if (-not $to -and $me -and $myRoom) { $to = "$me@$myRoom" }
    $cwd = Get-SmokeCwd
    if (-not $cwd) { Step $step 'fail' "could not resolve a folder on $Name to run in. pass -SmokeCwd"; return $false }

    # INSIDE THE ROOM'S LIST, or the room refuses the card and the smoke fails for a reason that is not a fault. The
    # default folder is the clone, which the list holds. A folder that is not (a room whose list was set by hand) moves
    # to the first allowed folder, and a -SmokeCwd that is not is the operator's to fix: say so rather than hit a refusal.
    # Only an enforced list counts, and the check is on the text of the paths, so a symlink can make it wrong.
    $fl = Get-RoomFolderList
    if ($fl.Ok -and $fl.Enforced -and $fl.Roots.Count -and -not (Test-UnderFolders $cwd $fl.Roots ($os -ne 'linux'))) {
        $roots = $fl.Roots -join ', '
        if ($SmokeCwd) {
            Step $step 'warn' "-SmokeCwd $cwd is outside $Name's allowed folders ($roots), so the room would refuse the smoke card and none was launched. pass a folder inside them, or allow it: $(Get-FoldersCommand @($cwd))"
            return $true
        }
        Step $step 'warn' "the smoke folder $cwd is outside $Name's allowed folders ($roots), so the card runs in $($fl.Roots[0])"
        $cwd = $fl.Roots[0]
    }

    $said = "smoke ok $Name $nonce"
    # THE TITLE CARRIES THE NONCE: a card's wire name comes from its title, and a
    # launch onto a name that already exists re-prompts THAT card. A fixed title
    # met the previous run's card, whose recap held the previous nonce.
    if ($runner -eq 'codex') {
        # THE ROUND TRIP CODEX CAN MAKE: it has no atrium-control tools, so it runs
        # `atrium finish`, which lands the nonce in the card's recap. Not lean
        # (lean is claude only). Model and effort are the row's own defaults, with
        # effort low: the row takes both, and the cheapest that works is the
        # smallest thinking, not a named model that an account may not have.
        $prompt = "This is an automated smoke test of the room $Name. Run exactly this one shell command and then stop: atrium finish `"$nonce`""
        $body = [ordered]@{
            harness = 'codex'; cwd = $cwd; title = "smoke: $Name codex $nonce"; prompt = $prompt
            tags = @('atrium:smoke'); effort = 'low'
            args = @('-a', 'never', '-s', 'workspace-write', '-c', 'sandbox_workspace_write.network_access=true')
        }
    } else {
    $prompt = "This is an automated smoke test of the room $Name. Do exactly these steps and nothing else. " +
        "Use only the atrium-control tools: no Bash, no file reads, no edits.`n"
    $n = 1
    if ($to) { $prompt += "$n. Call atrium_say to $to with the text: $said`n"; $n++ }
    $prompt += "$n. Call atrium_report with status done, the summary: $said, and no_commit: smoke test, no work.`n" +
        "Then stop. When you finish, get blocked, or need an answer, call atrium_report (or atrium_say your launcher) before you end your turn."
    $body = [ordered]@{
        harness = 'claude'; cwd = $cwd; title = "smoke: $Name claude $nonce"; prompt = $prompt
        tags = @('atrium:smoke'); lean = $true
        # SONNET, NOT HAIKU. Claude Code's auto mode does not run on Haiku, so a
        # Haiku card falls back to asking, and nobody answers. Seen on sg3: a
        # Sonnet worker went through while the Haiku smoke card stalled.
        model = 'claude-sonnet-5-5'; effort = 'low'
        # CLAUDE CODE ASKS BEFORE AN MCP TOOL'S FIRST CALL, at a prompt in the
        # card's own terminal that nobody is watching. Seen on sg3: the card sat
        # at "Do you want to proceed?" on atrium_say until it was exited. The two
        # tools are allowed for this card only. The room's own settings are not
        # touched, and a worker that is not a smoke card still asks.
        #
        # ONE ARGUMENT, WITH =. --allowedTools takes any number of values, so as
        # two arguments it also swallowed the prompt that follows it, and the
        # card came up at an empty input line with nothing to do. Seen on sg3.
        args = @('--allowedTools=mcp__atrium-control__atrium_say,mcp__atrium-control__atrium_report')
    }
    }
    # WHO LAUNCHED IT, so the report lands on the caller's card. A launcher on
    # another room is `me@room`, and its card `room~id`.
    if ($me -and $myRoom) { $body.spawned_by = "$me@$myRoom" }
    if ($myRoom -and $myCard) { $body.spawned_by_id = "$myRoom~$myCard" }

    $hdr = @{ 'X-Atrium-Room' = $Name }
    $card = $null
    $smokeErr = $null
    try {
        $card = Invoke-RestMethod -Method Post -Uri "http://$HubAddr/v1/launch" -Headers $hdr `
            -ContentType 'application/json' -Body ($body | ConvertTo-Json -Depth 5) -TimeoutSec 60
    } catch {
        # THE BODY SAYS WHY. The room answers every launch failure 400 with the
        # reason in the body, and the exception message alone is only "400 (Bad
        # Request)". Seen on m1mini straight after a restart, and not explainable.
        $why = "$($_.ErrorDetails.Message)".Trim()
        $smokeErr = "the hub would not launch it: $($_.Exception.Message)" + $(if ($why) { " $why" } else { '' })
    }
    if ($card -and -not $card.id) { $smokeErr = 'the hub answered the launch with no card'; $card = $null }

    $reported = $false
    if ($card) {
        $id = $card.id
        $t0 = Get-Date
        while (((Get-Date) - $t0).TotalSeconds -lt $SmokeTimeout) {
            Start-Sleep -Seconds 3
            try {
                $t = Invoke-RestMethod -Uri "http://$HubAddr/v1/tasks/$id" -Headers $hdr -TimeoutSec 10
                if ("$($t.recap)" -like "*$nonce*") { $reported = $true; break }
            } catch { }
        }
        $took = [int]((Get-Date) - $t0).TotalSeconds
        # WHAT THE CARD SHOWS, read before it is exited, so a smoke that did not report says why instead of only that it
        # did not. Seen on sgg: claude answered at once that it had no atrium_say or atrium_report, because the mcp step had
        # not run on a provision that stopped early, and the card then sat done, reporting nothing.
        $screen = @()
        if (-not $reported) {
            try {
                $txt = Invoke-RestMethod -Uri "http://$HubAddr/v1/tasks/$id/scrollback/text" -Headers $hdr -TimeoutSec 15
                $screen = @("$txt" -split "`r?`n" | Where-Object { $_.Trim() } | Select-Object -Last 14)
            } catch { }
        }
        # EXITED WHATEVER HAPPENED, so a smoke card never lingers on the room.
        try { Invoke-RestMethod -Method Post -Uri "http://$HubAddr/v1/tasks/$id/exit" -Headers $hdr -TimeoutSec 15 | Out-Null } catch { }
        $left = $false
        $t1 = Get-Date
        while (((Get-Date) - $t1).TotalSeconds -lt 30) {
            try {
                $t = Invoke-RestMethod -Uri "http://$HubAddr/v1/tasks/$id" -Headers $hdr -TimeoutSec 10
                if (-not $t.supervised) { $left = $true; break }
            } catch { }
            Start-Sleep -Seconds 2
        }
        $leftWord = if ($left) { 'and it exited' } else { 'but it did not leave within 30s, so exit it yourself' }
        if ($reported) {
            Step $step 'ok' "a $runner worker on $Name reported $nonce in ${took}s, $leftWord$(if ($to -and $runner -eq 'claude') { ", and said it to $to" })"
            if (-not $left) { Step $step 'warn' "card $id is still running on $Name" }
            if (-not (Invoke-SmokeOutside $runner $body $hdr $nonce)) { return $false }
        } else {
            $smokeErr = "the smoke card $id did not report $nonce in ${SmokeTimeout}s ($leftWord). look at it on the board, it is on $Name"
            $screenText = $screen -join ' '
            $sshT = (@($Ssh) + $SshOption + @('-t', $Target)) -join ' '
            if ($screenText -match "(?i)(atrium_say|atrium_report|atrium-control)[^.]*(not available|aren.t available|unavailable|not found)|(not available|aren.t available)[^.]*(atrium_say|atrium_report)") {
                $smokeErr += ". the card's own screen says claude there has no atrium-control tools: rerun without -SmokeOnly, whose mcp step writes ~/.atrium/mcp.json and names it in the claude runner row"
            } elseif ($screenText -match '(?i)trust this folder|choose the text style|select login method|Welcome to Claude|dark mode|press enter to continue') {
                $smokeErr += ". the card is at a claude first-run screen nobody answers. run once: $sshT claude, answer its screens, then /exit"
            }
            $script:smokeScreen = $screen
        }
    }
    if ($smokeErr) {
        Step $step 'fail' $smokeErr
        foreach ($l in @($script:smokeScreen)) { if ($l) { Write-Host "    card: $l" } }
        $script:smokeScreen = $null
        return $false
    }
    $true
}

# The gate, from outside: a card whose cwd is OUTSIDE the room's list must be refused, with an error that names the
# allowed folders. It runs once per run, after a smoke card has reported, and only when `folders list --json` says the
# list is enforced, so a room without the verb or without a list prints `smoke-outside skip <reason>`.
#
# THE OUTSIDE FOLDER is the remote home, else the filesystem root when the home is inside the list. A card that is
# launched all the same is exited, whatever else happens, so this never leaves one running, and that is the one result
# here that fails: the gate did not hold. Anything else unexpected is a `warn`.
function Invoke-SmokeOutside {
    param([string] $runner, $body, $hdr, [string] $nonce)
    if ($script:smokeOutsideDone) { return $true }
    $script:smokeOutsideDone = $true
    $fl = Get-RoomFolderList
    if ($fl.Missing) { Step 'smoke-outside' 'skip' "this atrium has no room folders verb yet, so $Name has no list to launch outside of"; return $true }
    if (-not $fl.Ok) { Step 'smoke-outside' 'skip' "$Name's folder list could not be read ($($fl.Text))"; return $true }
    if (-not $fl.Enforced) { Step 'smoke-outside' 'skip' "$Name has no allowed folders set, so a launch anywhere is allowed. set them: $(Get-FoldersCommand @())"; return $true }

    $ic = $os -ne 'linux'
    $outside = Get-FolderRemoteHome
    if (-not $outside -or (Test-UnderFolders $outside $fl.Roots $ic)) { $outside = if ($os -eq 'windows') { 'C:/' } else { '/' } }
    $roots = $fl.Roots -join ', '

    $b = [ordered]@{}
    foreach ($k in $body.Keys) { $b[$k] = $body[$k] }
    $b.cwd = $outside
    $b.title = "smoke-outside: $Name $runner $nonce"
    $card = $null
    $status = 0
    $msg = ''
    try {
        $card = Invoke-RestMethod -Method Post -Uri "http://$HubAddr/v1/launch" -Headers $hdr `
            -ContentType 'application/json' -Body ($b | ConvertTo-Json -Depth 5) -TimeoutSec 60
    } catch {
        if ($_.Exception.Response) { $status = [int] $_.Exception.Response.StatusCode }
        $msg = "$($_.ErrorDetails.Message)".Trim()
        if (-not $msg) { $msg = $_.Exception.Message }
    }
    if ($card -and $card.id) {
        try { Invoke-RestMethod -Method Post -Uri "http://$HubAddr/v1/tasks/$($card.id)/exit" -Headers $hdr -TimeoutSec 15 | Out-Null } catch { }
        $left = $false
        $t1 = Get-Date
        while (((Get-Date) - $t1).TotalSeconds -lt 30) {
            try {
                $t = Invoke-RestMethod -Uri "http://$HubAddr/v1/tasks/$($card.id)" -Headers $hdr -TimeoutSec 10
                if (-not $t.supervised) { $left = $true; break }
            } catch { }
            Start-Sleep -Seconds 2
        }
        $leftWord = if ($left) { 'and it exited' } else { 'but it did not leave within 30s, so exit it yourself' }
        Step 'smoke-outside' 'fail' "$Name launched a card in $outside, which is outside its allowed folders ($roots), so the launch bound is not holding. card $($card.id) was asked to exit ($leftWord)"
        return $false
    }
    if ($status -ge 400 -and $status -lt 500) {
        if (Test-FolderRefusal $msg $fl.Roots) { Step 'smoke-outside' 'ok' "$Name refused a launch in $outside (HTTP $status): $msg" }
        else { Step 'smoke-outside' 'warn' "$Name refused a launch in $outside (HTTP $status), but the error does not name its allowed folders ($roots): $msg" }
    } else {
        Step 'smoke-outside' 'warn' "a launch in $outside got no clear answer from the hub, so whether $Name's launch bound holds is not known: $msg"
    }
    $true
}

# Is codex signed in there. `codex login status` answers without a prompt. There
# is no runner-setup adapter for codex, so the hub cannot say, and this is the
# only check. Returns ok, no, missing or unknown. Unknown goes on to the smoke
# card, whose own outcome is then the answer.
function Test-CodexLogin {
    $cs = if ($os -eq 'windows') {
@'
$c = Get-Command codex -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $c) { 'codex=missing'; exit 0 }
$job = Start-Job { param($n) & $n login status 2>&1 | Out-String } -ArgumentList $c.Source
if (Wait-Job $job -Timeout 30) { 'json=' + [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes((Receive-Job $job | Out-String))) }
else { Stop-Job $job; 'codex=hung' }
'@
    } else {
@'
sh_=${SHELL:-/bin/sh}
if [ -z "$("$sh_" -lc 'command -v codex' 2>/dev/null)" ]; then echo codex=missing; exit 0; fi
o=$("$sh_" -lc 'codex login status' </dev/null 2>&1)
echo "json=$(printf '%s' "$o" | base64 | tr -d '\n')"
'@
    }
    $kv = ConvertFrom-KeyValue (Invoke-Remote $cs).Out
    if ($kv.codex -eq 'missing') { return 'missing' }
    if ($kv.codex -eq 'hung' -or -not $kv.json) { return 'unknown' }
    $text = try { [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($kv.json)) } catch { '' }
    if ($text -match '(?i)not logged in|not signed in') { return 'no' }
    if ($text -match '(?i)logged in') { return 'ok' }
    'unknown'
}

# ── 4. one room per machine ─────────────────────────────────────────────────

if (-not $Name) {
    $Name = if ($manifest) { $manifest.name } else { ($remoteHost.ToLower() -replace '[^a-z0-9._-]', '-') }
}
if (-not $manifest) {
    $what = @()
    if ($state.joinedroom) { $what += "it is already the room $($state.joinedroom)" }
    if ($state.service) { $what += "it has an atrium autostart ($($state.service))" }
    if ($state.up7781 -eq 'True') { $what += 'a room answers on 7781' }
    if ($state.up7778 -eq 'True') { $what += 'an atrium daemon answers on 7778' }
    if ($what) {
        Fail 'state' 6 "one room per machine, and $($what -join ', '), which this script did not put there. stop or remove that first"
    }
} elseif ($manifest.name -ne $Name -or ($joinedId -and $joinedId -ne $hubId)) {
    $was = if ($joinedId) { "$($manifest.name) of $joinedId" } else { $manifest.name }
    Fail 'state' 6 "one room per machine, and this one is already the room $was. -Remove it first to make it $Name of $hubId"
}

# -SMOKEONLY STOPS HERE, before anything is written. Everything above only
# reads, and every step below could restart the room: a new binary, a join, an
# autostart. So a room that is in use can be smoke tested and nothing else.
if ($SmokeOnly) {
    if (-not $manifest) { Fail 'state' 6 "-SmokeOnly is for a room this script provisioned, and $Target has no manifest" }
    Step 'state' 'ok' "provisioned before as $($manifest.name). -SmokeOnly, so nothing on it is changed"
    Invoke-Smoke (Test-ClaudeAuth)
    Finish 0
}

# ── -Restart ────────────────────────────────────────────────────────────────
#
# STOP, THEN START, NEVER A KILL. `atrium stop` winds the room down (supervised
# runners get ten seconds), and a bare kill would take every runner's pseudo
# terminal with it. Nothing is stopped without -Yes: without it this prints what
# it would do and exits 0 having changed nothing. It never registers, enables
# or disables anything, so a supervised room is started by the verb its own
# supervisor has, and a detached one the way provision starts one.
#
#   restart-mode   how the room was started, and the state directory it pins
#   restart-cards  the live cards, and which of them block
#   stop           atrium stop, then the room's ports closing
#   start          the same way it was started
#   attach         the hub shows it again under the same name, runner rows intact
if ($Restart) {
    if (-not $manifest) { Fail 'state' 6 "-Restart is for a room this script provisioned, and $Target has no manifest" }
    Step 'state' 'ok' "provisioned before as $($manifest.name)"
    $ports = if ($manifest.ports) { @($manifest.ports) } else { @(7781, 7777) }
    $hdr = @{ 'X-Atrium-Room' = $Name }

    # -- mode: the registration says how the room is started, and whether it is
    # a STICKY stop (registered but switched off, which a start would not undo).
    $ms = if ($os -eq 'windows') {
@'
$x = Sch /Query /TN atrium /XML
if ($LASTEXITCODE -eq 0) {
    'reg=task-logon'
    if (($x | Out-String) -match '<Enabled>false</Enabled>') { 'sticky=True' }
}
'@
    } else {
@'
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
if [ -f "$HOME/.config/systemd/user/atrium.service" ]; then
  echo reg=systemd-user
  if [ "$(systemctl --user is-enabled atrium 2>/dev/null)" = disabled ]; then echo sticky=True; fi
fi
if [ -f "$HOME/Library/LaunchAgents/io.github.dovholuknf.atrium.plist" ]; then
  echo reg=launchagent
  if launchctl print-disabled "gui/$(id -u)" 2>/dev/null | grep -Eq '"io.github.dovholuknf.atrium" => (disabled|true)'; then echo sticky=True; fi
fi
'@
    }
    $mk = ConvertFrom-KeyValue (Invoke-Remote $ms).Out
    $kind = if ($mk.reg) { $mk.reg } else { 'detached' }
    if ($kind -eq 'detached' -and $manifest.autostart -eq $true) {
        Fail 'restart-mode' 3 'the manifest says autostart was installed, and the registration is not there any more. rerun with -Autostart to register it again, this never does'
    }

    # -- the state directory the room was joined with, pinned for the start.
    $sd = Get-StateDirs
    if ($sd.room_a -and $sd.room_a -ne $Name -and -not $sd.has_w) { Fail 'restart-mode' 6 "room.json under $($sd.dir_a) says $($sd.room_a), and the manifest says $Name" }
    $pin = Resolve-StateDir $sd
    if (-not $pin.Dir) { Fail 'restart-mode' 3 "cannot tell which state directory the room uses: $($pin.Why)" }
    $wtrValue = $null
    $normA = ($sd.dir_a -replace '\\', '/').TrimEnd('/')
    $normP = ($pin.Dir -replace '\\', '/').TrimEnd('/')
    if ($normP -ine $normA) {
        if ($normP -match '^(.+)/hub$') { $wtrValue = $pin.Dir.Substring(0, $pin.Dir.Length - 4) }
        else { Fail 'restart-mode' 3 "the state directory is $($pin.Dir), which is neither ~/.atrium nor WORKTREE_ROOT/hub, so it cannot be pinned" }
    }
    $pinSays = if ($wtrValue) { "state $($pin.Dir), started with WORKTREE_ROOT=$wtrValue" } else { "state $($pin.Dir), started with WORKTREE_ROOT cleared" }
    if ($mk.sticky) {
        Step 'restart-mode' 'fail' "$kind, and it is switched off, so it stays down until it is switched on: run `"$(if ($os -eq 'windows') { 'atrium-service.ps1 start' } else { 'atrium-service.sh start' })`" on $Target (service start). $pinSays"
        Finish 10
    }
    Step 'restart-mode' 'ok' "$kind, $pinSays"

    # -- cards: which are live, and which would be lost mid-thought.
    $cards = $null
    try {
        $tr = Invoke-RestMethod -Uri "http://$HubAddr/v1/tasks" -Headers $hdr -TimeoutSec 15
        $cards = @(@($tr) + @($tr.tasks) | Where-Object { $_ -and $_.id -and $_.status })
    } catch { }
    $blocking = @()
    if ($null -eq $cards) {
        Step 'restart-cards' 'warn' "the hub would not list the cards of $Name, so whether any is mid-turn is not known"
        if (-not $Force) { Step 'restart-cards' 'fail' 'refusing without -Force'; Finish 9 }
    } else {
        $live = @($cards | Where-Object { $_.supervised -eq $true -and $_.status -notin @('done', 'dead', 'shelved', 'backlog') })
        $blocking = @($live | Where-Object { $_.status -eq 'needs-permission' -or ($_.status -eq 'running' -and [int64] $_.idle_seconds -lt 60) })
        $parked = @($live | Where-Object { $blocking -notcontains $_ })
        $name1 = { param($c) "$($c.id) `"$($c.display_title)`" ($($c.status))" }
        $detail = "$($live.Count) live"
        if ($blocking) { $detail += ", mid-turn or waiting on a permission: $((@($blocking | ForEach-Object { & $name1 $_ })) -join '; ')" }
        if ($parked) { $detail += ", will be parked and resumed: $((@($parked | ForEach-Object { & $name1 $_ })) -join '; ')" }
        if ($blocking -and -not $Force) { Step 'restart-cards' 'fail' "$detail. wait, or -Force"; Finish 9 }
        Step 'restart-cards' $(if ($blocking) { 'warn' } else { 'ok' }) $(if ($blocking) { "$detail. -Force given" } else { $detail })
    }
    # The runner rows as they stand, to compare once it is back.
    $rowsBefore = $null
    try {
        $hr = Invoke-RestMethod -Uri "http://$HubAddr/v1/harnesses" -Headers $hdr -TimeoutSec 10
        $rowsBefore = @(@($hr) + @($hr.harnesses) | Where-Object { $_ -and $_.id } | ForEach-Object { "$($_.id)" } | Sort-Object)
    } catch { }

    $portsText = $ports -join ', '
    $startSays = switch -Wildcard ($kind) {
        'detached'     { 'room --detach, through a login shell, pinned to the state above' }
        'task-logon'   { 'schtasks /Run /TN atrium, once the task reads Ready or Queued' }
        'systemd-user' { 'systemctl --user start atrium' }
        'launchagent'  { 'launchctl kickstart gui/<uid>/io.github.dovholuknf.atrium' }
    }
    if (-not $Yes) {
        Step 'stop' 'skip' "would run atrium stop --url $(Get-StopUrl), then wait up to 40s for ports $portsText to close. never a kill"
        Step 'start' 'skip' "would start it with $startSays"
        Step 'attach' 'skip' "would wait up to ${AttachTimeout}s for $Name on the hub, and compare its runner rows$(if ($rowsBefore) { " ($($rowsBefore -join ', '))" })"
        Step 'restart' 'ok' 'plan only. nothing was changed. add -Yes to do it'
        Finish 0
    }

    # -- stop.
    $sp = $(if ($os -eq 'windows') { "`$ports = @($($ports -join ', '))`n" } else { "ports='$($ports -join ' ')'`n" }) + $(if ($os -eq 'windows') {
@'
$ErrorActionPreference = 'Continue'
if (Test-Path $Bin) { & $Bin stop --url $StopUrl 2>&1 | Out-Null }
$end = (Get-Date).AddSeconds(40)
do {
    $open = @($ports | Where-Object { try { $c = New-Object Net.Sockets.TcpClient; $c.Connect('127.0.0.1', $_); $c.Close(); $true } catch { $false } })
    if (-not $open) { break }
    Start-Sleep -Seconds 1
} while ((Get-Date) -lt $end)
if ($open) { "open=$($open -join ',')"; exit 1 }
'closed=1'
'@
    } else {
@'
if [ -x "$Bin" ]; then "$Bin" stop --url $StopUrl >/dev/null 2>&1; fi
n=0
while :; do
  open=""
  for p in $ports; do curl -s -m 2 -o /dev/null "http://127.0.0.1:$p/"; if [ $? -ne 7 ]; then open="$open $p"; fi; done
  if [ -z "$open" ]; then break; fi
  n=$((n+1)); if [ $n -ge 40 ]; then break; fi
  sleep 1
done
if [ -n "$open" ]; then echo "open=$open"; exit 1; fi
echo closed=1
'@
    })
    $r = Invoke-Remote $sp
    $ok = (ConvertFrom-KeyValue $r.Out).closed
    if (-not $ok) { Fail 'stop' 3 "the room's ports $portsText did not all close in 40s. it was not killed: look at ~/.atrium/room/room.log, then stop it by hand" $r.Out }
    Step 'stop' 'done' "atrium stop, ports $portsText closed"

    # A TASK STILL READING Running IGNORES /Run (IgnoreNew), so wait for it.
    if ($kind -eq 'task-logon') {
        $tw = @'
$end = (Get-Date).AddSeconds(30)
do {
    $s = (Sch /Query /TN atrium /V /FO LIST | Where-Object { "$_" -match '^Status:\s*(.+)$' } | ForEach-Object { $Matches[1].Trim() } | Select-Object -First 1)
    if ($s -ne 'Running') { break }
    Start-Sleep -Seconds 1
} while ((Get-Date) -lt $end)
"task=$s"
'@
        $tk = (ConvertFrom-KeyValue (Invoke-Remote $tw).Out).task
        if ($tk -eq 'Running') { Fail 'stop' 3 'the ports closed, but the task still reads Running after 30s, so a /Run would be ignored. it was not killed' }
    }

    # -- start.
    $beforeStart = Get-Date
    $needSince = $beforeStart
    switch ($kind) {
        'detached' {
            $pinW = if ($wtrValue) { $wtrValue } else { '' }
            # TODO: pass --state-dir instead of the environment once `atrium room` has it (@runtime).
            $ds = if ($os -eq 'windows') {
                "`$ErrorActionPreference = 'Continue'`n`$e = Join-Path `$HOME '.atrium\toolchain\room-env.ps1'; if (Test-Path `$e) { . `$e }`n" +
                $(if ($pinW) { "`$env:WORKTREE_ROOT = $(Quote-Ps $pinW)`n" } else { "Remove-Item Env:WORKTREE_ROOT -ErrorAction SilentlyContinue`n" }) +
                "& `$Bin room --detach 2>&1`nexit `$LASTEXITCODE"
            } else {
                "`"`${SHELL:-/bin/sh}`" -lc 'if [ -n `"`$1`" ]; then WORKTREE_ROOT=`"`$1`"; export WORKTREE_ROOT; else unset WORKTREE_ROOT; fi; exec `"`$0`" room --detach' `"`$Bin`" $(Quote-Sh $pinW) 2>&1"
            }
            $r = Invoke-Remote $ds
            if ($r.Code -ne 0) { Fail 'start' 3 'the room would not start' $r.Out }
            Step 'start' 'done' 'room --detach, in the background, pinned to the same state directory'
        }
        'task-logon' {
            $st = @'
$null = Sch /Run /TN atrium
$up = $false
$end = (Get-Date).AddSeconds(30)
while (-not $up -and (Get-Date) -lt $end) {
    Start-Sleep -Seconds 1
    try { $null = Invoke-RestMethod http://127.0.0.1:7781/v1/health -TimeoutSec 2; $up = $true } catch {}
}
if ($up) { 'start=done' } else { 'start=fail the task did not bring the room up in 30s. an Interactive task needs the user logged in at the machine' }
'@
            $kv = ConvertFrom-KeyValue (Invoke-Remote $st).Out
            $w = ($kv.start -split ' ', 2)
            if ($w[0] -ne 'done') { Fail 'start' 3 $(if ($w.Count -gt 1) { $w[1] } else { 'the task did not start' }) }
            Step 'start' 'done' 'schtasks /Run /TN atrium'
        }
        default {
            $vs = if ($kind -eq 'systemd-user') {
                "export XDG_RUNTIME_DIR=`"`${XDG_RUNTIME_DIR:-/run/user/`$(id -u)}`"`nsystemctl --user start atrium 2>&1"
            } else {
                "launchctl kickstart `"gui/`$(id -u)/io.github.dovholuknf.atrium`" 2>&1"
            }
            $r = Invoke-Remote $vs
            if ($r.Code -ne 0) { Fail 'start' 3 "the supervisor would not start the room ($kind)" $r.Out }
            Step 'start' 'done' $startSays
        }
    }

    # -- attach, under the same name, and the runner rows still there.
    function Get-LiveNow {
        try {
            $live = Invoke-RestMethod -Uri "http://$HubAddr/_hub/rooms" -TimeoutSec 5
            $live.rooms | Where-Object { $_.name -eq $Name -and ([datetime] $_.since) -ge $needSince } | Select-Object -First 1
        } catch { $null }
    }
    $deadline = (Get-Date).AddSeconds($AttachTimeout)
    $seen = $null
    do {
        $seen = Get-LiveNow
        if ($seen) { break }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    if ($seen) {
        Start-Sleep -Seconds 5
        $still = Get-LiveNow
        if (-not $still -or $still.since -ne $seen.since) { $seen = $null }
    }
    if (-not $seen) { Fail 'attach' 4 "the hub has no lasting connection from $Name after ${AttachTimeout}s. $(Get-HubLog)" @(Get-HubRoom $Name) }
    $rowsNote = 'runner rows not compared'
    if ($null -ne $rowsBefore) {
        $rowsAfter = $null
        try {
            $hr = Invoke-RestMethod -Uri "http://$HubAddr/v1/harnesses" -Headers $hdr -TimeoutSec 10
            $rowsAfter = @(@($hr) + @($hr.harnesses) | Where-Object { $_ -and $_.id } | ForEach-Object { "$($_.id)" } | Sort-Object)
        } catch { }
        $lost = @($rowsBefore | Where-Object { $rowsAfter -notcontains $_ })
        if ($null -eq $rowsAfter) { $rowsNote = 'runner rows could not be read after the restart' }
        elseif ($lost) { Fail 'attach' 4 "$Name is back, but the runner rows $($lost -join ', ') are gone" }
        else { $rowsNote = "runner rows intact ($($rowsAfter -join ', '))" }
    }
    Step 'attach' 'ok' "$Name on the hub since $(([datetime] $seen.since).ToString('HH:mm:ss')), host $($seen.host), build $($seen.version). $rowsNote"
    # THE ONE WRITE, and only after a restart that worked: the state directory
    # this resolved, for a manifest that did not have it.
    if (-not $manifest.statedir) {
        $manifest | Add-Member -NotePropertyName statedir -NotePropertyValue $pin.Dir -Force
        Save-Manifest
    }
    Finish 0
}

$freshManifest = -not $manifest
if (-not $manifest) {
    $pre = [ordered]@{}
    foreach ($k in $preKeys) { $pre[$k] = if ($k -eq 'service') { [bool] $state.service } else { $state.$k -eq 'True' } }
    $manifest = [pscustomobject]@{
        name = $Name; hub = $hubId; transport = $transport; autostart = $false
        installed = @(); pre = [pscustomobject] $pre
    }
    Step 'state' 'ok' "fresh, will be room $Name"
} else {
    foreach ($f in 'autostart', 'installed', 'transport') {
        if ($null -eq $manifest.$f) {
            $v = switch ($f) { 'autostart' { $true } 'installed' { @() } 'transport' { $transport } }
            $manifest | Add-Member -NotePropertyName $f -NotePropertyValue $v
        }
    }
    Step 'state' 'ok' "provisioned before as $($manifest.name)"
}
$manifest.hub = $hubId
$manifest.transport = $transport
# Autostart, once installed, stays until -Remove.
#
# THE DEFAULT APPLIES TO A NEW PROVISION ONLY. A machine with a manifest keeps the mode the manifest records, so a
# rerun on a room that was provisioned without autostart does not start registering one. -Autostart on such a rerun
# still means what it says.
$hadAutostartBefore = [bool] $manifest.autostart
$useAutostart = $Autostart -or $hadAutostartBefore -or ($freshManifest -and -not $NoAutostart)
$manifest.autostart = $useAutostart
Save-Manifest

# ── 5. the binary ───────────────────────────────────────────────────────────

$ext = if ($os -eq 'windows') { '.exe' } else { '' }
New-Item -ItemType Directory -Force -Path $work | Out-Null

# Get-Release fetches the release archive for the remote, checks it against
# the release's own checksums.txt, and returns the binary inside it.
function Get-Release {
    $api = if ($Version) { "https://api.github.com/repos/dovholuknf/atrium/releases/tags/$Version" }
           else { 'https://api.github.com/repos/dovholuknf/atrium/releases/latest' }
    try {
        $rel = Invoke-RestMethod -Uri $api -Headers @{ 'User-Agent' = 'atrium-provision' } -TimeoutSec 30
    } catch {
        $code = $_.Exception.Response.StatusCode.value__
        if ($code -eq 404) {
            # NO RELEASE, AND A CHECKOUT AROUND THE SCRIPT: build from it, so the
            # one command needs no flags. A named -Version is never swapped for it.
            if ($inCheckout -and -not $Version) {
                Step 'fetch' 'warn' 'dovholuknf/atrium has no release on GitHub, so this builds atrium from the checkout the script is in'
                return $null
            }
            $which = if ($Version) { "no release called $Version" } else { 'no release yet' }
            Fail 'fetch' 1 "dovholuknf/atrium has $which on GitHub. -FromCheckout builds the binary from this checkout instead"
        }
        Fail 'fetch' 1 "could not ask GitHub for the release: $($_.Exception.Message)"
    }
    $v = $rel.tag_name
    $base = "atrium_${v}_${os}_$goarch"
    $file = if ($os -eq 'windows') { "$base.zip" } else { "$base.tar.gz" }
    $asset = $rel.assets | Where-Object name -eq $file | Select-Object -First 1
    $sums = $rel.assets | Where-Object name -eq 'checksums.txt' | Select-Object -First 1
    if (-not $asset) { Fail 'fetch' 1 "release $v has no $file for this machine" }
    if (-not $sums) { Fail 'fetch' 1 "release $v has no checksums.txt, so $file cannot be checked" }
    $dir = Join-Path $work "release/$v"
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    $archive = Join-Path $dir $file
    Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $archive -UseBasicParsing
    $sumText = (Invoke-WebRequest -Uri $sums.browser_download_url -UseBasicParsing).Content
    if ($sumText -is [byte[]]) { $sumText = [Text.Encoding]::UTF8.GetString($sumText) }
    $want = ($sumText -split "`n" | Where-Object { $_ -match "\s\*?$([regex]::Escape($file))\s*$" } |
        ForEach-Object { ($_ -split '\s+')[0] } | Select-Object -First 1)
    $got = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLower()
    if (-not $want -or $want.ToLower() -ne $got) { Fail 'fetch' 1 "$file does not match the release's checksums.txt" }
    $out = Join-Path $dir $base
    if (Test-Path $out) { Remove-Item -Recurse -Force $out }
    if ($os -eq 'windows') { Expand-Archive -LiteralPath $archive -DestinationPath $dir -Force }
    else { & tar -xzf $archive -C $dir; if ($LASTEXITCODE -ne 0) { Fail 'fetch' 1 "could not unpack $file" } }
    $bin = Join-Path $out "atrium$ext"
    if (-not (Test-Path $bin)) { Fail 'fetch' 1 "$file has no atrium$ext in it" }
    Step 'fetch' 'ok' "release $v, $file, checksum matches"
    $bin
}

function Build-Checkout {
    if (-not $inCheckout) { Fail 'build' 1 '-FromCheckout needs an atrium checkout around this script' }
    $outDir = Join-Path $work "${os}_$goarch"
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $Binary = Join-Path $outDir "atrium$ext"
    $ver = (git -C $checkout describe --tags --exact-match 2>$null)
    if (-not $ver) { $ver = 'dev' }
    $commit = (git -C $checkout rev-parse HEAD 2>$null)
    # Tracked files only, as the Makefile does. See `Tree` in internal/cli/version.go.
    $tree = if (git -C $checkout status --porcelain --untracked-files=no 2>$null) { 'modified' } else { 'clean' }
    $env:CGO_ENABLED = '0'; $env:GOOS = $os; $env:GOARCH = $goarch
    try {
        $b = & go -C $checkout build -trimpath -ldflags "-s -w -X github.com/dovholuknf/atrium/internal/cli.Version=$ver -X github.com/dovholuknf/atrium/internal/cli.Commit=$commit -X github.com/dovholuknf/atrium/internal/cli.Tree=$tree" -o $Binary ./cmd/atrium 2>&1
        $bc = $LASTEXITCODE
    } finally {
        Remove-Item Env:CGO_ENABLED, Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
    }
    if ($bc -ne 0) { Fail 'build' 1 "go build for $os/$goarch failed" $b }
    $Binary
}

$built = $false
if ($Binary) {
    if (-not (Test-Path -LiteralPath $Binary)) { Fail 'build' 1 "no binary at $Binary" }
} elseif ($FromCheckout) {
    $Binary = Build-Checkout; $built = $true
} else {
    $Binary = Get-Release
    if (-not $Binary) { $Binary = Build-Checkout; $built = $true }
}
$sha = (Get-FileHash -LiteralPath $Binary -Algorithm SHA256).Hash.ToLower()
if ($built -or $PSBoundParameters.ContainsKey('Binary')) { Step 'build' 'ok' "$os/$goarch $($sha.Substring(0, 12))" }

$binChanged = $false
if ($state.binsha -eq $sha) {
    Step 'binary' 'ok' 'same build already there'
} else {
    $remoteNew = if ($os -eq 'windows') { '.atrium/bin/atrium.exe.new' } else { '.local/bin/atrium.new' }
    $c = Copy-ToRemote $Binary $remoteNew
    if ($c.Code -ne 0) { Fail 'binary' 3 'scp failed' $c.Out }
    # A RUNNING ROOM IS WOUND DOWN FIRST, so the new build is what runs next.
    # On Windows a running exe cannot be replaced at all. Step 7 starts it.
    $swap = if ($os -eq 'windows') {
@'
if (Test-Path $Bin) {
    try { & $Bin stop --url $StopUrl 2>&1 | Out-Null } catch {}
    $null = Sch /End /TN atrium
    $deadline = (Get-Date).AddSeconds(20)
    while ((Get-Process atrium -ErrorAction SilentlyContinue | Where-Object Path -eq $Bin) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 500 }
    Get-Process atrium -ErrorAction SilentlyContinue | Where-Object Path -eq $Bin | Stop-Process -Force
    Start-Sleep -Milliseconds 500
}
Move-Item -Force "$Bin.new" $Bin
& $Bin version
'@
    } else {
@'
if [ -x "$Bin" ]; then
  "$Bin" stop --url $StopUrl >/dev/null 2>&1 || true
  n=0; while pgrep -f "$Bin room" >/dev/null 2>&1 && [ $n -lt 40 ]; do sleep 0.5; n=$((n+1)); done
  pkill -9 -f "$Bin room" 2>/dev/null || true
fi
chmod +x "$Bin.new" && mv -f "$Bin.new" "$Bin" && "$Bin" version
'@
    }
    $r = Invoke-Remote $swap
    if ($r.Code -ne 0) { Fail 'binary' 3 'could not put the binary in place' $r.Out }
    $binChanged = $true
    $where = if ($os -eq 'windows') { '~\.atrium\bin\atrium.exe' } else { '~/.local/bin/atrium' }
    Step 'binary' 'done' "$where, $((($r.Out | Select-Object -First 1) -replace '\s+', ' ').Trim())"
}

# ── 6. join this hub ────────────────────────────────────────────────────────

if ($state.joinedroom -eq $Name -and $joinedId -eq $hubId) {
    Step 'join' 'ok' "already joined as $Name over $transport"
} else {
    # THE OVERLAY'S CREDENTIAL FIRST, before anything is minted, because it is
    # the one thing this script cannot make.
    $jwt = $null
    if ($transport -eq 'ziti') {
        if ($ZitiJwt) {
            if (-not (Test-Path -LiteralPath $ZitiJwt)) { Fail 'overlay' 7 "no JWT at $ZitiJwt" }
            $jwt = (Get-Content -LiteralPath $ZitiJwt -Raw).Trim()
            $from = $ZitiJwt
        } elseif ($ZitiJwtCommand) {
            $cmd = $ZitiJwtCommand.Replace('{name}', $Name)
            $o = & pwsh -NoProfile -NonInteractive -Command $cmd 2>&1
            $jwt = $o | ForEach-Object { "$_".Trim() } |
                Where-Object { $_ -match '^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$' } | Select-Object -Last 1
            if (-not $jwt) { Fail 'overlay' 7 'the -ZitiJwtCommand printed no JWT' $o }
            $from = 'the -ZitiJwtCommand'
        } else {
            Fail 'overlay' 7 ("the hub links over ziti, so $Name needs its own identity on your network. " +
                "issue an enrollment JWT for it, for example ``ziti edge create identity $Name -a atrium-rooms -o $Name.jwt``, " +
                "then pass -ZitiJwt $Name.jwt, or name that command with -ZitiJwtCommand")
        }
        Step 'overlay' 'ok' "an enrollment JWT for $Name from $from"
    }
    if ($transport -eq 'zrok') {
        if ($state.zrokenv -ne 'True') {
            Fail 'overlay' 7 ("the hub links over zrok, and $Target has no zrok environment. " +
                "zrok is an account before it is a share, and enabling one takes your account token, which this does not carry. " +
                "run it there yourself: ssh $Target zrok2 enable <your account token>")
        }
        Step 'overlay' 'ok' "$Target has a zrok environment"
    }

    # A NAME ALREADY IN USE ON THE HUB is reused only when nothing ever joined
    # with it. One that has connected belongs to some machine, and a second
    # machine under the same name would be two rooms the hub thinks are one.
    $line = Get-HubRoom $Name
    if ($line -and $line -notmatch 'never connected') {
        Fail 'join' 4 "the hub already has a room called $Name that has connected before. pass -Name, or remove it: atrium rooms rm $Name --force" @($line)
    }
    $mintFlags = @('--link', $Link)
    if ($LinkAdvertise) { $mintFlags += @('--link-advertise', $LinkAdvertise) }
    if ($transport -eq 'ziti') { $mintFlags += @('--service', $zitiService) }
    if (-not $line) {
        $a = Invoke-Hub (@('rooms', 'add', $Name, '--transport', $transport) + $mintFlags)
        if ($a.Code -ne 0) { Fail 'join' 4 "the hub would not add $Name" $a.Out }
    }
    # `rooms token` prints the string alone, so it is what is read. The one
    # `rooms add` printed is retired by it.
    $t = Invoke-Hub (@('rooms', 'token', $Name) + $mintFlags)
    $token = ($t.Out | Where-Object { $_.Trim() } | Select-Object -Last 1).Trim()
    if ($t.Code -ne 0 -or -not $token) { Fail 'join' 4 "the hub would not mint a join string for $Name" $t.Out }

    # THE JWT TRAVELS AS A FILE, never as an argument. On Unix it goes on the
    # script's stdin into a 0600 file; on Windows it is copied with scp. The
    # join reads it, enrolls it with a key made on the remote, and it is
    # deleted there whatever happened.
    if ($os -eq 'windows') {
        $zflag = ''
        if ($jwt) {
            $tmp = Join-Path ([IO.Path]::GetTempPath()) ("atrium-" + [guid]::NewGuid().ToString('N') + '.jwt')
            try {
                [IO.File]::WriteAllText($tmp, $jwt)
                $c = Copy-ToRemote $tmp '.atrium/provision/enroll.jwt'
            } finally { Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue }
            if ($c.Code -ne 0) { Fail 'join' 4 'could not copy the JWT over' $c.Out }
            $zflag = " --openziti (Join-Path `$P 'enroll.jwt')"
        }
        # CONTINUE, because Windows PowerShell with Stop turns the first line
        # a native command writes to stderr, here the join's own log, into a
        # terminating error.
        $js = "`$ErrorActionPreference = 'Continue'`n" +
              "`$rc = 1`n" +
              "try { & `$Bin room join $(Quote-Ps $token)$zflag --no-run 2>&1 | ForEach-Object { `"`$_`" }; `$rc = `$LASTEXITCODE } " +
              "finally { Remove-Item (Join-Path `$P 'enroll.jwt') -Force -ErrorAction SilentlyContinue }`n" +
              # THE BINARY'S OWN EXIT CODE. Windows PowerShell would otherwise exit 1
              # for any stderr line, which is where the join logs.
              "exit `$rc"
    } else {
        $js = ''
        $zflag = ''
        if ($jwt) {
            $js = "( umask 077; cat > `"`$P/enroll.jwt`" <<'EOF'`n$jwt`nEOF`n)`n"
            $zflag = " --openziti `"`$P/enroll.jwt`""
        }
        $js += "`"`$Bin`" room join $(Quote-Sh $token)$zflag --no-run 2>&1`nrc=`$?`nrm -f `"`$P/enroll.jwt`"`nexit `$rc"
    }
    $j = Invoke-Remote $js
    $token = $null
    $jwt = $null
    if ($j.Code -ne 0) { Fail 'join' 4 "the remote could not join as $Name" $j.Out }
    $detail = "joined as $Name over $transport"
    if ($transport -eq 'direct') { $detail += " to $LinkAdvertise" }
    if ($transport -eq 'ziti') { $detail += ", identity enrolled on the remote" }
    Step 'join' 'done' $detail
}
# WHERE THE JOIN WROTE room.json, kept for -Restart (see Get-StateDirs).
Set-ManifestStateDir

# THE ROOM'S git_root, so its clones are looked for under the shared folder. Before any start: `room set` refuses a database
# a running room holds, and makes the database when the room has never run. A refusal is a warn with the command to run,
# since the clones still work from the shared folder through room-git.ps1 -GitRoot.
# A RUNNING ROOM CANNOT BE SET, so a rerun (the state read found the room up) READS the settings it serves instead and says
# `ok` when they are what they should be. git_root is the one the room does not serve, so it is compared with what the
# manifest recorded the first run set. A difference is a warn carrying the command to run once the room is stopped.
# THE ROOM'S scm_root, so a pasted link has somewhere to clone to and the hub's placement never finds a room that
# cannot. The shared folder or the work root's git folder when there is one, else ~/git. Set always: a room with none
# answers no_scm_root and is passed over. The room also falls back to ~/git by itself when that folder exists, so this is
# the record of the choice. A work root also sets reviews_root and context_handoff_dir, so nothing else lands on C:.
$scm = if ($sharedDir) { $sharedDir } else { '~/git' }
$want = [ordered]@{}
if ($sharedDir) { $want['git_root'] = $sharedDir }
$want['scm_root'] = $scm
if ($workRoot) { $want['reviews_root'] = (Format-WorkPath $workRoot) + '/reviews'; $want['context_handoff_dir'] = (Format-WorkPath $workRoot) + '/handoff' }
$served = @{ scm_root = 'git_scm_root'; reviews_root = 'reviews_root'; context_handoff_dir = 'context_handoff_dir' }
$stepOf = @{ git_root = 'git-root'; scm_root = 'scm-root'; reviews_root = 'reviews-root'; context_handoff_dir = 'handoff-dir' }
$have = $null; $setNow = @()
# WITH A WORK ROOT the room's own `atrium room setup --apply` does the lot, here, after the join and before any start: it makes the
# root and its folders, points the tool caches at it, sets the four settings above (a running room cannot be set, and that
# is a warn row carrying the command), and installs the agent pack. The pack's source is THIS machine's clone of the hub's
# mirror, sent as a tarball, because the room's /git/hub forwarder is tokenized per card and no card exists yet.
$packSent = $false; $packCommit = $null; $script:packRunners = $null
if ($workRoot -or -not $NoAgentPack) {
    if (-not $NoAgentPack) {
        $pk = New-PackSource -HubAddr $HubAddr -Repo $AgentPackRepo -Branch $AgentPackBranch -OutDir $work
        if (-not $pk.Ok) { Step 'agent-pack' 'warn' "$($pk.Why). the hub mirrors $AgentPackRepo only when it is watched there. rerun when it is" }
        else {
            $c = Copy-ToRemote $pk.Tgz '.atrium/provision/pack-src.tgz'
            if ($c.Code -ne 0) { Step 'agent-pack' 'warn' "could not copy the pack to $remoteHost ($(($c.Out | Select-Object -Last 1)))" }
            else {
                $u = Invoke-Remote (Get-PackUnpackScript $os)
                if ($u.Code -ne 0) { Step 'agent-pack' 'warn' "$remoteHost could not unpack the pack ($(($u.Out | Select-Object -Last 1)))" }
                else { $packSent = $true; $packCommit = $pk.Commit; $script:packRunners = @($pk.Runners) }
            }
        }
    }
    $ap = Invoke-RoomSetup 'apply' -UsePackDir:$packSent
    switch ($ap.Kind) {
        'ok' { Show-SetupRows $ap }
        default {
            if ($packSent) { Invoke-Remote $(if ($os -eq 'windows') { 'Remove-Item -LiteralPath (Join-Path $HOME ''.atrium\provision\pack-src''), (Join-Path $HOME ''.atrium\provision\pack-src.tgz'') -Recurse -Force -ErrorAction SilentlyContinue' } else { 'rm -rf "$HOME/.atrium/provision/pack-src" "$HOME/.atrium/provision/pack-src.tgz"' }) | Out-Null }
            if ($workRoot) { Step 'work-root' 'fail' "atrium room setup did not answer on ${remoteHost}: $(($ap.Other | Select-Object -First 3) -join ' | ')"; Finish 3 }
            Step 'agent-pack' 'warn' "atrium room setup did not answer on ${remoteHost}, so the agent pack was not installed: $(($ap.Other | Select-Object -First 3) -join ' | ')"
        }
    }
    foreach ($k in $want.Keys) { if (@($ap.Rows | Where-Object { $_.Step -eq $stepOf[$k] -and $_.Status -eq 'done' }).Count) { $setNow += $k } }
    # recorded once the pack is really there, and a run that was told -NoAgentPack records that as its own choice
    if ($packCommit -and @($ap.Rows | Where-Object { $_.Step -eq 'agent-pack' -and $_.Status -in 'ok', 'done' }).Count) { $manifest | Add-Member -NotePropertyName agentpack -NotePropertyValue ([pscustomobject]@{ repo = $AgentPackRepo; branch = $AgentPackBranch; commit = $packCommit; runners = @($script:packRunners) }) -Force }
    if ($ap.Kind -eq 'ok' -and $ap.Code -ne 0) {
        # an administrator's lines were printed. The root is recorded so the rerun after they ran is the same run
        if ($workRoot) { $manifest | Add-Member -NotePropertyName workroot -NotePropertyValue $workRoot -Force }
        Save-Manifest
        Finish $ap.Code
    }
}
if ($state.up7781 -eq 'True' -and -not $workRoot) {
    $rq = if ($os -eq 'windows') {
@'
try { $j = Invoke-RestMethod 'http://127.0.0.1:7781/v1/settings' -TimeoutSec 5
  "git_scm_root=$($j.git_scm_root)"; "reviews_root=$($j.reviews_root)"; "context_handoff_dir=$($j.context_handoff_dir)"; 'read=ok' } catch { 'read=fail' }
'@
    } else {
@'
j=$(curl -fsS -m 5 http://127.0.0.1:7781/v1/settings 2>/dev/null) || { echo read=fail; exit 0; }
for k in git_scm_root reviews_root context_handoff_dir; do echo "$k=$(printf '%s' "$j" | sed -n 's/.*"'"$k"'": *"\([^"]*\)".*/\1/p' | head -1)"; done
echo read=ok
'@
    }
    $have = ConvertFrom-KeyValue (Invoke-Remote $rq).Out
}
foreach ($k in @(if (-not $workRoot) { $want.Keys })) {
    $v = $want[$k]
    $rcmd = "$(if ($os -eq 'windows') { '.\.atrium\bin\atrium.exe' } else { '~/.local/bin/atrium' }) room set $k $v"
    $stop = "stop the room and run: $((@($Ssh) + $SshOption + @($Target)) -join ' ') $rcmd"
    if ($state.up7781 -eq 'True') {
        if ($k -eq 'git_root') {
            if ($manifest -and $manifest.settings -and "$($manifest.settings.git_root)" -and (Format-WorkPath "$($manifest.settings.git_root)") -ieq (Format-WorkPath $v)) { Step $stepOf[$k] 'ok' "the room's git_root is $v (as the first run recorded it)" }
            elseif ($manifest -and $manifest.settings -and "$($manifest.settings.git_root)") { Step $stepOf[$k] 'warn' "the room's git_root was set to $($manifest.settings.git_root), not $v. $stop" }
            else { Step $stepOf[$k] 'warn' "the room is running, so git_root cannot be read or set. $stop" }
            continue
        }
        if ($have.read -ne 'ok') { Step $stepOf[$k] 'warn' "the room is running and its settings could not be read, so $k was not checked. $stop"; continue }
        $cur = "$($have[$served[$k]])"
        $same = if ($v -eq '~/git') { $true } else { (Format-WorkPath $cur) -ieq (Format-WorkPath $v) }
        if ($same) { Step $stepOf[$k] 'ok' "the room's $k is $v" } else { Step $stepOf[$k] 'warn' "the room's $k is '$cur', not $v. $stop" }
        continue
    }
    $sr = if ($os -eq 'windows') {
        "& `$Bin room set $k $(Quote-Ps $v) 2>&1 | ForEach-Object { `"`$_`" }`nexit `$LASTEXITCODE"
    } else {
        "`"`$Bin`" room set $k $(Quote-Sh $v) 2>&1`nexit `$?"
    }
    $sx = Invoke-Remote $sr
    if ($sx.Code -eq 0) { $setNow += $k; Step $stepOf[$k] 'done' "the room's $k is $v" }
    else { Step $stepOf[$k] 'warn' "could not set the room's $k to $v ($(($sx.Out | Select-Object -Last 1))). $stop" }
}
if ($workRoot -or $sharedDir -or $packCommit -or $NoAgentPack) {
    $rec = [ordered]@{}
    if ($manifest.settings) { foreach ($p in $manifest.settings.PSObject.Properties) { $rec[$p.Name] = $p.Value } }
    foreach ($k in $setNow) { $rec[$k] = $want[$k] }
    $manifest | Add-Member -NotePropertyName settings -NotePropertyValue ([pscustomobject]$rec) -Force
    if ($workRoot) { $manifest | Add-Member -NotePropertyName workroot -NotePropertyValue $workRoot -Force }
    if ($NoAgentPack) { $manifest | Add-Member -NotePropertyName agentpack -NotePropertyValue ([pscustomobject]@{ none = $true }) -Force }
    Save-Manifest
}

# ── 7. install the runners asked for ────────────────────────────────────────

# Where each runner comes from: its vendor's own published installer or
# release, nothing else. A runner not listed here is not installed by this.
$triple = switch ("$os/$goarch") {
    'windows/amd64' { 'x86_64-pc-windows-msvc' }  'windows/arm64' { 'aarch64-pc-windows-msvc' }
    'linux/amd64'   { 'x86_64-unknown-linux-musl' } 'linux/arm64' { 'aarch64-unknown-linux-musl' }
    'darwin/amd64'  { 'x86_64-apple-darwin' }     'darwin/arm64'  { 'aarch64-apple-darwin' }
}
$sources = @{
    claude = if ($os -eq 'windows') { 'https://claude.ai/install.ps1' } else { 'https://claude.ai/install.sh' }
    # THE PACKAGE, NOT THE BARE BINARY. codex-<triple> is the one executable, and
    # codex needs codex-code-mode-host beside it, plus codex-resources and
    # codex-path. codex-package-<triple>.tar.gz holds all of them in the layout
    # codex looks for (bin/, codex-resources/, codex-path/).
    codex  = "https://github.com/openai/codex/releases/latest/download/codex-package-$triple.tar.gz"
}
function Get-InstallScript {
    param([string] $runner)
    $url = $sources[$runner]
    if ($os -eq 'windows') {
        switch ($runner) {
            'claude' { return @"
`$b = Join-Path `$HOME '.local\bin\claude.exe'; `$s = Join-Path `$HOME '.local\share\claude'
`$preb = Test-Path `$b; `$pres = Test-Path `$s
`$o = powershell -NoProfile -ExecutionPolicy Bypass -Command "```$ProgressPreference='SilentlyContinue'; irm $url | iex" *>&1
if (-not (Test-Path `$b)) { `$o; exit 1 }
if (-not `$preb) { "installed=`$b" }; if (-not `$pres -and (Test-Path `$s)) { "installed=`$s" }
"path=`$b"
"@ }
            # CODEX NEEDS ITS HELPERS BESIDE IT (see $sources). With node and npm
            # on the PATH, npm's own package puts them where they belong, in the
            # user's own folder (the prefix is ~\.local\bin, so the shims land on
            # the PATH this script already manages). Without, the whole package
            # goes to ~\.local\share\codex\<version> and codex.cmd in ~\.local\bin
            # calls into it. A .cmd and not a copy of codex.exe, because codex
            # finds its resources relative to where its own exe really is.
            'codex' { return ((@'
$ErrorActionPreference = 'Continue'; $ProgressPreference = 'SilentlyContinue'
$d = Join-Path $HOME '.local\bin'; New-Item -ItemType Directory -Force $d | Out-Null
$b = Join-Path $d 'codex.cmd'; $preb = Test-Path $b
$shims = @('codex', 'codex.cmd', 'codex.ps1') | ForEach-Object { Join-Path $d $_ }
$pre = @{}; foreach ($s in $shims) { $pre[$s] = Test-Path $s }
if ((Get-Command node -ErrorAction SilentlyContinue) -and (Get-Command npm -ErrorAction SilentlyContinue)) {
    $nm = Join-Path $d 'node_modules'; $prem = Test-Path $nm
    $o = & npm install -g --prefix $d '@openai/codex' 2>&1 | Out-String
    if (-not (Test-Path $b)) { $o; 'npm finished but codex.cmd is not there'; exit 1 }
    foreach ($s in $shims) { if (-not $pre[$s] -and (Test-Path $s)) { "installed=$s" } }
    if (-not $prem) { "installed=$nm" } else { "installed=$(Join-Path $nm '@openai\codex')" }
    "path=$b"; 'via=npm'; exit 0
}
$share = Join-Path $HOME '.local\share\codex'; $pres = Test-Path $share
$t = Join-Path $env:TEMP ('codex-' + [guid]::NewGuid().ToString('N')); New-Item -ItemType Directory -Force $t | Out-Null
$tgz = "$t.tgz"
try {
    Invoke-WebRequest '__URL__' -OutFile $tgz -UseBasicParsing
    $o = & tar -xzf $tgz -C $t 2>&1 | Out-String
    if (-not (Test-Path (Join-Path $t 'bin\codex.exe'))) { $o; 'the package had no bin\codex.exe'; exit 1 }
    $v = try { (Get-Content (Join-Path $t 'codex-package.json') -Raw | ConvertFrom-Json).version } catch { $null }
    if (-not $v) { $v = 'latest' }
    New-Item -ItemType Directory -Force $share | Out-Null
    $dest = Join-Path $share $v; $predest = Test-Path $dest
    if (-not $predest) { Move-Item $t $dest }
} finally {
    Remove-Item $tgz -Force -ErrorAction SilentlyContinue
    if (Test-Path $t) { Remove-Item $t -Recurse -Force -ErrorAction SilentlyContinue }
}
$exe = Join-Path $dest 'bin\codex.exe'
Set-Content -Path $b -Encoding ASCII -Value @('@echo off', ('"' + $exe + '" %*'))
if (-not $preb) { "installed=$b" }
if (-not $pres) { "installed=$share" } elseif (-not $predest) { "installed=$dest" }
"path=$b"; 'via=package'
'@) -replace '__URL__', $url) }
        }
    } else {
        switch ($runner) {
            'claude' { return @"
b="`$HOME/.local/bin/claude"; s="`$HOME/.local/share/claude"
if [ -e "`$b" ]; then preb=1; else preb=0; fi; if [ -e "`$s" ]; then pres=1; else pres=0; fi
if command -v curl >/dev/null 2>&1; then o=`$(curl -fsSL '$url' | bash 2>&1); else o=`$(wget -qO- '$url' | bash 2>&1); fi
if [ ! -e "`$b" ]; then echo "`$o"; exit 1; fi
if [ `$preb = 0 ]; then echo "installed=`$b"; fi
if [ `$pres = 0 ] && [ -e "`$s" ]; then echo "installed=`$s"; fi
echo "path=`$b"
"@ }
            # CODEX NEEDS ITS HELPERS BESIDE IT (see $sources). With node and npm
            # on a login shell's PATH, npm installs @openai/codex into ~/.local,
            # the user's own prefix, never sudo and never the system one. Without,
            # the whole release package goes to ~/.local/share/codex/<version>
            # and ~/.local/bin/codex is a two-line wrapper that execs into it: a
            # wrapper and not a symlink, because codex finds its resources
            # relative to where its own executable really is. HOME comes from the
            # environment, so a test can point it at a throwaway folder.
            'codex' { return (@'
d="$HOME/.local/bin"; b="$d/codex"; mkdir -p "$d"
if [ -e "$b" ] || [ -L "$b" ]; then preb=1; else preb=0; fi
sh_=${SHELL:-/bin/sh}
if "$sh_" -lc 'command -v node && command -v npm' </dev/null >/dev/null 2>&1; then
  nm="$HOME/.local/lib/node_modules/@openai/codex"
  if [ -e "$nm" ]; then pren=1; else pren=0; fi
  o=$("$sh_" -lc 'npm install -g --prefix "$HOME/.local" @openai/codex' </dev/null 2>&1) || { echo "$o"; exit 1; }
  if [ ! -e "$b" ]; then echo "$o"; echo "npm finished but $b is not there"; exit 1; fi
  if [ $preb = 0 ]; then echo "installed=$b"; fi
  if [ $pren = 0 ]; then echo "installed=$nm"; fi
  echo "path=$b"; echo "via=npm"; exit 0
fi
share="$HOME/.local/share/codex"
if [ -e "$share" ]; then pres=1; else pres=0; fi
t=$(mktemp -d)
if command -v curl >/dev/null 2>&1; then curl -fsSL '__URL__' | tar -xz -C "$t" || { rm -rf "$t"; exit 1; }
else wget -qO- '__URL__' | tar -xz -C "$t" || { rm -rf "$t"; exit 1; }; fi
if [ ! -x "$t/bin/codex" ]; then echo "the package had no bin/codex"; rm -rf "$t"; exit 1; fi
v=$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' "$t/codex-package.json" 2>/dev/null | head -1)
[ -n "$v" ] || v=latest
mkdir -p "$share"
if [ -d "$share/$v" ]; then predest=1; rm -rf "$t"; else predest=0; chmod 755 "$t"; mv "$t" "$share/$v" || exit 1; fi
printf '#!/bin/sh\nexec "%s" "$@"\n' "$share/$v/bin/codex" > "$b.tmp" && chmod +x "$b.tmp" && mv -f "$b.tmp" "$b" || exit 1
if [ $preb = 0 ]; then echo "installed=$b"; fi
if [ $pres = 0 ]; then echo "installed=$share"; elif [ $predest = 0 ]; then echo "installed=$share/$v"; fi
echo "path=$b"; echo "via=package"
'@ -replace '__URL__', $url) }
        }
    }
    $null
}

# Test-Runner finds a runner the way the room will: on Windows the user's PATH,
# which a room started over ssh or by the logon task shares, and elsewhere a
# login shell's PATH.
function Test-Runner {
    param([string] $runner)
    $rs = if ($os -eq 'windows') {
@"
`$c = Get-Command '$runner' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not `$c) { `$c = Get-Command '$runner' -ErrorAction SilentlyContinue | Select-Object -First 1 }
if (-not `$c) {
    foreach (`$x in '.exe', '.cmd') {
        `$h = Join-Path `$HOME ".local\bin\$runner`$x"
        if (Test-Path `$h) { "home=`$h"; break }
    }
    'runner=missing'; exit 0
}
"path=`$(`$c.Source)"
`$job = Start-Job { param(`$n) & `$n --version 2>&1 } -ArgumentList `$c.Source
if (Wait-Job `$job -Timeout 30) { "version=`$((Receive-Job `$job | Select-Object -First 1))"; 'runner=ok' }
else { Stop-Job `$job; 'runner=hung' }
"@
    } else {
@"
sh_=`${SHELL:-/bin/sh}
out=`$("`$sh_" -lc 'command -v $runner' 2>/dev/null)
if [ -z "`$out" ]; then
  if [ -x "`$HOME/.local/bin/$runner" ]; then echo "home=`$HOME/.local/bin/$runner"; fi
  echo runner=missing; exit 0
fi
echo "path=`$out"
v=`$("`$sh_" -lc '$runner --version' </dev/null 2>&1)
rc=`$?
echo "version=`$(printf '%s\n' "`$v" | head -1)"
if [ "`$rc" = 0 ]; then echo runner=ok; else echo runner=broken; fi
"@
    }
    ConvertFrom-KeyValue (Invoke-Remote $rs).Out
}

$bad = 0
foreach ($runner in $Install) {
    if (-not $sources.ContainsKey($runner)) {
        Step "install:$runner" 'fail' "no vendor installer this script knows for $runner. install it yourself"
        $bad++; continue
    }
    $have = Test-Runner $runner
    if ($have.runner -eq 'ok') { Step "install:$runner" 'ok' "already there at $($have.path)"; continue }
    if ($have.home) { Step "install:$runner" 'ok' "already there at $($have.home)"; $installedNow = $true; continue }
    # THE TRUST WARNING, before anything is fetched. -Install is the consent,
    # and this says what it was consent to.
    $via = if ($runner -eq 'codex') { "(or, when node and npm are there, npm's @openai/codex into ~/.local) " } else { '' }
    Step "install:$runner" 'warn' ("trusting atrium to fetch $($sources[$runner]) $via" + "and run it on $Target. " +
        "if you do not trust that, install $runner yourself and leave -Install off")
    $r = Invoke-Remote (Get-InstallScript $runner)
    $got = @($r.Out | Where-Object { $_ -like 'installed=*' } | ForEach-Object { $_.Substring(10) })
    if ($got) {
        $script:manifest.installed = @(@($script:manifest.installed) + $got | Where-Object { $_ } | Select-Object -Unique)
        Save-Manifest
    }
    if ($r.Code -ne 0) { Fail "install:$runner" 5 "the $runner installer failed" $r.Out }
    Step "install:$runner" 'done' ((ConvertFrom-KeyValue $r.Out).path)
    $installedNow = $true
}

# ~/.local/bin ON THE USER'S PATH, when -Install put something there and it is
# not. Both installers land there, and a runner the room cannot find on PATH is
# not a runner the room can start. Windows gets it in the user's own Path in
# the registry, Unix gets one marked line in the login profile. No admin, and
# -Remove takes either back out. A room already running is restarted, because
# it read its PATH when it started.
$pathChanged = $false
if ($installedNow -and -not $script:manifest.pathadded) {
    $ps = if ($os -eq 'windows') {
@'
$d = Join-Path $HOME '.local\bin'
$cur = [Environment]::GetEnvironmentVariable('Path', 'User')
if (@($cur -split ';') -contains $d) { 'pathadded=' } else {
    $new = if ($cur) { "$cur;$d" } else { $d }
    [Environment]::SetEnvironmentVariable('Path', $new, 'User')
    'pathadded=registry'
}
'@
    } else {
@'
d="$HOME/.local/bin"
lp=$("${SHELL:-/bin/sh}" -lc 'printf %s "$PATH"' 2>/dev/null)
case ":$lp:" in *":$d:"*) echo pathadded=; exit 0;; esac
case "${SHELL##*/}" in
  zsh) f="$HOME/.zprofile";;
  bash) if [ -f "$HOME/.bash_profile" ]; then f="$HOME/.bash_profile"; else f="$HOME/.profile"; fi;;
  *) f="$HOME/.profile";;
esac
printf '\n%s\n' 'export PATH="$HOME/.local/bin:$PATH" # added by atrium provision-room' >> "$f"
echo "pathadded=$f"
'@
    }
    $r = Invoke-Remote $ps
    $kv = ConvertFrom-KeyValue $r.Out
    if ($r.Code -ne 0) { Fail 'path' 5 'could not put ~/.local/bin on PATH' $r.Out }
    if ($kv.pathadded) {
        $script:manifest | Add-Member -NotePropertyName pathadded -NotePropertyValue $kv.pathadded -Force
        Save-Manifest
        $where = if ($kv.pathadded -eq 'registry') { "the user's Path" } else { $kv.pathadded }
        Step 'path' 'done' "~/.local/bin added to $where"
        $pathChanged = $true
    } else {
        Step 'path' 'ok' '~/.local/bin is already on PATH'
    }
}
# A ROOM STARTED WITH --detach IS STOPPED before autostart takes over, or the
# service's room would find the ports taken and exit.
if (($pathChanged -or ($useAutostart -and -not $hadAutostartBefore)) -and -not $binChanged) {
    $stop = if ($os -eq 'windows') {
        "`$ErrorActionPreference = 'Continue'; if (Test-Path `$Bin) { & `$Bin stop --url `$StopUrl 2>&1 | Out-Null }; Start-Sleep -Seconds 3"
    } else {
        "if [ -x `"`$Bin`" ]; then `"`$Bin`" stop --url `$StopUrl >/dev/null 2>&1; sleep 3; fi"
    }
    $null = Invoke-Remote $stop
}

# ── 8. run it: in the background, or through autostart ───────────────────────

$startedAt = Get-Date
$startedNow = $false
if (-not $useAutostart) {
    # THROUGH A LOGIN SHELL ON UNIX, so the room gets the PATH a person's
    # terminal has rather than the bare one a non-interactive ssh command gets,
    # and so finds the same runners the runner check finds. ON WINDOWS the
    # toolchain's room-env.ps1 goes first when room-toolchain.ps1 wrote one,
    # since the room and its runners inherit this PATH and the machine Path
    # alone can put a Cygwin git ahead of Git for Windows.
    $ds = if ($os -eq 'windows') { "`$ErrorActionPreference = 'Continue'`n`$e = Join-Path `$HOME '.atrium\toolchain\room-env.ps1'; if (Test-Path `$e) { . `$e }`n& `$Bin room --detach 2>&1`nexit `$LASTEXITCODE" }
          else { "`"`${SHELL:-/bin/sh}`" -lc 'exec `"`$0`" room --detach' `"`$Bin`" 2>&1" }
    $r = Invoke-Remote $ds
    if ($r.Code -ne 0) { Fail 'start' 3 'the room would not start' $r.Out }
    $said = ($r.Out -join ' ')
    if ($said -match 'already answers') { $startWord = 'ok'; Step 'start' 'ok' 'already running, no autostart' }
    else { $startWord = 'done'; Step 'start' 'done' 'in the background with room --detach, no autostart. it stops at restart or logout' }
} else {
    # THE SERVICE SCRIPTS GO OVER FIRST, with LF endings for Unix whatever this
    # checkout has, because a shell script with a carriage return on every line
    # does not run.
    $files = if ($os -eq 'windows') {
        @(@('scripts/atrium-service.ps1', 'scripts'), @('scripts/atrium-autostart.ps1', 'scripts'))
    } else {
        @(@('scripts/atrium-service.sh', 'scripts'), @('packaging/atrium.service', 'packaging'),
          @('packaging/atrium.plist', 'packaging'))
    }
    $stage = Join-Path $work "${os}_$goarch/files"
    New-Item -ItemType Directory -Force -Path $stage | Out-Null
    $mk = if ($os -eq 'windows') { "New-Item -ItemType Directory -Force -Path (Join-Path `$P 'scripts') | Out-Null" }
          else { "mkdir -p `"`$P/scripts`" `"`$P/packaging`"" }
    $null = Invoke-Remote $mk
    foreach ($f in $files) {
        $src = Join-Path $PSScriptRoot "../$($f[0])"
        if (-not (Test-Path $src)) { Fail 'autostart' 3 "$($f[0]) is not beside this script" }
        $dst = Join-Path $stage (Split-Path -Leaf $f[0])
        $text = [IO.File]::ReadAllText($src)
        if ($os -ne 'windows') { $text = $text -replace "`r`n", "`n" }
        [IO.File]::WriteAllText($dst, $text)
        $c = Copy-ToRemote $dst ".atrium/provision/$($f[1])/$(Split-Path -Leaf $f[0])"
        if ($c.Code -ne 0) { Fail 'autostart' 3 "scp of $($f[0]) failed" $c.Out }
    }

    if ($os -eq 'windows') {
        $as = Get-WindowsAutostartScript
    } else {
        $as = "LINGER=$(if ($Linger) { '1' } else { '' })`n" + @'
S="$P/scripts/atrium-service.sh"
u="$HOME/.config/systemd/user/atrium.service"
p="$HOME/Library/LaunchAgents/io.github.dovholuknf.atrium.plist"
if { [ -f "$u" ] && grep -q "^ExecStart=$Bin room" "$u"; } || { [ -f "$p" ] && grep -q "$Bin\" room" "$p"; }; then
  echo autostart=ok
else
  o=$(ATRIUM_EXE="$Bin" ATRIUM_SERVICE_VERB=room ATRIUM_LINGER="$LINGER" bash "$S" install 2>&1) || { echo "$o"; exit 1; }
  echo autostart=done
  case "$o" in *"not loaded now"*) echo "start=warn the LaunchAgent loads at the next desktop login, there is no GUI session now";; esac
fi
if [ "$(uname -s)" = Linux ]; then
  if systemctl --user is-active --quiet atrium; then echo start=ok; else
    systemctl --user start atrium 2>&1 && echo start=done || echo "start=fail systemctl --user start atrium failed"
  fi
elif launchctl print "gui/$(id -u)/io.github.dovholuknf.atrium" >/dev/null 2>&1; then echo start=ok
elif curl -fs --max-time 3 http://127.0.0.1:7781/v1/health >/dev/null 2>&1; then
  echo "start=ok the room answers. the LaunchAgent loads at a desktop login, so a reboot with nobody logged in leaves it down until auto-login is on"
else
  last=$(/bin/zsh -l -c 'exec "$0" room --detach --db "$HOME/.atrium/atrium.db"' "$Bin" 2>&1 | tail -n 1)
  if curl -fs --max-time 3 http://127.0.0.1:7781/v1/health >/dev/null 2>&1; then
    echo "start=done started detached, since the LaunchAgent needs a desktop login. a reboot with nobody logged in leaves the room down until auto-login is on, and a later login loads the LaunchAgent, whose room finds the port taken and is restarted every 10s while this one runs"
  else echo "start=warn the LaunchAgent is not loaded (it needs a desktop login) and a detached start did not answer: $last"
  fi
fi
'@
    }
    $r = Invoke-Remote $as
    $kv = ConvertFrom-KeyValue $r.Out
    if ($r.Code -ne 0 -or -not $kv.autostart) { Fail 'autostart' 3 'the service install failed' $r.Out }
    Step 'autostart' $kv.autostart $(if ($os -eq 'windows') { 'logon task atrium, RunLevel Limited' } elseif ($os -eq 'linux') { 'systemd user unit atrium.service' } else { 'LaunchAgent io.github.dovholuknf.atrium' })
    $startStatus = ($kv.start -split ' ', 2)
    $startWord = $startStatus[0]
    # A start the logon task could not make and `room --detach` did (a warn) is still a room started NOW, so the attach
    # wait wants a connection made after it.
    if ($kv.started -eq '1') { $startedNow = $true }
    Step 'start' $startWord $(if ($startStatus.Count -gt 1) { $startStatus[1] } else { '' })
    if ($startWord -eq 'fail') { Finish 3 }
}

# ── 9. attached to the hub ──────────────────────────────────────────────────

# THE HUB'S OWN CONNECTION LIST, not `rooms ls`, which infers "attached" from
# the last twenty seconds and would still say so about the room that was just
# stopped for a new binary. A room started by this run has to show a
# connection made after it started, and still be there a few seconds later,
# which is what catches a room that died with the ssh session that started it.
$needSince = if ($startWord -eq 'done' -or $startedNow) { $startedAt } else { [datetime]::MinValue }
function Get-Live {
    try {
        $live = Invoke-RestMethod -Uri "http://$HubAddr/_hub/rooms" -TimeoutSec 5
        $live.rooms | Where-Object { $_.name -eq $Name -and ([datetime] $_.since) -ge $needSince } |
            Select-Object -First 1
    } catch { $null }
}
$deadline = (Get-Date).AddSeconds($AttachTimeout)
$seen = $null
do {
    $seen = Get-Live
    if ($seen) { break }
    Start-Sleep -Seconds 2
} while ((Get-Date) -lt $deadline)
if ($seen) {
    Start-Sleep -Seconds 5
    $still = Get-Live
    if (-not $still -or $still.since -ne $seen.since) { $seen = $null; $dropped = $true }
}
if (-not $seen) {
    $why = if ($dropped) { "$Name attached and then went away. it may have died with the ssh session: try -Autostart" }
           else { "the hub has no live connection from $Name after ${AttachTimeout}s. $(Get-HubLog)" }
    Fail 'attached' 4 $why @(Get-HubRoom $Name)
}
Step 'attached' 'ok' "$Name on the hub since $(([datetime] $seen.since).ToString('HH:mm:ss')), host $($seen.host), build $($seen.version)"

# ── 10. the runners ─────────────────────────────────────────────────────────

foreach ($runner in @($Runners + $Install | Select-Object -Unique)) {
    if ($runner -notmatch '^[A-Za-z0-9._-]+$') { Step "runner:$runner" 'fail' 'not a command name'; $bad++; continue }
    $kv = Test-Runner $runner
    switch ($kv.runner) {
        'ok'      { Step "runner:$runner" 'ok' "$($kv.version) at $($kv.path)" }
        'missing' {
            if ($kv.home) { Step "runner:$runner" 'fail' "installed at $($kv.home), but that folder is not on PATH" }
            else { Step "runner:$runner" 'fail' 'not on PATH. -Install fetches it from its vendor, if you trust that' }
            $bad++
        }
        'hung'    { Step "runner:$runner" 'fail' "found at $($kv.path), --version did not return in 30s"; $bad++ }
        default   { Step "runner:$runner" 'fail' "found at $($kv.path), --version failed"; $bad++ }
    }
}

# ── 11. atrium-control for the room's claude sessions ──────────────────────

# THE STDIO CONTROL SERVER, in an MCP config file the room's claude runner row
# names, so a claude session on this room can call atrium_say, atrium_report and
# atrium_peers, and answer a card on another room. The hub's own control MCP is
# loopback only and cannot be reached from here. See
# docs/fabric/cross-room-say-design.md.
#
# A FILE THE RUNNER ROW NAMES, NOT `claude mcp add --scope user`. The claude row
# passes --strict-mcp-config, so a launched session reads ONLY the servers an
# --mcp-config file lists, and a user-scope server is never loaded. Strict stays:
# the user scope holds servers that prompt for authentication, and a prompt
# blocks a supervised launch. This mirrors the hub machine's own row and
# ~/.atrium/mcp.json. A lean launch cuts the same file down, so it keeps
# atrium-control too.
#
# One registered by somebody else is left alone. Never a failure: the room works
# without it, its sessions just cannot answer.
if (@($Runners + $Install) -contains 'claude') {
    $ms = if ($os -eq 'windows') {
@'
"home=$HOME"
"bin=$Bin"
$f = Join-Path $A 'mcp.json'
if (Test-Path $f) { 'file=' + [Convert]::ToBase64String([IO.File]::ReadAllBytes($f)) }
'@
    } else {
@'
echo "home=$HOME"
echo "bin=$Bin"
[ -f "$A/mcp.json" ] && echo "file=$(base64 < "$A/mcp.json" | tr -d '\n')"
'@
    }
    $kv = ConvertFrom-KeyValue (Invoke-Remote $ms).Out
    $sep = if ($os -eq 'windows') { '\' } else { '/' }
    $mcpPath = ("$($kv.home)" + $sep + '.atrium' + $sep + 'mcp.json')
    if ($os -eq 'windows') { $mcpPath = $mcpPath -replace '\\', '/' }
    $want = [ordered]@{ type = 'stdio'; command = "$($kv.bin)"; args = @('control') }
    $doc = $null
    if ($kv.file) {
        try { $doc = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($kv.file)) | ConvertFrom-Json -AsHashtable }
        catch { $doc = $null }
    }
    $fileWord = $null
    if ($kv.file -and -not $doc) {
        $fileWord = 'bad'
    } else {
        if (-not $doc) { $doc = [ordered]@{} }
        if (-not $doc.Contains('mcpServers')) { $doc['mcpServers'] = [ordered]@{} }
        $have = $doc['mcpServers']['atrium-control']
        if ($have -and "$($have.command)" -eq "$($kv.bin)") { $fileWord = 'ok' }
        elseif ($have) { $fileWord = 'other' }
        else {
            if (-not $doc.Contains('_comment')) {
                $doc['_comment'] = 'The MCP servers an atrium-launched claude session gets. Named by an ABSOLUTE path ' +
                    'in the claude runner row, which passes --strict-mcp-config --mcp-config <this file>. Written by ' +
                    'scripts/provision-room.ps1.'
            }
            $doc['mcpServers']['atrium-control'] = $want
            $tmp = Join-Path $work 'mcp.json'
            New-Item -ItemType Directory -Force (Split-Path $tmp) | Out-Null
            [IO.File]::WriteAllText($tmp, ($doc | ConvertTo-Json -Depth 8))
            $c = Copy-ToRemote $tmp '.atrium/mcp.json'
            if ($c.Code -ne 0) { $fileWord = 'fail' }
            else {
                $fileWord = 'done'
                if (-not $kv.file) {
                    $script:manifest | Add-Member -NotePropertyName mcpfile -NotePropertyValue $true -Force
                    Save-Manifest
                }
            }
        }
    }

    # THE RUNNER ROW, through the hub, because the hub is this machine and a
    # PowerShell here can edit JSON on any remote without jq or python there.
    $rowWord = 'skip'
    if ($fileWord -in @('ok', 'done')) {
        try {
            $hdr = @{ 'X-Atrium-Room' = $Name }
            $rows = Invoke-RestMethod -Uri "http://$HubAddr/v1/harnesses" -Headers $hdr -TimeoutSec 10
            $row = @($rows) + @($rows.harnesses) | Where-Object { $_ -and $_.id -eq 'claude' } | Select-Object -First 1
            if (-not $row) { $rowWord = 'norow' }
            else {
                $args0 = @($row.args | Where-Object { $_ -ne $null })
                $i = [Array]::IndexOf($args0, '--mcp-config')
                if ($i -ge 0 -and $i + 1 -lt $args0.Count -and $args0[$i + 1] -eq $mcpPath) { $rowWord = 'ok' }
                elseif ($i -ge 0) { $rowWord = 'otherrow' }
                else {
                    $row.args = @('--mcp-config', $mcpPath) + $args0
                    $res0 = @($row.resume_args | Where-Object { $_ -ne $null })
                    if ([Array]::IndexOf($res0, '--mcp-config') -lt 0) { $row.resume_args = $res0 + @('--mcp-config', $mcpPath) }
                    $body = $row | ConvertTo-Json -Depth 8
                    Invoke-RestMethod -Method Put -Uri "http://$HubAddr/v1/harnesses/claude" -Headers $hdr `
                        -ContentType 'application/json' -Body $body -TimeoutSec 10 | Out-Null
                    $rowWord = 'done'
                }
            }
        } catch { $rowWord = 'fail' }
    }

    switch ("$fileWord/$rowWord") {
        'ok/ok'   { Step 'mcp' 'ok' "atrium-control is in $mcpPath and the claude runner row names it" }
        { $_ -in 'done/done', 'done/ok', 'ok/done' } {
            Step 'mcp' 'done' "atrium-control in $mcpPath, named by the claude runner row with --mcp-config"
        }
        { $_ -like 'other/*' } { Step 'mcp' 'warn' "$mcpPath already has an atrium-control that runs something else. left as it is" }
        { $_ -like 'bad/*' }   { Step 'mcp' 'warn' "$mcpPath is not JSON. left as it is. its sessions cannot answer other rooms" }
        { $_ -like 'fail/*' }  { Step 'mcp' 'warn' "could not copy $mcpPath. its sessions cannot answer other rooms" }
        { $_ -like '*/norow' } { Step 'mcp' 'warn' "wrote $mcpPath, but the room has no claude runner row to name it" }
        { $_ -like '*/otherrow' } { Step 'mcp' 'warn' "wrote $mcpPath, but the claude runner row already names another --mcp-config. left as it is" }
        default   { Step 'mcp' 'warn' "wrote $mcpPath, but could not set the claude runner row. its sessions cannot answer other rooms" }
    }
}

# ── 11b. the status line ────────────────────────────────────────────────────

# CLINT'S CLAUDE CODE STATUS LINE on every room, so every agent on it shows one. The portable script is
# scripts/statusline-command.sh, copied to ~/.claude/statusline-command.sh, and ONLY the `statusLine` key of the
# account's settings.json is merged in: every other key stays, a settings.json.statusline-<stamp>.bak is written first,
# and nothing is touched when the key already holds the wanted value. The merge is done here, in PowerShell, because
# the remote may have no jq. Never a failure: the room works without it, and room-check.ps1 flags the gap.
#
# settings.json is round-tripped through ConvertFrom-Json and ConvertTo-Json, so the whole file may be reformatted
# (indent, spacing) and an ISO date string may change form. The backup holds the original.
#
# THE BASH. Windows: the `bash.exe` on PATH that is not a WSL launcher (System32, WindowsApps), else the first of the
# usual git-bash and cygwin installs. Either way it is written to settings.json by its full path. macOS and Linux: /bin/bash.
if (@($Runners + $Install) -contains 'claude') {
    $slSrc = Join-Path $PSScriptRoot 'statusline-command.sh'
    if (-not (Test-Path -LiteralPath $slSrc)) {
        Step 'statusline' 'warn' "no scripts/statusline-command.sh beside this script, so none was installed"
    } else {
        $slProbe = if ($os -eq 'windows') {
@'
"home=$($HOME -replace '\\', '/')"
New-Item -ItemType Directory -Force (Join-Path $HOME '.claude') | Out-Null
$b = Get-Command bash.exe -All -ErrorAction SilentlyContinue | Where-Object { $_.Source -notmatch '\\(Windows\\System32|WindowsApps)\\' } | Select-Object -First 1
$bp = $null
if ($b) { $bp = $b.Source -replace '\\', '/'; "bash=$bp" }
else {
    foreach ($c in 'C:/Program Files/Git/bin/bash.exe', 'C:/work/tools/cygwin/bin/bash.exe', 'C:/cygwin64/bin/bash.exe', 'C:/msys64/usr/bin/bash.exe') {
        if (Test-Path -LiteralPath $c) { "bash=$c"; $bp = $c; break }
    }
}
if ($bp) {
    $eap = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & $bp -c 'command -v jq' 2>&1 | Out-Null
    "jq=$(if ($LASTEXITCODE -eq 0) { 1 } else { 0 })"
    $ErrorActionPreference = $eap
}
$f = Join-Path $HOME '.claude\settings.json'
if (Test-Path -LiteralPath $f) { 'file=' + [Convert]::ToBase64String([IO.File]::ReadAllBytes($f)) }
$s = Join-Path $HOME '.claude\statusline-command.sh'
if (Test-Path -LiteralPath $s) { 'sha=' + (Get-FileHash -LiteralPath $s -Algorithm SHA256).Hash.ToLower() }
'@
        } else {
@'
echo "home=$HOME"
echo "bash=/bin/bash"
if /bin/bash -c 'command -v jq' >/dev/null 2>&1; then echo jq=1; else echo jq=0; fi
mkdir -p "$HOME/.claude"
[ -f "$HOME/.claude/settings.json" ] && echo "file=$(base64 < "$HOME/.claude/settings.json" | tr -d '\n')"
s="$HOME/.claude/statusline-command.sh"
if [ -f "$s" ]; then
  if command -v sha256sum >/dev/null 2>&1; then h=$(sha256sum "$s"); else h=$(shasum -a 256 "$s"); fi
  echo "sha=${h%% *}"
fi
'@
        }
        $slk = ConvertFrom-KeyValue (Invoke-Remote $slProbe).Out
        if (-not $slk.home -or -not $slk.bash) {
            Step 'statusline' 'warn' "no bash found on $Name to run the status line. install git-bash or cygwin, then rerun"
        } else {
            $slPath = "$($slk.home.TrimEnd('/'))/.claude/statusline-command.sh"
            # BOTH PARTS QUOTED: Claude Code runs the command through a shell, and C:/Program Files/Git/bin/bash.exe or a
            # home with a space would split at the space. The quoted form is also what the idempotent check compares.
            $slCmd = "`"$($slk.bash)`" `"$slPath`""

            # LF ONLY: a checkout with autocrlf holds CRLF, and bash reads `\r` as part of the command.
            $slBytes = [IO.File]::ReadAllBytes($slSrc)
            $slText = [Text.Encoding]::UTF8.GetString($slBytes) -replace "`r", ''
            $slLf = [Text.UTF8Encoding]::new($false).GetBytes($slText)
            $slSha = ([BitConverter]::ToString([Security.Cryptography.SHA256]::HashData($slLf)) -replace '-', '').ToLower()
            $slTmp = Join-Path $work 'statusline-command.sh'
            New-Item -ItemType Directory -Force (Split-Path $slTmp) | Out-Null
            [IO.File]::WriteAllBytes($slTmp, $slLf)

            $slScript = 'ok'
            if ($slk.sha -ne $slSha) {
                $c = Copy-ToRemote $slTmp '.claude/statusline-command.sh'
                $slScript = if ($c.Code -eq 0) { 'done' } else { 'fail' }
            }

            $slSettings = 'ok'
            $slDoc = $null
            if ($slk.file) {
                try { $slDoc = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($slk.file)) | ConvertFrom-Json }
                catch { $slDoc = $null }
            }
            if ($slk.file -and -not $slDoc) { $slSettings = 'bad' }
            else {
                if (-not $slDoc) { $slDoc = [pscustomobject]@{} }
                $slWant = [pscustomobject]@{ type = 'command'; command = $slCmd }
                $slHave = $slDoc.PSObject.Properties['statusLine']
                if ($slHave -and "$($slHave.Value.type)" -eq 'command' -and "$($slHave.Value.command)" -eq $slCmd) { $slSettings = 'ok' }
                else {
                    $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
                    $bak = if ($os -eq 'windows') {
                        "`$f = Join-Path `$HOME '.claude\settings.json'`nif (Test-Path -LiteralPath `$f) { Copy-Item -LiteralPath `$f `"`$f.statusline-$stamp.bak`"; 'bak=1' }"
                    } else {
                        "f=`"`$HOME/.claude/settings.json`"; if [ -f `"`$f`" ]; then cp `"`$f`" `"`$f.statusline-$stamp.bak`" && echo bak=1; fi"
                    }
                    $bk = ConvertFrom-KeyValue (Invoke-Remote $bak).Out
                    if ($slk.file -and -not $bk.bak) {
                        # An existing file with no backup is not rewritten.
                        $slSettings = 'nobak'
                    } else {
                        $slDoc | Add-Member -NotePropertyName statusLine -NotePropertyValue $slWant -Force
                        $slTmpJ = Join-Path $work 'settings.json'
                        [IO.File]::WriteAllText($slTmpJ, ($slDoc | ConvertTo-Json -Depth 20), [Text.UTF8Encoding]::new($false))
                        $c = Copy-ToRemote $slTmpJ '.claude/settings.json'
                        $slSettings = if ($c.Code -eq 0) { 'done' } else { 'fail' }
                    }
                }
            }

            switch ("$slScript/$slSettings") {
                'ok/ok' { Step 'statusline' 'ok' "$slCmd is the status line" }
                { $_ -in 'done/done', 'done/ok', 'ok/done' } { Step 'statusline' 'done' "status line set to $slCmd, settings.json backed up first" }
                { $_ -like '*/nobak' } { Step 'statusline' 'warn' "could not back up settings.json on $Name, so it was not changed and has no status line. rerun" }
                { $_ -like '*/bad' }  { Step 'statusline' 'warn' "$($slk.home)/.claude/settings.json is not JSON. left as it is, so no status line" }
                default { Step 'statusline' 'warn' "could not copy the status line to $Name ($slScript/$slSettings). rerun" }
            }
            # The script reads every percentage from one jq pass. Without jq it prints the folder, branch and clock only.
            if ($slk.jq -ne '1') {
                Step 'statusline' 'warn' "jq is not on $Name's PATH for $($slk.bash), so the status line shows only folder, branch and clock. install jq there (git-bash has none: put jq.exe in Git/usr/bin)"
            }
        }
    }
}

# The clone, made by room-git.ps1 init. Its `room-git cwd ok <path>` line is
# where the smoke card below runs, when init succeeded.
$clonePath = $null
if ($Repo -ne 'none') { & pwsh -NoProfile -File (Join-Path $PSScriptRoot 'room-git.ps1') init $Name -Target $Target -Ssh $Ssh -Scp $Scp @(if ($sharedDir) { '-GitRoot'; $sharedDir }) @(if ($GitVersion) { '-GitVersion'; $GitVersion }) @(if ($SshOption) { '-SshOption'; $SshOption -join ',' }) *>&1 | ForEach-Object { Write-Host $_; if ("$_" -match '^room-git cwd ok (.+)$') { $clonePath = $Matches[1].Trim() } }; if ($LASTEXITCODE -ne 0) { $clonePath = $null; Step 'git' 'warn' "room-git init exited $LASTEXITCODE. rerun: room-git.ps1 init $Name -Target $Target" } }

# The permission gate, after the hooks: room-gate.ps1 copies the one dotfiles script and registers it first.
& pwsh -NoProfile -File (Join-Path $PSScriptRoot 'room-gate.ps1') $Name -Target $Target -Ssh $Ssh -Scp $Scp @(if ($SshOption) { '-SshOption'; $SshOption -join ',' }) *>&1 | ForEach-Object { Write-Host $_ }; if ($LASTEXITCODE -ne 0) { Step 'gate' 'warn' "room-gate exited $LASTEXITCODE. rerun: room-gate.ps1 $Name -Target $Target" }

# DEFENDER, on a Windows room, after the clone so its build folder and worktrees are known. room-defender.ps1 reads the
# paths as the ssh login, which is the account the room runs as, sets GOTMPDIR, and excludes them when that login is
# elevated. Otherwise it prints the line for an administrator in a warn line.
if ($os -eq 'windows') {
    if ($NoDefender) {
        Step 'defender' 'skip' '-NoDefender'
    } else {
        & pwsh -NoProfile -File (Join-Path $PSScriptRoot 'room-defender.ps1') $Name -Target $Target -Ssh $Ssh @(if ($clonePath) { '-Clone'; $clonePath }) @(if ($User) { '-Runner'; $User }) @(if ($SshOption) { '-SshOption'; $SshOption -join ',' }) *>&1 | ForEach-Object { Write-Host $_ }
        if ($LASTEXITCODE -ne 0) { Step 'defender' 'warn' "room-defender exited $LASTEXITCODE. rerun: room-defender.ps1 $Name -Target $Target" }
    }
}

# THE FOLDERS THE ROOM MAY LAUNCH IN, once the room is up and the clone is there, and before the smoke launches a card.
Invoke-AllowedFolders

$authState = Test-ClaudeAuth

if ($bad -gt 0) { Finish 5 }

Invoke-Smoke $authState

# THE PROJECT'S REQUIREMENTS, LAST, so a bare machine ends at "meets atrium's requirements", not just "a room".
# Read only: never -Fix, since a fix here would be this script deciding for a human. -NoSmoke, because the smoke has
# just run. What it finds is a warn: the room is up, and each unmet line names its fix and who runs it.
$req = Join-Path $checkout 'atrium.requirements.yaml'
if ($Repo -eq 'none') {
    Step 'requirements' 'skip' '-Repo none'
} elseif (-not (Test-Path $req)) {
    Step 'requirements' 'skip' "no atrium.requirements.yaml beside this script. run room-check.ps1 $Name from a checkout"
} else {
    & pwsh -NoProfile -File (Join-Path $PSScriptRoot 'room-check.ps1') $Name -Target $Target -NoSmoke -Ssh $Ssh @(if ($SshOption) { '-SshOption'; $SshOption -join ',' }) *>&1 | ForEach-Object { Write-Host $_ }
    $rc = $LASTEXITCODE
    if ($rc -eq 0) { Step 'requirements' 'ok' "$Name meets atrium.requirements.yaml" }
    else { Step 'requirements' 'warn' "room-check exited ${rc}: the room works, and the lines above say what is unmet and who fixes it. rerun: room-check.ps1 $Name -Target $Target" }
}
Finish 0
