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
if (-not $NoLagLog) { $env:ATRIUM_DEBUG_INPUTLAG = '1' }

# 0. Snapshot the outgoing binary under its build id.
Save-Revert

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
$null = Start-Room
if (-not $WhatIf) {
  $h = Wait-Hub 60 1
  $ok = [bool]$h
  Say "room reattached: $ok build $($h.build) rooms=$($h.rooms)"
  if (-not $ok) {
    Say 'room did not reattach. last lines of room.err:'
    Get-Content (Join-Path $Base 'room.err') -Tail 20 | ForEach-Object { Say "  $_" }
  }
}
Say 'batch deploy done.'
