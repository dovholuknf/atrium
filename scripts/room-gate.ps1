# The permission gate for a room, run from THIS side over ssh: one script copied, one hook registered.
#
#   pwsh -File scripts\room-gate.ps1 m1mini -Check
#   pwsh -File scripts\room-gate.ps1 m1mini
#   pwsh -File scripts\room-gate.ps1 sg3 -Target user@host
#
# WHAT A ROOM IS MISSING. The gate is not one of atrium's hooks. On the operator's machine it is the dotfiles script
# ~/.claude/hooks/atrium-perm-hook.ps1, registered FIRST in PreToolUse. A room provisioned with atrium's own hooks has
# the badge and the session open and close, but a worker's tool calls never reach the board as permission prompts.
# This puts that ONE script on the room and registers it. Atrium shipping its own gate replaces this (backlog f-006).
#
# THE SCRIPT IS NOT IN THIS REPOSITORY. It belongs to dotfiles, so it is read from THIS operator's
# ~/.claude/hooks/atrium-perm-hook.ps1. Nothing else from dotfiles is ever copied, least of all the footgun guard.
#
# THE STEPS, in order:
#   source    the script here. Missing is a fail naming the path
#   pwsh      the gate RUNS under pwsh, so the room needs one. The absolute path found on the room (its
#             room-env.ps1 on Windows, its login-shell PATH elsewhere) goes into the hook command, because a bare
#             `pwsh` is not something a session's hook environment is promised to resolve
#   copy      to ~/.claude/hooks/atrium-perm-hook.ps1 there. Equal SHA-256 is `ok`, and nothing is written
#   register  the room's ~/.claude/settings.json: backed up first as settings.json.atrium-<yyyyMMdd-HHmmss>.bak, then
#             the gate goes FIRST in the PreToolUse group whose matcher is "", ahead of atrium's tool-start hook.
#             Every other entry and key stays. A gate already registered (matched on the script name) is left alone
#   reach     the gate POSTs to $ATRIUM_HUB_URL, default http://127.0.0.1:7777. The room's agent listener must be
#             there. It asks GET /gate?agent=probe first, and so does this. It also reads the room's runner row
#             here and warns when that row switches the gate off. Launched runners get ATRIUM_PERM_GATE=on from the
#             daemon (permGateDefault in launch.go), so nothing is set globally
#
# -Check reports every step and writes nothing. A room needs no restart: claude reads settings.json per session.
#
# THE SETTINGS EDIT happens HERE, in pwsh 7: the file is copied down, edited with its key order kept (ConvertTo-Json
# -Depth 20), and put back with scp. The remote never needs jq, python or a PowerShell that can edit JSON. Formatting
# changes to two-space indent. Nothing else does.
#
# THE REMOTE runs plain `sh -s` on Unix (no pwsh needed to RUN the installer) and Windows PowerShell 5.1
# (-EncodedCommand) on Windows, the way room-git.ps1 does.
#
# ONE LINE PER STEP, prefixed room-gate:
#
#   room-gate <step> <status> <detail>
#
# status is ok (already right), done (changed now), skip, warn or fail. The last line is `room-gate done ok` or
# `room-gate done fail <code>`. With -Check, `done` never appears as a step status: a step that would change says `todo`.
#
# EXIT CODES
#   0  done
#   1  a local problem: bad arguments, or the gate script is missing here
#   2  ssh could not reach the target, or its OS is not one this covers
#   3  no pwsh on the room. The fail line names room-toolchain.ps1
#   4  a remote step failed: copy or register. Nothing half-written is left behind on the settings file
#   5  the room's agent listener does not answer on 7777, so the gate would fail open. The gate is installed anyway

