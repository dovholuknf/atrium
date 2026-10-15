# Tests for scripts/room-gate.ps1: a room gets ONE permission gate, atrium's own. Needs no ssh, no hub and no room:
#
#   pwsh -NoProfile -File scripts/test-room-gate.ps1
#
# room-gate.ps1 is RUN here with -Local on a temp .claude folder, so the real ~/.claude is never read or written.
$ErrorActionPreference = 'Stop'

$script:failed = 0
$script:ran = 0
function Check {
    param([string] $name, $got, $want)
    $script:ran++
    $g = (@($got) | ForEach-Object { "$_" }) -join '|'
    $w = (@($want) | ForEach-Object { "$_" }) -join '|'
    if ($g -ceq $w) { Write-Host "ok   $name" }
    else { $script:failed++; Write-Host "FAIL $name`n     want: $w`n     got:  $g" }
}

$gate = Join-Path $PSScriptRoot 'room-gate.ps1'
$legacyCmd = 'pwsh -NoProfile -File C:/Users/x/.claude/hooks/atrium-perm-hook.ps1'
$ownCmd = 'C:/atrium/atrium.exe hook --event permission'

function New-Claude {
    param($pre)
    $dir = Join-Path ([IO.Path]::GetTempPath()) ("room-gate-test-" + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Force -Path (Join-Path $dir 'hooks') | Out-Null
    Set-Content -LiteralPath (Join-Path $dir "hooks/atrium-perm-hook.ps1") -Value '# old gate'
    $s = [ordered]@{ model = 'opus'; hooks = [ordered]@{ PreToolUse = $pre; SessionStart = @(@{ matcher = ''; hooks = @(@{ type = 'command'; command = 'C:/atrium/atrium.exe session --event start' }) }) } }
    ($s | ConvertTo-Json -Depth 20) | Set-Content -LiteralPath (Join-Path $dir 'settings.json')
    $dir
}
function Get-Pre { param($dir) @((Get-Content -LiteralPath (Join-Path $dir 'settings.json') -Raw | ConvertFrom-Json).hooks.PreToolUse) }
function Get-PreCommands { param($dir) @(Get-Pre $dir | ForEach-Object { @($_.hooks) } | ForEach-Object { $_.command }) }

# ── both gates become one ───────────────────────────────────────────────────

$dir = New-Claude @(
    @{ matcher = ''; hooks = @(@{ type = 'command'; command = $legacyCmd; timeout = 86400 }, @{ type = 'command'; command = $ownCmd; timeout = 86400 }, @{ type = 'command'; command = 'C:/atrium/atrium.exe hook --event tool-start' }) },
    @{ matcher = 'Bash'; hooks = @(@{ type = 'command'; command = 'echo keep' }) })
try {
    $out = & pwsh -NoProfile -File $gate t -Local $dir 2>&1 | ForEach-Object { "$_" }
    Check 'both gates: exit 0' $LASTEXITCODE 0
    Check 'both gates: the check named two gates before fixing' (@($out | Where-Object { $_ -match '^room-gate legacy warn two permission gates: atrium-perm-hook.ps1 and atrium hook' }).Count) 1
    Check 'both gates: one gate left' (@(Get-PreCommands $dir | Where-Object { $_ -match 'hook --event permission' }).Count) 1
    Check 'both gates: no old script entry' (@(Get-PreCommands $dir | Where-Object { $_ -match 'atrium-perm-hook' }).Count) 0
    Check 'both gates: other hooks stay' (@(Get-PreCommands $dir | Where-Object { $_ -match 'tool-start|echo keep' }).Count) 2
    Check 'both gates: other keys stay' ((Get-Content -LiteralPath (Join-Path $dir 'settings.json') -Raw | ConvertFrom-Json).model) 'opus'
    Check 'both gates: one backup, timestamped' (@(Get-ChildItem -LiteralPath $dir -Filter 'settings.json.bak-*' | Where-Object { $_.Name -match '^settings\.json\.bak-\d{14}$' }).Count) 1
    $bak = Get-ChildItem -LiteralPath $dir -Filter 'settings.json.bak-*' | Select-Object -First 1
    Check 'both gates: the backup holds the old script entry' ((Get-Content -LiteralPath $bak.FullName -Raw) -match 'atrium-perm-hook') $true
    Check 'both gates: the old script file is gone' (Test-Path -LiteralPath (Join-Path $dir 'hooks/atrium-perm-hook.ps1')) $false

    # A second run finds nothing to do and writes no second backup.
    $out2 = & pwsh -NoProfile -File $gate t -Local $dir 2>&1 | ForEach-Object { "$_" }
    Check 'again: exit 0' $LASTEXITCODE 0
    Check 'again: still one backup' (@(Get-ChildItem -LiteralPath $dir -Filter 'settings.json.bak-*').Count) 1
    Check 'again: legacy ok' (@($out2 | Where-Object { $_ -match '^room-gate legacy ok' }).Count) 1
} finally { Remove-Item -LiteralPath $dir -Recurse -Force -ErrorAction SilentlyContinue }

# ── only the old script: replaced by atrium's gate, first in its group ──────

$dir = New-Claude @(@{ matcher = ''; hooks = @(@{ type = 'command'; command = $legacyCmd }, @{ type = 'command'; command = 'C:/atrium/atrium.exe hook --event tool-start' }) })
try {
    & pwsh -NoProfile -File $gate t -Local $dir *> $null
    Check 'old only: exit 0' $LASTEXITCODE 0
    $cmds = Get-PreCommands $dir
    Check 'old only: atrium gate first' $cmds[0] $ownCmd
    Check 'old only: the old one is gone' (@($cmds | Where-Object { $_ -match 'atrium-perm-hook' }).Count) 0
    Check 'old only: the gate keeps its day timeout' ((Get-Pre $dir)[0].hooks[0].timeout) 86400
} finally { Remove-Item -LiteralPath $dir -Recurse -Force -ErrorAction SilentlyContinue }

# ── a room whose hooks run under PowerShell: `& "path" hook ...` with "shell": "powershell" ─

$psExe = '& "C:/Program Files/atrium/atrium.exe"'
$dir = New-Claude @(@{ matcher = ''; hooks = @(@{ type = 'command'; shell = 'powershell'; command = $legacyCmd }, @{ type = 'command'; shell = 'powershell'; command = "$psExe hook --event tool-start" }) })
try {
    & pwsh -NoProfile -File $gate t -Local $dir *> $null
    Check 'powershell form: exit 0' $LASTEXITCODE 0
    $g = @(Get-Pre $dir | ForEach-Object { @($_.hooks) } | Where-Object { $_.command -match 'hook\s+--event\s+permission\b' })
    Check 'powershell form: one gate' $g.Count 1
    Check 'powershell form: gate keeps the call operator' $g[0].command "$psExe hook --event permission"
    Check 'powershell form: gate carries the shell key' $g[0].shell 'powershell'
    & pwsh -NoProfile -File $gate t -Local $dir *> $null
    Check 'powershell form: a second run adds no second gate' (@(Get-PreCommands $dir | Where-Object { $_ -match 'hook --event permission' }).Count) 1
} finally { Remove-Item -LiteralPath $dir -Recurse -Force -ErrorAction SilentlyContinue }

# ── -Check writes nothing ───────────────────────────────────────────────────

$dir = New-Claude @(@{ matcher = ''; hooks = @(@{ type = 'command'; command = $legacyCmd }, @{ type = 'command'; command = $ownCmd }) })
try {
    $before = Get-Content -LiteralPath (Join-Path $dir 'settings.json') -Raw
    $out = & pwsh -NoProfile -File $gate t -Check -Local $dir 2>&1 | ForEach-Object { "$_" }
    Check '-Check: settings untouched' ((Get-Content -LiteralPath (Join-Path $dir 'settings.json') -Raw) -ceq $before) $true
    Check '-Check: no backup' (@(Get-ChildItem -LiteralPath $dir -Filter 'settings.json.bak-*').Count) 0
    Check '-Check: script kept' (Test-Path -LiteralPath (Join-Path $dir 'hooks/atrium-perm-hook.ps1')) $true
    Check '-Check: says todo for legacy' (@($out | Where-Object { $_ -match '^room-gate legacy todo' }).Count) 1
} finally { Remove-Item -LiteralPath $dir -Recurse -Force -ErrorAction SilentlyContinue }

# ── a room with no atrium hook at all: the old entry still goes, and it says why no gate was added ─

$dir = Join-Path ([IO.Path]::GetTempPath()) ("room-gate-test-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $dir | Out-Null
try {
    '{"hooks":{"PreToolUse":[{"matcher":"","hooks":[{"type":"command","command":"' + $legacyCmd + '"}]}]}}' | Set-Content -LiteralPath (Join-Path $dir 'settings.json')
    $out = & pwsh -NoProfile -File $gate t -Local $dir 2>&1 | ForEach-Object { "$_" }
    Check 'no atrium: exit 4' $LASTEXITCODE 4
    Check 'no atrium: says to install the hooks' (@($out | Where-Object { $_ -match '^room-gate register fail .*hooks/install' }).Count) 1
    Check 'no atrium: the old entry is gone' (@((Get-Content -LiteralPath (Join-Path $dir 'settings.json') -Raw) -match 'atrium-perm-hook')) $false
} finally { Remove-Item -LiteralPath $dir -Recurse -Force -ErrorAction SilentlyContinue }

# ── the callers ─────────────────────────────────────────────────────────────

$src = Get-Content -LiteralPath $gate -Raw
Check 'room-gate: never copies the old script to a room' ($src -match 'Copy-ToRemote \$Source|\$Source =') $false
$e = $null
[Management.Automation.Language.Parser]::ParseFile($gate, [ref] $null, [ref] $e) | Out-Null
Check 'room-gate: parses' @($e).Count 0

if ($script:failed) { Write-Host "$script:failed of $script:ran checks FAILED"; exit 1 }
Write-Host "all $script:ran checks pass"
