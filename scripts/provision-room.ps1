# Make a machine an atrium room of THIS hub, over ssh, with no admin or sudo.
#
#   pwsh -File scripts\provision-room.ps1 user@host
#   pwsh -File scripts\provision-room.ps1 user@host -Name lab1 -Runners claude,codex
#   pwsh -File scripts\provision-room.ps1 user@host -Remove
#
# Run it on the machine that runs the hub. It finds the hub from the running
# `atrium run` process, detects the remote OS and arch, puts a matching atrium in
# the remote home folder, joins that machine's room to the hub, installs
# autostart as that user, and checks the runners asked for. Run it again and it
# changes only what is not already right. `-Remove` undoes what it did.
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
#   1  a local problem: bad arguments, no hub found, the build failed
#   2  ssh could not reach the target, or its OS is not one this covers
#   3  a remote install step failed: binary, scripts, autostart or start
#   4  the join failed, or the room did not attach to the hub
#   5  installed and attached, but a runner asked for is missing or does not start
#   6  refused: the remote already has an atrium this script did not install
#
# CREDENTIALS FOLLOW THE OVERLAYS RULE. This names the ssh command and holds no
# key: ssh uses whatever the operator's own ssh config and agent say. It runs ssh
# with BatchMode, so a target that wants a password fails at once rather than
# waiting at a prompt nobody can see. The one secret it handles is the room's
# single-use join string, minted for this run, good for an hour, and spent by the
# join. Nothing stores it.
#
# WHAT GOES WHERE ON THE REMOTE
#   Windows  ~\.atrium\bin\atrium.exe, a logon task named atrium (RunLevel Limited)
#   Linux    ~/.local/bin/atrium, a systemd user unit atrium.service
#   macOS    ~/.local/bin/atrium, a LaunchAgent io.github.dovholuknf.atrium
#   all      ~/.atrium/room (the room's key and certificate), ~/.atrium/atrium.db,
#            ~/.atrium/provision (the service scripts and manifest.json)
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
    # Undo everything a previous run did, on the remote and on the hub.
    [switch] $Remove,
    # Take over an atrium on the remote that this script did not install.
    [switch] $Force,

    # The ssh and scp commands and any extra options for both (-i, -p, -J ...).
    [string] $Ssh = 'ssh',
    [string] $Scp = 'scp',
    [string[]] $SshOption = @(),

    # A prebuilt atrium for the remote's OS and arch. Default: built from this
    # checkout into build.claude/provision/<os>_<arch>/. It must know
    # `room join --no-run`.
    [string] $Binary,

    # The hub. Default: read from the running `atrium run` process.
    [string] $HubExe,
    [string] $HubDir,
    [string] $Link,
    [string] $LinkAdvertise,
    [string] $HubAddr,

    # How long to wait for the room to show as attached on the hub, in seconds.
    [int] $AttachTimeout = 60
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$repo = Split-Path -Parent $PSScriptRoot

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
    Write-Host 'usage: provision-room.ps1 <user@host> [-Name room] [-Runners claude,codex] [-Remove]'
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
# on every OS.
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
# login shell's quoting either.
function Invoke-Remote {
    param([string] $script)
    if ($script:remoteOS -eq 'windows') {
        $full = "`$ErrorActionPreference='Stop'; `$ProgressPreference='SilentlyContinue'`n" +
            "`$A = Join-Path `$HOME '.atrium'; `$Bin = Join-Path `$A 'bin\atrium.exe'`n" +
            "`$P = Join-Path `$A 'provision'; `$M = Join-Path `$P 'manifest.json'`n" +
            "`$L = Join-Path `$env:LOCALAPPDATA 'atrium'`n" + $script
        $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($full))
        if ($enc.Length -gt 7800) { throw "remote script too long for cmd.exe ($($enc.Length))" }
        $out = & $Ssh @sshBase $Target "powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand $enc" 2>&1
    } else {
        $full = "A=`"`$HOME/.atrium`"; Bin=`"`$HOME/.local/bin/atrium`"`n" +
            "P=`"`$A/provision`"; M=`"`$P/manifest.json`"`n" +
            # Where the room writes its address and lastdb.json. internal/daemon/whereami.go.
            "if [ `"`$(uname -s)`" = Darwin ]; then L=`"`$HOME/Library/Caches/atrium`"; " +
            "else L=`"`${XDG_RUNTIME_DIR:-`${XDG_STATE_HOME:-`$HOME/.local/state}}/atrium`"; fi`n" + $script
        $full = $full -replace "`r", ''
        $out = $full | & $Ssh @sshBase $Target 'sh -s' 2>&1
    }
    [pscustomobject]@{ Out = @($out | ForEach-Object { "$_" }); Code = $LASTEXITCODE }
}