param(
    [Parameter(Position = 0)] [string] $Room,
    # The ssh destination. Default: the room's own name, as an ssh alias.
    [string] $Target,
    [switch] $Check,
    # Where the room's runner row is read, for the reach step. The hub on this machine.
    [string] $HubAddr = '127.0.0.1:7778',
    # The gate script on this side.
    [string] $Source = (Join-Path $HOME '.claude/hooks/atrium-perm-hook.ps1'),
    [string] $Ssh = 'ssh',
    [string] $Scp = 'scp',
    [string[]] $SshOption = @()
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
# UTF-8 without a BOM for what is piped to ssh. See provision-room.ps1.
$OutputEncoding = [Text.UTF8Encoding]::new($false)
$SshOption = @($SshOption | ForEach-Object { "$_" -split ',' } | Where-Object { $_ })

function Step {
    param([string] $step, [string] $status, [string] $detail = '')
    $line = "room-gate $step $status"
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
# The word for a step that changed, or in -Check would.
function Changed { if ($Check) { 'todo' } else { 'done' } }

if (-not $Room) { Write-Host 'usage: room-gate.ps1 <room> [-Target user@host] [-Check]'; exit 1 }
if ($Room -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]*$') { Fail 'args' 1 "bad room name '$Room'" }

$sshBase = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=25') + $SshOption
$script:sshTarget = if ($Target) { $Target } else { $Room }
$script:remoteOS = $null
$gateName = 'atrium-perm-hook.ps1'

# ── source ──────────────────────────────────────────────────────────────────

if (-not (Test-Path -LiteralPath $Source)) {
    Fail 'source' 1 "$Source is missing on this machine. the gate belongs to dotfiles: put it there, or pass -Source"
}
$Source = (Resolve-Path -LiteralPath $Source).Path
$srcHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $Source).Hash.ToLower()
Step 'source' 'ok' "$($Source -replace '\\', '/') sha256 $($srcHash.Substring(0, 12))"

# ── the remote ──────────────────────────────────────────────────────────────

function Invoke-Remote {
    param([string] $script)
    if ($script:remoteOS -eq 'windows') {
        $full = "`$ErrorActionPreference='Continue'; `$ProgressPreference='SilentlyContinue'`n" +
            "`$env:Path = (@([Environment]::GetEnvironmentVariable('Path', 'Machine'), " +
            "[Environment]::GetEnvironmentVariable('Path', 'User')) | Where-Object { `$_ }) -join ';'`n" +
            # The room's own toolchain next: this is where a session finds the pwsh 7 that room-toolchain.ps1 put.
            "`$e = Join-Path `$HOME '.atrium\toolchain\room-env.ps1'; if (Test-Path `$e) { . `$e }`n" + $script
        $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($full))
        if ($enc.Length -gt 7800) { throw "remote script too long for cmd.exe ($($enc.Length))" }
        $out = & $Ssh @sshBase $script:sshTarget "powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand $enc" 2>&1
    } else {
        # A login shell's PATH, so a Homebrew pwsh is found. A COMMENT LAST: PowerShell ends what it pipes with CRLF.
        $full = "lp=`$(`"`${SHELL:-/bin/sh}`" -lc 'printf %s `"`$PATH`"' 2>/dev/null); [ -n `"`$lp`" ] && PATH=`"`$lp:`$PATH`"`n" + $script
        $full = ($full -replace "`r", '') + "`n#"
        $out = $full | & $Ssh @sshBase $script:sshTarget 'sh -s' 2>&1
    }
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

# Copy-ToRemote puts a local file at a path under the remote home, which scp resolves itself.
function Copy-ToRemote {
    param([string] $local, [string] $rel)
    $o = & $Scp @sshBase -q $local "$($script:sshTarget):$rel" 2>&1
    [pscustomobject]@{ Out = @($o | ForEach-Object { "$_" }); Code = $LASTEXITCODE }
}

# ── ssh ─────────────────────────────────────────────────────────────────────

$probe = & $Ssh @sshBase $script:sshTarget 'uname -sm' 2>&1
$code = $LASTEXITCODE
if ($code -eq 255) { Fail 'ssh' 2 "cannot reach $($script:sshTarget) over ssh" $probe }
$text = ($probe | ForEach-Object { "$_" }) -join ' '
if ($code -eq 0 -and $text -match '^(Linux|Darwin)\s') {
    $script:remoteOS = 'unix'; $kind = if ($Matches[1] -eq 'Linux') { 'linux' } else { 'mac' }
} else {
    $script:remoteOS = 'windows'; $kind = 'windows'
    $r = Invoke-Remote '"ok"'
    if ($r.Code -ne 0) { Fail 'ssh' 2 'not Linux, macOS or Windows PowerShell' ($probe + $r.Out) }
}
Step 'ssh' 'ok' "$($script:sshTarget) ($kind)"

# ── what the room has now, in one round trip ────────────────────────────────

$facts = if ($script:remoteOS -eq 'windows') {
    @'
$d = Join-Path $HOME '.claude\hooks\atrium-perm-hook.ps1'
"home=$($HOME -replace '\\', '/')"
$p = Get-Command pwsh -ErrorAction SilentlyContinue
if ($p) { "pwsh=$($p.Source -replace '\\', '/')"; "pwshver=$(& $p.Source -NoProfile -Command '$PSVersionTable.PSVersion.ToString()')" }
if (Test-Path -LiteralPath $d) { "hash=$((Get-FileHash -Algorithm SHA256 -LiteralPath $d).Hash.ToLower())" }
"settings=$(if (Test-Path -LiteralPath (Join-Path $HOME '.claude\settings.json')) { 'yes' } else { 'no' })"
try { "reach=$((Invoke-WebRequest -UseBasicParsing -TimeoutSec 4 'http://127.0.0.1:7777/gate?agent=probe').Content)" } catch { "reacherr=$($_.Exception.Message)" }
'@
} else {
    @'
d="$HOME/.claude/hooks/atrium-perm-hook.ps1"
echo "home=$HOME"
p=$(command -v pwsh) && { echo "pwsh=$p"; echo "pwshver=$("$p" -NoProfile -Command '$PSVersionTable.PSVersion.ToString()' 2>/dev/null)"; }
if [ -f "$d" ]; then
  h=$(shasum -a 256 "$d" 2>/dev/null | cut -d' ' -f1)
  [ -z "$h" ] && h=$(sha256sum "$d" 2>/dev/null | cut -d' ' -f1)
  echo "hash=$h"
fi
[ -f "$HOME/.claude/settings.json" ] && echo settings=yes || echo settings=no
r=$(curl -s -m 4 'http://127.0.0.1:7777/gate?agent=probe') && echo "reach=$r" || echo "reacherr=curl exit $?"
'@
}
$r = Invoke-Remote $facts
$kv = ConvertFrom-KeyValue $r.Out
if (-not $kv.home) { Fail 'ssh' 2 'could not read the remote home' $r.Out }
$home_ = $kv.home.TrimEnd('/')
$destRel = '.claude/hooks/atrium-perm-hook.ps1'
$dest = "$home_/$destRel"

# ── pwsh ────────────────────────────────────────────────────────────────────

if (-not $kv.pwsh) {
    Fail 'pwsh' 3 "no pwsh on $($script:sshTarget), and the gate runs under it. install it there: room-toolchain.ps1 $Room -Tools pwsh (Windows), or brew install powershell (macOS)"
}
$pwshPath = $kv.pwsh
Step 'pwsh' 'ok' "$pwshPath $($kv.pwshver)"

# ── copy ────────────────────────────────────────────────────────────────────

if ($kv.hash -eq $srcHash) {
    Step 'copy' 'ok' "$dest is already this script"
} elseif ($Check) {
    Step 'copy' 'todo' "$dest $(if ($kv.hash) { 'differs' } else { 'is missing' }), would copy"
} else {
    $m = if ($script:remoteOS -eq 'windows') {
        'New-Item -ItemType Directory -Force -Path (Join-Path $HOME ".claude\hooks") | Out-Null; "ok"'
    } else { 'mkdir -p "$HOME/.claude/hooks" && echo ok' }
    $r = Invoke-Remote $m
    if ($r.Code -ne 0) { Fail 'copy' 4 "could not make the hooks folder on $($script:sshTarget)" $r.Out }
    $c = Copy-ToRemote $Source $destRel
    if ($c.Code -ne 0) { Fail 'copy' 4 "scp to $dest failed" $c.Out }
    $r = Invoke-Remote $facts
    $now = (ConvertFrom-KeyValue $r.Out).hash
    if ($now -ne $srcHash) { Fail 'copy' 4 "$dest does not match after the copy (remote $now)" }
    Step 'copy' 'done' "$dest ($($srcHash.Substring(0, 12)))"
}

# ── register ────────────────────────────────────────────────────────────────

$cmdPath = if ($pwshPath -match '\s') { "`"$pwshPath`"" } else { $pwshPath }
$hookCmd = "$cmdPath -NoProfile -File $dest"

$readS = if ($script:remoteOS -eq 'windows') {
    '$f = Join-Path $HOME ".claude\settings.json"; if (Test-Path -LiteralPath $f) { [IO.File]::ReadAllText($f) }'
} else { 'f="$HOME/.claude/settings.json"; [ -f "$f" ] && cat "$f"' }
$settingsText = ''
if ($kv.settings -eq 'yes') {
    $r = Invoke-Remote $readS
    if ($r.Code -ne 0) { Fail 'register' 4 'could not read settings.json on the room' $r.Out }
    $settingsText = ($r.Out -join "`n").TrimStart([char]0xFEFF)
}
$settings = $null
if ($settingsText.Trim()) {
    try { $settings = $settingsText | ConvertFrom-Json -ErrorAction Stop }
    catch { Fail 'register' 4 'the room settings.json is not JSON. left as it is' }
} else { $settings = [pscustomobject]@{} }

function Get-Registered {
    param($s)
    $hits = @()
    foreach ($g in @($s.hooks.PreToolUse)) {
        foreach ($h in @($g.hooks)) { if ("$($h.command)" -match [regex]::Escape($gateName)) { $hits += $h } }
    }
    $hits
}

$already = @(Get-Registered $settings)
if ($already.Count -gt 0) {
    $first = @($settings.hooks.PreToolUse | Where-Object { "$($_.matcher)" -eq '' })
    $isFirst = $first.Count -gt 0 -and "$(@($first[0].hooks)[0].command)" -match [regex]::Escape($gateName)
    $note = if ($already.Count -gt 1) { "WARNING $($already.Count) entries name it" } elseif (-not $isFirst) { 'not first in its group, left as it is' } else { 'first' }
    Step 'register' 'ok' "the gate is already in PreToolUse ($note)"
} else {
    $entry = [pscustomobject]@{ type = 'command'; command = $hookCmd; timeout = 86400 }
    if (-not $settings.PSObject.Properties['hooks']) { $settings | Add-Member -NotePropertyName hooks -NotePropertyValue ([pscustomobject]@{}) }
    if (-not $settings.hooks.PSObject.Properties['PreToolUse']) { $settings.hooks | Add-Member -NotePropertyName PreToolUse -NotePropertyValue @() }
    $groups = @($settings.hooks.PreToolUse)
    $idx = -1
    for ($i = 0; $i -lt $groups.Count; $i++) { if ("$($groups[$i].matcher)" -eq '') { $idx = $i; break } }
    if ($idx -ge 0) {
        $groups[$idx].hooks = @($entry) + @($groups[$idx].hooks)
    } else {
        $groups = @([pscustomobject]@{ matcher = ''; hooks = @($entry) }) + $groups
    }
    $settings.hooks.PreToolUse = @($groups)
    if ($Check) {
        Step 'register' 'todo' "would put `"$hookCmd`" first in the PreToolUse group with matcher `"`", timeout 86400, after a backup of settings.json"
    } else {
        $json = ($settings | ConvertTo-Json -Depth 20)
        # Round trip before it goes anywhere: it must parse, and hold exactly one gate.
        $back = $json | ConvertFrom-Json
        if (@(Get-Registered $back).Count -ne 1) { Fail 'register' 4 'the edited settings did not hold exactly one gate. nothing written' }
        $tmp = Join-Path ([IO.Path]::GetTempPath()) "room-gate-$Room-settings.json"
        [IO.File]::WriteAllText($tmp, $json + "`n", (New-Object Text.UTF8Encoding($false)))
        try {
            $c = Copy-ToRemote $tmp '.claude/settings.json.atrium-new'
            if ($c.Code -ne 0) { Fail 'register' 4 'scp of the edited settings failed. nothing changed' $c.Out }
        } finally { Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue }
        $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
        $bak = "settings.json.atrium-$stamp.bak"
        $w = if ($script:remoteOS -eq 'windows') {
            "`$c = Join-Path `$HOME '.claude'; `$s = Join-Path `$c 'settings.json'; `$n = Join-Path `$c 'settings.json.atrium-new'`n" +
            "if (Test-Path -LiteralPath `$s) { Copy-Item -LiteralPath `$s -Destination (Join-Path `$c '$bak') -Force }`n" +
            "Copy-Item -LiteralPath `$n -Destination `$s -Force; Remove-Item -LiteralPath `$n -Force; 'ok'"
        } else {
            "c=`"`$HOME/.claude`"`n" +
            "if [ -f `"`$c/settings.json`" ]; then cp -p `"`$c/settings.json`" `"`$c/$bak`" || exit 4; fi`n" +
            # cat into the file, not mv, so a mode the operator set on settings.json survives.
            "cat `"`$c/settings.json.atrium-new`" > `"`$c/settings.json`" && rm -f `"`$c/settings.json.atrium-new`" && echo ok"
        }
        $r = Invoke-Remote $w
        if ($r.Code -ne 0) { Fail 'register' 4 'could not swap the edited settings in. the .atrium-new file is left beside it' $r.Out }
        $r = Invoke-Remote $readS
        $verify = $null
        try { $verify = ($r.Out -join "`n").TrimStart([char]0xFEFF) | ConvertFrom-Json } catch {}
        if (-not $verify -or @(Get-Registered $verify).Count -ne 1) {
            Fail 'register' 4 "settings.json does not hold exactly one gate after the swap. restore ~/.claude/$bak"
        }
        Step 'register' 'done' "gate first in PreToolUse. backup ~/.claude/$bak"
    }
}

# ── reach ───────────────────────────────────────────────────────────────────

$reachFail = $false
if ($kv.reach -match '"gate"') {
    Step 'reach' 'ok' "the room's agent listener answers on 127.0.0.1:7777 ($($kv.reach))"
} else {
    $reachFail = $true
    $why = if ($kv.reacherr) { $kv.reacherr } else { "answered '$($kv.reach)'" }
    Step 'reach' 'warn' "nothing that answers /gate on 127.0.0.1:7777 on $($script:sshTarget): $why. the gate fails open there. find the room's listener in its daemon.json and set ATRIUM_HUB_URL=http://127.0.0.1:<port> in the claude runner row's env"
}

# The runner row, read here through the hub. Best effort: no hub, no row, or no answer is a skip, not a fail.
try {
    $rows = Invoke-RestMethod -Uri "http://$HubAddr/v1/harnesses" -Headers @{ 'X-Atrium-Room' = $Room } -TimeoutSec 10
    $row = @($rows) + @($rows.harnesses) | Where-Object { $_ -and $_.id -eq 'claude' } | Select-Object -First 1
    if (-not $row) {
        Step 'runner' 'skip' "$Room has no claude runner row"
    } else {
        $envNames = @($row.env.PSObject.Properties)
        $g = $envNames | Where-Object { $_.Name -ieq 'ATRIUM_PERM_GATE' } | Select-Object -First 1
        $u = $envNames | Where-Object { $_.Name -ieq 'ATRIUM_HUB_URL' } | Select-Object -First 1
        if ($g -and "$($g.Value)".ToLower() -eq 'off') { Step 'runner' 'warn' "the claude row on $Room sets ATRIUM_PERM_GATE=off, so its sessions are not gated" }
        elseif ($u) { Step 'runner' 'warn' "the claude row on $Room sets ATRIUM_HUB_URL=$($u.Value). the gate posts there, not to 7777" }
        else { Step 'runner' 'ok' "the claude row on $Room leaves ATRIUM_PERM_GATE to the launch default (on)" }
    }
} catch {
    Step 'runner' 'skip' "could not read the runner row from http://$HubAddr for $Room"
}

if ($reachFail) { Finish 5 }
Finish 0
