# Git for a room, run from THIS side: the clone is made by PUSH, and its work comes back by FETCH.
#
# SUPERSEDED for init, push-base and fetch by `atrium rooms git sync <room> --init` and `atrium rooms git collect
# <room>`, and by the hub doing both on its own timers. See docs/rnd/git-sync-design.md (f-019). worktree and remove
# stay here until stage 2.
#
#   pwsh -File scripts\room-git.ps1 init      m1mini [-Target user@host] [-Repo <path>] [-Path <remote path>]
#   pwsh -File scripts\room-git.ps1 init      m1mini -Check [-Path <remote path>] [-From claude/main]
#   pwsh -File scripts\room-git.ps1 push-base m1mini [-From claude/main]
#   pwsh -File scripts\room-git.ps1 fetch     m1mini
#   pwsh -File scripts\room-git.ps1 worktree  m1mini fb02-proof [-Base hub-main] [-Root <remote dir>]
#   pwsh -File scripts\room-git.ps1 remove    m1mini [-Force]   undo init, keeping the clone when it holds unpushed work
#
# WHY PUSH. A room holds no GitHub credential and has no route back to this machine, so a private repository
# cannot be cloned there. The operator's own ssh reaches the room already, so this side makes the repository on the
# remote (`git init`) and pushes into it. The remote needs no credential and no route back.
#
# THE SHAPE, per room:
#   remote clone     ~/git/github/<owner>/<repo>  (Windows: $HOME\git\github\<owner>\<repo>), owner and repo read
#                    from this repository's origin URL. receive.denyCurrentBranch=updateInstead, so a push to the
#                    branch that is checked out moves the work tree, and is refused when the work tree is dirty.
#   hub-main         the branch the clone has checked out. It MIRRORS claude/main here (push-base, force), tracks
#                    nothing, and nobody commits on it.
#   worktrees        <clone>-worktrees/<name> on branch claude/<name>, off hub-main. A sibling of the clone, never
#                    inside its work tree. The path it prints is the `cwd` for `atrium_launch room=<room>`.
#   a git remote     `<room>` in this repository, url ssh://<host>/<absolute path>. Shared by every worktree of this
#                    repository, because a remote is repository config.
#   coming back      `fetch` brings the room's claude/* branches to refs/remotes/<room>/claude/*. The Release
#                    department merges those here. THIS SCRIPT NEVER MERGES.
#
# ALL GIT LIVES HERE, run by an operator or a director. The atrium binary never learns git or makes a worktree
# (docs/fabric/remote-launch.md section 3).
#
# THE REMOTE runs plain `sh -s` on Unix and Windows PowerShell 5.1 (-EncodedCommand) on Windows, the way
# provision-room.ps1 does. No pwsh is needed there, only git. When git is missing the script names the install and
# stops with exit 3.
#
# WINDOWS REMOTES. git-over-ssh runs `git-upload-pack '<path>'` in the ssh server's default shell. That works in
# PowerShell, which is what a stock OpenSSH for Windows install uses here. It cannot work in cmd, which does not
# understand single quotes. remote.<room>.uploadpack and receivepack are set to `git upload-pack` and
# `git receive-pack`, so only git.exe has to be on the PATH the ssh session gets. The url is scp-style, `host:C:/Users/...`,
# because an ssh:// url sends `/C:/Users/...` and git on Windows does not resolve that.
#
# ONE LINE PER STEP, the same shape as provision-room.ps1, prefixed room-git:
#
#   room-git <step> <status> <detail>
#
# status is ok (already right), done (changed now), skip, warn or fail. The last line is `room-git done ok` or
# `room-git done fail <code>`. worktree also prints `room-git cwd ok <absolute remote path>`.
#
# EXIT CODES
#   0  done
#   1  a local problem: bad arguments, no repository, the base branch is missing here
#   2  ssh could not reach the target, or its OS is not one this covers
#   3  git is not on the remote. The fail line names the install
#   4  a remote step failed: init, config, checkout or worktree
#   5  a push or fetch failed, most often a remote work tree that is dirty: see the output under the fail line
#
# init -Check WRITES NOTHING, here or there, so it is safe against a room in use. It reports one line per fact:
# remote (the git remote is an ssh url), git (on the remote, and Git for Windows on Windows: a Cygwin or MSYS git
# counts as missing), clone (present, receive.denyCurrentBranch=updateInstead), checkout (hub-main is checked out),
# fresh (hub-main there equals -From here), and a warn when the clone's work tree is dirty. Every fail names its fix.
# Its exit codes are its own, and scripts/room-check.ps1 calls exactly this:
#   0  every fact holds
#   1  a local problem, as above
#   2  ssh could not reach the target
#   3  something is unmet and `init` or `push-base` would fix it
#   4  something is unmet and a human is needed: no git (or the wrong git) on the remote

