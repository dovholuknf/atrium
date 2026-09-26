# Starts this machine's atrium: the hub, then the room, both from the one binary. The work is in
# scripts\live\start-atrium.ps1, deployed to C:\Users\claude\.atrium2\scripts\. This runs the deployed copy, so a
# checkout of some other branch cannot change what starts.
#
# Run it from a shell atrium does not supervise. -WhatIf says what it would do. -Switch is accepted and ignored.
param([switch]$Switch, [switch]$NoLagLog, [switch]$WhatIf)

& 'C:\Users\claude\.atrium2\scripts\start-atrium.ps1' @PSBoundParameters
exit $LASTEXITCODE
