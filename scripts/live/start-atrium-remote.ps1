# Start every remote room the hub does not list (sg3, sgg, sgg-zrok, m1mini), detached, over ssh. A room that starts
# brings back every card that was live on it. start-atrium.ps1 runs this once the hub is up, since the "<room>.atrium"
# ssh route rides the hub.
#
# -Rooms names the rooms to start, default all of them. -WhatIf says what it would start and starts nothing. Exits 1
# when a room it started is not on the hub within 90s, or ssh could not reach one.
param([string[]]$Rooms, [switch]$WhatIf)

$ErrorActionPreference = 'Continue'
$LiveTag = 'START-REMOTE'
. "$PSScriptRoot\refuse-supervised.ps1"
Assert-NotSupervised
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'remote-start.log'

$ssh = (Get-Command ssh).Source
$sshOpts = @('-o', 'ConnectTimeout=25', '-o', 'BatchMode=yes')
function Get-OnHub { try { @((Invoke-RestMethod $HubRooms -TimeoutSec 3).rooms | ForEach-Object { $_.name }) } catch { @() } }

$names = if ($Rooms) { $Rooms } else { @($RemoteStart.Keys) }
$onHub = Get-OnHub
$started = @()
$failed = $false
foreach ($name in $names) {
  $r = $RemoteStart[$name]
  if (-not $r) { Say "${name}: not a remote room this script knows"; $failed = $true; continue }
  if ($onHub -contains $name) { Say "$name already on the hub. nothing to do."; continue }
  $user = Get-RoomSshUser $r.Ssh
  $route = Resolve-SshRoute $r.Ssh { param($cand) Invoke-Capped $ssh ($sshOpts + @($cand, 'echo ok')) 40 } $user
  if (-not $route.Host) { Say "${name}: $(Get-SshFailure $route $user)"; $failed = $true; continue }
  Invoke-Step "start $name over ssh $($route.Host)" {
    $x = Invoke-Capped $ssh ($sshOpts + @($route.Host, $r.Cmd)) 60
    if ($x.TimedOut -or $x.Code -ne 0) {
      Say "${name}: the start exited $($x.Code)$(if ($x.TimedOut) { ', cut at 60s' }): $($x.Out.Trim())"
    }
  }
  $started += $name
}

if (-not $WhatIf -and $started) {
  $deadline = (Get-Date).AddSeconds(90)
  do {
    $onHub = Get-OnHub
    $missing = @($started | Where-Object { $onHub -notcontains $_ })
    if ($missing) { Start-Sleep 2 }
  } while ($missing -and (Get-Date) -lt $deadline)
  foreach ($name in $started) {
    if ($missing -contains $name) { Say "$name is not on the hub after 90s"; $failed = $true }
    else { Say "$name is back on the hub" }
  }
}
if ($failed) { exit 1 }
