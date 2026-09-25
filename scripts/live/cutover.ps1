# The one-atrium cutover on this machine: atrium2.exe out, atrium.exe in, for the hub, the room and the hooks at once.
#
# docs/one-atrium-cutover.md is the runbook, and says what to check before and after. This is its window, steps 4 to
# 11 and 16 to 17, as one script, so nothing is typed by hand while every agent is down.
#
# RUN IT DETACHED, from a shell atrium does not supervise, with every peer idle. Stopping the room ends every
# supervised terminal, the orchestrator's included, and the room resumes them on start:
#
#   Start-Process pwsh -WindowStyle Hidden -ArgumentList '-NoProfile','-File',`
#     'D:\worktrees\claude\atrium\orchestrator\scripts\live\cutover.ps1'
#
# -WhatIf prints every process it would stop and every file it would copy or rename, and changes nothing.
# -Rollback puts the atrium2 pair back (runbook step 16) and does nothing else.
#
# Everything is logged to C:\Users\claude\.atrium2\deploy-batch.log, tagged CUTOVER.
param([switch]$WhatIf, [switch]$Rollback)

$ErrorActionPreference = 'Continue'
$LiveTag = 'CUTOVER'
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'deploy-batch.log'

$OldBin      = Join-Path $Base 'bin\atrium2.exe'
$PreCutover  = Join-Path $AtriumBinDir 'atrium.pre-cutover.exe'
$Scripts     = Join-Path $Base 'scripts'
$ScriptsKeep = Join-Path $Base 'scripts.pre-cutover'
$OldLocation = Join-Path $env:LOCALAPPDATA 'atrium2\room\daemon.json'
$NewLocation = Join-Path $env:LOCALAPPDATA 'atrium\daemon.json'

# The old pair is found the old way. Name='atrium2.exe' is safe here: no hook runs atrium2.exe.
function Find-Old([string]$Sub) {
  Get-CimInstance Win32_Process -Filter "Name='atrium2.exe'" | Where-Object { $_.CommandLine -match " $Sub " }
}

function Restore-Old {
  Say 'ROLLBACK: putting the atrium2 pair back'
  foreach ($p in @(Find-Atrium room) + @(Find-Atrium run)) {
    if ($p) { Invoke-Step "stop pid $($p.ProcessId) ($($p.CommandLine))" { Stop-Process -Id $p.ProcessId -Force } }
  }
  if (Test-Path $PreCutover) {
    $failed = Join-Path $AtriumBinDir "atrium.failed-$(Get-Date -Format yyyyMMddHHmmss).exe"
    Invoke-Step "rename $AtriumBin -> $failed, and $PreCutover -> $AtriumBin" {
      Rename-Item $AtriumBin $failed
      Rename-Item $PreCutover (Split-Path $AtriumBin -Leaf)
    }
  } else { Say "WARN: no $PreCutover, so the hook binary is left as it is" }
  if (Test-Path $ScriptsKeep) {
    Invoke-Step "put the old scripts back: $ScriptsKeep -> $Scripts" {
      Remove-Item $Scripts -Recurse -Force
      Rename-Item $ScriptsKeep (Split-Path $Scripts -Leaf)
    }
  }
  if (Test-Path $NewLocation) {
    Invoke-Step "remove $NewLocation, which the new room wrote" { Remove-Item $NewLocation -Force }
  }
  Invoke-Step "start $OldBin hub, then room, with ATRIUM_LOCATION=$OldLocation" {
    Clear-SessionEnv
    $env:ATRIUM_LOCATION = $OldLocation
    Start-Process -FilePath $OldBin -WindowStyle Hidden `
      -ArgumentList 'hub', '--addr', '127.0.0.1:7778', '--link', '0.0.0.0:7779',
        '--link-advertise', '192.168.1.68:7779', '--dir', $HubDir `
      -RedirectStandardOutput (Join-Path $Base 'hub.out') -RedirectStandardError (Join-Path $Base 'hub.err')
    if (-not (Wait-Hub 25)) { Say 'ROLLBACK: the old hub did not come healthy. see hub.err' }
    Start-Process -FilePath $OldBin -WindowStyle Hidden `
      -ArgumentList 'room', '--dir', $RoomDir, '--db', $RoomDb, '--http', '127.0.0.1:7781',
        '--agent', '127.0.0.1:7777' `
      -RedirectStandardOutput (Join-Path $Base 'room.out') -RedirectStandardError (Join-Path $Base 'room.err')
    $h = Wait-Hub 60 1
    Say "ROLLBACK: old room reattached: $([bool]$h) build $($h.build) rooms=$($h.rooms)"
  }
}

if ($Rollback) { Restore-Old; exit 0 }

# ── before the window: nothing here stops anything ──

