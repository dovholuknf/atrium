# Deploy a new atrium build to BOTH the hub and the room, in one detached pass.
#
# Run it DETACHED, because stopping the room ends every supervised terminal, the orchestrator's included:
#   Start-Process pwsh -ArgumentList '-NoProfile','-File','C:\Users\claude\.atrium2\scripts\deploy-batch.ps1' `
#     -WindowStyle Hidden
# On restart the room's reopenSaved resumes every card. Everything is logged to deploy-batch.log.
#
# -WhatIf prints every process it would stop and every file it would copy or rename, and changes nothing.
#
# The room is restarted by this script, not by restart_atrium, so it does not depend on the restarter inside the
# room that is being replaced. Input-lag logging is on for both unless -NoLagLog is passed.
param([switch]$NoLagLog, [switch]$WhatIf)

$ErrorActionPreference = 'Continue'
$LiveTag = 'BATCH'
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'deploy-batch.log'

if (-not (Test-Path $AtriumNew)) { Say "FATAL: no new build at $AtriumNew"; exit 1 }
if (-not (Test-NewBuildStamped)) { exit 1 }
if (-not $NoLagLog) { $env:ATRIUM_DEBUG_INPUTLAG = '1' }

# A new context under way is waited out first: stopping the room cuts a clear off between its steps.
if (-not $WhatIf) {
  if (-not (Wait-NewContextsDone)) { Say 'a new context is still under way, nothing changed'; exit 0 }
} else { Say 'WHATIF: wait for every new context under way to finish' }

# 0. Snapshot the outgoing binary under its build id.
$revert = Save-Revert

# 1. Stop the room gracefully first, so it parks its runners and saves what to reopen.
Stop-Room

# 2. Stop the hub.
Stop-Hub

# 3. Swap the binary: stage, then two renames. Hooks in flight keep the image they loaded.
if (-not (Install-Atrium)) { exit 1 }

# 4. Start the hub with its exact args, then wait for health.
Backup 'hub.err'
Start-Hub
if (-not $WhatIf) {
  $h = Wait-Hub 25
  if ($h) { Say "hub up build $($h.build)" } else { Say 'FATAL: hub did not come healthy'; exit 1 }
}

# 5. Start the room with its exact args, then wait for it to reattach.
Backup 'room.err'
$roomStart = Get-Date
$null = Start-Room
$attached = $false
if (-not $WhatIf) {
  # THIS room, by name, seen by the hub and by the room, and still attached 30s later. Not a room count.
  $r = Wait-RoomAttached $roomStart
  $attached = [bool]$r
  $h = Wait-Hub 5
  Say "room $RoomName attached and held: $attached since $($r.since) build $($h.build) rooms=$($h.rooms)"
  if (-not $attached) {
    Say "room $RoomName did not attach and stay attached. room link: '$(Get-RoomLink)'. last lines of room.err:"
    Get-Content (Join-Path $Base 'room.err') -Tail 20 | ForEach-Object { Say "  $_" }
  }
}

# 6. The room is healthy only when it answers from its store, now and again after the startup sweep and the reopen.
#    Reattached is not enough: the link attaches before the sweep, and a frozen store still answers /v1/health.
#    And serving is not enough either: /v1/settings is the room's own answer, given whether or not the hub has it.
if (-not $WhatIf) {
  $serves = Test-RoomServes
  Say "room serves ${RoomServes}: $serves"
  if ($serves) {
    Start-Sleep -Seconds 30
    $serves = Test-RoomServes
    Say "room serves $RoomServes 30s later: $serves"
  }
  if (-not $serves) {
    Say "ROOM IS NOT SERVING. reverting to $revert. last lines of room.err:"
    Get-Content (Join-Path $Base 'room.err') -Tail 20 | ForEach-Object { Say "  $_" }
    # A frozen room does not answer the shutdown post either. Stop-Room forces after its 45s.
    Stop-Room
    Stop-Hub
    if (-not (Install-Atrium $revert)) { Say 'FATAL: revert install failed. nothing is running'; exit 1 }
    Start-Hub
    $null = Wait-Hub 25
    $revertStart = Get-Date
    $null = Start-Room
    $back = [bool](Wait-RoomAttached $revertStart)
    Say "reverted. room serves ${RoomServes}: $(Test-RoomServes), $RoomName attached and held: $back"
    exit 1
  }
  # SERVING BUT NOT ATTACHED IS A FAILURE, NOT A REVERT. The room works for its own agents and its own board at
  # $RoomServes, and a revert would restart every card a second time for what may be the hub refusing it. Said loudly,
  # with its own exit code, so nobody reads the board going quiet as a finished deploy.
  if (-not $attached) {
    Say "BATCH DEPLOY FAILED: $RoomName serves locally but is not on the hub. nothing reverted"
    exit 2
  }
}
Say 'batch deploy done.'
