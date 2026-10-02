# Start the sg4-control room, detached: the second room on sg4, for atrium's own agents (the orchestrator).
#
# Isolated from claude-sg4 three ways:
#  - its own binary, C:\Users\claude\.atrium\ctl-bin\atrium.exe, so Find-Atrium (which matches .atrium\bin) never
#    stops or counts it
#  - --isolated, so its address goes in a private file and the machine's hooks keep pointing at claude-sg4
#  - its own dir, db and ports (board 7791, agents 7787)
#
# First run on a machine: pass -Join '<join string from atrium rooms add>'. After that, no arguments. It does nothing
# if the room is already running. -WhatIf says what it would start and starts nothing.
param([string]$Join, [switch]$WhatIf)

$ErrorActionPreference = 'Continue'
$LiveTag = 'START-CONTROL'
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'control-start.log'

$CtlExe = Join-Path $CtlBinDir 'atrium.exe'

$running = Find-Control | Select-Object -First 1
if ($running) { Say "sg4-control already running, pid $($running.ProcessId). nothing to do."; exit 0 }

if (-not (Test-Path $CtlExe)) {
  Invoke-Step "copy $AtriumBin -> $CtlExe (sg4-control has its own binary)" {
    New-Item -ItemType Directory -Force $CtlBinDir | Out-Null
    Copy-Item $AtriumBin $CtlExe
  }
}

$common = @('--dir', $CtlDir, '--db', (Join-Path $CtlDir 'atrium.db'), '--http', '127.0.0.1:7791',
  '--agent', '127.0.0.1:7787', '--isolated')
# `room join` takes the join string only as an argument (no env var, file or stdin), so it sits in this process's own
# command line for the run. That is fine: the string is spent after one use and no credential survives the run.
$argv = if ($Join) { @('room', 'join', $Join) + $common } else { @('room') + $common }
$shown = if ($Join) { ($argv -replace [regex]::Escape($Join), '<join>') -join ' ' } else { $argv -join ' ' }

Invoke-Step "start sg4-control: $CtlExe $shown" {
  Clear-SessionEnv
  # A supervised session exports these too, and the room must not inherit them.
  foreach ($n in 'ATRIUM_HUB_URL', 'ATRIUM_PERM_GATE', 'ATRIUM_DEBUG_INPUTLAG', 'ATRIUM_NEW_BUILD') {
    Remove-Item "Env:$n" -ErrorAction SilentlyContinue
  }
  Get-ChildItem env: | Where-Object { $_.Name -like 'CLAUDE_CODE_*' -or $_.Name -like 'CLAUDECODE*' } |
    ForEach-Object { Remove-Item "Env:$($_.Name)" }
  New-Item -ItemType Directory -Force $CtlDir | Out-Null
  Start-Process -FilePath $CtlExe -ArgumentList $argv -WindowStyle Hidden `
    -RedirectStandardOutput (Join-Path $CtlDir 'room.out') -RedirectStandardError (Join-Path $CtlDir 'room.err')
}

if (-not $WhatIf) {
  $up = $false
  for ($i = 0; $i -lt 30 -and -not $up; $i++) { $up = Test-Up $CtlHealth; if (-not $up) { Start-Sleep 1 } }
  if ($up) { Say "sg4-control up at http://127.0.0.1:7791" }
  else { Say 'sg4-control did not answer in 30s. last lines of room.err:'; Get-Content (Join-Path $CtlDir 'room.err') -Tail 20 }
}
