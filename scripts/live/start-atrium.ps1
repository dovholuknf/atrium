# Bring atrium up after a reboot, or from cold: the atrium (hub) first, then the claude-sg4 room, and the sg4-control
# room only on request. Hub and claude-sg4 come from C:\Users\claude\.atrium\bin\atrium.exe, sg4-control from its own copy in
# .atrium\ctl-bin. Any one already running is left alone.
#
# RUN FROM A SHELL ATRIUM DOES NOT SUPERVISE. The room owns its terminals, so a later stop of the room takes a script
# running inside one of them down with it.
#
#     pwsh -File C:\Users\claude\.atrium2\scripts\start-atrium.ps1
#
# -WhatIf says what it would do and does nothing. sg4-control is OPT-IN: it starts only with both -Control and
# -IKnowItKillsMyShell, and one without the other is refused. -NoControl is accepted and ignored. -Switch is accepted and ignored: it
# replaced the rollback pair, which no longer exists.
param([switch]$Switch, [switch]$NoLagLog, [switch]$NoControl, [switch]$WhatIf, [switch]$Control,
  [switch]$IKnowItKillsMyShell)

$ErrorActionPreference = 'Continue'
. "$PSScriptRoot\refuse-supervised.ps1"
Assert-NotSupervised
Assert-ControlOptIn -Control:$Control -IKnowItKillsMyShell:$IKnowItKillsMyShell
$pass = @{ WhatIf = $WhatIf; NoLagLog = $NoLagLog }
& "$PSScriptRoot\start-atrium-hub.ps1" @pass
& "$PSScriptRoot\start-atrium-room.ps1" @pass
if ($Control -and -not $NoControl) { & "$PSScriptRoot\start-atrium-control.ps1" -WhatIf:$WhatIf }