param(
    [Parameter(Position = 0)] [string] $Command,
    [Parameter(Position = 1)] [string] $Room,
    # worktree: the name. The branch is claude/<name>.
    [Parameter(Position = 2)] [string] $Name,
    # The ssh destination. Default: the room's own name, as an ssh alias.
    [string] $Target,
    # The local repository. Default: the main checkout of the one this script lives in.
    [string] $Repo,
    # init: the clone's place on the remote. Default ~/git/github/<owner>/<repo>. A leading ~ is the remote home.
    [string] $Path,
    # push-base: what hub-main is made to mirror.
    [string] $From = 'claude/main',
    # worktree: the branch the new one starts from.
    [string] $Base = 'hub-main',
    # worktree: the directory worktrees go under. Default <clone>-worktrees.
    [string] $Root,
    # remove: take the clone even when it holds a branch or stash that is not on this machine.
    [switch] $Force,
    # init: write nothing anywhere, report what init and push-base would have to fix.
    [switch] $Check,
    [string] $Ssh = 'ssh',
    [string[]] $SshOption = @()
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
# UTF-8 without a BOM for what is piped to ssh. See provision-room.ps1.
$OutputEncoding = [Text.UTF8Encoding]::new($false)
# `pwsh -File` hands `-SshOption -o,Port=2222` over as one string, so commas split.
$SshOption = @($SshOption | ForEach-Object { "$_" -split ',' } | Where-Object { $_ })

function Step {
    param([string] $step, [string] $status, [string] $detail = '')
    $line = "room-git $step $status"
    if ($detail) { $line += " $detail" }
    Write-Host $line
}
function Finish {
    param([int] $code)
    if ($code -eq 0) { Step 'done' 'ok' } else { Step 'done' 'fail' "$code" }
    exit $code
}
function Fail {
    param([string] $step, [int] $code, [string] $detail, $output)
    Step $step 'fail' $detail
    if ($output) { $output | ForEach-Object { Write-Host "    $_" } }
    Finish $code
}

$commands = 'init', 'push-base', 'fetch', 'worktree', 'remove'
if ($Command -notin $commands -or -not $Room -or ($Command -eq 'worktree' -and -not $Name)) {
    Write-Host 'usage: room-git.ps1 init|push-base|fetch|remove <room> [-Target user@host] [-Repo path] [-Path remote] [-From claude/main] [-Force]'
    Write-Host '       room-git.ps1 worktree <room> <name> [-Base hub-main] [-Root remote dir]'
    exit 1
}
if ($Check -and $Command -ne 'init') { Fail 'args' 1 '-Check goes with init only' }
if ($Room -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]*$') { Fail 'args' 1 "bad room name '$Room'" }
if ($Name -and $Name -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]*$') { Fail 'args' 1 "bad worktree name '$Name'" }
if ($Base -notmatch '^[A-Za-z0-9][A-Za-z0-9._/-]*$') { Fail 'args' 1 "bad -Base '$Base'" }

# ── this side ───────────────────────────────────────────────────────────────

# The main checkout, even when run from a worktree: the first entry of the list is the main one.
if (-not $Repo) {
    $Repo = Split-Path -Parent $PSScriptRoot
    $first = (git -C $Repo worktree list --porcelain 2>$null | Select-Object -First 1)
    if ($first -match '^worktree (.+)$') { $Repo = $Matches[1] }
}
if (-not (Test-Path -LiteralPath (Join-Path $Repo '.git'))) { Fail 'repo' 1 "$Repo is not a git checkout" }
$Repo = (Resolve-Path -LiteralPath $Repo).Path

# git's ssh, with BatchMode so a target that wants a password fails at once rather than at a prompt.
$sshBase = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=25') + $SshOption
$env:GIT_SSH_COMMAND = (@($Ssh) + $sshBase | ForEach-Object { if ($_ -match '\s') { "'$_'" } else { $_ } }) -join ' '

function Invoke-Git {
    param([string[]] $gitArgs)
    $out = & git -C $Repo @gitArgs 2>&1
    [pscustomobject]@{ Out = @($out | ForEach-Object { "$_" }); Code = $LASTEXITCODE }
}

# ── the remote ──────────────────────────────────────────────────────────────

function Quote-Ps { param([string] $s) "'" + ($s -replace "'", "''") + "'" }
function Quote-Sh { param([string] $s) "'" + ($s -replace "'", "'\''") + "'" }

$script:remoteOS = $null

# Invoke-Remote runs one script on the remote. Windows gets -EncodedCommand, which passes cmd, Windows PowerShell
# and pwsh untouched. Unix gets the script on stdin to `sh -s`, so nothing in it meets the login shell's quoting.
function Invoke-Remote {
    param([string] $script, [string] $sshTarget = $script:sshTarget)
    if ($script:remoteOS -eq 'windows') {
        $full = "`$ErrorActionPreference='Continue'; `$ProgressPreference='SilentlyContinue'`n" +
            # PATH from the registry, so git is found whatever the ssh session inherited.
            "`$env:Path = (@([Environment]::GetEnvironmentVariable('Path', 'Machine'), " +
            "[Environment]::GetEnvironmentVariable('Path', 'User')) | Where-Object { `$_ }) -join ';'`n" +
            # THE ROOM'S OWN TOOLCHAIN NEXT, when room-toolchain.ps1 recorded one, so this git is the git the room's
            # workers run. The machine Path alone can put a Cygwin git first, and a worktree that git adds has a
            # /cygdrive/c/... gitfile that Git for Windows cannot resolve (seen on sg3: `git status` failed in it).
            "`$e = Join-Path `$HOME '.atrium\toolchain\room-env.ps1'; if (Test-Path `$e) { . `$e }`n" +
            # git's own folder first, because git finds its children on PATH and a Cygwin git exits 127 without it.
            "`$g = (Get-Command git -ErrorAction SilentlyContinue).Source; if (`$g) { `$env:Path = (Split-Path -Parent `$g) + ';' + `$env:Path }`n" + $script
        $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($full))
        if ($enc.Length -gt 7800) { throw "remote script too long for cmd.exe ($($enc.Length))" }
        $out = & $Ssh @sshBase $sshTarget "powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand $enc" 2>&1
    } else {
        # The PATH a login shell has, so a Homebrew git is found. A COMMENT LAST, because PowerShell ends what it
        # pipes to a native command with CRLF, and `fi` followed by a carriage return is not `fi`.
        $full = "lp=`$(`"`${SHELL:-/bin/sh}`" -lc 'printf %s `"`$PATH`"' 2>/dev/null); [ -n `"`$lp`" ] && PATH=`"`$lp:`$PATH`"`n" + $script
        $full = ($full -replace "`r", '') + "`n#"
        $out = $full | & $Ssh @sshBase $sshTarget 'sh -s' 2>&1
    }
    # Windows PowerShell writes a CLIXML preamble to stderr for some hosts. It is noise here.
    $lines = @($out | ForEach-Object { "$_" } | Where-Object { $_ -notmatch '^#< CLIXML|^<Objs |^</Objs>' })
    [pscustomobject]@{ Out = $lines; Code = $LASTEXITCODE }
}

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

