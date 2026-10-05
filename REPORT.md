# REPORT f-new-review-8df971e4

Done: `Get-LiveHosts` (User, else Machine) used by Start-Hub and Start-Room in `scripts/live/live-common.ps1`, with
`Get-LiveHostsSaid` and `Set-LiveHosts`. room-defender header line rewrapped. Changelog written, item and queue 0b/0c
marked BUILT. Test: `scripts/live/test-live-hosts.ps1`, 7 checks pass.

Left: nothing. Nothing was started, stopped or deployed.

Verify: `pwsh -NoProfile -File scripts/live/test-live-hosts.ps1`. Start-Room's new lines are not run live.
