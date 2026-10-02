# Does a room meet a project's requirements? Run from THIS side, the hub's.
#
#   pwsh -File scripts\room-check.ps1 sg3
#   pwsh -File scripts\room-check.ps1 m1mini -Project D:\git\github\me\other
#   pwsh -File scripts\room-check.ps1 sg3 -Fix
#   pwsh -File scripts\room-check.ps1 sg3 -Fix -Yes         # clint's alone on a real room
#
# It reads the project's atrium.requirements.yaml (docs/fabric/room-requirements-design.md), checks what the room has against
# it, and with -Fix does what a script can do about the rest. The requirements are the design's section 4 table, one
# `room-check` line each.
#
# THE FILE IS PARSED BY THE BINARY, `atrium requirements <file> --json`, and never by this script. The binary is -Binary,
# else the running hub's own, else one built from this checkout into build.claude/. A binary that does not know the
# subcommand is not used.
#
# IT CALLS, IT DOES NOT REIMPLEMENT
#   toolchain on disk   room-toolchain.ps1 -Check           (and, with -Fix, room-toolchain.ps1)
#   clone, mirror       room-git.ps1 init <room> -Check      (and, with -Fix, room-git.ps1 init). A room-git that has no
#                       -Check yet gives `skip`, never a crash
#   hub-main fresh      git ls-remote, then room-git.ps1 push-base with -Fix
#   permission gate     room-gate.ps1 -Check
#   smoke               provision-room.ps1 -SmokeOnly        (LAUNCHES a small card on the room, which exits itself.
#                                                             -NoSmoke leaves it out)
#   the room's runners  GET /v1/harnesses through the hub, X-Atrium-Room: <room>
#   atrium hooks        that row's setup check, and with -Fix -Yes POST /v1/hooks/install
#   account             who the room runs as (the ssh login), by the same read-only probe provision-room.ps1 and
#                       room-toolchain.ps1 use (scripts/room-account.ps1). ok, or warn naming the reason: an
#                       administrator (Windows token, Administrators, Domain Admins, root, admin, sudo, wheel,
#                       passwordless sudo) or the operator's own everyday account. Never a failure, never fixed, and it
#                       does not move the exit code. -IAcceptRunningAsMe makes the warn an ok that still names the
#                       reason, -OperatorAccount lists accounts that are the operator's. See docs/room-accounts.md
#   allowed-folders     `atrium room folders list --json` on the room (room-folders.ps1). ok with the roots when the list is
#                       enforced, warn "no allowed folders set" when it is empty, skip when that atrium has no such verb.
#                       With -Fix -Yes, `folders allow <clone> <clone>-worktrees`: a room's own setting, but it turns the
#                       launch bound on, so it needs -Yes like the hooks do
#
# WITHOUT -Fix IT WRITES NOTHING, on the room or here. (The smoke card is the one thing that runs.) With -Fix it does
# every `apply` fix whose scope is machine or room. A fix at ACCOUNT scope (the hooks, which change every project and
# every room on that account) and any RESTART need -Yes as well, and on a real room -Yes is clint's alone. Before
# either, the change is printed, with the file's backup path.
#
# ROWS THAT NEED WHAT IS NOT BUILT are `skip` and say what they wait for: the state dir, sign-in, `survives`, env,
# services. A tool measured from ssh says so: `warn measured from ssh, not from the room`, unless the room answers
# POST /v1/preflight through the hub, in which case its answer is used.
#
# ONE LINE PER REQUIREMENT
#
#   room-check <requirement> <status> <detail>
#
# status is ok, done (fixed now), warn, fail, skip, or human. `human` is a failure only a person can fix, and its detail
# is the exact command and who runs it. The last line is `room-check done ok` or `room-check done fail <code>`.
#
# EXIT CODES
#   0  met
#   1  a local problem: bad arguments, a bad requirements file, no hub, no binary
#   2  ssh could not reach the target
#   3  unmet, and -Fix could fix it
#   4  unmet, and a human is needed
#   5  met except a restart it was not allowed to do
#
# SCOPE. machine facts (toolchain on disk, clone, hub-main, worktrees, helpers) are shared by every room on the machine.
# account facts (hooks, gate) by every room on the account: a room on a host another attached room also uses gets
# `warn`, and its hooks are never rewritten toward the room being checked. room facts belong to the room named.