# Get-RoomUrl is the room's git remote in this repository: the host part and the path on the remote, and what OS
# that path says. `/C:/Users/x` is Windows, `/Users/x` or `/home/x` is not.
function Get-RoomUrl {
    $r = Invoke-Git @('remote', 'get-url', $Room)
    if ($r.Code -ne 0) { return $null }
    $url = ($r.Out | Select-Object -First 1).Trim()
    # A Windows remote is scp-style, host:DRIVE:/dir, because git-upload-pack there cannot take the /C:/ an ssh:// url sends.
    if ($url -match '^([^/:]+):([A-Za-z]:/.*)$') {
        return [pscustomobject]@{ Url = $url; Host = $Matches[1]; Path = $Matches[2]; OS = 'windows' }
    }
    if ($url -notmatch '^ssh://([^/]+)(/.*)$') { return [pscustomobject]@{ Url = $url; Host = $null; Path = $null; OS = $null } }
    $p = $Matches[2]
    $win = $p -match '^/[A-Za-z]:/'
    [pscustomobject]@{ Url = $url; Host = $Matches[1]; Path = $(if ($win) { $p.Substring(1) } else { $p }); OS = $(if ($win) { 'windows' } else { 'unix' }) }
}

# Resolve-Target: -Target, else the host the room's remote already names, else the room's name as an ssh alias.
$existing = Get-RoomUrl
$script:sshTarget = if ($Target) { $Target } elseif ($existing -and $existing.Host) { $existing.Host } else { $Room }

function Test-Ssh {
    $probe = & $Ssh @sshBase $script:sshTarget 'uname -sm' 2>&1
    $code = $LASTEXITCODE
    if ($code -eq 255) { Fail 'ssh' 2 "cannot reach $($script:sshTarget) over ssh" $probe }
    $text = ($probe | ForEach-Object { "$_" }) -join ' '
    if ($code -eq 0 -and $text -match '^(Linux|Darwin)\s') {
        $script:remoteOS = 'unix'
        $script:remoteKind = if ($Matches[1] -eq 'Linux') { 'linux' } else { 'mac' }
    } else {
        $script:remoteOS = 'windows'
        $script:remoteKind = 'windows'
        $r = Invoke-Remote '"ok"'
        if ($r.Code -ne 0) { Fail 'ssh' 2 'not Linux, macOS or Windows PowerShell' ($probe + $r.Out) }
    }
    Step 'ssh' 'ok' "$($script:sshTarget) ($($script:remoteKind))"
}

# What to install when git is missing, as a command the operator can run there.
function Get-GitInstallHint {
    switch ($script:remoteKind) {
        'mac' { 'xcode-select --install' }
        'windows' { 'winget install --id Git.Git -e' }
        default {
            $r = Invoke-Remote 'if [ -r /etc/os-release ]; then . /etc/os-release; echo "id=$ID $ID_LIKE"; fi'
            $id = (ConvertFrom-KeyValue $r.Out).id
            switch -Regex ($id) {
                'debian|ubuntu' { 'sudo apt-get install -y git'; break }
                'fedora|rhel|centos' { 'sudo dnf install -y git'; break }
                'arch' { 'sudo pacman -S git'; break }
                'alpine' { 'sudo apk add git'; break }
                'suse' { 'sudo zypper install git'; break }
                default { "install git with the distribution's package manager" }
            }
        }
    }
}

# Test-RemoteGit stops with exit 3 and the install when the remote has no git.
function Test-RemoteGit {
    $s = if ($script:remoteOS -eq 'windows') {
        "if (Get-Command git -ErrorAction SilentlyContinue) { `"git=`$(git --version)`" } else { exit 3 }"
    } else {
        'if git --version >/dev/null 2>&1; then echo "git=$(git --version)"; else exit 3; fi'
    }
    $r = Invoke-Remote $s
    if ($r.Code -ne 0) {
        Fail 'git' 3 "git is not on $($script:sshTarget). install it there, then rerun: $(Get-GitInstallHint)"
    }
    $v = (ConvertFrom-KeyValue $r.Out).git
    Step 'git' 'ok' "$v on $($script:sshTarget)"
}

# Get-RemoteHome is the remote home as an absolute path with forward slashes.
function Get-RemoteHome {
    $s = if ($script:remoteOS -eq 'windows') { '"home=$($HOME -replace ''\\'', ''/'')"' } else { 'echo "home=$HOME"' }
    $r = Invoke-Remote $s
    $h = (ConvertFrom-KeyValue $r.Out).home
    if ($r.Code -ne 0 -or -not $h) { Fail 'home' 4 'could not read the remote home' $r.Out }
    $h.TrimEnd('/')
}

