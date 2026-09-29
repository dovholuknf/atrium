# Shared by the live scripts in this directory. Dot-source it:
#
#   . "$PSScriptRoot\live-common.ps1"
#
# These are this machine's deploy scripts, kept in the repo so they are reviewed and versioned. They are deployed by
# copying them over C:\Users\claude\.atrium2\scripts\. See docs/one-atrium-cutover.md.
#
# THE ONE BINARY IS ALSO THE HOOK BINARY. C:\Users\claude\.atrium\bin\atrium.exe is what every hook line in
# settings.json runs, several times a second, as well as the atrium (`run --no-room`) and the room (`room`). Two rules
# follow, and every script here keeps them:
#
#   1. A process is matched by its image directory AND its subcommand, never by image name. `Name='atrium.exe'` alone
#      would stop every hook in flight.
#   2. A new binary goes in by staging it as atrium.next.exe and swapping with two renames. A copy over the live name
#      fails while anything runs it, and would widen the gap a hook can land in from microseconds to the whole copy.

$AtriumBin    = 'C:\Users\claude\.atrium\bin\atrium.exe'
$AtriumBinDir = Split-Path $AtriumBin
$AtriumNew    = 'D:\worktrees\claude\atrium\orchestrator\build.claude\atrium.exe'
# A different build to deploy, for a -WhatIf rehearsal from another worktree.
if ($env:ATRIUM_NEW_BUILD) { $AtriumNew = $env:ATRIUM_NEW_BUILD }
$Base         = 'C:\Users\claude\.atrium2'
$HubDir       = Join-Path $Base 'hub'
$RoomDir      = Join-Path $Base 'room'
$RoomDb       = 'C:\Users\claude\.atrium\atrium.db'
$HubHealth    = 'http://127.0.0.1:7778/_hub/health'
$RoomShutdown = 'http://127.0.0.1:7781/v1/shutdown'
$RoomHealth   = 'http://127.0.0.1:7781/v1/health'

# The exact command lines. The hub's directory flag is --atrium-dir, because `run` also takes the room's --dir.
$HubArgs = @('run', '--no-room', '--addr', '127.0.0.1:7778', '--link', '0.0.0.0:7779',
  '--link-advertise', '192.168.1.68:7779', '--atrium-dir', $HubDir)
$RoomArgs = @('room', '--dir', $RoomDir, '--db', $RoomDb, '--http', '127.0.0.1:7781', '--agent', '127.0.0.1:7777')

# $WhatIf is set by the script that dot-sources this, from its own -WhatIf switch.
if (-not (Test-Path variable:WhatIf)) { $WhatIf = $false }

# Say logs a line and shows it. To the host, not the pipeline, so a function that says something still returns
# only what it returns.
function Say($m) {
  $line = "$(Get-Date -Format o)  $LiveTag $m"
  if ($WhatIf) { Write-Host $line; return }
  $line | Tee-Object -FilePath $LiveLog -Append | Write-Host
}

# Invoke-Step runs one step that changes something, or under -WhatIf says what it would do and changes nothing.
function Invoke-Step([string]$What, [scriptblock]$Do) {
  if ($WhatIf) { Say "WHATIF: $What"; return }
  Say $What
  & $Do
}

# Find-Atrium finds the atrium (`run`) or the room (`room`) by image directory and subcommand.
#
# THE DIRECTORY, NOT THE FILE. A hub-only deploy renames the file the room is running to atrium.old-<ts>.exe, and
# what Windows reports as that process's image path is not something to bet a second room on. Every image in
# .atrium\bin named atrium*.exe counts, and the subcommand picks which. atrium-control.exe runs ` control`, which
# matches neither.
function Find-Atrium([ValidateSet('run', 'room')][string]$Sub) {
  Get-CimInstance Win32_Process |
    Where-Object {
      $_.ExecutablePath -and
      ((Split-Path $_.ExecutablePath) -eq $AtriumBinDir) -and
      ((Split-Path $_.ExecutablePath -Leaf) -like 'atrium*.exe') -and
      ($_.CommandLine -match " $Sub( |$)")
    }
}

