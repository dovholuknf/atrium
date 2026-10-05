# The one click behind "deploy ready": build the tip the board showed, then run today's hub-only deploy.
#
# The hub starts this detached with -Tip, the full SHA the board displayed. It refuses when claude/main has moved, so a
# click never deploys a commit nobody saw a verdict line for. By hand: pwsh -File scripts\live\deploy-ready.ps1 -Tip <sha> -WhatIf
param(
  [Parameter(Mandatory)][ValidatePattern('^[0-9a-f]{40}$')][string]$Tip,
  [switch]$WhatIf
)

$ErrorActionPreference = 'Continue'
# An inherited ATRIUM_NEW_BUILD would name some other binary as the one to install. See live-common.ps1.
Remove-Item Env:ATRIUM_NEW_BUILD -ErrorAction SilentlyContinue
$LiveTag = 'READY'
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'deploy.log'

$head = (git -C $Repo rev-parse refs/heads/claude/main 2>$null)
if ($head -ne $Tip) { Say "FATAL: claude/main is $head, not $Tip. nothing deployed"; exit 1 }

# What this deploy puts live, for HANDOFF. Read before anything changes.
Write-DeployQueue

$flags = @(); if ($WhatIf) { $flags += '-WhatIf' }
& pwsh -NoProfile -File "$PSScriptRoot\build-deploy.ps1" @flags
if ($LASTEXITCODE -ne 0) { Say "build failed ($LASTEXITCODE). nothing deployed"; exit $LASTEXITCODE }
if (-not $WhatIf) {
  # The binary about to be installed must be the commit the board showed. The version line may carry a short sha.
  $built = Join-Path $Repo 'build.claude\atrium.exe'
  $line = @(& $built version 2>$null) | Where-Object { $_ -match '^commit\s' } | Select-Object -First 1
  $sha = if ($line -match '^commit\s+([0-9a-f]{7,40})') { $Matches[1] } else { '' }
  if (-not $sha -or -not $Tip.StartsWith($sha)) {
    Say "FATAL: $built reports commit '$sha', not $Tip. nothing deployed"
    exit 1
  }
}
& pwsh -NoProfile -File "$PSScriptRoot\deploy-hub-only.ps1" @flags
exit $LASTEXITCODE