# Get-ClonePath is the clone's absolute path, from the room's git remote, and it also fixes the remote OS.
function Get-ClonePath {
    if (-not $existing -or -not $existing.Path) {
        Fail 'remote' 1 "this repository has no ssh remote called $Room. run: room-git.ps1 init $Room"
    }
    $script:remoteOS = $existing.OS
    $existing.Path
}

# ── init ────────────────────────────────────────────────────────────────────

function Invoke-PushBase {
    $sha = Invoke-Git @('rev-parse', '--verify', '-q', "refs/heads/$From^{commit}")
    if ($sha.Code -ne 0) { Fail 'push-base' 1 "no branch $From here to mirror" }
    $want = $sha.Out[0].Trim()
    $ls = Invoke-Git @('ls-remote', $Room, 'refs/heads/hub-main')
    if ($ls.Code -ne 0) { Fail 'push-base' 5 "could not read $Room over ssh" $ls.Out }
    $have = if ($ls.Out.Count -gt 0) { ($ls.Out[0] -split '\s+')[0] } else { '' }
    if ($have -eq $want) {
        Step 'push-base' 'ok' "hub-main on $Room is already $($want.Substring(0, 9)) ($From)"
        return
    }
    # FORCE, because hub-main mirrors. With hub-main checked out on the remote, updateInstead moves its work tree,
    # and refuses when that is dirty.
    $p = Invoke-Git @('push', '--force', $Room, "refs/heads/${From}:refs/heads/hub-main")
    if ($p.Code -ne 0) {
        $dirty = ($p.Out -join ' ') -match 'updateInstead|working tree|uncommitted|unstaged'
        $why = if ($dirty) { "$Room refused: the work tree of its hub-main is not clean. clean it there (git status), then rerun" } else { "push to $Room failed" }
        Fail 'push-base' 5 $why $p.Out
    }
    Step 'push-base' 'done' "hub-main on $Room is now $($want.Substring(0, 9)) ($From)"
}

function Invoke-InitCheck {
    $origin = (Invoke-Git @('remote', 'get-url', 'origin')).Out | Select-Object -First 1
    if (-not $Path -and -not ($existing -and $existing.Path) -and $origin -notmatch '[:/]([^/:]+)/([^/]+?)(\.git)?\s*$') {
        Fail 'repo' 1 "cannot read an owner and repo from origin ($origin). pass -Path"
    }
    $owner = $Matches[1]; $repoName = $Matches[2]
    $sha = Invoke-Git @('rev-parse', '--verify', '-q', "refs/heads/$From^{commit}")
    if ($sha.Code -ne 0) { Fail 'fresh' 1 "no branch $From here to compare hub-main with" }
    $want = $sha.Out[0].Trim()

    $fixable = 0
    $human = 0
    Test-Ssh

    # The remote here. Not made, only read.
    if (-not $existing) {
        Step 'remote' 'fail' "this repository has no git remote called $Room. run: room-git.ps1 init $Room"
        $fixable++
    } elseif (-not $existing.Host) {
        Step 'remote' 'fail' "$Room is $($existing.Url), not an ssh url. run: room-git.ps1 init $Room"
        $fixable++
    } else {
        Step 'remote' 'ok' "$Room = $($existing.Url)"
    }

    # git on the remote, and the right git on Windows.
    $s = if ($script:remoteOS -eq 'windows') {
        @'
$g = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $g) { exit 3 }
"git=$(git --version)"
"path=$g"
'@
    } else {
        'if git --version >/dev/null 2>&1; then echo "git=$(git --version)"; else exit 3; fi'
    }
    $r = Invoke-Remote $s
    if ($r.Code -ne 0) {
        Step 'git' 'fail' "git is not on $($script:sshTarget). install it there, then rerun: $(Get-GitInstallHint)"
        Finish 4
    }
    $gk = ConvertFrom-KeyValue $r.Out
    if ($script:remoteOS -eq 'windows' -and ($gk.git -notmatch '\.windows\.' -or $gk.path -match 'cygwin|msys')) {
        Step 'git' 'fail' "$($gk.git) at $($gk.path) is not Git for Windows, which counts as missing. install it there, then rerun: $(Get-GitInstallHint)"
        Finish 4
    }
    Step 'git' 'ok' "$($gk.git) on $($script:sshTarget)"

    $home_ = Get-RemoteHome
    $clone = if ($Path) { $Path -replace '\\', '/' } elseif ($existing -and $existing.Path) { $existing.Path } else { "$home_/git/github/$owner/$repoName" }
    if ($clone -eq '~') { $clone = $home_ } elseif ($clone.StartsWith('~/')) { $clone = $home_ + $clone.Substring(1) }
    if ($clone -notmatch '^(/|[A-Za-z]:/)') { Fail 'repo' 1 "the remote path must be absolute: $clone" }

    # The clone, READ ONLY: no git init, no config, no checkout.
    $c = if ($script:remoteOS -eq 'windows') {
        "`$C = $(Quote-Ps $clone)`n" + @'
if (-not (Test-Path -LiteralPath (Join-Path $C '.git'))) { 'clone=missing'; exit 0 }
Set-Location $C
'clone=present'
"deny=$(git config receive.denyCurrentBranch 2>$null)"
"branch=$(git symbolic-ref --short -q HEAD 2>$null)"
"hubmain=$(git rev-parse --verify -q 'refs/heads/hub-main^{commit}' 2>$null)"
"dirty=$(if (git status --porcelain 2>$null) { 'yes' } else { 'no' })"
'@
    } else {
        "C=$(Quote-Sh $clone)`n" + @'
if [ ! -e "$C/.git" ]; then echo clone=missing; exit 0; fi
cd "$C" || exit 4
echo clone=present
echo "deny=$(git config receive.denyCurrentBranch 2>/dev/null)"
echo "branch=$(git symbolic-ref --short -q HEAD 2>/dev/null)"
echo "hubmain=$(git rev-parse --verify -q 'refs/heads/hub-main^{commit}' 2>/dev/null)"
if [ -n "$(git status --porcelain 2>/dev/null)" ]; then echo dirty=yes; else echo dirty=no; fi
'@
    }
    $r = Invoke-Remote $c
    if ($r.Code -ne 0) { Fail 'clone' 4 "could not read the clone at $clone on $($script:sshTarget)" $r.Out }
    $kv = ConvertFrom-KeyValue $r.Out
    $initFix = "run: room-git.ps1 init $Room"
    if ($kv.clone -ne 'present') {
        Step 'clone' 'fail' "no clone at $clone on $($script:sshTarget). $initFix"
        Step 'checkout' 'skip' 'no clone'
        Step 'fresh' 'skip' 'no clone'
        $fixable++
    } else {
        if ($kv.deny -eq 'updateInstead') { Step 'clone' 'ok' "$clone, receive.denyCurrentBranch=updateInstead" }
        else {
            Step 'clone' 'fail' "$clone has receive.denyCurrentBranch='$($kv.deny)', not updateInstead. $initFix"
            $fixable++
        }
        if ($kv.branch -eq 'hub-main') { Step 'checkout' 'ok' 'hub-main' }
        elseif (-not $kv.hubmain) {
            Step 'checkout' 'fail' "hub-main does not exist at $clone (checked out: '$($kv.branch)'). run: room-git.ps1 init $Room, which pushes it and checks it out"
            $fixable++
        } else {
            Step 'checkout' 'fail' "$clone has '$($kv.branch)' checked out, not hub-main. $initFix"
            $fixable++
        }
        if (-not $kv.hubmain) {
            Step 'fresh' 'fail' "no hub-main at $clone. run: room-git.ps1 push-base $Room -From $From"
            $fixable++
        } elseif ($kv.hubmain -eq $want) {
            Step 'fresh' 'ok' "hub-main is $($want.Substring(0, 9)), equal to $From here"
        } else {
            Step 'fresh' 'fail' "hub-main is $($kv.hubmain.Substring(0, 9)) there, $From is $($want.Substring(0, 9)) here. run: room-git.ps1 push-base $Room -From $From"
            $fixable++
        }
        if ($kv.dirty -eq 'yes') { Step 'worktree' 'warn' "the work tree at $clone is not clean, so a push-base would be refused. clean it there (git status)" }
    }
    if ($human) { Finish 4 }
    if ($fixable) { Finish 3 }
    Finish 0
}

