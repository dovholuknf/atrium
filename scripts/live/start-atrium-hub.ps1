# Start the atrium (the hub), detached, with its exact arguments. The board is http://127.0.0.1:7778.
#
# Use this after a reboot or hibernate, or whenever the hub is down. It does nothing if a hub is already running.
# The room is separate: start-atrium-room.ps1. Start the hub first, and the room dials it.
#
# Input-lag timing is on by default (hops over 20ms are logged to hub.err). Pass -NoLagLog to start without it.
# -WhatIf says what it would start and starts nothing.
param([switch]$NoLagLog, [switch]$WhatIf)

$ErrorActionPreference = 'Continue'
$LiveTag = 'START-HUB'
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'hub-start.log'

$running = Find-Atrium run | Select-Object -First 1
if ($running) { Say "hub already running, pid $($running.ProcessId). nothing to do."; exit 0 }

Backup 'hub.err'
if (-not $NoLagLog) { $env:ATRIUM_DEBUG_INPUTLAG = '1' }
Start-Hub
Say "hub started (lag log $(if ($NoLagLog) { 'off' } else { 'on' }))"

if (-not $WhatIf) {
  $h = Wait-Hub 25
  if ($h) { Say "hub up, build $($h.build), rooms=$($h.rooms)" }
  else { Say 'hub did not come healthy. last lines of hub.err:'; Get-Content (Join-Path $Base 'hub.err') -Tail 20 }
}