# Clear-SessionEnv drops what a supervised session exports, so the processes started here do not inherit the
# session that ran the script. ATRIUM_LOCATION above all: a room keeps an inherited one, and would write its address
# to that file instead of the machine's one place.
function Clear-SessionEnv {
  foreach ($n in 'ATRIUM_LOCATION', 'ATRIUM_SHARED_LOCATION', 'ATRIUM_AGENT_NAME', 'ATRIUM_TASK_ID', 'ATRIUM_ROOM',
    'ATRIUM_RUNNER', 'ATRIUM_HOOK_EXE') {
    Remove-Item "Env:$n" -ErrorAction SilentlyContinue
  }
}

function Test-Up([string]$Url) {
  try { Invoke-RestMethod $Url -TimeoutSec 3 | Out-Null; return $true } catch { return $false }
}

# Wait-Hub waits for the atrium's health, and with -Rooms for at least that many attached rooms.
function Wait-Hub([int]$Seconds = 25, [int]$Rooms = 0) {
  $dl = (Get-Date).AddSeconds($Seconds)
  while ((Get-Date) -lt $dl) {
    try {
      $h = Invoke-RestMethod $HubHealth -TimeoutSec 3
      if ($h.ok -and $h.rooms -ge $Rooms) { return $h }
    } catch {}
    Start-Sleep -Milliseconds 600
  }
  return $null
}

# Get-BinLabel names a binary by what the FILE says it is, from `atrium version`: its commit (7 characters) and its
# board hash (8). Returns '' when the file cannot answer: it will not run, it is an old build with no `version`, or it
# reports no board hash. Never ask the hub. The hub reports the build it RUNS, and the file on disk can be newer when
# the room was deployed after the hub.
function Get-BinLabel([string]$Path) {
  $commit = ''; $board = ''
  try {
    foreach ($line in (& $Path version 2>$null)) {
      if ($line -match '^commit\s+([0-9a-f]{7,})') { $commit = $Matches[1].Substring(0, 7) }
      elseif ($line -match '^board\s+([0-9a-f]{8,})') { $board = $Matches[1].Substring(0, 8) }
    }
  } catch { return '' }
  if (-not $board) { return '' }
  if ($commit) { return "$commit-$board" }
  return $board
}

# Save-Revert keeps one copy of the outgoing binary under its own identity, for going back. A file that cannot say
# what it is gets unknown-<timestamp> and a warning, since a wrong name is worse than a plain one and a snapshot that
# cannot be labelled must not fail the deploy.
function Save-Revert {
  $label = Get-BinLabel $AtriumBin
  if (-not $label) {
    $label = "unknown-$(Get-Date -Format yyyyMMddHHmmss)"
    Say "WARNING: $AtriumBin did not answer 'atrium version'. revert snapshot is named $label"
  }
  $revert = Join-Path $AtriumBinDir "atrium.revert-$label.exe"
  Invoke-Step "copy $AtriumBin -> $revert (revert snapshot, $label)" { Copy-Item $AtriumBin $revert -Force }
  Get-ChildItem (Join-Path $AtriumBinDir 'atrium.revert-*.exe') | Where-Object FullName -ne $revert |
    ForEach-Object { $f = $_.FullName; Invoke-Step "remove old revert $f" { Remove-Item $f -Force } }
}

# Install-Atrium stages the new build and swaps it in with two renames. Returns $true when it is in place.
#
# Retries each rename, because a hook starting at that instant can hold the name for a moment. The file moved aside is
# left for the prune below: a running room or a hook may still be executing it, which a rename permits and a delete
# would not.
function Install-Atrium([string]$From = $AtriumNew) {
  if (-not (Test-Path $From)) { Say "FATAL: no new build at $From"; return $false }
  $next = Join-Path $AtriumBinDir 'atrium.next.exe'
  $aside = Join-Path $AtriumBinDir "atrium.old-$(Get-Date -Format yyyyMMddHHmmss).exe"
  Invoke-Step "copy $From -> $next (staged, nothing has it open)" { Copy-Item $From $next -Force }
  if ($WhatIf) {
    Say "WHATIF: rename $AtriumBin -> $aside"
    Say "WHATIF: rename $next -> $AtriumBin"
    return $true
  }
  $moved = $false
  for ($i = 0; $i -lt 20 -and -not $moved; $i++) {
    try { Rename-Item $AtriumBin $aside -ErrorAction Stop; $moved = $true } catch { Start-Sleep -Milliseconds 400 }
  }
  if (-not $moved) { Say "FATAL: could not rename $AtriumBin aside. nothing changed"; return $false }
  $placed = $false
  for ($i = 0; $i -lt 20 -and -not $placed; $i++) {
    try { Rename-Item $next (Split-Path $AtriumBin -Leaf) -ErrorAction Stop; $placed = $true }
    catch { Start-Sleep -Milliseconds 400 }
  }
  if (-not $placed) {
    # Put the old one back. No file at the hook path is an outage for every hook on the machine.
    Rename-Item $aside (Split-Path $AtriumBin -Leaf) -ErrorAction SilentlyContinue
    Say "FATAL: could not rename $next into place. the old binary is back"
    return $false
  }
  Say "renamed $AtriumBin -> $aside, and $next -> $AtriumBin"
  # Best effort: an aside file something still runs cannot be deleted, and is left for next time.
  Get-ChildItem (Join-Path $AtriumBinDir 'atrium.old-*.exe') | Sort-Object LastWriteTime -Descending |
    Select-Object -Skip 3 | ForEach-Object { Remove-Item $_.FullName -Force -ErrorAction SilentlyContinue }
  return $true
}

