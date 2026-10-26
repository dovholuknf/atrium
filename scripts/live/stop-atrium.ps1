# Stop atrium on this machine before a reboot: the claude-sg4 room, then the hub. sg4-control is OPT-IN: it is stopped,
# first, only with both -Control and -IKnowItKillsMyShell, and one without the other is refused.
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
# It ends everything atrium on this machine in one run: the room, its pty host and every runner the host holds, and the
# hub, then lists what is still running. -KillRunners is accepted and implied. `atrium stop` is the gentler stop, and
# leaves a supervised runner running.
#
# -WhatIf says what it would stop and stops nothing. -KeepControl is accepted and ignored. The claude-sg4 room first
# asks its working cards to wrap up (up to its restart_wrap_wait_s). -Immediate skips that.
param([switch]$KeepControl, [switch]$Force, [switch]$WhatIf, [switch]$Immediate, [switch]$KillRunners, [switch]$Control,
  [switch]$IKnowItKillsMyShell)

$ErrorActionPreference = 'Continue'
$LiveTag = 'STOP'
. "$PSScriptRoot\refuse-supervised.ps1"
Assert-NotSupervised
Assert-ControlOptIn -Control:$Control -IKnowItKillsMyShell:$IKnowItKillsMyShell
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'stop.log'
$KillRunners = $true

if ($env:ATRIUM_TASK_ID -and -not $Force -and -not $WhatIf) {
  Say "refusing: this shell is atrium card $env:ATRIUM_TASK_ID, and stopping its room stops this script. run it from a plain PowerShell, or pass -Force"
  exit 1
}

$withControl = $Control -and $IKnowItKillsMyShell
$finished = $false
# The finally runs on exit and Ctrl+C too, so every run ends with what is left.
try {
  if ($withControl) { Stop-Control }
  Say 'this ends the room, its pty host with every runner it holds (typed text not sent is lost), and the hub'
  if (-not (Stop-Room)) { Say 'the room refused to stop. nothing was stopped'; exit 1 }
  Stop-Hub
  $finished = $true
} finally {
  if (-not $WhatIf) {
    $why = if ($finished) { 'it did not exit after its stop' } else { 'this script ended before it stopped it' }
    for ($i = 0; $i -lt 10; $i++) {
      $left = @(Find-Atrium run | ForEach-Object { "hub pid $($_.ProcessId)" }) +
        @(Find-Atrium room | ForEach-Object { "room pid $($_.ProcessId)" }) +
        @(Find-Atrium ptyhost | ForEach-Object { "pty host pid $($_.ProcessId)" })
      if ($withControl) { $left += @(Find-Control | ForEach-Object { "sg4-control pid $($_.ProcessId)" }) }
      if (-not $left.Count -or -not $finished) { break }
      Start-Sleep -Seconds 1
    }
    if ($left.Count) { Say "still running: $($left -join ', '). $why. see $LiveLog" }
    else { Say 'atrium stopped. start it again with start-atrium.ps1' }
  }
}