function Invoke-Init {
    if ($Check) { Invoke-InitCheck; return }
    $origin = (Invoke-Git @('remote', 'get-url', 'origin')).Out | Select-Object -First 1
    if (-not $Path -and $origin -notmatch '[:/]([^/:]+)/([^/]+?)(\.git)?\s*$') {
        Fail 'repo' 1 "cannot read an owner and repo from origin ($origin). pass -Path"
    }
    $owner = $Matches[1]; $repoName = $Matches[2]

    Test-Ssh
    Test-RemoteGit
    $home_ = Get-RemoteHome

    $clone = if ($Path) { $Path -replace '\\', '/' } elseif ($existing -and $existing.Path) { $existing.Path } else { "$home_/git/github/$owner/$repoName" }
    if ($clone -eq '~') { $clone = $home_ } elseif ($clone.StartsWith('~/')) { $clone = $home_ + $clone.Substring(1) }
    if ($existing -and $existing.Path -and -not $Path) { $clone = $existing.Path }
    if ($clone -notmatch '^(/|[A-Za-z]:/)') { Fail 'repo' 1 "the remote path must be absolute: $clone" }

    # THE REPOSITORY ON THE REMOTE. HEAD stays on an unborn branch until hub-main has been pushed.
    $s = if ($script:remoteOS -eq 'windows') {
        "`$C = $(Quote-Ps $clone)`n" + @'
New-Item -ItemType Directory -Force -Path $C | Out-Null
Set-Location $C
$new = -not (Test-Path (Join-Path $C '.git'))
if ($new) { git init -q 2>&1 | Out-Null; if ($LASTEXITCODE -ne 0) { exit 4 } }
$cur = "$(git config receive.denyCurrentBranch 2>$null)"
if ($cur -ne 'updateInstead') { git config receive.denyCurrentBranch updateInstead; "config=changed" } else { "config=same" }
"repo=$(if ($new) { 'new' } else { 'existing' })"
'@
    } else {
        "C=$(Quote-Sh $clone)`n" + @'
mkdir -p "$C" && cd "$C" || exit 4
if [ -e .git ]; then echo repo=existing; else git init -q >/dev/null 2>&1 || exit 4; echo repo=new; fi
if [ "$(git config receive.denyCurrentBranch 2>/dev/null)" = updateInstead ]; then echo config=same; else git config receive.denyCurrentBranch updateInstead; echo config=changed; fi
'@
    }
    $r = Invoke-Remote $s
    if ($r.Code -ne 0) { Fail 'repo' 4 "could not make a repository at $clone on $($script:sshTarget)" $r.Out }
    $kv = ConvertFrom-KeyValue $r.Out
    if ($kv.repo -eq 'new') { Step 'repo' 'done' "git init at $clone, receive.denyCurrentBranch=updateInstead" }
    elseif ($kv.config -eq 'changed') { Step 'repo' 'done' "$clone existed, receive.denyCurrentBranch is now updateInstead" }
    else { Step 'repo' 'ok' "$clone" }

    # THE REMOTE HERE. The host is what ssh was told, so an alias keeps working. The path is absolute, since `~` in
    # an ssh:// url is not portable. A Windows remote is host:DRIVE:/Users/..., since an ssh:// url sends /C:/Users/...,
    # which git on Windows does not resolve.
    $url = if ($script:remoteOS -eq 'windows') { "$($script:sshTarget):$clone" } else { "ssh://$($script:sshTarget)$clone" }
    if (-not $existing) {
        $a = Invoke-Git @('remote', 'add', $Room, $url)
        if ($a.Code -ne 0) { Fail 'remote' 1 "git remote add $Room failed" $a.Out }
        Step 'remote' 'done' "$Room = $url"
    } elseif ($existing.Url -ne $url) {
        $a = Invoke-Git @('remote', 'set-url', $Room, $url)
        if ($a.Code -ne 0) { Fail 'remote' 1 "git remote set-url $Room failed" $a.Out }
        Step 'remote' 'done' "$Room was $($existing.Url), now $url"
    } else {
        Step 'remote' 'ok' "$Room = $url"
    }
    if ($script:remoteOS -eq 'windows') {
        # A WRAPPER, because git's children are found on PATH and the ssh session's PATH may not hold git's folder
        # (measured on sg3, a Cygwin git: receive-pack ran, its unpack-objects child exited 127). The wrapper puts
        # git's own folder first, then runs git. It is one small .cmd in ~\.room-git, and PowerShell runs it.
        $w = Invoke-Remote @'
$g = (Get-Command git).Source
$d = Split-Path -Parent $g
$dir = Join-Path $HOME '.room-git'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$f = Join-Path $dir 'git.cmd'
$body = "@echo off`r`nset PATH=$d;%PATH%`r`n`"$g`" %*`r`n"
if (-not (Test-Path $f) -or (Get-Content $f -Raw) -ne $body) { [IO.File]::WriteAllText($f, $body); 'wrapper=changed' } else { 'wrapper=same' }
"path=$($f -replace '\\', '/')"
'@
        $wk = ConvertFrom-KeyValue $w.Out
        if ($w.Code -ne 0 -or -not $wk.path) { Fail 'remote' 4 'could not write the git wrapper on the remote' $w.Out }
        $changed = $false
        foreach ($kvp in @(@('uploadpack', "$($wk.path) upload-pack"), @('receivepack', "$($wk.path) receive-pack"))) {
            $cur = (Invoke-Git @('config', "remote.$Room.$($kvp[0])")).Out | Select-Object -First 1
            if ($cur -ne $kvp[1]) { $null = Invoke-Git @('config', "remote.$Room.$($kvp[0])", $kvp[1]); $changed = $true }
        }
        Step 'remote' $(if ($changed -or $wk.wrapper -eq 'changed') { 'done' } else { 'ok' }) "uploadpack and receivepack go through $($wk.path)"
    }
    $script:existing = Get-RoomUrl

    Invoke-PushBase

    # HUB-MAIN CHECKED OUT, tracking nothing. A push cannot move a branch that is not checked out, so this is what
    # makes the next push-base reach the work tree.
    $c = if ($script:remoteOS -eq 'windows') {
        "`$C = $(Quote-Ps $clone)`n" + @'
Set-Location $C
$b = "$(git symbolic-ref --short -q HEAD 2>$null)"
if ($b -eq 'hub-main') { 'checkout=ok'; exit 0 }
git checkout -q hub-main 2>&1 | ForEach-Object { "$_" }
if ($LASTEXITCODE -ne 0) { exit 4 }
"checkout=done from $b"
'@
    } else {
        "C=$(Quote-Sh $clone)`n" + @'
cd "$C" || exit 4
b=$(git symbolic-ref --short -q HEAD 2>/dev/null)
if [ "$b" = hub-main ]; then echo checkout=ok; exit 0; fi
git checkout -q hub-main 2>&1 || exit 4
echo "checkout=done from $b"
'@
    }
    $r = Invoke-Remote $c
    if ($r.Code -ne 0) { Fail 'checkout' 4 "hub-main could not be checked out at $clone" $r.Out }
    $kv = ConvertFrom-KeyValue $r.Out
    if ($kv.checkout -eq 'ok') { Step 'checkout' 'ok' 'hub-main' } else { Step 'checkout' 'done' "hub-main ($($kv.checkout))" }
    # The clone, on its own line, so provision can run its smoke card there.
    Step 'cwd' 'ok' $clone
}

