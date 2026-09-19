# RETIRED. This machine runs atrium2 (hub + room), not the v1 daemon.
#
# Starting the v1 daemon here opens C:\Users\claude\.atrium\atrium.db, which the
# room also opens. Two daemons on one sqlite file corrupts it. That is why this
# script no longer starts anything.
#
# Use the hub/room start script instead:
#
#     pwsh -File C:\Users\claude\.atrium2\start-atrium2.ps1

Write-Host "This machine runs atrium2 (hub + room), not the v1 daemon."
Write-Host "Starting v1 here would open the same database the room uses, and corrupt it."
Write-Host ""
Write-Host "Use:"
Write-Host "    pwsh -File C:\Users\claude\.atrium2\start-atrium2.ps1"
exit 1
