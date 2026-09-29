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
# NO AUTOSTART BY DEFAULT. The room is started in the background with
# `atrium room --detach` and runs until the machine restarts or the user logs
# out. `-Autostart` installs the logon task, systemd user unit or LaunchAgent as
# well. `-Linger` (Linux, with -Autostart) keeps it running after logout.
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
#   6  refused: the remote is already a room, or runs an atrium this script did not install
#   7  the overlay needs a credential only the operator can give: see the fail line
#   8  installed and attached, but the smoke card did not report
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
#   -Autostart adds a logon task `atrium` (RunLevel Limited), a systemd user unit
#            atrium.service, or a LaunchAgent io.github.dovholuknf.atrium, and
#            their scripts under ~/.atrium/provision
#   -Install  adds the runner where its own installer puts it, usually ~/.local
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

    # Build the binary from this checkout instead of fetching a release.
    [switch] $FromCheckout,
    # The release to fetch. Default: the latest.
    [string] $Version,
    # A prebuilt atrium for the remote's OS and arch, instead of either.
    [string] $Binary,

    # Install autostart as well as starting the room now.
    [switch] $Autostart,
    # Linux with -Autostart: turn on lingering so the room survives logout.
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

    # How long to wait for the room to show as attached on the hub, in seconds.
    [int] $AttachTimeout = 60,

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
    [int] $SmokeTimeout = 180
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
if ($SmokeOnly -and ($Remove -or $NoSmoke)) { Write-Host 'provision args fail -SmokeOnly goes with neither -Remove nor -NoSmoke'; exit 1 }
$checkout = Split-Path -Parent $PSScriptRoot
$inCheckout = Test-Path (Join-Path $checkout 'go.mod')
$work = if ($inCheckout) { Join-Path $checkout 'build.claude/provision' } else { Join-Path ([IO.Path]::GetTempPath()) 'atrium-provision' }