param(
    [Parameter(Position = 0)] [string] $Room,
    # The project whose file and history this is. Default: the checkout this script sits in.
    [string] $Project,
    # The requirements file. Default: <Project>/atrium.requirements.yaml.
    [string] $File,
    [switch] $Fix,
    [switch] $Yes,
    # The ssh destination. Default: the host the room's git remote already names, else the room's own name.
    [string] $Target,
    [switch] $NoSmoke,
    [int] $SmokeTimeout = 180,
    # The hub. Default: read from the running `atrium run` process, else 127.0.0.1:7778.
    [string] $HubAddr,
    # An atrium that has the `requirements` subcommand.
    [string] $Binary,
    [string] $Ssh = 'ssh',
    [string[]] $SshOption = @(),
    # The account row, see above.
    [string[]] $OperatorAccount = @(),
    [switch] $IAcceptRunningAsMe
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$OutputEncoding = [Text.UTF8Encoding]::new($false)
function Split-List { param($v) @($v | ForEach-Object { "$_" -split ',' } | ForEach-Object { $_.Trim() } | Where-Object { $_ }) }
$SshOption = Split-List $SshOption
. (Join-Path $PSScriptRoot 'room-account.ps1')
$operators = @(Get-OperatorList (Split-List $OperatorAccount))
foreach ($o in $operators) { $why = Test-OperatorArg $o; if ($why) { Write-Host "room-check args fail $why"; exit 1 } }

$script:unmet = @{ fixable = 0; human = 0; restart = 0 }

function Row {
    param([string] $req, [string] $status, [string] $detail = '')
    $line = "room-check $req $status"
    if ($detail) { $line += " $detail" }
    Write-Host $line
}
function Note { param([string] $text) Write-Host "    $text" }
function Finish {
    param([int] $code)
    if ($code -eq 0) { Row 'done' 'ok' } else { Row 'done' 'fail' "$code" }
    exit $code
}
function Fail-Now { param([string] $req, [int] $code, [string] $detail) Row $req 'fail' $detail; Finish $code }

# Unmet records a requirement that is not met and how it can be met: fixable (-Fix does it), human, or restart.
function Unmet { param([string] $kind) $script:unmet[$kind]++ }

if (-not $Room) {
    Write-Host 'usage: room-check.ps1 <room> [-Project <checkout>] [-File <yaml>] [-Fix] [-Yes] [-Target user@host] [-NoSmoke]'
    exit 1
}
if ($Room -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]*$') { Fail-Now 'args' 1 "bad room name '$Room'" }
if ($Yes -and -not $Fix) { Fail-Now 'args' 1 '-Yes goes with -Fix' }

. (Join-Path $PSScriptRoot 'room-folders.ps1')
$checkout = Split-Path -Parent $PSScriptRoot
if (-not $Project) { $Project = $checkout }
if (-not (Test-Path -LiteralPath (Join-Path $Project '.git'))) { Fail-Now 'project' 1 "$Project is not a git checkout" }
$Project = (Resolve-Path -LiteralPath $Project).Path
if (-not $File) { $File = Join-Path $Project 'atrium.requirements.yaml' }
if (-not (Test-Path -LiteralPath $File)) { Fail-Now 'file' 1 "no requirements file at $File" }
$isWin = $IsWindows -or $env:OS -eq 'Windows_NT'
$sshBase = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=25') + $SshOption
$childSsh = @('-Ssh', $Ssh) + $(if ($SshOption.Count) { @('-SshOption', ($SshOption -join ',')) } else { @() })

# ── the hub, which is this machine ──────────────────────────────────────────

function Find-Hub {
    $lines = @()
    if ($isWin) {
        $lines = @(Get-CimInstance Win32_Process -Filter "Name='atrium.exe'" |
            Where-Object { $_.CommandLine -match '\s+run(\s|$)' } |
            ForEach-Object { [pscustomobject]@{ Cmd = $_.CommandLine; Exe = $_.ExecutablePath } })
    } else {
        $lines = @(& ps -eo args 2>$null | Where-Object { $_ -match '(^|/)atrium\s+run(\s|$)' } |
            ForEach-Object { [pscustomobject]@{ Cmd = $_; Exe = ($_ -split '\s+')[0] } })
    }
    $flag = {
        param($cmd, $n)
        if ($cmd -and $cmd -match "--$n[ =](?:`"([^`"]*)`"|(\S+))") { if ($Matches[1]) { $Matches[1] } else { $Matches[2] } } else { $null }
    }
    $hubs = @($lines | ForEach-Object { [pscustomobject]@{ Exe = $_.Exe; Addr = (& $flag $_.Cmd 'addr') } })
    if ($HubAddr) { $hubs = @($hubs | Where-Object { $_.Addr -eq $HubAddr }) }
    $hubs | Select-Object -First 1
}
$hub = Find-Hub
if (-not $HubAddr) { $HubAddr = if ($hub -and $hub.Addr) { $hub.Addr } else { '127.0.0.1:7778' } }
$hdr = @{ 'X-Atrium-Room' = $Room }

# ── the file, parsed by the binary ──────────────────────────────────────────

function Test-Binary {
    param([string] $exe)
    if (-not $exe -or -not (Test-Path -LiteralPath $exe)) { return $false }
    & $exe requirements --help *>$null
    $LASTEXITCODE -eq 0
}
$bin = $null
foreach ($c in @($Binary, $(if ($hub) { $hub.Exe }), (Get-Command atrium -ErrorAction SilentlyContinue).Source)) {
    if (Test-Binary $c) { $bin = $c; break }
}
if (-not $bin -and (Test-Path (Join-Path $checkout 'go.mod'))) {
    $built = Join-Path $checkout ('build.claude/atrium-room-check' + $(if ($isWin) { '.exe' } else { '' }))
    Push-Location $checkout
    try { & go build -o $built ./cmd/atrium *>$null } finally { Pop-Location }
    if (Test-Binary $built) { $bin = $built }
}
if (-not $bin) { Fail-Now 'binary' 1 'no atrium with the requirements subcommand: pass -Binary, or build one with make build' }

$json = & $bin requirements $File --json 2>&1
if ($LASTEXITCODE -ne 0) {
    Row 'file' 'fail' "$File is not a valid requirements file"
    $json | ForEach-Object { Note "$_" }
    Finish 1
}
$req = ($json -join "`n") | ConvertFrom-Json -AsHashtable
Row 'file' 'ok' "$File, version $($req.version), read by $bin"

# ── the room, as the hub knows it ───────────────────────────────────────────

$rooms = @()
try { $rooms = @((Invoke-RestMethod -Uri "http://$HubAddr/_hub/rooms" -TimeoutSec 10).rooms) }
catch { Fail-Now 'hub' 1 "http://$HubAddr/_hub/rooms did not answer: $($_.Exception.Message)" }
$info = $rooms | Where-Object { $_.name -eq $Room } | Select-Object -First 1
$roomOS = $null

# owner and repo, from the project's origin, for the templates
$origin = (& git -C $Project remote get-url origin 2>$null | Select-Object -First 1)
$owner = $null; $repoName = $null
if ($origin -match '[:/]([^/:]+)/([^/]+?)(\.git)?\s*$') { $owner = $Matches[1]; $repoName = $Matches[2] }

# The room's git remote in the project: where room-git put the clone, and the host that answers ssh.
$roomUrl = $null
$ru = (& git -C $Project remote get-url $Room 2>$null | Select-Object -First 1)
if ($LASTEXITCODE -eq 0 -and $ru) {
    $ru = "$ru".Trim()
    if ($ru -match '^([^/:]+):([A-Za-z]:/.*)$') { $roomUrl = [pscustomobject]@{ Host = $Matches[1]; Path = $Matches[2]; OS = 'windows' } }
    elseif ($ru -match '^ssh://([^/]+)(/.*)$') {
        $p = $Matches[2]; $h = $Matches[1]
        if ($p -match '^/[A-Za-z]:/') { $roomUrl = [pscustomobject]@{ Host = $h; Path = $p.Substring(1); OS = 'windows' } }
        else { $roomUrl = [pscustomobject]@{ Host = $h; Path = $p; OS = 'unix' } }
    }
}
if (-not $Target) { $Target = if ($roomUrl -and $roomUrl.Host) { $roomUrl.Host } else { $Room } }

# ── 1. attached ─────────────────────────────────────────────────────────────

if ($info) {
    $roomOS = if ($info.os -eq 'windows') { 'windows' } else { 'unix' }
    Row 'attached' 'ok' "$Room on $($info.host), $($info.os) $($info.arch), since $($info.since)"
} else {
    Row 'attached' 'human' "$Room is not attached to the hub at $HubAddr. run: pwsh -File scripts\provision-room.ps1 $Target -Name $Room (the operator, once)"
    Unmet 'human'
    $roomOS = if ($roomUrl) { $roomUrl.OS } else { $null }
}

# ── talking to the remote ───────────────────────────────────────────────────

function Quote-Ps { param([string] $s) "'" + ($s -replace "['\u2018\u2019\u201A\u201B]", '$0$0') + "'" }
function Quote-Sh { param([string] $s) "'" + ($s -replace "'", "'\''") + "'" }
function ConvertFrom-KeyValue {
    param($lines)
    $h = @{}
    foreach ($l in $lines) {
        $s = "$l"; $i = $s.IndexOf('=')
        if ($i -gt 0) { $h[$s.Substring(0, $i).Trim()] = $s.Substring($i + 1).TrimEnd() }
    }
    $h
}

# Windows gets -EncodedCommand (cmd, Windows PowerShell and pwsh all pass one token untouched), with the room's own
# toolchain in front of PATH so `git` is the git the room runs. Unix gets the script on stdin, under the PATH a login
# shell has. Over cmd.exe's line limit the script is deflated and a short loader inflates it.
function Invoke-Remote {
    param([string] $script)
    if ($script:remoteOS -eq 'windows') {
        $full = "`$ErrorActionPreference='Stop'; `$ProgressPreference='SilentlyContinue'`n" +
            "`$env:Path = (@([Environment]::GetEnvironmentVariable('Path', 'Machine'), [Environment]::GetEnvironmentVariable('Path', 'User')) | Where-Object { `$_ }) -join ';'`n" +
            "`$e = Join-Path `$HOME '.atrium\toolchain\room-env.ps1'; if (Test-Path `$e) { . `$e }`n" + $script
        $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($full))
        if ($enc.Length -gt 7800) {
            $ms = New-Object IO.MemoryStream
            $ds = New-Object IO.Compression.DeflateStream($ms, [IO.Compression.CompressionMode]::Compress, $true)
            $bytes = [Text.Encoding]::UTF8.GetBytes($full); $ds.Write($bytes, 0, $bytes.Length); $ds.Dispose()
            $loader = "`$m = New-Object IO.MemoryStream(,[Convert]::FromBase64String('$([Convert]::ToBase64String($ms.ToArray()))')); " +
                "`$r = New-Object IO.StreamReader((New-Object IO.Compression.DeflateStream(`$m, [IO.Compression.CompressionMode]::Decompress)), [Text.Encoding]::UTF8); Invoke-Expression `$r.ReadToEnd()"
            $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($loader))
            if ($enc.Length -gt 7800) { throw "remote script too long for cmd.exe ($($enc.Length))" }
        }
        $out = & $Ssh @sshBase $Target "powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand $enc" 2>&1
    } else {
        $full = "lp=`$(`"`${SHELL:-/bin/sh}`" -lc 'printf %s `"`$PATH`"' 2>/dev/null); [ -n `"`$lp`" ] && PATH=`"`$lp:`$PATH`"`n" + $script
        $full = ($full -replace "`r", '') + "`n#"
        $out = $full | & $Ssh @sshBase $Target 'sh -s' 2>&1
    }
    $lines = @($out | ForEach-Object { "$_" } | Where-Object { $_ -notmatch '^#< CLIXML|^<Objs |^</Objs>' })
    [pscustomobject]@{ Out = $lines; Code = $LASTEXITCODE }
}

# The probe that decides exit 2. It needs no shell: `uname` on Unix, anything else is tried as Windows.
$probe = & $Ssh @sshBase $Target 'uname -sm' 2>&1
if ($LASTEXITCODE -eq 255) { Fail-Now 'ssh' 2 "cannot reach $Target over ssh" }
$probeText = ($probe | ForEach-Object { "$_" }) -join ' '
$script:remoteOS = if ($probeText -match '^(Linux|Darwin)\s') { 'unix' } else { 'windows' }
if (-not $roomOS) { $roomOS = $script:remoteOS }
$kind = if ($probeText -match '^Darwin') { 'darwin' } elseif ($probeText -match '^Linux') { 'linux' } else { 'windows' }
Row 'ssh' 'ok' "$Target ($kind)"

# who the room runs as: advice, so ok or warn and never counted as unmet
$ar = Invoke-AccountProbe $script:remoteOS $Ssh $sshBase $Target
$av = Get-AccountResult $ar.Out $ar.Code $(if ($kind -eq 'darwin') { 'mac' } else { $kind }) $Target (Get-LocalIdentity) $operators $false ([bool]$IAcceptRunningAsMe) $false
Row 'account' $av.Status $av.Detail

$hs = if ($script:remoteOS -eq 'windows') { '"home=$($HOME -replace ''\\'', ''/'')"' } else { 'echo "home=$HOME"' }
$remoteHome = ((ConvertFrom-KeyValue (Invoke-Remote $hs).Out).home)
if ($remoteHome) { $remoteHome = $remoteHome.TrimEnd('/') }

# Run a sibling script and hand its lines back as they are said.
function Invoke-Script {
    param([string] $name, [string[]] $scriptArgs)
    $out = & pwsh -NoProfile -File (Join-Path $PSScriptRoot $name) @scriptArgs 2>&1
    [pscustomobject]@{ Out = @($out | ForEach-Object { "$_" }); Code = $LASTEXITCODE }
}

function Compare-Ver {
    param([string] $have, [string] $min)
    $pad = { param($v) $p = @(($v -split '\.') + '0', '0', '0'); [version]("$($p[0]).$($p[1]).$($p[2])") }
    (& $pad $have).CompareTo((& $pad $min))
}

# ── 2. the binary floor ─────────────────────────────────────────────────────

if ($req.atrium) {
    $min = $req.atrium.min
    $v = if ($info) { "$($info.version)" } else { '' }
    if (-not $info) { Row 'binary' 'skip' 'the room is not attached, so it has no version' }
    elseif (-not $v -or $v -eq 'dev') { Row 'binary' 'warn' "cannot tell: the room says its version is '$(if ($v) { $v } else { 'nothing' })', and the floor is $min" }
    else {
        $sha = (& git -C $Project rev-parse --verify -q "$v^{commit}" 2>$null | Select-Object -First 1)
        if (-not $sha) { Row 'binary' 'warn' "cannot tell: the room's version $v is not a commit here, and the floor is $min" }
        else {
            & git -C $Project merge-base --is-ancestor $min $sha 2>$null
            if ($LASTEXITCODE -eq 0) { Row 'binary' 'ok' "$v has $min as an ancestor" }
            else {
                Row 'binary' 'human' "$v does not have $min in its history. run: pwsh -File scripts\provision-room.ps1 $Target -Name $Room -FromCheckout, then -Restart -Yes (clint, a restart is a real room's)"
                Unmet 'human'
            }
        }
    }
}

# ── 3. toolchain, on disk ───────────────────────────────────────────────────

$known = 'git', 'pwsh', 'go', 'node'
$wantTools = @()
foreach ($t in ($req.toolchain.Keys | Sort-Object)) {
    $spec = $req.toolchain[$t]
    if ($spec.os -and @($spec.os) -notcontains $kind) { Row "toolchain.$t" 'skip' "only on $(@($spec.os) -join ', ')"; continue }
    $wantTools += $t
}
$script:toolNeedsRestart = $false
function Get-ToolLines {
    param([string[]] $tools, [switch] $Check)
    $a = @($Target) + $(if ($Check) { '-Check' } else { @() }) + @('-Tools', ($tools -join ',')) + $childSsh
    $goSpec = $req.toolchain['go']
    if ($goSpec -and $goSpec.from -eq 'go.mod' -and ($tools -contains 'go')) {
        $gm = Join-Path $Project 'go.mod'
        if ((Test-Path $gm) -and ((Get-Content $gm -Raw) -match '(?m)^go\s+(\d+\.\d+(\.\d+)?)')) { $a += @('-GoVersion', $Matches[1]) }
    }
    Invoke-Script 'room-toolchain.ps1' $a
}
$installable = @($wantTools | Where-Object { $_ -in $known -and -not ($_ -in 'git', 'pwsh' -and $roomOS -ne 'windows' -and $_ -eq 'pwsh') })
$failedTools = @()
if ($installable.Count) {
    $r = Get-ToolLines $installable -Check
    if ($r.Code -eq 2) { Fail-Now 'toolchain' 2 "room-toolchain could not reach $Target" }
    $says = @{}
    foreach ($l in $r.Out) { if ($l -match '^room-toolchain (\S+) (\S+)\s*(.*)$') { $says[$Matches[1]] = @{ status = $Matches[2]; detail = $Matches[3] } } }
    foreach ($t in $installable) {
        $s = $says[$t]
        if (-not $s) {
            # room-toolchain says nothing about git on a machine that is not Windows
            if ($t -eq 'git') { continue }
            Row "toolchain.$t" 'warn' 'room-toolchain did not report it'
            continue
        }
        if ($s.status -in 'ok', 'done', 'skip') { Row "toolchain.$t" $s.status $s.detail }
        elseif ($s.status -eq 'warn') { Row "toolchain.$t" 'fail' "$($s.detail)"; $failedTools += $t }
        else { Row "toolchain.$t" 'fail' "$($s.detail)"; $failedTools += $t }
    }
    if ($r.Code -ne 0 -and -not $failedTools.Count) { Row 'toolchain' 'fail' "room-toolchain -Check exited $($r.Code)"; Unmet 'human' }
}
# git on a machine room-toolchain does not cover, and a key it has no installer for, are measured here and check-only.
foreach ($t in $wantTools) {
    if ($t -in $installable -and -not ($t -eq 'git' -and $roomOS -ne 'windows')) { continue }
    if ($t -eq 'pwsh' -and $roomOS -ne 'windows') { continue }
    $cmd = switch ($t) { 'git' { 'git --version' } 'go' { 'go version' } 'node' { 'node --version' } default { "command -v $t" } }
    $sc = if ($script:remoteOS -eq 'windows') { "`$c = Get-Command $($t -replace '[^A-Za-z0-9._-]','') -EA SilentlyContinue | Select-Object -First 1; if (`$c) { 'found=' + `$c.Source } else { exit 3 }" }
          else { "p=`$(command -v $($t -replace '[^A-Za-z0-9._-]','')) || exit 3; echo `"found=`$p`"; $($cmd -replace '^command -v.*','true') 2>&1 | head -1 | sed 's/^/ver=/'" }
    $x = Invoke-Remote $sc
    $kv = ConvertFrom-KeyValue $x.Out
    $spec = $req.toolchain[$t]
    if ($x.Code -ne 0 -or -not $kv.found) {
        Row "toolchain.$t" 'human' "$t is not on $Target. install it there (the operator), room-toolchain has no installer for it"
        Unmet 'human'
    } elseif ($t -eq 'git' -and $spec.min -and $kv.ver -match '(\d+\.\d+(\.\d+)?)' -and (Compare-Ver $Matches[1] $spec.min) -lt 0) {
        Row "toolchain.$t" 'human' "$($kv.ver) at $($kv.found) is older than $($spec.min). update it there (the operator)"
        Unmet 'human'
    } else {
        Row "toolchain.$t" 'ok' "$(if ($kv.ver) { $kv.ver } else { 'present' }) at $($kv.found)"
    }
}
if ($failedTools.Count) {
    if ($Fix) {
        $f = Get-ToolLines $failedTools
        $fsays = @{}
        foreach ($l in $f.Out) { if ($l -match '^room-toolchain (\S+) (\S+)\s*(.*)$') { $fsays[$Matches[1]] = @{ status = $Matches[2]; detail = $Matches[3] } } }
        foreach ($t in $failedTools) {
            $s = $fsays[$t]
            if ($s -and $s.status -in 'ok', 'done') { Row "toolchain.$t" 'done' $s.detail; $script:toolNeedsRestart = $true }
            else { Row "toolchain.$t" 'fail' "room-toolchain could not install it: $(if ($s) { $s.detail } else { "exit $($f.Code)" })"; Unmet 'human' }
        }
    } else {
        foreach ($t in $failedTools) { Unmet 'fixable' }
        Note "-Fix runs: pwsh -File scripts\room-toolchain.ps1 $Target -Tools $($failedTools -join ',')"
    }
}

# ── 4. toolchain, on the room's PATH ────────────────────────────────────────

$pre = $null
if ($info -and $wantTools.Count) {
    try {
        $pre = Invoke-RestMethod -Method Post -Uri "http://$HubAddr/v1/preflight" -Headers $hdr -ContentType 'application/json' `
            -Body (@{ tools = @($wantTools) } | ConvertTo-Json -Compress) -TimeoutSec 70
        if (-not $pre.tools) { $pre = $null }
    } catch { $pre = $null }
}
if ($pre) {
    foreach ($t in $wantTools) {
        $it = $pre.tools.$t
        $spec = $req.toolchain[$t]
        if (-not $it) { Row "toolchain-path.$t" 'warn' 'the room did not answer for it'; continue }
        $vv = if ("$($it.output)" -match '(\d+\.\d+(\.\d+)?)') { $Matches[1] } else { $null }
        $why = $null
        if (-not $it.ok) { $why = "not on the room's PATH: $($it.error)" }
        elseif ($spec.windows -eq 'git-for-windows' -and $roomOS -eq 'windows' -and "$($it.output)" -notmatch 'windows') { $why = "the room's git is not Git for Windows: $($it.output) at $($it.path)" }
        elseif ($spec.min -and $vv -and (Compare-Ver $vv $spec.min) -lt 0) { $why = "$vv is older than $($spec.min)" }
        if ($why) { Row "toolchain-path.$t" 'fail' "$why. it needs the room restarted after the record changes"; Unmet 'restart' }
        else { Row "toolchain-path.$t" 'ok' "$($it.output.Trim()) at $($it.path), as the room sees it (pid $($pre.pid))" }
    }
} elseif ($wantTools.Count) {
    Row 'toolchain-path' 'warn' "measured from ssh, not from the room: POST /v1/preflight through the hub did not answer for $Room"
}

# ── 5. the clone and hub-main ───────────────────────────────────────────────

$gitReq = $req.git
$clonePath = if ($roomUrl -and $roomUrl.Path) { $roomUrl.Path } else { $null }
$defaultClone = '{home}/git/github/{owner}/{repo}'
if (-not $gitReq) {
    Row 'clone' 'skip' 'the file has no git section'
} else {
    if (-not $clonePath -and $remoteHome -and $owner) {
        $clonePath = $gitReq.clone.Replace('{home}', $remoteHome).Replace('{owner}', $owner).Replace('{repo}', $repoName)
    }
    $rg = Join-Path $PSScriptRoot 'room-git.ps1'
    $hasCheck = (Select-String -LiteralPath $rg -Pattern '\[switch\]\s*\$Check' -Quiet)
    if (-not $hasCheck) {
        Row 'clone' 'skip' 'room-git has no -Check yet'
    } else {
        $ga = @('init', $Room, '-Check', '-Target', $Target, '-Repo', $Project) + $childSsh
        if ($gitReq.clone -ne $defaultClone) { $ga += @('-Path', $gitReq.clone.Replace('{home}', '~').Replace('{owner}', $owner).Replace('{repo}', $repoName)) }
        $r = Invoke-Script 'room-git.ps1' $ga
        $facts = @($r.Out | Where-Object { $_ -match '^room-git (\S+) (\S+)\s*(.*)$' -and $Matches[1] -notin 'done', 'ssh' } |
            ForEach-Object { if ($_ -match '^room-git (\S+) (\S+)\s*(.*)$') { [pscustomobject]@{ Step = $Matches[1]; Status = $Matches[2]; Detail = $Matches[3] } } })
        $said = ($facts | ForEach-Object { "$($_.Step): $($_.Detail)" }) -join '; '
        switch ($r.Code) {
            0 { Row 'clone' 'ok' $said }
            2 { Fail-Now 'clone' 2 "room-git could not reach $Target" }
            3 {
                $notFresh = @($facts | Where-Object { $_.Status -notin 'ok', 'done', 'skip' -and $_.Step -ne 'fresh' })
                if ($Fix -and -not $notFresh.Count) {
                    Row 'clone' 'fail' "$said. only hub-main is behind, so the hub-main row below is the one that fixes it"
                } elseif ($Fix) {
                    $f = Invoke-Script 'room-git.ps1' (@('init', $Room, '-Target', $Target, '-Repo', $Project) + $childSsh + $(if ($ga -contains '-Path') { @('-Path', $ga[$ga.IndexOf('-Path') + 1]) } else { @() }))
                    if ($f.Code -eq 0) { Row 'clone' 'done' 'room-git init made it' }
                    else { Row 'clone' 'fail' "room-git init exited $($f.Code): $(($f.Out | Select-Object -Last 2) -join ' | ')"; Unmet 'human' }
                } else { Row 'clone' 'fail' "$said. -Fix runs: room-git.ps1 init $Room"; Unmet 'fixable' }
            }
            default { Row 'clone' 'human' "$said. a person has to look: room-git.ps1 init $Room -Check names why (the operator)"; Unmet 'human' }
        }
    }

    # hub-main fresh, on this side: the room's remote branch against the base here
    if ($gitReq.fresh) {
        $base = $gitReq.base; $mirror = $gitReq.mirror
        $want = (& git -C $Project rev-parse --verify -q "refs/heads/$base^{commit}" 2>$null | Select-Object -First 1)
        if (-not $roomUrl) { Row 'hub-main' 'skip' "the project has no git remote called $Room yet, so the clone row covers it" }
        elseif (-not $want) { Row 'hub-main' 'warn' "no branch $base here to compare against" }
        else {
            $env:GIT_SSH_COMMAND = (@($Ssh) + $sshBase | ForEach-Object { if ($_ -match '\s') { "'$_'" } else { $_ } }) -join ' '
            $ls = & git -C $Project ls-remote $Room "refs/heads/$mirror" 2>&1
            if ($LASTEXITCODE -ne 0) { Row 'hub-main' 'fail' "could not read $Room over ssh: $(($ls | Select-Object -First 1))"; Unmet 'human' }
            else {
                $have = if (@($ls).Count -gt 0) { ("$(@($ls)[0])" -split '\s+')[0] } else { '' }
                if ($have -eq $want) { Row 'hub-main' 'ok' "$mirror on $Room is $($want.Substring(0, 9)), which is $base" }
                else {
                    $now = if ($have) { $have.Substring(0, 9) } else { 'not there' }
                    if ($Fix) {
                        $f = Invoke-Script 'room-git.ps1' (@('push-base', $Room, '-From', $base, '-Target', $Target, '-Repo', $Project) + $childSsh)
                        if ($f.Code -eq 0) { Row 'hub-main' 'done' "$mirror on $Room is now $($want.Substring(0, 9)) ($base)" }
                        else { Row 'hub-main' 'fail' "push-base exited $($f.Code): $(($f.Out | Select-Object -Last 2) -join ' | ')"; Unmet 'human' }
                    } else {
                        Row 'hub-main' 'fail' "$mirror on $Room is $now and $base here is $($want.Substring(0, 9)). -Fix runs: room-git.ps1 push-base $Room -From $base"
                        Unmet 'fixable'
                    }
                }
            }
        }
    }

    # worktree gitfiles: every one a path the git the room runs can read
    if (-not $clonePath) { Row 'worktrees' 'skip' 'no clone path is known yet' }
    else {
        $sc = if ($script:remoteOS -eq 'windows') {
@"
`$clone = $(Quote-Ps ($clonePath -replace '/', '\'))
if (-not (Test-Path -LiteralPath `$clone)) { 'noclone=1'; exit 0 }
`$g = (Get-Command git -ErrorAction SilentlyContinue).Source
'git=' + `$g + ' ' + (& git --version)
`$first = `$true
foreach (`$l in (& git -C `$clone worktree list --porcelain)) {
    if ("`$l" -like 'worktree *') {
        `$w = "`$l".Substring(9)
        if (`$first) { `$first = `$false; continue }
        'wt=' + `$w
        `$gf = Join-Path `$w '.git'
        if (Test-Path -LiteralPath `$gf -PathType Leaf) { 'gf=' + `$w + '|' + ((Get-Content -LiteralPath `$gf -TotalCount 1) -replace '^gitdir:\s*', '') }
        elseif (-not (Test-Path -LiteralPath `$w)) { 'gone=' + `$w }
    }
}
"@
        } else {
@"
clone=$(Quote-Sh $clonePath)
[ -d "`$clone" ] || { echo noclone=1; exit 0; }
echo "git=`$(command -v git) `$(git --version)"
first=1
git -C "`$clone" worktree list --porcelain | while IFS= read -r l; do
  case "`$l" in
    "worktree "*)
      w="`${l#worktree }"
      if [ "`$first" = 1 ]; then first=0; continue; fi
      echo "wt=`$w"
      if [ -f "`$w/.git" ]; then echo "gf=`$w|`$(head -n 1 "`$w/.git" | sed 's/^gitdir: *//')"
      elif [ ! -e "`$w" ]; then echo "gone=`$w"; fi ;;
  esac
done
"@
        }
        $x = Invoke-Remote $sc
        $wt = @($x.Out | Where-Object { $_ -like 'wt=*' } | ForEach-Object { $_.Substring(3) })
        $gfs = @($x.Out | Where-Object { $_ -like 'gf=*' } | ForEach-Object { $_.Substring(3) })
        $gone = @($x.Out | Where-Object { $_ -like 'gone=*' } | ForEach-Object { $_.Substring(5) })
        $gitLine = ($x.Out | Where-Object { $_ -like 'git=*' } | Select-Object -First 1)
        if ($x.Out -contains 'noclone=1') { Row 'worktrees' 'skip' "no clone at $clonePath yet" }
        elseif ($x.Code -ne 0 -and -not $gitLine) { Row 'worktrees' 'fail' "could not list them: $(($x.Out | Select-Object -Last 1))"; Unmet 'human' }
        else {
            $bad = @()
            foreach ($g in $gfs) {
                $w, $d = $g -split '\|', 2
                $native = if ($script:remoteOS -eq 'windows') { $d -match '^[A-Za-z]:[\\/]' } else { $d -match '^/' -and $d -notmatch '^/cygdrive/' }
                if (-not $native) { $bad += "$w gitdir is $d" }
            }
            foreach ($w in $wt) {
                $native = if ($script:remoteOS -eq 'windows') { $w -match '^[A-Za-z]:[\\/]' } else { $w -match '^/' -and $w -notmatch '^/cygdrive/' }
                if (-not $native) { $bad += "worktree path $w is not native" }
            }
            foreach ($w in $gone) { if (-not ($bad | Where-Object { $_ -like "*$w*" })) { $bad += "$w is gone" } }
            if ($bad.Count -gt 4) { $bad = @($bad | Select-Object -First 3) + "and $($bad.Count - 3) more" }
            $gitWord = if ($gitLine) { ($gitLine.Substring(4)) } else { 'git' }
            if (-not $bad.Count) { Row 'worktrees' 'ok' "$($wt.Count) worktrees, every gitfile native, read with $gitWord" }
            elseif ($Fix) {
                $rep = if ($script:remoteOS -eq 'windows') { "`$clone = $(Quote-Ps ($clonePath -replace '/', '\')); & git -C `$clone worktree repair; & git -C `$clone worktree prune; 'ok=1'" }
                       else { "git -C $(Quote-Sh $clonePath) worktree repair; git -C $(Quote-Sh $clonePath) worktree prune; echo ok=1" }
                $f = Invoke-Remote $rep
                if ((ConvertFrom-KeyValue $f.Out).ok) { Row 'worktrees' 'done' "git worktree repair and prune, with $gitWord. was: $($bad -join '; ')" }
                else { Row 'worktrees' 'fail' "repair did not run: $(($f.Out | Select-Object -Last 1))"; Unmet 'human' }
            } else {
                Row 'worktrees' 'fail' "$($bad -join '; '). -Fix runs git worktree repair and prune in the clone, with $gitWord"
                Unmet 'fixable'
            }
        }
    }
}

# ── 6. the runners ──────────────────────────────────────────────────────────

$rows = @()
if ($info) {
    try {
        $hr = Invoke-RestMethod -Uri "http://$HubAddr/v1/harnesses" -Headers $hdr -TimeoutSec 15
        $rows = @(@($hr) + @($hr.harnesses) | Where-Object { $_ -and $_.id })
    } catch { Row 'runners' 'fail' "the room's /v1/harnesses did not answer through the hub: $($_.Exception.Message)"; Unmet 'human' }
}
$sameHost = @($rooms | Where-Object { $info -and $_.name -ne $Room -and $_.host -eq $info.host })

foreach ($rn in ($req.runners.Keys | Sort-Object)) {
    $spec = $req.runners[$rn]
    $row = $rows | Where-Object { $_.id -eq $rn } | Select-Object -First 1
    if (-not $info) { Row "runner.$rn" 'skip' 'the room is not attached'; continue }
    if (-not $row) { Row "runner.$rn" 'human' "$Room has no $rn runner row. install it: pwsh -File scripts\provision-room.ps1 $Target -Name $Room -Install $rn (the operator)"; Unmet 'human'; continue }
    if (-not $row.found) { Row "runner.$rn" 'human' "$rn is not installed on $Room. run: pwsh -File scripts\provision-room.ps1 $Target -Name $Room -Install $rn (the operator)"; Unmet 'human' }
    elseif (-not $row.enabled) {
        if ($Fix) {
            try {
                $row.enabled = $true
                Invoke-RestMethod -Method Put -Uri "http://$HubAddr/v1/harnesses/$rn" -Headers $hdr -ContentType 'application/json' -Body ($row | ConvertTo-Json -Depth 8) -TimeoutSec 15 | Out-Null
                Row "runner.$rn" 'done' "enabled the $rn row, found at $($row.found)"
            } catch { Row "runner.$rn" 'fail' "could not enable the row: $($_.Exception.Message)"; Unmet 'human' }
        } else { Row "runner.$rn" 'fail' "$rn is at $($row.found) but its row is switched off. -Fix enables it"; Unmet 'fixable' }
    } else { Row "runner.$rn" 'ok' "$rn found at $($row.found), enabled" }

    # helpers: files that must sit beside, or in the package of, the binary the row resolves
    foreach ($h in @($spec.helpers)) {
        if (-not $h) { continue }
        if (-not $row -or -not $row.found) { Row "helpers.$rn.$h" 'skip' "no $rn binary to look beside"; continue }
        $hn = $h -replace '[^A-Za-z0-9._-]', ''
        $sc = if ($script:remoteOS -eq 'windows') {
@"
`$f = $(Quote-Ps $row.found); `$d = Split-Path -Parent `$f
`$dirs = @(`$d, (Join-Path `$d 'node_modules'), (Join-Path (Split-Path -Parent `$d) 'lib')) | Where-Object { Test-Path -LiteralPath `$_ }
`$m = `$dirs | ForEach-Object { Get-ChildItem -LiteralPath `$_ -Recurse -Depth 12 -Filter '$hn*' -ErrorAction SilentlyContinue } | Select-Object -First 1
if (`$m) { 'helper=' + `$m.FullName }
"@
        } else {
@"
f=$(Quote-Sh $row.found)
d=`$(dirname "`$f"); real=`$(readlink -f "`$f" 2>/dev/null || echo "`$f"); rd=`$(dirname "`$real")
m=`$(find "`$d" "`$d/../lib/node_modules" "`$rd" "`$rd/.." -maxdepth 12 -name '$hn*' 2>/dev/null | head -n 1)
[ -n "`$m" ] && echo "helper=`$m"
true
"@
        }
        $x = Invoke-Remote $sc
        $found = (ConvertFrom-KeyValue $x.Out).helper
        if ($found) { Row "helpers.$rn.$h" 'ok' $found }
        elseif ($Fix) { Row "helpers.$rn.$h" 'human' "$h is missing beside $($row.found). provision's runner install is the fix and is not callable on its own yet: reinstall $rn with provision-room.ps1 $Target -Name $Room -Install $rn (the operator)"; Unmet 'human' }
        else { Row "helpers.$rn.$h" 'fail' "$h is not beside $($row.found). the fix is provision's runner install (provision-room.ps1 $Target -Name $Room -Install $rn)"; Unmet 'human' }
    }

    # mcp-config: the row names a file, and the file names atrium-control
    foreach ($m in @($spec.mcp)) {
        if (-not $m) { continue }
        if (-not $row) { Row "mcp.$rn.$m" 'skip' "no $rn row"; continue }
        $args0 = @($row.args | Where-Object { $_ -ne $null })
        $i = [Array]::IndexOf($args0, '--mcp-config')
        $mcpArg = if ($i -ge 0 -and $i + 1 -lt $args0.Count) { $args0[$i + 1] } else { $null }
        $mcpFile = if ($remoteHome) { "$remoteHome/.atrium/mcp.json" } else { $null }
        $mcpTarget = if ($mcpArg) { $mcpArg } else { $mcpFile }
        $has = $false
        if ($mcpTarget) {
            $sc = if ($script:remoteOS -eq 'windows') { "if ((Test-Path -LiteralPath $(Quote-Ps $mcpTarget)) -and ((Get-Content -Raw -LiteralPath $(Quote-Ps $mcpTarget)) -match '\`"$m\`"')) { 'has=1' }" }
                  else { "grep -q '\`"$m\`"' $(Quote-Sh $mcpTarget) 2>/dev/null && echo has=1; true" }
            $has = [bool](ConvertFrom-KeyValue (Invoke-Remote $sc).Out).has
        }
        if ($mcpArg -and $has) { Row "mcp.$rn.$m" 'ok' "the $rn row names $mcpArg, and it holds $m" }
        elseif ($mcpArg) { Row "mcp.$rn.$m" 'human' "the $rn row names $mcpArg, which has no $m. run: pwsh -File scripts\provision-room.ps1 $Target -Name $Room, which writes it (the operator)"; Unmet 'human' }
        elseif (-not $has) { Row "mcp.$rn.$m" 'human' "the $rn row has no --mcp-config and $mcpFile has no $m. run: pwsh -File scripts\provision-room.ps1 $Target -Name $Room (the operator)"; Unmet 'human' }
        elseif ($Fix) {
            try {
                $row.args = @('--mcp-config', $mcpFile) + $args0
                $res0 = @($row.resume_args | Where-Object { $_ -ne $null })
                if ([Array]::IndexOf($res0, '--mcp-config') -lt 0) { $row.resume_args = $res0 + @('--mcp-config', $mcpFile) }
                Invoke-RestMethod -Method Put -Uri "http://$HubAddr/v1/harnesses/$rn" -Headers $hdr -ContentType 'application/json' -Body ($row | ConvertTo-Json -Depth 8) -TimeoutSec 15 | Out-Null
                Row "mcp.$rn.$m" 'done' "the $rn row now names $mcpFile"
            } catch { Row "mcp.$rn.$m" 'fail' "could not set the row: $($_.Exception.Message)"; Unmet 'human' }
        } else { Row "mcp.$rn.$m" 'fail' "$mcpFile holds $m but the $rn row does not name it. -Fix sets --mcp-config on the row"; Unmet 'fixable' }
    }

    # atrium hooks, account scope
    if ($spec.hooks -eq 'atrium') {
        $chk = if ($row -and $row.setup) { @($row.setup.checks) | Where-Object { $_.id -eq 'hooks' } | Select-Object -First 1 } else { $null }
        if (-not $chk) { Row "hooks.$rn" 'skip' "atrium has no setup check for $rn, so it cannot say whether its hooks are wired" }
        elseif ($chk.state -eq 'ok') {
            if ($sameHost.Count) { Row "hooks.$rn" 'warn' "wired, but two rooms may share these hooks: $Room and $(($sameHost | ForEach-Object { $_.name }) -join ', ') are on $($info.host). they point at one room, and this never rewrites them toward $Room" }
            else { Row "hooks.$rn" 'ok' "$($chk.detail) ($($chk.path))" }
        } elseif ($sameHost.Count) {
            Row "hooks.$rn" 'warn' "not wired ($($chk.detail)), and two rooms may share this account ($(($sameHost | ForEach-Object { $_.name }) -join ', ')), so this will not rewrite them toward $Room"
        } else {
            $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
            $path = if ($chk.path) { $chk.path } else { 'the account settings.json' }
            if ($Fix -and $Yes) {
                Note "changing $path on $Room, the account's, for every project and room on it: adds atrium's own hooks (no Stop)"
                Note "backup: $path.atrium-$stamp.bak (the install keeps it and reports the exact name)"
                try {
                    $res = Invoke-RestMethod -Method Post -Uri "http://$HubAddr/v1/hooks/install" -Headers $hdr -ContentType 'application/json' -Body '{}' -TimeoutSec 30
                    Row "hooks.$rn" 'done' "installed atrium's hooks in $path, backup $(if ($res.backup) { $res.backup } else { 'none needed' })"
                } catch { Row "hooks.$rn" 'fail' "POST /v1/hooks/install: $($_.Exception.Message)"; Unmet 'human' }
            } else {
                Row "hooks.$rn" 'fail' "$($chk.detail) ($path). this is an ACCOUNT change: -Fix -Yes runs POST /v1/hooks/install on $Room, backup $path.atrium-<stamp>.bak. -Yes is clint's alone on a real room"
                Unmet 'fixable'
            }
        }
    }
    # the gate is an account fact and the same for every runner asking
    if ($spec.gate -eq 'required' -and -not $script:gateDone) {
        $script:gateDone = $true
        $g = Invoke-Script 'room-gate.ps1' (@($Room, '-Check', '-Target', $Target, '-HubAddr', $HubAddr) + $childSsh)
        $todo = @($g.Out | Where-Object { $_ -match '^room-gate \S+ (todo|fail)\b' })
        if ($g.Code -eq 0 -and -not $todo.Count) { Row 'gate' 'ok' "room-gate -Check finds the permission gate registered first and reachable" }
        else {
            $why = if ($todo.Count) { ($todo | ForEach-Object { $_ -replace '^room-gate ', '' }) -join '; ' } else { "room-gate -Check exited $($g.Code)" }
            Row 'gate' 'human' "$why. run: pwsh -File scripts\room-gate.ps1 $Room -Target $Target (clint: it copies a dotfiles script, which needs the yes)"
            Unmet 'human'
        }
    }
    # the status line is an account fact too: the room's runner settings.json has a statusLine key, or it shows none
    if ($spec.statusline -eq 'required' -and -not $script:statuslineDone) {
        $script:statuslineDone = $true
        $rd = if ($script:remoteOS -eq 'windows') {
            "`$f = Join-Path `$HOME '.claude\settings.json'`nif (Test-Path -LiteralPath `$f) { 'file=' + [Convert]::ToBase64String([IO.File]::ReadAllBytes(`$f)) }"
        } else { "f=`"`$HOME/.claude/settings.json`"; [ -f `"`$f`" ] && echo `"file=`$(base64 < `"`$f`" | tr -d '\n')`"; true" }
        $slf = (ConvertFrom-KeyValue (Invoke-Remote $rd).Out).file
        $slCmd = $null
        if ($slf) {
            try { $slCmd = ([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($slf)) | ConvertFrom-Json).statusLine.command } catch { $slCmd = $null }
        }
        $fixHint = "run: pwsh -File scripts\provision-room.ps1 $Target -Name $Room, whose statusline step installs it (the operator)"
        if (-not $slCmd) {
            Row 'statusline' 'human' "the account's .claude/settings.json has no statusLine command, so every agent on $Room shows none. $fixHint"
            Unmet 'human'
        } else {
            # RUN IT ONCE with `{}` on stdin: a key naming a missing script or an unusable bash reads the same as a good one.
            # No session_id in the payload, so the script does not append to the usage log. jq is probed with the same bash.
            if ($script:remoteOS -eq 'windows') {
                $m = [regex]::Match($slCmd, '^\s*(?:"([^"]+)"|(\S+))\s+(?:"([^"]+)"|(\S+))\s*$')
                if (-not $m.Success) { $run = $null }
                else {
                    $exe = if ($m.Groups[1].Success) { $m.Groups[1].Value } else { $m.Groups[2].Value }
                    $scr = if ($m.Groups[3].Success) { $m.Groups[3].Value } else { $m.Groups[4].Value }
                    $run = "`$ErrorActionPreference = 'Continue'`n`$o = '{}' | & $(Quote-Ps $exe) $(Quote-Ps $scr) 2>&1`n`"rc=`$LASTEXITCODE`"`nif (`$o) { 'out=1' }`n" +
                        "& $(Quote-Ps $exe) -c 'command -v jq' 2>&1 | Out-Null`n`"jq=`$(if (`$LASTEXITCODE -eq 0) { 1 } else { 0 })`""
                }
            } else {
                $run = "o=`$(echo '{}' | sh -c $(Quote-Sh $slCmd) 2>&1); echo rc=`$?; [ -n `"`$o`" ] && echo out=1`n" +
                    "if sh -c 'command -v jq' >/dev/null 2>&1; then echo jq=1; else echo jq=0; fi"
            }
            $rk = if ($run) { ConvertFrom-KeyValue (Invoke-Remote $run).Out } else { @{} }
            if (-not $run) { Row 'statusline' 'human' "the statusLine command on $Room is $slCmd, which this cannot split into a bash and a script to try. $fixHint"; Unmet 'human' }
            elseif ($rk.rc -ne '0' -or -not $rk.out) {
                Row 'statusline' 'human' "the statusLine command on $Room did not run (exit $($rk.rc), output $(if ($rk.out) { 'yes' } else { 'none' })): $slCmd. $fixHint"
                Unmet 'human'
            } elseif ($rk.jq -ne '1') {
                Row 'statusline' 'human' "the statusLine runs on $Room but jq is missing for its bash, so it shows only folder, branch and clock. install jq there (git-bash has none: jq.exe in Git/usr/bin)"
                Unmet 'human'
            } else { Row 'statusline' 'ok' "the statusLine command runs on $Room and jq is there: $slCmd" }
        }
    }
}

# ── 6b. the folders the room may launch in ──────────────────────────────────

# `atrium room folders list --json` on the room, through room-folders.ps1 (the verb is the room's, this only asks). A
# room with a list is bounded to it and claude's folder trust is written for each, so no launch meets the trust dialog.
# A room with NO list launches anywhere, as before, which is a warn: it works, and it can sit at a dialog nobody answers.
#
# -FIX NEEDS -YES HERE, though the setting belongs to one room. Setting a list turns the launch bound on, and a room
# that launches in folders outside the clone and its worktrees (sg4 launches into D:\git) would start refusing them. So
# like the hooks row it prints what it will do first, and only -Fix -Yes does it. It allows the clone and its
# <clone>-worktrees folder, the default a new provision uses.
if (-not $info) {
    Row 'allowed-folders' 'skip' 'the room is not attached'
} else {
    $fr = Invoke-Remote (Get-FolderScript $kind @('list', '--json'))
    $fl = ConvertFrom-FolderList $fr.Out
    $sshCmd = (@($Ssh) + $SshOption + @($Target)) -join ' '
    $feExe = if ($kind -eq 'windows') { '.\.atrium\bin\atrium.exe' } else { '~/.local/bin/atrium' }
    $want = if ($clonePath) { @(Get-DefaultFolders $clonePath $null) } else { @() }
    $wantArgs = ($want | ForEach-Object { if ($_ -match '\s') { "`"$_`"" } else { $_ } }) -join ' '
    $fixCmd = "$sshCmd $feExe room folders allow $(if ($wantArgs) { $wantArgs } else { '<dir>...' })"
    if (Test-FolderVerbMissing $fr.Code $fr.Out) {
        Row 'allowed-folders' 'skip' "this atrium has no room folders verb yet, so there is no list to read. it waits for the atrium that has `atrium room folders` (runtime). then run: $fixCmd"
    } elseif ($fr.Code -ne 0 -or -not $fl.Ok) {
        Row 'allowed-folders' 'warn' "could not read $Room's folder list: $(($fr.Out | Where-Object { $_.Trim() } | Select-Object -First 1))"
    } elseif ($fl.Enforced) {
        Row 'allowed-folders' 'ok' "$Room launches only in $($fl.Roots -join ', ')"
    } elseif (-not $want.Count) {
        Row 'allowed-folders' 'warn' "no allowed folders set, so $Room launches anywhere and a card can sit at a trust dialog. no clone path is known to default to: $fixCmd"
    } elseif ($Fix -and $Yes) {
        Note "setting $Room's allowed folders to $($want -join ', '): the room will then launch only there, and claude's folder trust is written for each"
        $ar = Invoke-Remote (Get-FolderScript $kind (@('allow') + $want))
        foreach ($l in $ar.Out) { if ($l.Trim()) { Note $l } }
        $got = @(ConvertFrom-FolderAllow $ar.Out | Where-Object { $_.Kind -in 'allowed', 'trusted' })
        if ($ar.Code -eq 0 -and $got.Count) { Row 'allowed-folders' 'done' "$Room launches only in $(($got | ForEach-Object { $_.Dir } | Select-Object -Unique) -join ', ')" }
        else { Row 'allowed-folders' 'fail' "folders allow exited $($ar.Code). the lines above say why. by hand: $fixCmd"; Unmet 'human' }
    } else {
        Row 'allowed-folders' 'warn' "no allowed folders set, so $Room launches anywhere and a card can sit at a trust dialog. -Fix -Yes runs: $fixCmd"
    }
}

# ── 7. rows waiting on what is not built ────────────────────────────────────

Row 'state-dir' 'skip' 'needs the manifest to record the state dir (design section 4). not built'
if ($req.room.runner_auth.Count) { Row 'runner-auth' 'skip' "$(@($req.room.runner_auth) -join ', '): needs POST /v1/preflight runner_auth, believed per room-autostart-design section 4. not built" }
if ($req.room.survives -and $req.room.survives -ne 'none') { Row 'survives' 'skip' "$($req.room.survives): needs room-autostart-design section 4. not built" }
if ($req.env.Count) { Row 'env' 'skip' "$(@($req.env.Keys) -join ', '): needs POST /v1/preflight env_present. not built" }
if ($req.services.Count) { Row 'services' 'skip' 'needs the inventory of f-003. not built' }

# ── 8. a restart, when a fix asked for one ──────────────────────────────────

if ($script:toolNeedsRestart) { Unmet 'restart' }
if ($script:unmet.restart -gt 0) {
    if ($Fix -and $Yes) {
        Note "restarting $Room, a real room's: stops it with `atrium stop` and starts it the way it was started"
        $r = Invoke-Script 'provision-room.ps1' (@($Target, '-Name', $Room, '-Restart', '-Yes') + $childSsh + @('-HubAddr', $HubAddr))
        if ($r.Code -eq 0) { Row 'restart' 'done' "$Room came back attached"; $script:unmet.restart = 0 }
        else { Row 'restart' 'fail' "provision -Restart exited $($r.Code): $(($r.Out | Select-Object -Last 2) -join ' | ')"; Unmet 'human' }
    } else {
        Row 'restart' 'warn' "$Room needs a restart to see what changed. pwsh -File scripts\provision-room.ps1 $Target -Name $Room -Restart -Yes (clint's alone on a real room)"
    }
}

# ── 9. smoke ────────────────────────────────────────────────────────────────

foreach ($rn in ($req.runners.Keys | Sort-Object)) {
    if (-not $req.runners[$rn].Smoke -and -not $req.runners[$rn].smoke) { continue }
    if ($NoSmoke) { Row "smoke.$rn" 'skip' '-NoSmoke'; continue }
    if (-not $info) { Row "smoke.$rn" 'skip' 'the room is not attached'; continue }
    # ONE RUNNER PER CALL, named twice: -Runners because provision smokes only a runner it was told the room has,
    # and -SmokeRunners so it smokes only this one. Provision's steps are `smoke:<runner>` (f-015), or a bare `smoke`
    # for a line about the whole step.
    $sa = @($Target, '-Name', $Room, '-SmokeOnly', '-Runners', $rn, '-SmokeRunners', $rn,
        '-SmokeTimeout', "$SmokeTimeout", '-HubAddr', $HubAddr) + $childSsh
    if ($clonePath) { $sa += @('-SmokeCwd', $clonePath) }
    $r = Invoke-Script 'provision-room.ps1' $sa
    $pat = "^provision smoke(:$([regex]::Escape($rn))|-outside)? "
    $sm = @($r.Out | Where-Object { $_ -match $pat })
    if ($r.Code -eq 2) { Fail-Now "smoke.$rn" 2 "provision could not reach $Target" }
    if (-not $sm.Count) { Row "smoke.$rn" 'fail' "provision -SmokeOnly said nothing about smoke (exit $($r.Code)): $(($r.Out | Select-Object -Last 2) -join ' | ')"; Unmet 'human'; continue }
    foreach ($l in $sm) {
        if ($l -match "^provision smoke(?<o>-outside|:\S+)? (?<s>\S+)\s*(?<d>.*)$") {
            $st = $Matches['s']; $d = $Matches['d']
            Row $(if ($Matches['o'] -eq '-outside') { "smoke-outside.$rn" } else { "smoke.$rn" }) $st $d
            if ($st -eq 'fail') { Unmet 'human' }
        }
    }
}

# ── the end ─────────────────────────────────────────────────────────────────

if ($script:unmet.human -gt 0) { Finish 4 }
if ($script:unmet.fixable -gt 0) { Finish 3 }
if ($script:unmet.restart -gt 0) { Finish 5 }
Finish 0
