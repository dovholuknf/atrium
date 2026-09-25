# Bring atrium up after a reboot, or from cold: the atrium (hub) first, then the claude-sg4 room, both from the one
# binary, C:\Users\claude\.atrium\bin\atrium.exe. Either one already running is left alone.
#
# RUN FROM A SHELL ATRIUM DOES NOT SUPERVISE. The room owns its terminals, so a later stop of the room takes a script
# running inside one of them down with it.
#
#     pwsh -File C:\Users\claude\.atrium2\scripts\start-atrium.ps1
#
# The pre-cutover pair (`.atrium2\bin\atrium2.exe hub` and `room`) is only the rollback now. When it is running this
# refuses, since its room holds the same database. -Switch stops that pair (the room asked to wind down first, so it
# saves what to reopen) and then starts the one binary. -WhatIf says what it would do and does nothing.
param([switch]$Switch, [switch]$NoLagLog, [switch]$WhatIf)

$ErrorActionPreference = 'Continue'
$LiveTag = 'START'
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'hub-start.log'

$old = @(Get-CimInstance Win32_Process -Filter "Name='atrium2.exe'" |
    Where-Object { $_.CommandLine -match ' (hub|room)( |$)' })
if ($old) {
  foreach ($p in $old) { Say "rollback binary running: pid $($p.ProcessId) $($p.CommandLine)" }
  if (-not $Switch) {
    Say 'refusing to start beside it. run again with -Switch to stop it and start the one binary.'
    exit 1
  }
  foreach ($p in $old | Where-Object { $_.CommandLine -match ' room( |$)' }) {
    Invoke-Step "stop rollback room pid $($p.ProcessId): POST $RoomShutdown, wait up to 45s, then force" {
      try { Invoke-RestMethod $RoomShutdown -Method Post -TimeoutSec 8 | Out-Null }
      catch { Say "room shutdown post: $($_.Exception.Message)" }
      try { Wait-Process -Id $p.ProcessId -Timeout 45 -ErrorAction Stop } catch {}
      if (Get-Process -Id $p.ProcessId -ErrorAction SilentlyContinue) {
        Say 'room still up after 45s, force stop'
        Stop-Process -Id $p.ProcessId -Force
      }
    }
  }
  foreach ($p in $old | Where-Object { $_.CommandLine -match ' hub( |$)' }) {
    Invoke-Step "stop rollback hub pid $($p.ProcessId)" {
      Stop-Process -Id $p.ProcessId -Force
      try { Wait-Process -Id $p.ProcessId -Timeout 10 -ErrorAction Stop } catch {}
    }
  }
}

$pass = @{ WhatIf = $WhatIf; NoLagLog = $NoLagLog }
& "$PSScriptRoot\start-atrium-hub.ps1" @pass
& "$PSScriptRoot\start-atrium-room.ps1" @pass