# ── fetch ───────────────────────────────────────────────────────────────────

function Get-RoomRefs {
    $r = Invoke-Git @('for-each-ref', '--format=%(refname:short) %(objectname)', "refs/remotes/$Room/claude/")
    $h = @{}
    foreach ($l in $r.Out) { if ($l -match '^(\S+) (\S+)$') { $h[$Matches[1]] = $Matches[2] } }
    $h
}

function Invoke-Fetch {
    $before = Get-RoomRefs
    $f = Invoke-Git @('fetch', '--prune', $Room, "+refs/heads/claude/*:refs/remotes/$Room/claude/*")
    if ($f.Code -ne 0) { Fail 'fetch' 5 "fetch from $Room failed" $f.Out }
    $after = Get-RoomRefs
    $changed = @($after.Keys | Where-Object { $before[$_] -ne $after[$_] } | Sort-Object)
    $gone = @($before.Keys | Where-Object { -not $after.ContainsKey($_) } | Sort-Object)
    if (-not $changed -and -not $gone) {
        Step 'fetch' 'ok' "$Room has $($after.Count) claude/* branch(es), nothing new"
    } else {
        Step 'fetch' 'done' "$($changed.Count) new or moved, $($gone.Count) gone, $($after.Count) in all"
        foreach ($b in $changed) {
            $n = (Invoke-Git @('rev-list', '--count', "hub-main..$b")).Out
            $how = if ($before.ContainsKey($b)) { 'moved' } else { 'new' }
            Step 'ref' $how "$b $($after[$b].Substring(0, 9))"
        }
        foreach ($b in $gone) { Step 'ref' 'gone' $b }
    }
    foreach ($b in ($after.Keys | Sort-Object)) {
        if ($b -notin $changed) { Step 'ref' 'ok' "$b $($after[$b].Substring(0, 9))" }
    }
}