# `pwsh -File` hands `-Runners claude,codex` over as one string, so commas split.
function Split-List { param($v) @($v | ForEach-Object { "$_" -split ',' } | ForEach-Object { $_.Trim() } | Where-Object { $_ }) }
$Runners = Split-List $Runners
$Install = Split-List $Install

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
    Write-Host 'usage: provision-room.ps1 <user@host> [-Name room] [-Runners claude,codex] [-Install claude] [-Remove]'
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
function Invoke-Remote {
    param([string] $script)
    if ($script:remoteOS -eq 'windows') {
        $full = "`$ErrorActionPreference='Stop'; `$ProgressPreference='SilentlyContinue'`n$(if ($script -match '\b(Sch|Get-AT)\b') { $winHelpers })`n" +
            "`$A = Join-Path `$HOME '.atrium'; `$Bin = Join-Path `$A 'bin\atrium.exe'`n" +
            "`$P = Join-Path `$A 'provision'; `$M = Join-Path `$P 'manifest.json'`n" +
            "`$L = Join-Path `$env:LOCALAPPDATA 'atrium'`n" +
            # PATH FROM THE REGISTRY, so a Path entry this run added is seen by the
            # room it starts and the runner check, whatever the ssh server's
            # session inherited.
            "`$env:Path = (@([Environment]::GetEnvironmentVariable('Path', 'Machine'), " +
            "[Environment]::GetEnvironmentVariable('Path', 'User')) | Where-Object { `$_ }) -join ';'`n" + $script
        $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($full))
        if ($enc.Length -gt 7800) { throw "remote script too long for cmd.exe ($($enc.Length))" }
        $out = & $Ssh @sshBase $Target "powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand $enc" 2>&1
    } else {
        $full = "A=`"`$HOME/.atrium`"; Bin=`"`$HOME/.local/bin/atrium`"`n" +
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
function Quote-Ps { param([string] $s) "'" + ($s -replace "'", "''") + "'" }
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
# 2026-09-28, so its default wait is doubled.
if ($transport -eq 'zrok' -and -not $PSBoundParameters.ContainsKey('AttachTimeout')) { $AttachTimeout = 120 }
try {
    $h = Invoke-RestMethod -Uri "http://$HubAddr/_hub/health" -TimeoutSec 5
    Step 'hub' 'ok' "$says, $($h.rooms) attached now"
} catch {
    Step 'hub' 'warn' "$says, but http://$HubAddr/_hub/health did not answer"
}

# ── 3. what the remote already has ──────────────────────────────────────────

$stateScript = if ($os -eq 'windows') {
@'
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
if (Test-Path $Bin) { try { & $Bin stop --url http://127.0.0.1:7781 2>&1 | Out-Null } catch {} }
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
if [ -x "$Bin" ]; then "$Bin" stop --url http://127.0.0.1:7781 >/dev/null 2>&1 || true; fi
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
    function Get-SmokeCwd {
        if ($SmokeCwd) { return $SmokeCwd }
        # THE CLONE room-git.ps1 init made, when it made one, so the worker
        # proves the room in a repository. Otherwise the remote home, which
        # exists on every machine.
        if ($script:clonePath) { return $script:clonePath }
        $hs = if ($os -eq 'windows') { '"home=$HOME"' } else { 'echo "home=$HOME"' }
        (ConvertFrom-KeyValue (Invoke-Remote $hs).Out).home
    }

    $smokeWhy = $null
    if ($NoSmoke) { $smokeWhy = '-NoSmoke' }
    elseif (@($Runners + $Install) -notcontains 'claude') { $smokeWhy = 'claude is not a runner for this room' }
    elseif ($authState -eq 'no') { $smokeWhy = "claude is not signed in on $Name, so a worker there cannot answer. sign in, then rerun" }
    elseif ($authState -ne 'ok') { $smokeWhy = 'whether claude is signed in is not known, so the smoke card would only guess' }
    if ($smokeWhy) {
        Step 'smoke' 'skip' $smokeWhy
        Finish 0
    }

    $nonce = -join ((1..8) | ForEach-Object { '{0:x}' -f (Get-Random -Maximum 16) })
    $me = $env:ATRIUM_AGENT_NAME; $myRoom = $env:ATRIUM_ROOM; $myCard = $env:ATRIUM_TASK_ID
    $to = $SmokeTo
    if (-not $to -and $me -and $myRoom) { $to = "$me@$myRoom" }
    $cwd = Get-SmokeCwd
    if (-not $cwd) { Step 'smoke' 'fail' "could not resolve a folder on $Name to run in. pass -SmokeCwd"; Finish 8 }

    $said = "smoke ok $Name $nonce"
    $prompt = "This is an automated smoke test of the room $Name. Do exactly these steps and nothing else. " +
        "Use only the atrium-control tools: no Bash, no file reads, no edits.`n"
    $n = 1
    if ($to) { $prompt += "$n. Call atrium_say to $to with the text: $said`n"; $n++ }
    $prompt += "$n. Call atrium_report with status done, the summary: $said, and no_commit: smoke test, no work.`n" +
        "Then stop. When you finish, get blocked, or need an answer, call atrium_report (or atrium_say your launcher) before you end your turn."
    $body = [ordered]@{
        harness = 'claude'; cwd = $cwd; title = "smoke: $Name"; prompt = $prompt
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
    } catch { $smokeErr = "the hub would not launch it: $($_.Exception.Message)" }
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
            Step 'smoke' 'ok' "a claude worker on $Name reported $nonce in ${took}s, $leftWord$(if ($to) { ", and said it to $to" })"
            if (-not $left) { Step 'smoke' 'warn' "card $id is still running on $Name" }
        } else {
            $smokeErr = "the smoke card $id did not report $nonce in ${SmokeTimeout}s ($leftWord). look at it on the board, it is on $Name"
        }
    }
    if ($smokeErr) { Step 'smoke' 'fail' $smokeErr; Finish 8 }
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
$hadAutostartBefore = [bool] $manifest.autostart
$useAutostart = $Autostart -or $hadAutostartBefore
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
    $env:CGO_ENABLED = '0'; $env:GOOS = $os; $env:GOARCH = $goarch
    try {
        $b = & go -C $checkout build -trimpath -ldflags "-s -w -X github.com/dovholuknf/atrium/internal/cli.Version=$ver -X github.com/dovholuknf/atrium/internal/cli.Commit=$commit" -o $Binary ./cmd/atrium 2>&1
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
    try { & $Bin stop --url http://127.0.0.1:7781 2>&1 | Out-Null } catch {}
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
  "$Bin" stop --url http://127.0.0.1:7781 >/dev/null 2>&1 || true
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
    codex  = "https://github.com/openai/codex/releases/latest/download/codex-$triple$(if ($os -eq 'windows') { '.exe' } else { '.tar.gz' })"
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
            'codex' { return @"
`$d = Join-Path `$HOME '.local\bin'; New-Item -ItemType Directory -Force `$d | Out-Null
`$b = Join-Path `$d 'codex.exe'; `$preb = Test-Path `$b
Invoke-WebRequest '$url' -OutFile `$b -UseBasicParsing
if (-not `$preb) { "installed=`$b" }
"path=`$b"
"@ }
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
            'codex' { return @"
d="`$HOME/.local/bin"; b="`$d/codex"; mkdir -p "`$d"
if [ -e "`$b" ]; then preb=1; else preb=0; fi
t=`$(mktemp -d)
if command -v curl >/dev/null 2>&1; then curl -fsSL '$url' | tar -xz -C "`$t" || exit 1; else wget -qO- '$url' | tar -xz -C "`$t" || exit 1; fi
f=`$(ls "`$t" | grep '^codex' | head -1)
[ -n "`$f" ] || { echo "the archive had no codex binary"; exit 1; }
mv -f "`$t/`$f" "`$b" && chmod +x "`$b"; rm -rf "`$t"
if [ `$preb = 0 ]; then echo "installed=`$b"; fi
echo "path=`$b"
"@ }
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
    `$h = Join-Path `$HOME '.local\bin\$runner.exe'
    if (Test-Path `$h) { "home=`$h" }
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
    Step "install:$runner" 'warn' ("trusting atrium to fetch $($sources[$runner]) and run it on $Target. " +
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
        "`$ErrorActionPreference = 'Continue'; if (Test-Path `$Bin) { & `$Bin stop --url http://127.0.0.1:7781 2>&1 | Out-Null }; Start-Sleep -Seconds 3"
    } else {
        "if [ -x `"`$Bin`" ]; then `"`$Bin`" stop --url http://127.0.0.1:7781 >/dev/null 2>&1; sleep 3; fi"
    }
    $null = Invoke-Remote $stop
}

# ── 8. run it: in the background, or through autostart ───────────────────────

$startedAt = Get-Date
if (-not $useAutostart) {
    # THROUGH A LOGIN SHELL ON UNIX, so the room gets the PATH a person's
    # terminal has rather than the bare one a non-interactive ssh command gets,
    # and so finds the same runners the runner check finds.
    $ds = if ($os -eq 'windows') { "`$ErrorActionPreference = 'Continue'`n& `$Bin room --detach 2>&1`nexit `$LASTEXITCODE" }
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
        $as = @'
$svc = Get-AT
if ($svc -and $svc -like "*$Bin*room --db*") { "autostart=ok" }
else {
    $o = & (Join-Path $P 'scripts\atrium-service.ps1') install -Verb room -Exe $Bin *>&1
    if (-not (Get-AT)) { $o; exit 1 }
    "autostart=done"
}
$up = $false
try { $null = Invoke-RestMethod http://127.0.0.1:7781/v1/health -TimeoutSec 3; $up = $true } catch {}
if ($up) { "start=ok" } else {
    $null = Sch /Run /TN atrium
    $deadline = (Get-Date).AddSeconds(30)
    while (-not $up -and (Get-Date) -lt $deadline) {
        Start-Sleep -Seconds 1
        try { $null = Invoke-RestMethod http://127.0.0.1:7781/v1/health -TimeoutSec 2; $up = $true } catch {}
    }
    if ($up) { "start=done" } else {
        $lr = (Sch /Query /TN atrium /V /FO LIST | Where-Object { "$_" -match '^Last Result:\s*(.+)$' } | ForEach-Object { $Matches[1].Trim() } | Select-Object -First 1)
        "start=fail the task did not bring the room up, last result $lr. an Interactive task needs the user logged in at the machine"
    }
}
'@
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
else echo "start=warn the LaunchAgent is not loaded. it needs a desktop login"
fi
'@
    }
    $r = Invoke-Remote $as
    $kv = ConvertFrom-KeyValue $r.Out
    if ($r.Code -ne 0 -or -not $kv.autostart) { Fail 'autostart' 3 'the service install failed' $r.Out }
    Step 'autostart' $kv.autostart $(if ($os -eq 'windows') { 'logon task atrium, RunLevel Limited' } elseif ($os -eq 'linux') { 'systemd user unit atrium.service' } else { 'LaunchAgent io.github.dovholuknf.atrium' })
    $startStatus = ($kv.start -split ' ', 2)
    $startWord = $startStatus[0]
    Step 'start' $startWord $(if ($startStatus.Count -gt 1) { $startStatus[1] } else { '' })
    if ($startWord -eq 'fail') { Finish 3 }
}

# ── 9. attached to the hub ──────────────────────────────────────────────────

# THE HUB'S OWN CONNECTION LIST, not `rooms ls`, which infers "attached" from
# the last twenty seconds and would still say so about the room that was just
# stopped for a new binary. A room started by this run has to show a
# connection made after it started, and still be there a few seconds later,
# which is what catches a room that died with the ssh session that started it.
$needSince = if ($startWord -eq 'done') { $startedAt } else { [datetime]::MinValue }
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
           else { "the hub has no live connection from $Name after ${AttachTimeout}s" }
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
# docs/cross-room-say-design.md.
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
# The clone, made by room-git.ps1 init. Its `room-git cwd ok <path>` line is
# where the smoke card below runs, when init succeeded.
$clonePath = $null
if ($Repo -ne 'none') { & pwsh -NoProfile -File (Join-Path $PSScriptRoot 'room-git.ps1') init $Name -Target $Target -Ssh $Ssh @(if ($SshOption) { '-SshOption'; $SshOption -join ',' }) *>&1 | ForEach-Object { Write-Host $_; if ("$_" -match '^room-git cwd ok (.+)$') { $clonePath = $Matches[1].Trim() } }; if ($LASTEXITCODE -ne 0) { $clonePath = $null; Step 'git' 'warn' "room-git init exited $LASTEXITCODE. rerun: room-git.ps1 init $Name -Target $Target" } }

$authState = Test-ClaudeAuth

if ($bad -gt 0) { Finish 5 }

Invoke-Smoke $authState
Finish 0
