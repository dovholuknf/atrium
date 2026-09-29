# HUB-ONLY deploy: snapshot the outgoing binary, stop ONLY the hub, swap the binary, start the hub with its exact
# wide args. THE ROOM IS NEVER TOUCHED: it keeps running the image it loaded, which the rename leaves in place.
#
# -WhatIf prints every process it would stop and every file it would copy or rename, and changes nothing. The gate
# is skipped under -WhatIf, because asking the board is itself a toast on clint's screen.
param([switch]$WhatIf)

$ErrorActionPreference = 'Continue'
$LiveTag = 'HUBONLY'
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'deploy.log'

if (-not (Test-Path $AtriumNew)) { Say "FATAL: no new build at $AtriumNew"; exit 1 }

# Gate: wait for an idle board, count down in a toast clint can click to pause (docs/hub-restart-gate.md).
# Exit 0 means go (or a hub too old to ask), anything else means leave the hub alone.
if (-not $WhatIf) {
  & 'D:\git\github\dovholuknf\atrium\scripts\hub-restart-gate.ps1' | ForEach-Object { Say "gate: $_" }
  if ($LASTEXITCODE -ne 0) { Say 'restart held by the board, nothing changed'; exit 0 }
} else { Say 'WHATIF: run scripts\hub-restart-gate.ps1 and stop here unless it says go' }

# 0. Snapshot the outgoing binary under its build id.
Save-Revert

# 1. Stop ONLY the hub.
Stop-Hub

# 2. Swap the binary: stage, then two renames. The room and every hook keep the image they loaded.
if (-not (Install-Atrium)) { exit 1 }

# 3. Start the hub with its exact wide args (the room dials this link).
Start-Hub

# 4. Verify: hub up on the NEW build, room still attached.
if (-not $WhatIf) {
  $h = Wait-Hub 25
  if ($h) { Say "hub up build $($h.build) only=$($h.only) rooms=$($h.rooms)" }
  else { Say 'FATAL: hub did not come healthy' }
}