# ── worktree ────────────────────────────────────────────────────────────────

function Invoke-Worktree {
    $clone = Get-ClonePath
    Test-Ssh
    $branch = "claude/$Name"
    $wt = if ($Root) { ($Root -replace '\\', '/').TrimEnd('/') + "/$Name" } else { "$clone-worktrees/$Name" }
    $s = if ($script:remoteOS -eq 'windows') {
        "`$C = $(Quote-Ps $clone); `$W = $(Quote-Ps $wt); `$Br = $(Quote-Ps $branch); `$Base = $(Quote-Ps $Base)`n" + @'
Set-Location $C
git rev-parse --verify -q "refs/heads/$Base" 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) { "err=no branch $Base in $C. run push-base"; exit 4 }
if (Test-Path -LiteralPath $W) {
    $b = "$(git -C $W symbolic-ref --short -q HEAD 2>$null)"
    if ($b -eq $Br) { 'worktree=ok'; exit 0 }
    "err=$W exists and is on '$b', not $Br"; exit 4
}
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $W) | Out-Null
git rev-parse --verify -q "refs/heads/$Br" 2>&1 | Out-Null
if ($LASTEXITCODE -eq 0) { $o = git worktree add $W $Br 2>&1 } else { $o = git worktree add -b $Br $W $Base 2>&1 }
if ($LASTEXITCODE -ne 0) { $o | ForEach-Object { "err=$_" }; exit 4 }
'worktree=done'
'@
    } else {
        "C=$(Quote-Sh $clone); W=$(Quote-Sh $wt); Br=$(Quote-Sh $branch); Base=$(Quote-Sh $Base)`n" + @'
cd "$C" || exit 4
git rev-parse --verify -q "refs/heads/$Base" >/dev/null 2>&1 || { echo "err=no branch $Base in $C. run push-base"; exit 4; }
if [ -e "$W" ]; then
  b=$(git -C "$W" symbolic-ref --short -q HEAD 2>/dev/null)
  if [ "$b" = "$Br" ]; then echo worktree=ok; exit 0; fi
  echo "err=$W exists and is on '$b', not $Br"; exit 4
fi
mkdir -p "$(dirname "$W")" || exit 4
if git rev-parse --verify -q "refs/heads/$Br" >/dev/null 2>&1; then o=$(git worktree add "$W" "$Br" 2>&1); else o=$(git worktree add -b "$Br" "$W" "$Base" 2>&1); fi
if [ $? -ne 0 ]; then echo "err=$o"; exit 4; fi
# CLAUDE.md symlinks, only where the clone has them. A clone made by push does not, so this is usually silent.
find "$C" -name CLAUDE.md -type l -not -path '*/.git/*' 2>/dev/null | while IFS= read -r l; do
  rel=${l#"$C"/}; mkdir -p "$W/$(dirname "$rel")"; ln -sfn "$(readlink "$l")" "$W/$rel"
done
echo worktree=done
'@
    }
    $r = Invoke-Remote $s
    if ($r.Code -ne 0) { Fail 'worktree' 4 "could not make $wt on $($script:sshTarget)" $r.Out }
    $kv = ConvertFrom-KeyValue $r.Out
    $rootSaid = if ($Root) { 'under -Root' } else { 'beside the clone, in <clone>-worktrees' }
    if ($kv.worktree -eq 'ok') { Step 'worktree' 'ok' "$branch is already at $wt" }
    else { Step 'worktree' 'done' "$branch off $Base at $wt ($rootSaid)" }
    Step 'cwd' 'ok' $wt
}

# ── remove ──────────────────────────────────────────────────────────────────

