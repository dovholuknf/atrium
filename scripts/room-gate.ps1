# The permission gate for a room, run from THIS side over ssh: ONE gate, atrium's own.
#
#   pwsh -File scripts\room-gate.ps1 m1mini -Check
#   pwsh -File scripts\room-gate.ps1 m1mini
#   pwsh -File scripts\room-gate.ps1 sg3 -Target user@host
#   pwsh -File scripts\room-gate.ps1 x -Local <a .claude folder>      # this machine, no ssh. The tests use it
#
# THE GATE IS `atrium hook --event permission`, registered in ~/.claude/settings.json by atrium's own hooks install. The
# dotfiles script ~/.claude/hooks/atrium-perm-hook.ps1 is the gate it replaced, and atrium's hooks are the superset. With
# both registered every tool call asks the human twice (TwoGates), so this script never installs the script. It takes it
# out.
#
# THE STEPS, in order:
#   ssh       reach the room and read its home
#   legacy    a PreToolUse entry naming atrium-perm-hook.ps1 is removed from settings.json. Nothing else in the file
#             changes. Both gates registered is said as `two permission gates`
#   register  atrium's own gate (`<atrium> hook --event permission`, timeout 86400) is put first in the PreToolUse group
#             whose matcher is "" when it is absent. The atrium path is read from the room's other atrium hook
#             commands, so a room with none says so and is left for POST /v1/hooks/install
#   script    the old script file ~/.claude/hooks/atrium-perm-hook.ps1 is deleted
#   reach     the gate POSTs to $ATRIUM_HUB_URL, default http://127.0.0.1:7777. The room's agent listener must be
#             there. It asks GET /gate?agent=probe first, and so does this. It also reads the room's runner row
#             here and warns when that row switches the gate off
#
# settings.json is written ONCE, after a timestamped backup beside it, settings.json.bak-<yyyymmddHHmmss>. -Check
# reports every step and writes nothing. A room needs no restart: claude reads settings.json per session.
#
# THE SETTINGS EDIT happens HERE, in pwsh 7: the file is read down, edited with its key order kept (ConvertTo-Json
# -Depth 20), and put back with scp. The remote never needs jq, python or a PowerShell that can edit JSON. Formatting
# changes to two-space indent. Nothing else does.
#
# THE REMOTE runs plain `sh -s` on Unix and Windows PowerShell 5.1 (-EncodedCommand) on Windows, the way room-git.ps1 does.
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
#   1  a local problem: bad arguments
#   2  ssh could not reach the target, or its OS is not one this covers
#   4  a remote step failed, or the room has no atrium hook to take the atrium path from. Nothing half-written is left
#      behind on the settings file
#   5  the room's agent listener does not answer on 7777, so the gate would fail open. The gate is installed anyway

