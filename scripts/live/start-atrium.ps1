# Bring atrium up after a reboot, or from cold: the atrium (hub) first, then the claude-sg4 room, then the sg4-control
# room, then every remote room the hub does not list (start-atrium-remote.ps1). Hub and claude-sg4 come from
# C:\Users\claude\.atrium\bin\atrium.exe, sg4-control from its own copy in .atrium\ctl-bin. Any one already running is
# left alone.
#
# RUN FROM A SHELL ATRIUM DOES NOT SUPERVISE. The room owns its terminals, so a later stop of the room takes a script
# running inside one of them down with it.
#
#     pwsh -File C:\Users\claude\.atrium2\scripts\start-atrium.ps1
#
# -WhatIf says what it would do and does nothing. sg4-control starts by default, outside any job, and the script exits 1
# when it does not answer on 7791. -NoControl skips it. -NoRemote skips the remote rooms, which also exit 1 when one
# does not come back on the hub. -Control and -IKnowItKillsMyShell are accepted and ignored: they
# guard stopping it, not starting it. -Switch is accepted and ignored: it replaced the rollback pair, which no longer
# exists.
param([switch]$Switch, [switch]$NoLagLog, [switch]$NoControl, [switch]$WhatIf, [switch]$Control,
  [switch]$IKnowItKillsMyShell, [switch]$NoRemote)

$ErrorActionPreference = 'Continue'
. "$PSScriptRoot\refuse-supervised.ps1"
Assert-NotSupervised
$pass = @{ WhatIf = $WhatIf; NoLagLog = $NoLagLog }
& "$PSScriptRoot\start-atrium-hub.ps1" @pass
& "$PSScriptRoot\start-atrium-room.ps1" @pass
$code = 0
if (-not $NoControl) {
  & "$PSScriptRoot\start-atrium-control.ps1" -WhatIf:$WhatIf
  $code = $LASTEXITCODE
}
if (-not $NoRemote) {
  & "$PSScriptRoot\start-atrium-remote.ps1" -WhatIf:$WhatIf
  if (-not $code) { $code = $LASTEXITCODE }
}
exit $code
