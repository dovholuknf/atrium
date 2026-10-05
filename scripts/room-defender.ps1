# Windows Defender exclusions for a room, worked out AS THE ROOM'S RUNNER USER, over ssh or on this machine.
#
#   pwsh -File scripts\room-defender.ps1 sg3 -Check
#   pwsh -File scripts\room-defender.ps1 sg3 -Target claude@sg3
#   pwsh -File scripts\room-defender.ps1 local
#
# WHY. Real-time protection scans every file a Go build writes and every test binary `go test` links, and on a room
# running several agents `MsMpEng.exe` can take more CPU than any of them (sg4, 2026-09-30: 103%, and 24% to 177% of a
# core after the cache paths were excluded, from the test binaries in %TEMP%). See docs/user-guide.md, Pattern 13.
#
# WHY IT RUNS AS THE RUNNER USER. The paths are that account's: `go env GOCACHE GOMODCACHE` and its %LOCALAPPDATA%.
# An administrator's shell expands them for whoever opened it, which is how a hand run once excluded the wrong
# profile. So the paths are read by the account the room runs as (the ssh login, or whoever runs `local`), and
# whatever needs elevation is handed over as literal paths.
#
# THE STEPS, in order:
#   ssh       reach the target. Not Windows is `skip`, and nothing else runs
#   defender  the WinDefend service. Missing or stopped is `skip`, and nothing else runs
#   who       the account and whether its token is elevated. `local` run elevated needs -Runner naming that account,
#             so an administrator's own profile is never what gets excluded
#   paths     what is excluded: GOCACHE and GOMODCACHE (from go, through the room's room-env.ps1 when there is one),
#             GOTMPDIR, the clone's build.claude, and the worktree root. Build output, Go's caches and the agents' own
#             worktrees, which the agents already run as code. GOMODCACHE holds downloaded modules and a worktree can
#             hold somebody else's pull request, so excluding them trusts that code the way running it already does
#   gotmpdir  `go test` links its binaries under %TEMP%\go-build* unless GOTMPDIR says otherwise, and %TEMP% is too
#             broad to exclude. A GOTMPDIR already set is kept. Otherwise it becomes <GOCACHE>\tmp, inside an
#             excluded path, set with `go env -w` so every go the account runs sees it, a running room included
#   exclude   elevated: Add-MpPreference for what is missing, then read back. Not elevated: nothing is excluded, and
#             the one Add-MpPreference line is printed for an administrator to paste. It is a `warn`, not a failure:
#             provisioning runs without admin on purpose
#
# NOTHING IS WRITTEN FOR AN ADMINISTRATOR TO RUN. A script left in the runner's profile is one every agent on the
# room can append to, and an administrator running it runs the agents' line too (@review, 53ccc3b4). The line is only
# printed, here, on the operator's machine.
#
# EVERY PATH IS CHECKED BEFORE IT IS USED, because the Go ones come from the runner's `go env`, which any agent there
# can change. A path must be fully qualified, hold no wildcard, `~`, `..` or control character, and lie inside one of
# these roots: the runner's <profile>\AppData\Local and <profile>\go, where Go keeps its caches by default, with the
# profile read from HKLM ProfileList by the account's SID, which the account cannot change, then the worktree root and
# the clone's build.claude. A Go path must be strictly inside one of them, never a root itself, so GOCACHE pointed at
# Downloads or at the profile excludes nothing. A root is refused when it is a drive root, less than two folders
# deep, or under Windows, Program Files or ProgramData. A refused path is named in a `warn` line and left out. So
# GOCACHE=C:\ excludes nothing.
#
# THE CLONE is -Clone, else, for a room, the path in this repository's git remote named for it (room-git.ps1 init),
# and for `local`, the main checkout this script belongs to. THE WORKTREE ROOT is -WorktreeRoot, else
# <clone>-worktrees for a room (room-git.ps1's convention), and new-worktree.ps1's -Root default for `local`.
#
# -Check reports every step and writes nothing. Nothing here is undone by provision-room.ps1 -Remove: an exclusion
# needs elevation to take back (Remove-MpPreference -ExclusionPath), and GOTMPDIR is `go env -u GOTMPDIR`.
#
# ONE LINE PER STEP, prefixed room-defender:
#
#   room-defender <step> <status> <detail>
#
# status is ok (already right), done (changed now), skip, warn or fail. With -Check a step that would change says
# `todo`. The last line is `room-defender done ok` or `room-defender done fail <code>`.
#
# EXIT CODES
#   0  done, skipped, or handed to an administrator (a warn line). A refused path is a warn too
#   1  a local problem: bad arguments, or `local` run elevated without -Runner naming the account
#   2  ssh could not reach the target
#   3  a step failed on the room: GOTMPDIR could not be set, or the exclusions did not read back