function Copy-ToRemote {
    param([string] $local, [string] $remote)
    $out = & $Scp @sshBase -q $local "${Target}:$remote" 2>&1
    [pscustomobject]@{ Out = @($out | ForEach-Object { "$_" }); Code = $LASTEXITCODE }
}

# ── the hub, which is this machine ──────────────────────────────────────────

# Find-Hub reads the running hub's own command line, so the join string names the
# address rooms really dial and the store the hub really uses. Any of it can be
# overridden by a parameter.
function Find-Hub {
    $cmd = $null
    $exe = $null
    if ($IsWindows -or $env:OS -eq 'Windows_NT') {
        $p = Get-CimInstance Win32_Process -Filter "Name='atrium.exe'" |
            Where-Object { $_.CommandLine -match '\s+run(\s|$)' } | Select-Object -First 1
        if ($p) { $cmd = $p.CommandLine; $exe = $p.ExecutablePath }
    } else {
        $line = & ps -eo args 2>$null | Where-Object { $_ -match '(^|/)atrium\s+run(\s|$)' } |
            Select-Object -First 1
        if ($line) { $cmd = $line; $exe = ($line -split '\s+')[0] }
    }
    $flag = {
        param($n)
        if ($cmd -and $cmd -match "--$n[ =]`"?([^`"\s]+)") { $Matches[1] } else { $null }
    }
    [pscustomobject]@{
        Found     = [bool] $cmd
        Exe       = $exe
        Dir       = & $flag 'atrium-dir'
        Link      = & $flag 'link'
        Advertise = & $flag 'link-advertise'
        Addr      = & $flag 'addr'
        Transport = & $flag 'transport'
    }
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
    $hn = Invoke-Remote 'hostname'
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

# ── 2. the hub ──────────────────────────────────────────────────────────────

$hub = Find-Hub
if (-not $HubExe)        { $HubExe = if ($hub.Exe) { $hub.Exe } else { (Get-Command atrium -ErrorAction SilentlyContinue).Source } }
if (-not $HubDir)        { $HubDir = $hub.Dir }
if (-not $Link)          { $Link = if ($hub.Link) { $hub.Link } else { '127.0.0.1:7779' } }
if (-not $LinkAdvertise) { $LinkAdvertise = $hub.Advertise }
if (-not $HubAddr)       { $HubAddr = if ($hub.Addr) { $hub.Addr } else { '127.0.0.1:7778' } }
if (-not $HubExe) { Fail 'hub' 1 'no running hub and no atrium on PATH. pass -HubExe' }
if ($hub.Transport -and $hub.Transport -ne 'direct') {
    Fail 'hub' 1 "the hub links over $($hub.Transport). this script joins direct rooms only so far"
}
if (-not $LinkAdvertise) {
    if ($Link -match '^(127\.|localhost|\[::1\])') {
        Fail 'hub' 1 "the hub's link is on loopback ($Link), which no other machine can dial. start it with --link and --link-advertise"
    }
    $LinkAdvertise = $Link
}
try {
    $h = Invoke-RestMethod -Uri "http://$HubAddr/_hub/health" -TimeoutSec 5
    Step 'hub' 'ok' "rooms dial $LinkAdvertise, $($h.rooms) attached now"
} catch {
    Step 'hub' 'warn' "rooms dial $LinkAdvertise, but http://$HubAddr/_hub/health did not answer"
}

# ── 3. what the remote already has ──────────────────────────────────────────

$stateScript = if ($os -eq 'windows') {
@'
"home=$HOME"
"atriumdir=$(Test-Path $A)"
"bindir=$(Test-Path (Split-Path -Parent $Bin))"
"locdir=$(Test-Path $L)"
"bin=$(Test-Path $Bin)"
if (Test-Path $Bin) { "binsha=$((Get-FileHash $Bin -Algorithm SHA256).Hash.ToLower())" }
"db=$(Test-Path (Join-Path $A 'atrium.db'))"
"roomdir=$(Test-Path (Join-Path $A 'room'))"
$rj = Join-Path $A 'room\room.json'
if (Test-Path $rj) { $j = Get-Content $rj -Raw | ConvertFrom-Json; "joinedroom=$($j.room)"; "joinedhub=$($j.hub)" }
$t = Get-ScheduledTask -TaskName atrium -ErrorAction SilentlyContinue
if ($t) { "service=$($t.Actions[0].Execute) $($t.Actions[0].Arguments)" }
if (Test-Path $M) { "manifest=$((Get-Content $M -Raw) -replace '\r?\n', ' ')" }
try { $null = Invoke-RestMethod http://127.0.0.1:7781/v1/health -TimeoutSec 3; "up=True" } catch { "up=False" }
'@
} else {
@'
echo "home=$HOME"
tf() { if [ -e "$1" ]; then echo True; else echo False; fi; }
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
if [ -f "$A/room/room.json" ]; then
  echo "joinedroom=$(sed -n 's/.*"room": *"\([^"]*\)".*/\1/p' "$A/room/room.json")"
  echo "joinedhub=$(sed -n 's/.*"hub": *"\([^"]*\)".*/\1/p' "$A/room/room.json")"
fi
u="$HOME/.config/systemd/user/atrium.service"
p="$HOME/Library/LaunchAgents/io.github.dovholuknf.atrium.plist"
if [ -f "$u" ]; then echo "service=$(grep '^ExecStart=' "$u")"; fi
if [ -f "$p" ]; then echo "service=$(grep -A0 'exec ' "$p" | head -1)"; fi
if [ -f "$M" ]; then echo "manifest=$(tr '\n' ' ' < "$M")"; fi
if [ "$(uname -s)" = Linux ]; then
  if systemctl --user is-active --quiet atrium 2>/dev/null; then echo up=True; else echo up=False; fi
else
  if launchctl print "gui/$(id -u)/io.github.dovholuknf.atrium" >/dev/null 2>&1; then echo up=True; else echo up=False; fi
fi
'@
}
$st = Invoke-Remote $stateScript
if ($st.Code -ne 0) { Fail 'state' 3 'could not read what the remote has' $st.Out }
$state = ConvertFrom-KeyValue $st.Out
# What the manifest records as already there before the first run.
$preKeys = @('atriumdir', 'bindir', 'bin', 'db', 'roomdir', 'locdir', 'service')
$manifest = if ($state.manifest) { $state.manifest | ConvertFrom-Json } else { $null }

# ── -Remove ─────────────────────────────────────────────────────────────────

if ($Remove) {
    if (-not $manifest) {
        Step 'state' 'skip' 'this script never provisioned this machine. nothing to undo'
        Finish 0
    }
    $room = if ($Name) { $Name } else { $manifest.name }
    Step 'state' 'ok' "provisioned as $room"
    # WHAT WAS THERE BEFORE IS KEPT. A flag the manifest does not have, from an
    # older run, reads as "was there", so the doubt falls on keeping.
    $pre = $manifest.pre
    $was = @{}
    foreach ($k in $preKeys) { $was[$k] = if ($null -eq $pre.$k) { $true } else { [bool] $pre.$k } }

    $rmScript = if ($os -eq 'windows') {
        (($preKeys | ForEach-Object { "`$pre_$_ = `$$($was[$_])" }) -join "`n") + "`n" + @'
if (Test-Path $Bin) { try { & $Bin stop --url http://127.0.0.1:7781 2>&1 | Out-Null } catch {} }
if (-not $pre_service -and (Get-ScheduledTask -TaskName atrium -ErrorAction SilentlyContinue)) {
    Stop-ScheduledTask -TaskName atrium -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName atrium -Confirm:$false
    "service=removed"
}
$deadline = (Get-Date).AddSeconds(20)
while ((Get-Process atrium -ErrorAction SilentlyContinue | Where-Object Path -eq $Bin) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 500 }
Get-Process atrium -ErrorAction SilentlyContinue | Where-Object Path -eq $Bin | Stop-Process -Force
Start-Sleep -Milliseconds 500
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
        (($preKeys | ForEach-Object { "pre_$_=$($was[$_])" }) -join "`n") + "`n" + @'
if [ -x "$Bin" ]; then "$Bin" stop --url http://127.0.0.1:7781 >/dev/null 2>&1 || true; fi
if [ "$pre_service" = False ] && [ -f "$P/scripts/atrium-service.sh" ]; then
  ATRIUM_EXE="$Bin" ATRIUM_SERVICE_VERB=room bash "$P/scripts/atrium-service.sh" uninstall >/dev/null 2>&1 || true
  echo "service=removed"
fi
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
    $rm = Invoke-Remote $rmScript
    if ($rm.Code -ne 0) { Fail 'remove' 3 'the remote clean-up failed' $rm.Out }
    $kv = ConvertFrom-KeyValue $rm.Out
    if ($kv.service) { Step 'autostart' 'done' 'removed' } else { Step 'autostart' 'skip' 'it was there before this script' }
    Step 'files' 'done' $kv.files

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
    Finish 0
}

# ── 4. the room's name, and whether the remote belongs to someone else ───────

if (-not $Name) {
    $Name = if ($manifest) { $manifest.name } else { ($remoteHost.ToLower() -replace '[^a-z0-9._-]', '-') }
}
if (-not $manifest -and ($state.service -or $state.joinedroom) -and -not $Force) {
    Fail 'state' 6 "this machine already has an atrium ($($state.service)$($state.joinedroom)) that this script did not install. -Force takes it over"
}
if (-not $manifest) {
    $pre = [ordered]@{}
    foreach ($k in $preKeys) { $pre[$k] = if ($k -eq 'service') { [bool] $state.service } else { $state.$k -eq 'True' } }
    $manifest = [pscustomobject]@{ name = $Name; hub = $LinkAdvertise; pre = [pscustomobject] $pre }
    Step 'state' 'ok' "fresh, will be room $Name"
} else {
    Step 'state' 'ok' "provisioned before as $($manifest.name)"
}
$manifest.name = $Name
$manifest.hub = $LinkAdvertise
$manifestJson = $manifest | ConvertTo-Json -Compress

$mk = if ($os -eq 'windows') {
    "New-Item -ItemType Directory -Force -Path (Join-Path `$A 'bin'), (Join-Path `$P 'scripts') | Out-Null`n" +
    "[IO.File]::WriteAllText(`$M, '$($manifestJson -replace "'", "''")')"
} else {
    "mkdir -p `"`$HOME/.local/bin`" `"`$P/scripts`" `"`$P/packaging`"`ncat > `"`$M`" <<'EOF'`n$manifestJson`nEOF"
}
$r = Invoke-Remote $mk
if ($r.Code -ne 0) { Fail 'state' 3 'could not write the manifest' $r.Out }

# ── 5. the binary ───────────────────────────────────────────────────────────

$ext = if ($os -eq 'windows') { '.exe' } else { '' }
if (-not $Binary) {
    if (-not (Test-Path (Join-Path $repo 'go.mod'))) { Fail 'build' 1 'not in an atrium checkout. pass -Binary' }
    $outDir = Join-Path $repo "build.claude/provision/${os}_$goarch"
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $Binary = Join-Path $outDir "atrium$ext"
    $version = (git -C $repo describe --tags --exact-match 2>$null)
    if (-not $version) { $version = 'dev' }
    $commit = (git -C $repo rev-parse HEAD 2>$null)
    $env:CGO_ENABLED = '0'; $env:GOOS = $os; $env:GOARCH = $goarch
    try {
        $b = & go -C $repo build -trimpath -ldflags "-s -w -X github.com/dovholuknf/atrium/internal/cli.Version=$version -X github.com/dovholuknf/atrium/internal/cli.Commit=$commit" -o $Binary ./cmd/atrium 2>&1
        $bc = $LASTEXITCODE
    } finally {
        Remove-Item Env:CGO_ENABLED, Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
    }
    if ($bc -ne 0) { Fail 'build' 1 "go build for $os/$goarch failed" $b }
}
if (-not (Test-Path -LiteralPath $Binary)) { Fail 'build' 1 "no binary at $Binary" }
$sha = (Get-FileHash -LiteralPath $Binary -Algorithm SHA256).Hash.ToLower()
Step 'build' 'ok' "$os/$goarch $($sha.Substring(0, 12))"

$binChanged = $false
if ($state.binsha -eq $sha) {
    Step 'binary' 'ok' 'same build already there'
} else {
    $remoteNew = if ($os -eq 'windows') { '.atrium/bin/atrium.exe.new' } else { '.local/bin/atrium.new' }
    $c = Copy-ToRemote $Binary $remoteNew
    if ($c.Code -ne 0) { Fail 'binary' 3 'scp failed' $c.Out }
    # ON WINDOWS A RUNNING EXE CANNOT BE REPLACED, so the room is wound down
    # first and started again by step 8.
    $swap = if ($os -eq 'windows') {
@'
if (Test-Path $Bin) {
    try { & $Bin stop --url http://127.0.0.1:7781 2>&1 | Out-Null } catch {}
    Stop-ScheduledTask -TaskName atrium -ErrorAction SilentlyContinue
    $deadline = (Get-Date).AddSeconds(20)
    while ((Get-Process atrium -ErrorAction SilentlyContinue | Where-Object Path -eq $Bin) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 500 }
    Get-Process atrium -ErrorAction SilentlyContinue | Where-Object Path -eq $Bin | Stop-Process -Force
    Start-Sleep -Milliseconds 500
}
Move-Item -Force "$Bin.new" $Bin
& $Bin version
'@
    } else {
        "chmod +x `"`$Bin.new`" && mv -f `"`$Bin.new`" `"`$Bin`" && `"`$Bin`" version"
    }
    $r = Invoke-Remote $swap
    if ($r.Code -ne 0) { Fail 'binary' 3 'could not put the binary in place' $r.Out }
    $binChanged = $true
    $where = if ($os -eq 'windows') { '~\.atrium\bin\atrium.exe' } else { '~/.local/bin/atrium' }
    Step 'binary' 'done' "$where, $((($r.Out | Select-Object -First 1) -replace '\s+', ' ').Trim())"
}

# ── 6. the service scripts ──────────────────────────────────────────────────

# UNIX FILES GO OVER WITH LF ENDINGS whatever this checkout has, because a
# shell script with a carriage return on every line does not run.
$files = if ($os -eq 'windows') {
    @(@('scripts/atrium-service.ps1', 'scripts'), @('scripts/atrium-autostart.ps1', 'scripts'))
} else {
    @(@('scripts/atrium-service.sh', 'scripts'), @('packaging/atrium.service', 'packaging'),
      @('packaging/atrium.plist', 'packaging'))
}
$stage = Join-Path $repo "build.claude/provision/${os}_$goarch/files"
New-Item -ItemType Directory -Force -Path $stage | Out-Null
foreach ($f in $files) {
    $src = Join-Path $PSScriptRoot "../$($f[0])"
    $dst = Join-Path $stage (Split-Path -Leaf $f[0])
    $text = [IO.File]::ReadAllText($src)
    if ($os -ne 'windows') { $text = $text -replace "`r`n", "`n" }
    [IO.File]::WriteAllText($dst, $text)
    $c = Copy-ToRemote $dst ".atrium/provision/$($f[1])/$(Split-Path -Leaf $f[0])"
    if ($c.Code -ne 0) { Fail 'scripts' 3 "scp of $($f[0]) failed" $c.Out }
}
Step 'scripts' 'done' "$($files.Count) files in ~/.atrium/provision"

# ── 7. join this hub ────────────────────────────────────────────────────────

if ($state.joinedroom -eq $Name -and $state.joinedhub -eq $LinkAdvertise) {
    Step 'join' 'ok' "already joined as $Name"
} else {
    # A NAME ALREADY IN USE ON THE HUB is reused only when nothing ever joined
    # with it. One that has connected belongs to some machine, and a second
    # machine under the same name would be two rooms the hub thinks are one.
    $line = Get-HubRoom $Name
    if ($line -and $line -notmatch 'never connected') {
        Fail 'join' 4 "the hub already has a room called $Name that has connected before. pass -Name, or remove it: atrium rooms rm $Name --force" @($line)
    }
    if (-not $line) {
        $a = Invoke-Hub @('rooms', 'add', $Name, '--link', $Link, '--link-advertise', $LinkAdvertise)
        if ($a.Code -ne 0) { Fail 'join' 4 "the hub would not add $Name" $a.Out }
    }
    # `rooms token` prints the string alone, so it is what is read. The one
    # `rooms add` printed is retired by it.
    $t = Invoke-Hub @('rooms', 'token', $Name, '--link', $Link, '--link-advertise', $LinkAdvertise)
    $token = ($t.Out | Where-Object { $_.Trim() } | Select-Object -Last 1).Trim()
    if ($t.Code -ne 0 -or -not $token) { Fail 'join' 4 "the hub would not mint a join string for $Name" $t.Out }

    $js = if ($os -eq 'windows') {
        "& `$Bin room join '$token' --no-run"
    } else {
        "`"`$Bin`" room join '$token' --no-run"
    }
    $j = Invoke-Remote $js
    $token = $null
    if ($j.Code -ne 0) { Fail 'join' 4 "the remote could not join as $Name" $j.Out }
    Step 'join' 'done' "joined $LinkAdvertise as $Name"
}

# ── 8. autostart, and running now ───────────────────────────────────────────

if ($os -eq 'windows') {
    $as = @'
$t = Get-ScheduledTask -TaskName atrium -ErrorAction SilentlyContinue
if ($t -and $t.Actions[0].Arguments -like "*$Bin*room --db*") { "autostart=ok" }
else {
    $o = & (Join-Path $P 'scripts\atrium-service.ps1') install -Verb room -Exe $Bin *>&1
    if (-not (Get-ScheduledTask -TaskName atrium -ErrorAction SilentlyContinue)) { $o; exit 1 }
    "autostart=done"
}
$up = $false
try { $null = Invoke-RestMethod http://127.0.0.1:7781/v1/health -TimeoutSec 3; $up = $true } catch {}
if ($up) { "start=ok" } else {
    Start-ScheduledTask -TaskName atrium
    $deadline = (Get-Date).AddSeconds(30)
    while (-not $up -and (Get-Date) -lt $deadline) {
        Start-Sleep -Seconds 1
        try { $null = Invoke-RestMethod http://127.0.0.1:7781/v1/health -TimeoutSec 2; $up = $true } catch {}
    }
    if ($up) { "start=done" } else {
        $i = Get-ScheduledTaskInfo -TaskName atrium
        "start=fail the task did not bring the room up, last result 0x$('{0:X}' -f $i.LastTaskResult). an Interactive task needs the user logged in at the machine"
    }
}
'@
} else {
    $as = @'
S="$P/scripts/atrium-service.sh"
u="$HOME/.config/systemd/user/atrium.service"
p="$HOME/Library/LaunchAgents/io.github.dovholuknf.atrium.plist"
if { [ -f "$u" ] && grep -q "^ExecStart=$Bin room" "$u"; } || { [ -f "$p" ] && grep -q "$Bin\" room" "$p"; }; then
  echo autostart=ok
  if [ "$CHANGED" = 1 ]; then ATRIUM_EXE="$Bin" ATRIUM_SERVICE_VERB=room bash "$S" restart >/dev/null 2>&1 || true; fi
else
  o=$(ATRIUM_EXE="$Bin" ATRIUM_SERVICE_VERB=room bash "$S" install 2>&1) || { echo "$o"; exit 1; }
  echo autostart=done
  case "$o" in *"not loaded now"*) echo "start=warn the LaunchAgent loads at the next desktop login, there is no GUI session now";; esac
fi
if [ "$(uname -s)" = Linux ]; then
  if systemctl --user is-active --quiet atrium; then echo start=ok; else
    systemctl --user start atrium 2>&1 && echo start=done || echo "start=fail systemctl --user start atrium failed"
  fi
elif launchctl print "gui/$(id -u)/io.github.dovholuknf.atrium" >/dev/null 2>&1; then echo start=ok
else echo "start=warn the LaunchAgent is not loaded. it needs a desktop login"
fi
'@
    $as = "CHANGED=$([int]$binChanged)`n" + $as
}
$startedAt = Get-Date
$r = Invoke-Remote $as
$kv = ConvertFrom-KeyValue $r.Out
if ($r.Code -ne 0 -or -not $kv.autostart) { Fail 'autostart' 3 'the service install failed' $r.Out }
Step 'autostart' $kv.autostart $(if ($os -eq 'windows') { 'logon task atrium, RunLevel Limited' } elseif ($os -eq 'linux') { 'systemd user unit atrium.service' } else { 'LaunchAgent io.github.dovholuknf.atrium' })

# A Windows room stopped for a new binary comes back through the task, and
# the check above already started it. Say so rather than "ok".
$startStatus = ($kv.start -split ' ', 2)
$startWord = $startStatus[0]
if ($startWord -eq 'ok' -and $binChanged) { $startWord = 'done'; $startStatus = @('done', 'restarted on the new build') }
Step 'start' $startWord $(if ($startStatus.Count -gt 1) { $startStatus[1] } else { '' })
if ($startWord -eq 'fail') { Finish 3 }

# ── 9. attached to the hub ──────────────────────────────────────────────────

# THE HUB'S OWN CONNECTION LIST, not `rooms ls`, which infers "attached" from
# the last twenty seconds and would still say so about the room that was just
# stopped for a new binary. A room started by this run has to show a
# connection made after it started.
$needSince = if ($startWord -eq 'done') { $startedAt } else { [datetime]::MinValue }
$deadline = (Get-Date).AddSeconds($AttachTimeout)
$seen = $null
do {
    try {
        $live = Invoke-RestMethod -Uri "http://$HubAddr/_hub/rooms" -TimeoutSec 5
        $seen = $live.rooms | Where-Object { $_.name -eq $Name -and ([datetime] $_.since) -ge $needSince } |
            Select-Object -First 1
    } catch { $seen = $null }
    if ($seen) { break }
    Start-Sleep -Seconds 2
} while ((Get-Date) -lt $deadline)
if (-not $seen) {
    Fail 'attached' 4 "the hub has no live connection from $Name after ${AttachTimeout}s" @(Get-HubRoom $Name)
}
Step 'attached' 'ok' "$Name on the hub since $(([datetime] $seen.since).ToString('HH:mm:ss')), host $($seen.host), build $($seen.version)"

# ── 10. the runners ─────────────────────────────────────────────────────────

# PRESENT AND ANSWERS --version, found the way the room will find it: on
# Windows the user's PATH, which the logon task shares, and elsewhere a login
# shell's PATH.
$missing = 0
# `pwsh -File` hands `-Runners claude,codex` over as one string, so commas split.
$Runners = @($Runners | ForEach-Object { $_ -split ',' } | ForEach-Object { $_.Trim() } | Where-Object { $_ })
foreach ($runner in $Runners) {
    if ($runner -notmatch '^[A-Za-z0-9._-]+$') { Step "runner:$runner" 'fail' 'not a command name'; $missing++; continue }
    $rs = if ($os -eq 'windows') {
@"
`$c = Get-Command '$runner' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not `$c) { `$c = Get-Command '$runner' -ErrorAction SilentlyContinue | Select-Object -First 1 }
if (-not `$c) { 'runner=missing'; exit 0 }
"path=`$(`$c.Source)"
`$job = Start-Job { param(`$n) & `$n --version 2>&1 } -ArgumentList `$c.Source
if (Wait-Job `$job -Timeout 30) { "version=`$((Receive-Job `$job | Select-Object -First 1))"; 'runner=ok' }
else { Stop-Job `$job; 'runner=hung' }
"@
    } else {
@"
sh_=`${SHELL:-/bin/sh}
out=`$("`$sh_" -lc 'command -v $runner' 2>/dev/null)
if [ -z "`$out" ]; then echo runner=missing; exit 0; fi
echo "path=`$out"
v=`$("`$sh_" -lc '$runner --version' </dev/null 2>&1)
rc=`$?
echo "version=`$(printf '%s\n' "`$v" | head -1)"
if [ "`$rc" = 0 ]; then echo runner=ok; else echo runner=broken; fi
"@
    }
    $r = Invoke-Remote $rs
    $kv = ConvertFrom-KeyValue $r.Out
    switch ($kv.runner) {
        'ok'      { Step "runner:$runner" 'ok' "$($kv.version) at $($kv.path)" }
        'missing' { Step "runner:$runner" 'fail' 'not on PATH'; $missing++ }
        'hung'    { Step "runner:$runner" 'fail' "found at $($kv.path), --version did not return in 30s"; $missing++ }
        default   { Step "runner:$runner" 'fail' "found at $($kv.path), --version failed"; $missing++ }
    }
}
if ($missing -gt 0) { Finish 5 }
Finish 0