# Stop-Room asks the room to wind down so it parks its runners and saves what to reopen, and forces only after 45s.
function Stop-Room {
  $room = Find-Atrium room
  if (-not $room) { Say 'no running room found'; return }
  foreach ($p in $room) {
    Invoke-Step "stop room pid $($p.ProcessId): POST $RoomShutdown, wait up to 45s, then force" {
      try { Invoke-RestMethod $RoomShutdown -Method Post -TimeoutSec 8 | Out-Null }
      catch { Say "room shutdown post: $($_.Exception.Message)" }
      try { Wait-Process -Id $p.ProcessId -Timeout 45 -ErrorAction Stop } catch {}
      if (Get-Process -Id $p.ProcessId -ErrorAction SilentlyContinue) {
        Say 'room still up after 45s, force stop'
        Stop-Process -Id $p.ProcessId -Force
      }
    }
  }
}

# Stop-Hub stops the atrium. It holds nothing, so a hard stop is fine.
function Stop-Hub {
  $hub = Find-Atrium run
  if (-not $hub) { Say 'no running hub found'; return }
  foreach ($p in $hub) {
    Invoke-Step "stop hub pid $($p.ProcessId)" {
      Stop-Process -Id $p.ProcessId -Force
      try { Wait-Process -Id $p.ProcessId -Timeout 10 -ErrorAction Stop } catch {}
    }
  }
}

# Backup keeps the previous run's log, since a redirect overwrites it.
function Backup([string]$Name) {
  $p = Join-Path $Base $Name
  if (Test-Path $p) {
    Invoke-Step "keep $p as $Name.<timestamp>" {
      Copy-Item $p (Join-Path $Base "$Name.$(Get-Date -Format yyyyMMdd-HHmmss)") -Force
    }
  }
}

# Start-Hub starts the atrium detached, through cmd so the redirect appends and the caller's output can end.
function Start-Hub {
  $line = "`"$AtriumBin`" $($HubArgs -join ' ') >> $Base\hub.out 2>> $Base\hub.err"
  Invoke-Step "start hub: $AtriumBin $($HubArgs -join ' ')" {
    Clear-SessionEnv
    Add-Content (Join-Path $Base 'hub.err') ("===== hub start {0} =====" -f (Get-Date -Format o))
    Start-Process -FilePath 'cmd.exe' -WindowStyle Hidden -ArgumentList '/c', $line
  }
}

# Start-Room starts the room detached, and refuses when one already answers: two rooms on one database is the one
# thing the store is not built for.
function Start-Room {
  # Under -WhatIf nothing was stopped, so the live room answering says nothing about the real run.
  if (-not $WhatIf -and (Test-Up $RoomHealth)) {
    Say "a room already answers at $RoomHealth. not starting another"
    return $false
  }
  Invoke-Step "start room: $AtriumBin $($RoomArgs -join ' ')" {
    Clear-SessionEnv
    Start-Process -FilePath $AtriumBin -ArgumentList $RoomArgs -WindowStyle Hidden `
      -RedirectStandardOutput (Join-Path $Base 'room.out') -RedirectStandardError (Join-Path $Base 'room.err')
  }
  return $true
}