param(
    [Parameter(Position = 0)] [string] $Room,
    # The ssh destination. Default: the room's own name, as an ssh alias.
    [string] $Target,
    [switch] $Check,
    # Where the room's runner row is read, for the reach step. The hub on this machine.
    [string] $HubAddr = '127.0.0.1:7778',
    # A .claude folder on THIS machine to work on instead of a room over ssh. No ssh, no reach, no runner row.
    [string] $Local,
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

if (-not $Room) { Write-Host 'usage: room-gate.ps1 <room> [-Target user@host] [-Check] [-Local <.claude folder>]'; exit 1 }
if ($Room -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]*$') { Fail 'args' 1 "bad room name '$Room'" }

$sshBase = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=25') + $SshOption
$script:sshTarget = if ($Target) { $Target } else { $Room }
$script:remoteOS = $null
$legacyName = 'atrium-perm-hook.ps1'
$legacyRe = [regex]::Escape('atrium-perm-hook')
# atrium's own gate: `<atrium> hook --event permission`.
$ownRe = 'hook\s+--event\s+permission\b'

# ── the remote ──────────────────────────────────────────────────────────────

function Invoke-Remote {
    param([string] $script)
    if ($script:remoteOS -eq 'windows') {
        $full = "`$ErrorActionPreference='Continue'; `$ProgressPreference='SilentlyContinue'`n" + $script
        $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($full))
        if ($enc.Length -gt 7800) { throw "remote script too long for cmd.exe ($($enc.Length))" }
        $out = & $Ssh @sshBase $script:sshTarget "powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand $enc" 2>&1
    } else {
        # A login shell's PATH. A COMMENT LAST: PowerShell ends what it pipes with CRLF.
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

# ── the settings edit, pure ─────────────────────────────────────────────────

function Get-Commands {
    param($s)
    foreach ($g in @($s.hooks.PreToolUse)) { foreach ($h in @($g.hooks)) { "$($h.command)" } }
}

# Update-Gates takes the legacy entries out and puts atrium's gate in when it is absent.
function Update-Gates {
    param($s)
    $removed = 0; $added = $false; $noExe = $false
    $kept = @()
    foreach ($g in @($s.hooks.PreToolUse | Where-Object { $_ })) {
        $hs = @($g.hooks)
        $keep = @($hs | Where-Object { "$($_.command)" -notmatch $legacyRe })
        $removed += $hs.Count - $keep.Count
        if ($keep.Count -eq $hs.Count) { $kept += $g; continue }
        # A group left with no command goes too.
        if ($keep.Count -gt 0) { $g.hooks = $keep; $kept += $g }
    }
    $have = @($kept | ForEach-Object { @($_.hooks) } | Where-Object { "$($_.command)" -match $ownRe })
    if (-not $have.Count) {
        # The atrium path comes from the room's own other atrium hook commands.
        # $exe keeps a leading `& ` when the room's hooks run under PowerShell, and the new entry copies their `shell` key.
        $exe = $null; $shell = $null
        foreach ($ev in @($s.hooks.PSObject.Properties)) {
            foreach ($g in @($ev.Value)) {
                foreach ($h in @($g.hooks)) {
                    $c = "$($h.command)"
                    if (-not $exe -and $c -match 'atrium' -and $c -notmatch $legacyRe -and $c -match '^(.+?)\s+(hook|session|turn)\s+--event\s') {
                        $exe = $Matches[1]
                        if ($h.PSObject.Properties['shell']) { $shell = "$($h.shell)" }
                    }
                }
            }
        }
        if ($exe) {
            $entry = [pscustomobject]@{ type = 'command'; command = "$exe hook --event permission"; timeout = 86400 }
            if ($shell) { $entry | Add-Member -NotePropertyName shell -NotePropertyValue $shell }
            $idx = -1
            for ($i = 0; $i -lt $kept.Count; $i++) { if ("$($kept[$i].matcher)" -eq '') { $idx = $i; break } }
            if ($idx -ge 0) { $kept[$idx].hooks = @($entry) + @($kept[$idx].hooks) }
            else { $kept = @([pscustomobject]@{ matcher = ''; hooks = @($entry) }) + $kept }
            $added = $true
        } else { $noExe = $true }
    }
    if ($removed -gt 0 -or $added) {
        if (-not $s.PSObject.Properties['hooks']) { $s | Add-Member -NotePropertyName hooks -NotePropertyValue ([pscustomobject]@{}) }
        if ($s.hooks.PSObject.Properties['PreToolUse']) { $s.hooks.PreToolUse = @($kept) }
        else { $s.hooks | Add-Member -NotePropertyName PreToolUse -NotePropertyValue @($kept) }
    }
    [pscustomobject]@{ Settings = $s; Removed = $removed; Added = $added; NoExe = $noExe; Have = $have.Count }
}

# ── ssh, or a local folder ──────────────────────────────────────────────────

if ($Local) {
    $script:remoteOS = 'local'
    $claudeDir = (New-Item -ItemType Directory -Force -Path $Local).FullName
    Step 'ssh' 'skip' "-Local $($claudeDir -replace '\\', '/')"
} else {
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
}

# What the room has now, in one round trip.
$facts = if ($script:remoteOS -eq 'windows') {
    @'
"home=$($HOME -replace '\\', '/')"
"script=$(if (Test-Path -LiteralPath (Join-Path $HOME '.claude\hooks\atrium-perm-hook.ps1')) { 'yes' } else { 'no' })"
"settings=$(if (Test-Path -LiteralPath (Join-Path $HOME '.claude\settings.json')) { 'yes' } else { 'no' })"
try { "reach=$((Invoke-WebRequest -UseBasicParsing -TimeoutSec 4 'http://127.0.0.1:7777/gate?agent=probe').Content)" } catch { "reacherr=$($_.Exception.Message)" }
'@
} else {
    @'
echo "home=$HOME"
[ -f "$HOME/.claude/hooks/atrium-perm-hook.ps1" ] && echo script=yes || echo script=no
[ -f "$HOME/.claude/settings.json" ] && echo settings=yes || echo settings=no
r=$(curl -s -m 4 'http://127.0.0.1:7777/gate?agent=probe') && echo "reach=$r" || echo "reacherr=curl exit $?"
'@
}
if ($Local) {
    $kv = @{
        script   = if (Test-Path -LiteralPath (Join-Path $claudeDir "hooks/$legacyName")) { 'yes' } else { 'no' }
        settings = if (Test-Path -LiteralPath (Join-Path $claudeDir 'settings.json')) { 'yes' } else { 'no' }
    }
} else {
    $r = Invoke-Remote $facts
    $kv = ConvertFrom-KeyValue $r.Out
    if (-not $kv.home) { Fail 'ssh' 2 'could not read the remote home' $r.Out }
}

$readS = if ($script:remoteOS -eq 'windows') {
    '$f = Join-Path $HOME ".claude\settings.json"; if (Test-Path -LiteralPath $f) { [IO.File]::ReadAllText($f) }'
} else { 'f="$HOME/.claude/settings.json"; [ -f "$f" ] && cat "$f"' }
function Read-Settings {
    if ($Local) {
        $f = Join-Path $claudeDir 'settings.json'
        if (Test-Path -LiteralPath $f) { return @{ Code = 0; Text = [IO.File]::ReadAllText($f).TrimStart([char]0xFEFF) } }
        return @{ Code = 0; Text = '' }
    }
    $r = Invoke-Remote $readS
    @{ Code = $r.Code; Text = ($r.Out -join "`n").TrimStart([char]0xFEFF); Out = $r.Out }
}

$settingsText = ''
if ($kv.settings -eq 'yes') {
    $rs = Read-Settings
    if ($rs.Code -ne 0) { Fail 'legacy' 4 'could not read settings.json on the room' $rs.Out }
    $settingsText = $rs.Text
}
$settings = $null
if ($settingsText.Trim()) {
    try { $settings = $settingsText | ConvertFrom-Json -ErrorAction Stop }
    catch { Fail 'legacy' 4 'the room settings.json is not JSON. left as it is' }
} else { $settings = [pscustomobject]@{} }

$hadLegacy = @(Get-Commands $settings | Where-Object { $_ -match $legacyRe }).Count
$hadOwn = @(Get-Commands $settings | Where-Object { $_ -notmatch $legacyRe -and $_ -match $ownRe }).Count
$edit = Update-Gates $settings
$changed = $edit.Removed -gt 0 -or $edit.Added
$stamp = Get-Date -Format 'yyyyMMddHHmmss'
$bak = "settings.json.bak-$stamp"

# ── legacy and register: one write ──────────────────────────────────────────

if ($hadLegacy -gt 0 -and $hadOwn -gt 0) {
    Step 'legacy' 'warn' "two permission gates: $legacyName and atrium hook, so every tool call asks twice"
}
if ($changed -and -not $Check) {
    $json = ($edit.Settings | ConvertTo-Json -Depth 20)
    # Round trip before it goes anywhere: it must parse, name no legacy script, and hold one gate of atrium's.
    $cmds = @(Get-Commands ($json | ConvertFrom-Json))
    if (@($cmds | Where-Object { $_ -match $legacyRe }).Count -ne 0 -or @($cmds | Where-Object { $_ -match $ownRe }).Count -gt 1) {
        Fail 'register' 4 'the edited settings did not come out with one gate. nothing written'
    }
    if ($Local) {
        $s = Join-Path $claudeDir 'settings.json'
        if (Test-Path -LiteralPath $s) { Copy-Item -LiteralPath $s -Destination (Join-Path $claudeDir $bak) -Force }
        [IO.File]::WriteAllText($s, $json + "`n", (New-Object Text.UTF8Encoding($false)))
    } else {
        $tmp = Join-Path ([IO.Path]::GetTempPath()) "room-gate-$Room-settings.json"
        [IO.File]::WriteAllText($tmp, $json + "`n", (New-Object Text.UTF8Encoding($false)))
        try {
            $c = Copy-ToRemote $tmp '.claude/settings.json.atrium-new'
            if ($c.Code -ne 0) { Fail 'register' 4 'scp of the edited settings failed. nothing changed' $c.Out }
        } finally { Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue }
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
    }
    $verify = $null
    try { $verify = (Read-Settings).Text | ConvertFrom-Json } catch {}
    if (-not $verify -or @(Get-Commands $verify | Where-Object { $_ -match $legacyRe }).Count -ne 0) {
        Fail 'register' 4 "settings.json still names $legacyName after the swap. restore ~/.claude/$bak"
    }
}
$word = if ($Check) { 'todo' } else { 'done' }
if ($edit.Removed) {
    Step 'legacy' $word "$(if ($Check) { 'would remove' } else { 'removed' }) $($edit.Removed) PreToolUse entry naming $legacyName. backup ~/.claude/$bak"
} else { Step 'legacy' 'ok' "no registered entry names $legacyName" }
if ($edit.Added) {
    Step 'register' $word "$(if ($Check) { 'would put' } else { 'put' }) atrium's hook --event permission first in PreToolUse, timeout 86400"
} elseif ($edit.NoExe) {
    Step 'register' 'fail' "no atrium hook is registered on $Room to take the atrium path from. run POST /v1/hooks/install there"
} else { Step 'register' 'ok' "atrium's hook --event permission is in PreToolUse" }

# ── the old script file ─────────────────────────────────────────────────────

if ($kv.script -eq 'yes') {
    if ($Check) { Step 'script' 'todo' "would delete ~/.claude/hooks/$legacyName" }
    else {
        if ($Local) { Remove-Item -LiteralPath (Join-Path $claudeDir "hooks/$legacyName") -Force }
        else {
            $d = if ($script:remoteOS -eq 'windows') { 'Remove-Item -LiteralPath (Join-Path $HOME ".claude\hooks\atrium-perm-hook.ps1") -Force; "ok"' }
            else { 'rm -f "$HOME/.claude/hooks/atrium-perm-hook.ps1" && echo ok' }
            $r = Invoke-Remote $d
            if ($r.Code -ne 0) { Fail 'script' 4 "could not delete ~/.claude/hooks/$legacyName" $r.Out }
        }
        Step 'script' 'done' "deleted ~/.claude/hooks/$legacyName"
    }
} else {
    Step 'script' 'ok' "no ~/.claude/hooks/$legacyName"
}

# ── reach ───────────────────────────────────────────────────────────────────

if ($Local) { Finish $(if ($edit.NoExe) { 4 } else { 0 }) }

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

if ($edit.NoExe) { Finish 4 }
if ($reachFail) { Finish 5 }
Finish 0
