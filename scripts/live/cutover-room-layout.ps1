# One-shot move of sg4's two rooms, claude-sg4 and sg4-control, into one folder per room under $AtriumRoot.
#
# RUN FROM A PLAIN POWERSHELL WINDOW. It stops both rooms, both pty hosts and the hub, so a shell inside any of them
# dies halfway through.
#
#     pwsh -File <repo>\scripts\live\cutover-room-layout.ps1 -From <new atrium.exe>
#
# In order: checks everything it needs before it stops anything, runs stop-atrium.ps1 -Control
# -IKnowItKillsMyShell, ends any pty host still running from bin or ctl-bin, installs -From into bin and ctl-bin, runs
# `atrium room move` for each room, installs these scripts as the live ones, and runs start-atrium.ps1. The old folders
# are left as they were. -WhatIf says what it would do and does nothing.
param([string]$From, [switch]$WhatIf,
  [string]$RoomDir = 'C:\Users\claude\.atrium2\room', [string]$RoomDb = 'C:\Users\claude\.atrium\atrium.db',
  [string]$CtlDir = 'C:\Users\claude\.atrium2\ctl', [string]$CtlDb = 'C:\Users\claude\.atrium2\ctl\atrium.db')

$ErrorActionPreference = 'Continue'
$LiveTag = 'CUTOVER'
. "$PSScriptRoot\refuse-supervised.ps1"
Assert-NotSupervised
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'cutover.log'
if (-not $From) { $From = $AtriumNew }

$moves = @(
  @{ Name = $RoomName; Dir = $RoomDir; Db = $RoomDb },
  @{ Name = $CtlName; Dir = $CtlDir; Db = $CtlDb }
)

$bad = @()
if (-not (Test-Path $AtriumRoot -PathType Container)) { $bad += "$AtriumRoot does not exist. create it first" }
if (-not (Test-Path $From)) { $bad += "no build at $From" }
else {
  & $From room move --help *> $null
  if ($LASTEXITCODE) { $bad += "$From has no ``room move``. build it from a branch that has it" }
}
foreach ($m in $moves) {
  foreach ($p in $m.Dir, $m.Db) { if (-not (Test-Path $p)) { $bad += "$($m.Name): $p does not exist" } }
  $cfg = Join-Path $AtriumRoot "$($m.Name)\config"
  if ((Test-Path $cfg) -and (Get-ChildItem $cfg -Force | Select-Object -First 1)) {
    $bad += "$($m.Name): $cfg already holds files"
  }
}
if ($bad.Count) { $bad | ForEach-Object { Say "refusing: $_" }; Say 'nothing was stopped'; exit 1 }

& "$PSScriptRoot\stop-atrium.ps1" -Control -IKnowItKillsMyShell -WhatIf:$WhatIf

# Stop-Control has no runners flag, so the control room's pty host outlives it. A pty host left on the old folder
# would hold runners the moved room never finds.
$hosts = Get-CimInstance Win32_Process | Where-Object {
  $_.ExecutablePath -and ((Split-Path $_.ExecutablePath) -in $AtriumBinDir, $CtlBinDir) -and
  ($_.CommandLine -match ' ptyhost( |$)')
}
foreach ($p in $hosts) {
  Invoke-Step "stop pty host pid $($p.ProcessId) ($($p.ExecutablePath))" {
    Stop-Process -Id $p.ProcessId -Force
    try { Wait-Process -Id $p.ProcessId -Timeout 10 -ErrorAction Stop } catch {}
  }
}

if (-not $WhatIf) {
  $left = @(Find-Atrium run) + @(Find-Atrium room) + @(Find-Atrium ptyhost) + @(Find-Control) |
    Where-Object { $_ } | ForEach-Object { "pid $($_.ProcessId)" }
  if ($left.Count) { Say "refusing to move: still running: $($left -join ', '). nothing was moved"; exit 1 }
}

if (-not (Install-Atrium $From)) { Say 'the new build is not in place. nothing was moved. start-atrium.ps1 brings the old layout back'; exit 1 }
$ctlExe = Join-Path $CtlBinDir 'atrium.exe'
Invoke-Step "copy $From -> $ctlExe" { Copy-Item $From $ctlExe -Force }

foreach ($m in $moves) {
  $argv = @('room', 'move', '--root', $AtriumRoot, '--name', $m.Name, '--dir', $m.Dir, '--db', $m.Db)
  Invoke-Step "$AtriumBin $($argv -join ' ')" {
    & $AtriumBin @argv 2>&1 | ForEach-Object { Say "  $_" }
    if ($LASTEXITCODE) { Say "room move for $($m.Name) failed. nothing old changed. stopping here"; exit 1 }
  }
}

Invoke-Step "install-live-scripts.ps1" { & "$PSScriptRoot\install-live-scripts.ps1" | ForEach-Object { Say "  $_" } }
& "$PSScriptRoot\start-atrium.ps1" -WhatIf:$WhatIf
if ($LASTEXITCODE) { exit $LASTEXITCODE }
if (-not $WhatIf) { Say "both rooms run from $AtriumRoot. the old folders are untouched until you delete them" }
