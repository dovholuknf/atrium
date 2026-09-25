# Combined maintenance window: deploy the new binary to BOTH hub and room in one detached pass.
#
# This is deploy-batch.ps1, which already does exactly that with a graceful room stop first. Kept as a name so a
# note that says "run the maintenance window" still has something to run.
#
# MUST run detached (Start-Process): stopping the room ends the orchestrator's own supervised terminal, so an inline
# run dies before the room comes back. -WhatIf and -NoLagLog pass through.
param([switch]$NoLagLog, [switch]$WhatIf)

& "$PSScriptRoot\deploy-batch.ps1" -NoLagLog:$NoLagLog -WhatIf:$WhatIf
exit $LASTEXITCODE
