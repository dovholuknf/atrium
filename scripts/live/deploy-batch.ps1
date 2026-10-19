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
#
# The room is stopped gracefully: each working card is asked to wrap up and is woken after. -Immediate skips that.
#
# A deploy hold is set on the room first and waited out, so the room that comes back wakes every card that was
# working. -NoHold skips it, and so does a room that keeps its runners (Test-RunnersSurvive). Do not send the "commit and wait" say as well: a card told to wait by a say is idle at
# the hold and is correctly not woken.
# -KillRunners lets a room whose pty host would die with it be stopped anyway, ending every runner and any typed text.
# Without it that refusal stops the deploy. -RoomOnly leaves the hub running: only the room stops and starts, the hub keeps its process and the image it loaded.
param([switch]$NoLagLog, [switch]$WhatIf, [switch]$NoHold, [switch]$Immediate, [switch]$RoomOnly, [switch]$KillRunners)

$ErrorActionPreference = 'Continue'
$LiveTag = 'BATCH'
. "$PSScriptRoot\refuse-supervised.ps1"
# It stops claude-sg4's room, and the hub holds no shell, so only a shell under claude-sg4 is refused.
Assert-NotSupervised -Targets 'claude-sg4'
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'deploy-batch.log'

if (-not (Test-Path $AtriumNew)) { Say "FATAL: no new build at $AtriumNew"; exit 1 }
if (-not (Test-NewBuildStamped)) { exit 1 }
if (-not $NoLagLog) { $env:ATRIUM_DEBUG_INPUTLAG = '1' }

# A stop the room is going to refuse is found out here, before any hold is set: a hold set and then called off has
# already refused every card's calls once. Only -KillRunners gets past it.
if (Test-StopWouldRefuse) {
  try {
    $pf = Invoke-RestMethod $RoomPreflightUrl -TimeoutSec 5
    Say "REFUSED: the pty host would die with the room, so this stop would end every runner. runners held by the pty host: $($pf.runners.host), in the room's own process: $($pf.runners.in_process)."
  } catch { Say 'REFUSED: the pty host would die with the room, so this stop would end every runner.' }
  Say 'FATAL: nothing changed and no deploy hold was set. pass -KillRunners to end the runners'
  exit 1
}

# A new context under way is waited out first: stopping the room cuts a clear off between its steps.
if (-not $WhatIf) {
  if (-not (Wait-NewContextsDone)) { Say 'a new context is still under way, nothing changed'; exit 0 }
} else { Say 'WHATIF: wait for every new context under way to finish' }

# Hold the room, so a card working now is told to continue after the restart and one waiting for a human is left
# alone. The hold's own wake replaces the "commit and wait" say. -NoHold skips it. The restart then wakes only a card
# that is mid-turn at the wind-down, and a card that ended its turn on a say is not woken.
# A room whose runners survive the stop (pty_host on, host out of the task's job, nothing in the room's own process)
# has no turn to protect: the restart ends no agent, so the hold is skipped and nothing is woken.
if (-not $NoHold -and -not $WhatIf -and (Test-RunnersSurvive)) {
  Say 'no deploy hold: this room keeps its runners across the restart (pty_host on, host outside the task job)'
} elseif ($NoHold) {
  Say 'WARNING: -NoHold, no deploy hold set. nothing will be woken but a card mid-turn at the wind-down'
} elseif ($WhatIf) {
  Say 'WHATIF: set a deploy hold on the room and wait for it to go quiet'
} elseif (Set-DeployHold) {
  if (-not (Wait-HoldQuiet)) { Say 'the room is not quiet, restarting anyway. a card still working is told to continue' }
} else {
  Say 'WARNING: no deploy hold, going on without it. nothing will be woken but a card mid-turn at the wind-down'
}

# What this deploy puts live, for HANDOFF. Read before anything changes.
Write-DeployQueue

# 0. Snapshot the outgoing binary under its build id.
$revert = Save-Revert

# 1. Stop the room gracefully first, so it parks its runners and saves what to reopen. A room that refuses because its
#    pty host would die with it stops the deploy here, before anything has changed. Only -KillRunners ends them.
if (-not (Stop-Room)) {
  # Nothing was stopped, so the hold set above must not keep every card waiting. The preflight above makes this rare.
  try { Invoke-RestMethod -Method Post -Uri $RoomHoldUrl -ContentType 'application/json' -Body '{"action":"lift","by":"deploy-batch","outcome":"cancelled"}' -TimeoutSec 15 | Out-Null } catch { Say "could not lift the deploy hold: $($_.Exception.Message)" }
  Say 'FATAL: the room refused to stop without ending its runners. nothing changed, the deploy hold is lifted. pass -KillRunners to end them'; exit 1
}

# 2. Stop the hub, unless this is a room-only deploy.
if ($RoomOnly) { Say 'room only: the hub is left running' } else { Stop-Hub }

# 3. Swap the binary: stage, then two renames. Hooks in flight keep the image they loaded.
if (-not (Install-Atrium)) { exit 1 }

# 4. Start the hub with its exact args, then wait for health.
if ($RoomOnly) {
  if (-not $WhatIf -and -not (Wait-Hub 5)) { Say 'FATAL: room only, but the hub is not answering'; exit 1 }
} else {
  Backup 'hub.err'
  Start-Hub
  if (-not $WhatIf) {
    $h = Wait-Hub 25
    if ($h) { Say "hub up build $($h.build)" } else { Say 'FATAL: hub did not come healthy'; exit 1 }
  }
}

# 5. Start the room with its exact args, then wait for it to reattach.
Backup 'room.err'
$roomStart = Get-Date
$null = Start-Room
$attached = $false
if (-not $WhatIf) {
  # THIS room, by name, seen by the hub and by the room, and still attached StaySeconds later. Not a room count.
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
    Start-Sleep -Seconds $StaySeconds
    $serves = Test-RoomServes
    Say "room serves $RoomServes ${StaySeconds}s later: $serves"
  }
  if (-not $serves) {
    Say "ROOM IS NOT SERVING. reverting to $revert. last lines of room.err:"
    Get-Content (Join-Path $Base 'room.err') -Tail 20 | ForEach-Object { Say "  $_" }
    # A frozen room does not answer the shutdown post either. Stop-Room forces after its 45s.
    if (-not (Stop-Room)) { Say 'FATAL: the room refused to stop (its pty host would die with it), so the revert is not installed over a room that is still running. nothing was changed. pass -KillRunners to end its runners'; exit 1 }
    if (-not $RoomOnly) { Stop-Hub }
    if (-not (Install-Atrium $revert)) { Say 'FATAL: revert install failed. nothing is running'; exit 1 }
    if (-not $RoomOnly) { Start-Hub; $null = Wait-Hub 25 }
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
