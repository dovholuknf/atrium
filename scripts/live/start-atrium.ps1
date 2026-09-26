# Bring atrium up after a reboot, or from cold: the atrium (hub) first, then the claude-sg4 room, both from the one
# binary, C:\Users\claude\.atrium\bin\atrium.exe. Either one already running is left alone.
#
# RUN FROM A SHELL ATRIUM DOES NOT SUPERVISE. The room owns its terminals, so a later stop of the room takes a script
# running inside one of them down with it.
#
#     pwsh -File C:\Users\claude\.atrium2\scripts\start-atrium.ps1
#
# -WhatIf says what it would do and does nothing. -Switch is accepted and ignored: it replaced the rollback pair,
# which no longer exists.
param([switch]$Switch, [switch]$NoLagLog, [switch]$WhatIf)

$ErrorActionPreference = 'Continue'
$pass = @{ WhatIf = $WhatIf; NoLagLog = $NoLagLog }
& "$PSScriptRoot\start-atrium-hub.ps1" @pass
& "$PSScriptRoot\start-atrium-room.ps1" @pass
