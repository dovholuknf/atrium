# Stop atrium on this machine before a reboot: the sg4-control room, then the claude-sg4 room, then the hub.
#
# Each room is ASKED to wind down (POST /v1/shutdown), so it parks its runners and saves what to reopen, and is forced
# only after 45 seconds. A reboot or a taskkill skips that and ends every runner mid-turn. The hub holds nothing and
# goes last, so the rooms can still report to it while they wind down.
#
# RUN FROM A SHELL ATRIUM DOES NOT SUPERVISE. A script running inside one of the rooms' terminals is stopped with
# that room, halfway through. Refuses when it sees it is inside one, unless -Force.
#
#     pwsh -File C:\Users\claude\.atrium2\scripts\stop-atrium.ps1
#
# -WhatIf says what it would stop and stops nothing. -KeepControl leaves sg4-control running. The claude-sg4 room first
# asks its working cards to wrap up (up to its restart_wrap_wait_s). -Immediate skips that.
param([switch]$KeepControl, [switch]$Force, [switch]$WhatIf, [switch]$Immediate)

$ErrorActionPreference = 'Continue'
$LiveTag = 'STOP'
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'stop.log'

if ($env:ATRIUM_TASK_ID -and -not $Force -and -not $WhatIf) {
  Say "refusing: this shell is atrium card $env:ATRIUM_TASK_ID, and stopping its room stops this script. run it from a plain PowerShell, or pass -Force"
  exit 1
}

if (-not $KeepControl) { Stop-Control }
Stop-Room
Stop-Hub

if (-not $WhatIf) {
  $left = @(Find-Atrium run) + @(Find-Atrium room)
  if (-not $KeepControl) { $left += @(Find-Control) }
  if ($left.Count) { Say "still running: $(($left | ForEach-Object { $_.ProcessId }) -join ', ')" }
  else { Say 'atrium stopped. start it again with start-atrium.ps1' }
}
