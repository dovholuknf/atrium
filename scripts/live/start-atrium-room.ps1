# Start the claude-sg4 room, detached, with its exact arguments.
#
# Use this when the room is down and the hub is up, for example after a restart_atrium whose restarter never
# brought the room back. It does nothing if a room is already running.
#
# Input-lag timing is on by default (hops over 20ms are logged to room.err). Pass -NoLagLog to start without it.
# -WhatIf says what it would start and starts nothing.
param([switch]$NoLagLog, [switch]$WhatIf)

$ErrorActionPreference = 'Continue'
$LiveTag = 'START-ROOM'
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'room-restart.log'

$running = Find-Atrium room | Select-Object -First 1
if ($running) { Say "room already running, pid $($running.ProcessId). nothing to do."; exit 0 }

Backup 'room.err'
if (-not $NoLagLog) { $env:ATRIUM_DEBUG_INPUTLAG = '1' }
if (-not (Start-Room)) { exit 0 }
Say "room started (lag log $(if ($NoLagLog) { 'off' } else { 'on' }))"

if (-not $WhatIf) {
  $h = Wait-Hub 60 1
  Say "room reattached: $([bool]$h) (hub build $($h.build), rooms=$($h.rooms))"
  if (-not $h) {
    Say 'room did not reattach. last lines of room.err:'
    Get-Content (Join-Path $Base 'room.err') -Tail 20
  }
}