if (-not (Test-Path $AtriumNew)) { Say "FATAL: no new build at $AtriumNew"; exit 1 }
# The build has to be the one binary: `run`, `room` and `hook` all parse. An older atrium.exe has none of the first two.
foreach ($probe in @(@('run', '--no-room', '--help'), @('room', '--help'), @('hook', '--help'))) {
  & $AtriumNew @probe *> $null
  if ($LASTEXITCODE -ne 0) { Say "FATAL: $AtriumNew does not answer '$($probe -join ' ')'. not the one binary"; exit 1 }
}
Say "$AtriumNew is $(& $AtriumNew version --short)"
$oldRoom = Find-Old room
$oldHub = Find-Old hub
if (-not $oldRoom -or -not $oldHub) {
  Say ("FATAL: expected the atrium2 hub and room running, found hub=$([bool]$oldHub) room=$([bool]$oldRoom). " +
    "already cut over?")
  exit 1
}
Say "found atrium2 hub pid $($oldHub.ProcessId) and room pid $($oldRoom.ProcessId)"
$lags = Get-CimInstance Win32_Process |
  Where-Object { $_.ExecutablePath -eq $AtriumBin -and $_.CommandLine -match ' (hook|session|turn) ' }
Say "hooks running from $AtriumBin right now: $(@($lags).Count) (they keep their image through the rename)"

# Step 4 and 5: stage the build and keep a copy of today's hook binary to go back to.
$next = Join-Path $AtriumBinDir 'atrium.next.exe'
Invoke-Step "copy $AtriumNew -> $next (staged, nothing has it open)" { Copy-Item $AtriumNew $next -Force }
Invoke-Step "copy $AtriumBin -> $PreCutover (the rollback hook binary)" { Copy-Item $AtriumBin $PreCutover -Force }
Say "left in place, untouched: $OldBin (the rollback atrium2)"

# ── the window ──

# Step 6: stop the room gracefully, so it parks its runners and saves what to reopen.
foreach ($p in $oldRoom) {
  Invoke-Step "stop atrium2 room pid $($p.ProcessId): POST $RoomShutdown, wait up to 45s, then force" {
    try { Invoke-RestMethod $RoomShutdown -Method Post -TimeoutSec 8 | Out-Null } catch { Say "shutdown post: $_" }
    try { Wait-Process -Id $p.ProcessId -Timeout 45 -ErrorAction Stop } catch {}
    if (Get-Process -Id $p.ProcessId -ErrorAction SilentlyContinue) {
      Say 'room still up after 45s, force stop'; Stop-Process -Id $p.ProcessId -Force
    }
  }
}

# Step 7: the database, now that nothing has it open.
foreach ($suffix in '', '-wal', '-shm') {
  $f = "$RoomDb$suffix"
  if (Test-Path $f) {
    Invoke-Step "copy $f -> $RoomDb.pre-cutover$suffix" { Copy-Item $f "$RoomDb.pre-cutover$suffix" -Force }
  }
}

# Step 8: stop the hub. It holds nothing.
foreach ($p in $oldHub) {
  Invoke-Step "stop atrium2 hub pid $($p.ProcessId)" {
    Stop-Process -Id $p.ProcessId -Force
    try { Wait-Process -Id $p.ProcessId -Timeout 10 -ErrorAction Stop } catch {}
  }
}

# Step 9: swap the hook binary with two renames. Install-Atrium re-stages from the build, which is the same file.
if (-not (Install-Atrium)) { Restore-Old; exit 1 }

# The scripts, in the same window. After this moment the old ones would look for atrium2.exe, find nothing, and start
# a second pair on the same ports and database, so they cannot wait a day.
Invoke-Step "keep $Scripts as $ScriptsKeep, and copy $PSScriptRoot\*.ps1 into $Scripts" {
  if (Test-Path $ScriptsKeep) { Remove-Item $ScriptsKeep -Recurse -Force }
  Copy-Item $Scripts $ScriptsKeep -Recurse
  Copy-Item (Join-Path $PSScriptRoot '*.ps1') $Scripts -Force
}

# Step 10: the atrium.
Backup 'hub.err'
Start-Hub
if (-not $WhatIf) {
  $h = Wait-Hub 25
  if (-not $h) { Say 'FATAL: the atrium did not come healthy'; Restore-Old; exit 1 }
  Say "atrium up, build $($h.build)"
}

# Step 11: the room. It writes the machine's address file, naming atrium.exe.
Backup 'room.err'
$null = Start-Room
if (-not $WhatIf) {
  $h = Wait-Hub 60 1
  if (-not $h) {
    Say 'FATAL: the room did not reattach. last lines of room.err:'
    Get-Content (Join-Path $Base 'room.err') -Tail 20 | ForEach-Object { Say "  $_" }
    Restore-Old
    exit 1
  }
  Say "room reattached, build $($h.build) rooms=$($h.rooms)"
  $exe = ''
  try { $exe = (Get-Content $NewLocation -Raw | ConvertFrom-Json).exe } catch {}
  if ($exe -ne ($AtriumBin -replace '\\', '/')) {
    Say "WARN: $NewLocation names '$exe', want $AtriumBin. check before walking away"
  } else { Say "$NewLocation names $exe" }
}
Say 'cutover window done. now the checks in docs/one-atrium-cutover.md, steps 12 to 15.'