param(
    # The room's name, or `local` for this machine.
    [Parameter(Position = 0)] [string] $Room,
    # The ssh destination. Default: the room's own name, as an ssh alias.
    [string] $Target,
    [switch] $Check,
    # The account the room runs as. Checked against who the paths were read as.
    [string] $Runner,
    [string] $Clone,
    [string[]] $WorktreeRoot = @(),
    [string] $Ssh = 'ssh',
    [string[]] $SshOption = @()
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$OutputEncoding = [Text.UTF8Encoding]::new($false)
$SshOption = @($SshOption | ForEach-Object { "$_" -split ',' } | Where-Object { $_ })
$WorktreeRoot = @($WorktreeRoot | ForEach-Object { "$_" -split ',' } | ForEach-Object { $_.Trim() } | Where-Object { $_ })

function Step {
    param([string] $step, [string] $status, [string] $detail = '')
    $line = "room-defender $step $status"
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
function Quote-Ps { param([string] $s) "'" + ($s -replace "['\u2018\u2019\u201A\u201B]", '$0$0') + "'" }

if (-not $Room) { Write-Host 'usage: room-defender.ps1 <room|local> [-Target user@host] [-Check] [-Runner account]'; exit 1 }
if ($Room -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]*$') { Fail 'args' 1 "bad room name '$Room'" }
$local = $Room -eq 'local'
$sshBase = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=25') + $SshOption
$sshTarget = if ($Target) { $Target } else { $Room }
$checkout = Split-Path -Parent $PSScriptRoot

# ── the room ────────────────────────────────────────────────────────────────

# Windows PowerShell 5.1 with -EncodedCommand, over ssh or here, with the room's own PATH: over ssh the registry's,
# here the one this shell already has, then the toolchain's room-env.ps1, which is where a room finds the go that
# room-toolchain.ps1 put.
function Invoke-Room {
    param([string] $script)
    $full = "`$ErrorActionPreference='Continue'; `$ProgressPreference='SilentlyContinue'`n" +
        $(if (-not $local) {
            "`$env:Path = (@([Environment]::GetEnvironmentVariable('Path', 'Machine'), " +
            "[Environment]::GetEnvironmentVariable('Path', 'User')) | Where-Object { `$_ }) -join ';'`n"
        }) +
        "`$e = Join-Path `$HOME '.atrium\toolchain\room-env.ps1'; if (Test-Path `$e) { . `$e }`n" + $script
    $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($full))
    if ($enc.Length -gt 7800) { throw "room script too long for cmd.exe ($($enc.Length))" }
    $cmd = "powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand $enc"
    # BY ITS FULL PATH: a session's PATH need not hold System32's WindowsPowerShell (sg4's agents do not).
    $ps = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    $out = if ($local) { & $ps -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand $enc 2>&1 }
           else { & $Ssh @sshBase $sshTarget $cmd 2>&1 }
    $code = $LASTEXITCODE
    $lines = @($out | ForEach-Object { "$_" } | Where-Object { $_ -notmatch '^#< CLIXML|^<Objs |^</Objs>' })
    [pscustomobject]@{ Out = $lines; Code = $code }
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

# ── ssh ─────────────────────────────────────────────────────────────────────

if ($local) {
    if (-not $IsWindows -and $PSVersionTable.PSEdition -eq 'Core') { Step 'ssh' 'skip' 'this machine is not Windows'; Finish 0 }
    Step 'ssh' 'ok' 'local, no ssh'
} else {
    $probe = & $Ssh @sshBase $sshTarget 'uname -sm' 2>&1
    $code = $LASTEXITCODE
    if ($code -eq 255) { Fail 'ssh' 2 "cannot reach $sshTarget over ssh" $probe }
    if ($code -eq 0 -and "$($probe -join ' ')" -match '^(Linux|Darwin)\s') { Step 'ssh' 'skip' "$sshTarget is not Windows"; Finish 0 }
    Step 'ssh' 'ok' "$sshTarget (windows)"
}

# ── what the room has, as the runner user, in one round trip ────────────────

$r = Invoke-Room @'
$id = [Security.Principal.WindowsIdentity]::GetCurrent()
"user=$($id.Name)"
"elevated=$(([Security.Principal.WindowsPrincipal]$id).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator))"
"host=$env:COMPUTERNAME"
$pl = Get-ItemProperty "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\$($id.User.Value)" -ErrorAction SilentlyContinue
"profile=$($pl.ProfileImagePath)"
$s = Get-Service WinDefend -ErrorAction SilentlyContinue
"defender=$(if ($s) { $s.Status } else { 'missing' })"
$g = Get-Command go -ErrorAction SilentlyContinue
if ($g) {
  $v = @(& $g.Source env GOCACHE GOMODCACHE GOTMPDIR)
  "go=$($g.Source)"; "gocache=$($v[0])"; "gomodcache=$($v[1])"; "gotmpdir=$($v[2])"
}
'@
$kv = ConvertFrom-KeyValue $r.Out
if (-not $kv.user) { Fail 'ssh' 2 "could not read the account on $sshTarget" $r.Out }
$where = if ($local) { 'this machine' } else { $kv.host }

# ── defender ────────────────────────────────────────────────────────────────

if ($kv.defender -ne 'Running') { Step 'defender' 'skip' "Defender is $($kv.defender) on $where, so there is nothing to exclude from"; Finish 0 }
Step 'defender' 'ok' "running on $where"

# ── who ─────────────────────────────────────────────────────────────────────

$elevated = $kv.elevated -eq 'True'
$account = $kv.user
if ($Runner -and ($account -split '\\')[-1] -ne ($Runner -split '\\')[-1]) {
    Fail 'who' 1 "the paths would be read as $account, not $Runner. run this as $Runner"
}
if ($local -and $elevated -and -not $Runner) {
    Fail 'who' 1 "this shell is elevated, so the paths would be $account's. run it unelevated as the account the agents run as, which prints the line for an administrator, or say -Runner $(($account -split '\\')[-1]) if the agents do run as $account"
}
Step 'who' 'ok' "$account, $(if ($elevated) { 'elevated' } else { 'not elevated' })"

# ── paths ───────────────────────────────────────────────────────────────────

function Win { param([string] $p) ($p -replace '/', '\').TrimEnd('\') }

if (-not $Clone) {
    if ($local) {
        # The main checkout, whichever worktree this script was run from.
        $first = (& git -C $checkout worktree list --porcelain 2>$null | Select-Object -First 1)
        $Clone = if ("$first" -match '^worktree (.+)$') { $Matches[1] } else { $checkout }
    } else {
        $url = & git -C $checkout remote get-url $Room 2>$null
        if ($LASTEXITCODE -eq 0 -and $url) {
            $url = "$url".Trim()
            if ($url -match '^[^/:]+:([A-Za-z]:/.*)$') { $Clone = $Matches[1] }
            elseif ($url -match '^ssh://[^/]+/([A-Za-z]:/.*)$') { $Clone = $Matches[1] }
        }
    }
}
if (-not $WorktreeRoot.Count) {
    if ($local) {
        $nw = Join-Path $PSScriptRoot 'new-worktree.ps1'
        if ((Test-Path $nw) -and ((Get-Content -Raw $nw) -match "\[string\]\s*\`$Root\s*=\s*'([^']+)'")) { $WorktreeRoot = @($Matches[1]) }
    } elseif ($Clone) { $WorktreeRoot = @("$Clone-worktrees") }
}

# Why a path cannot be used at all, or nothing.
function Get-PathProblem {
    param([string] $p)
    if ($p -match '^[A-Za-z]:$') { return 'is a drive root' }
    if ($p -notmatch '^[A-Za-z]:\\') { return 'is not a full path on a drive' }
    # The quote, backtick, dollar and semicolon too: an administrator pastes this line, so nothing in it may be able
    # to end the quoting, however well Quote-Ps escapes it.
    if ($p -match "[*?`"<>|~'``$;]" -or $p -match '[\x00-\x1f]') { return 'holds a wildcard, a ~, a quote, or a character no path should' }
    $parts = @($p -split '\\')
    if ($parts -contains '..' -or $parts -contains '.') { return 'holds . or ..' }
    ''
}
# Why a path cannot be a ROOT the others are checked against, or nothing.
function Get-RootProblem {
    param([string] $p)
    $why = Get-PathProblem $p
    if ($why) { return $why }
    $parts = @($p -split '\\' | Where-Object { $_ })
    if ($parts.Count -lt 3) { return 'is a drive root, or less than two folders deep' }
    if ($parts[1] -in 'Windows', 'Program Files', 'Program Files (x86)', 'ProgramData') { return "is under $($parts[0])\$($parts[1])" }
    ''
}
function Test-Inside { param([string] $p, [string] $root) $p.StartsWith($root + '\', [StringComparison]::OrdinalIgnoreCase) }

function Refuse { param([string] $what, [string] $p, [string] $why) Step 'paths' 'warn' "left out $what $p, which $why" }

# THE ROOTS. The profile comes from HKLM by the account's SID, never from its environment, and only the two folders
# in it where Go keeps its caches are roots, not the profile as a whole.
$roots = [Collections.Generic.List[string]]::new()
if (-not $kv.profile) { Step 'paths' 'warn' "could not read $account's profile from the registry, so no Go cache is excluded" }
else {
    $pr = Win $kv.profile
    $why = Get-RootProblem $pr
    if ($why) { Refuse 'the profile' $pr $why } else { $roots.Add("$pr\AppData\Local"); $roots.Add("$pr\go") }
}
$paths = [Collections.Generic.List[string]]::new()
function Add-Path { param([string] $p) if (-not ($paths | Where-Object { $_ -ieq $p })) { $paths.Add($p) } }
# The worktree root and the build folder are roots AND exclusions.
foreach ($w in @($WorktreeRoot) + @(if ($Clone) { Join-Path (Win $Clone) 'build.claude' })) {
    if (-not "$w".Trim()) { continue }
    $w = Win "$w"
    $why = Get-RootProblem $w
    if ($why) { Refuse 'the folder' $w $why } else { $roots.Add($w); Add-Path $w }
}
# THE GO PATHS come from the runner's `go env`, which any agent there can set, so each must be strictly inside a root.
function Test-GoPath {
    param([string] $what, [string] $p)
    $p = Win $p
    $why = Get-PathProblem $p
    if (-not $why -and -not ($roots | Where-Object { Test-Inside $p $_ })) {
        $why = "is not inside $account's AppData\Local or go folder, the worktree root or the build folder"
    }
    if ($why) { Refuse $what $p $why; return '' }
    $p
}
$gocache = if ("$($kv.gocache)".Trim()) { Test-GoPath 'GOCACHE' $kv.gocache } else { '' }
$gomod = if ("$($kv.gomodcache)".Trim()) { Test-GoPath 'GOMODCACHE' $kv.gomodcache } else { '' }
$tmpNow = "$($kv.gotmpdir)".Trim()
$tmpWant = if ($tmpNow) { Test-GoPath 'GOTMPDIR' $tmpNow } elseif ($gocache) { Join-Path $gocache 'tmp' } else { '' }
foreach ($p in $gocache, $gomod, $tmpWant) { if ($p) { Add-Path $p } }

if (-not $kv.go) { Step 'paths' 'warn' "no go for $account on $where, so its caches are not known. install it (room-toolchain.ps1) and run this again" }
if (-not $paths.Count) { Step 'paths' 'skip' 'nothing to exclude'; Finish 0 }
Step 'paths' 'ok' ($paths -join ', ')

# ── gotmpdir ────────────────────────────────────────────────────────────────

if (-not $kv.go) {
    Step 'gotmpdir' 'skip' 'no go'
} elseif (-not $tmpWant) {
    Step 'gotmpdir' 'skip' 'GOTMPDIR, or the GOCACHE it would go inside, was left out above'
} elseif ($tmpNow) {
    Step 'gotmpdir' 'ok' "$tmpNow, already set"
} elseif ($Check) {
    Step 'gotmpdir' 'todo' "would make $tmpWant and set it with go env -w"
} else {
    $r = Invoke-Room ("`$d = $(Quote-Ps $tmpWant); `$go = $(Quote-Ps $kv.go)`n" +
        "New-Item -ItemType Directory -Force -Path `$d | Out-Null`n" +
        "& `$go env -w `"GOTMPDIR=`$d`"`n" +
        "`"gotmpdir=`$(& `$go env GOTMPDIR)`"")
    $now = (ConvertFrom-KeyValue $r.Out).gotmpdir
    if ((Win "$now") -ine (Win $tmpWant)) { Fail 'gotmpdir' 3 "could not set GOTMPDIR for $account" $r.Out }
    Step 'gotmpdir' 'done' "$tmpWant, set with go env -w. go test links its binaries there from now on"
}

# ── exclude ─────────────────────────────────────────────────────────────────

$list = ($paths | ForEach-Object { Quote-Ps $_ }) -join ', '
if ($elevated) {
    if ($Check) {
        $r = Invoke-Room ("`$want = @($list)`n`$have = @((Get-MpPreference).ExclusionPath)`n" +
            "`"missing=`$((@(`$want | Where-Object { `$have -notcontains `$_ })) -join '|')`"")
        $miss = (ConvertFrom-KeyValue $r.Out).missing
        if ($miss) { Step 'exclude' 'todo' "would exclude $($miss -replace '\|', ', ')" } else { Step 'exclude' 'ok' 'every path is already excluded' }
    } else {
        $r = Invoke-Room ("`$want = @($list)`n`$have = @((Get-MpPreference).ExclusionPath)`n" +
            "`$miss = @(`$want | Where-Object { `$have -notcontains `$_ })`n" +
            "if (`$miss.Count) { Add-MpPreference -ExclusionPath `$miss }`n" +
            "`"added=`$(`$miss -join '|')`"`n" +
            "`$have = @((Get-MpPreference).ExclusionPath)`n" +
            "`"left=`$((@(`$want | Where-Object { `$have -notcontains `$_ })) -join '|')`"")
        $x = ConvertFrom-KeyValue $r.Out
        if ($x.left -or -not $r.Out.Count) { Fail 'exclude' 3 "Defender did not take every exclusion on $where" $r.Out }
        if ($x.added) { Step 'exclude' 'done' "excluded $($x.added -replace '\|', ', ')" } else { Step 'exclude' 'ok' 'every path is already excluded' }
    }
} else {
    # PRINTED, NEVER WRITTEN on the room. See the header.
    $said = if ($Check) { 'todo' } else { 'warn' }
    Step 'exclude' $said "$account is not elevated, so nothing was excluded. an administrator on $where pastes the line below into an elevated shell. it holds only the paths listed above"
    Write-Host "    Add-MpPreference -ExclusionPath $list"
}

Finish 0