# Invoke-Remove undoes init: the clone on the remote (and <clone>-worktrees, and ~\.room-git on Windows), then the
# git remote here. THE CLONE MAY HOLD WORK THAT EXISTS NOWHERE ELSE, so it goes only when every branch and stash in it
# is on this machine, and its work tree and worktrees are clean. Otherwise it stays, the remote here stays with it
# (it is how the work gets fetched), and a warn line names what kept it and the command that removes it anyway.
function Invoke-Remove {
    if (-not $existing) { Step 'remote' 'skip' "this repository has no remote called $Room, so there is nothing of init to undo"; return }
    $clone = Get-ClonePath
    Test-Ssh
    $hand = "pwsh -File scripts/room-git.ps1 remove $Room -Force"
    $inspect = if ($script:remoteOS -eq 'windows') {
        "`$C = $(Quote-Ps $clone)`n" + @'
if (-not (Test-Path -LiteralPath $C)) { 'gone=1'; exit 0 }
Set-Location $C
git for-each-ref '--format=branch=%(refname:short) %(objectname)' refs/heads
git stash list '--format=stash=%H %gs'
if (git status --porcelain) { "dirty=$C" }
foreach ($l in (git worktree list --porcelain)) { if ("$l" -match '^worktree (.+)$' -and $Matches[1] -ne ($C -replace '/', '\') -and $Matches[1] -ne $C) { if (git -C $Matches[1] status --porcelain) { "dirty=$($Matches[1])" } } }
'@
    } else {
        "C=$(Quote-Sh $clone)`n" + @'
if [ ! -e "$C" ]; then echo gone=1; exit 0; fi
cd "$C" || exit 4
git for-each-ref '--format=branch=%(refname:short) %(objectname)' refs/heads
git stash list '--format=stash=%H %gs'
if [ -n "$(git status --porcelain)" ]; then echo "dirty=$C"; fi
git worktree list --porcelain | sed -n 's/^worktree //p' | while IFS= read -r w; do
  if [ "$w" != "$C" ] && [ -n "$(git -C "$w" status --porcelain 2>/dev/null)" ]; then echo "dirty=$w"; fi
done
'@
    }
    $r = Invoke-Remote $inspect
    if ($r.Code -ne 0) { Fail 'remove' 4 "could not read the clone at $clone on $($script:sshTarget)" $r.Out }
    $gone = $r.Out -contains 'gone=1'

    $kept = @()
    if (-not $gone) {
        $localStash = @((Invoke-Git @('stash', 'list', '--format=%H')).Out)
        foreach ($l in $r.Out) {
            if ($l -match '^branch=(\S+) (\S+)$') {
                $c = Invoke-Git @('for-each-ref', '--contains', $Matches[2], '--format=%(refname)', 'refs/heads', 'refs/tags')
                if ($c.Code -ne 0 -or -not ($c.Out | Where-Object { $_ })) { $kept += "branch $($Matches[1]) ($($Matches[2].Substring(0, 9))) is not on this machine" }
            } elseif ($l -match '^stash=(\S+) (.*)$') {
                $sha = $Matches[1]; $msg = $Matches[2]
                $c = Invoke-Git @('for-each-ref', '--contains', $sha, '--format=%(refname)', 'refs/heads', 'refs/tags')
                if ($localStash -notcontains $sha -and ($c.Code -ne 0 -or -not ($c.Out | Where-Object { $_ }))) { $kept += "stash $($sha.Substring(0, 9)) `"$msg`" is not on this machine" }
            } elseif ($l -match '^dirty=(.+)$') {
                $kept += "$($Matches[1]) has uncommitted changes"
            }
        }
    }
    if ($kept -and -not $Force) {
        Step 'clone' 'warn' "kept $clone on $($script:sshTarget): $($kept -join '; '). the git remote $Room is kept too. to remove it anyway: $hand"
        return
    }
    if ($kept) { Step 'clone' 'warn' "-Force: removing $clone although $($kept -join '; ')" }

    if (-not $gone) {
        # ONLY UNDER THE REMOTE HOME. The path came from a git remote url, and a delete keyed on it should not be able
        # to reach outside where init put things by default.
        $s = if ($script:remoteOS -eq 'windows') {
            "`$C = $(Quote-Ps $clone)`n" + @'
$h = $HOME -replace '\\', '/'
if (-not $C.StartsWith($h + '/', [StringComparison]::OrdinalIgnoreCase)) { "err=$C is not under $h"; exit 4 }
foreach ($d in $C, "$C-worktrees") { if (Test-Path -LiteralPath $d) { Remove-Item -LiteralPath $d -Recurse -Force -ErrorAction Stop; "removed=$d" } }
$rg = Join-Path $HOME '.room-git'
if (Test-Path -LiteralPath $rg) { Remove-Item -LiteralPath $rg -Recurse -Force; "removed=$rg" }
'@
        } else {
            "C=$(Quote-Sh $clone)`n" + @'
case "$C" in "$HOME"/?*) ;; *) echo "err=$C is not under $HOME"; exit 4;; esac
for d in "$C" "$C-worktrees"; do if [ -e "$d" ]; then rm -rf "$d" || exit 4; echo "removed=$d"; fi; done
'@
        }
        $d = Invoke-Remote $s
        if ($d.Code -ne 0) { Fail 'clone' 4 "could not remove $clone on $($script:sshTarget)" $d.Out }
        $what = @($d.Out | Where-Object { $_ -like 'removed=*' } | ForEach-Object { $_.Substring(8) })
        Step 'clone' 'done' "removed $($what -join ', ') on $($script:sshTarget)"
    } else {
        Step 'clone' 'skip' "$clone is already gone on $($script:sshTarget)"
    }
    $rm = Invoke-Git @('remote', 'remove', $Room)
    if ($rm.Code -ne 0) { Fail 'remote' 1 "git remote remove $Room failed" $rm.Out }
    Step 'remote' 'done' "removed the git remote $Room here"
}

# ── go ──────────────────────────────────────────────────────────────────────

switch ($Command) {
    'remove' { Invoke-Remove }
    'init' { Invoke-Init }
    'push-base' {
        if (-not $existing) { Fail 'remote' 1 "this repository has no ssh remote called $Room. run: room-git.ps1 init $Room" }
        Invoke-PushBase
    }
    'fetch' {
        if (-not $existing) { Fail 'remote' 1 "this repository has no ssh remote called $Room. run: room-git.ps1 init $Room" }
        Invoke-Fetch
    }
    'worktree' { Invoke-Worktree }
}
Finish 0
