# The one click behind "deploy ready": build the tip the board showed, then run today's hub-only deploy.
#
# The hub starts this detached with -Tip, the full SHA the board displayed. It refuses when claude/main has moved, so a
# click never deploys a commit nobody saw a verdict line for. By hand: pwsh -File scripts\live\deploy-ready.ps1 -Tip <sha> -WhatIf
param(
  [Parameter(Mandatory)][ValidatePattern('^[0-9a-f]{40}$')][string]$Tip,
  [switch]$WhatIf
)

$ErrorActionPreference = 'Continue'
$LiveTag = 'READY'
. "$PSScriptRoot\live-common.ps1"
$LiveLog = Join-Path $Base 'deploy.log'

$head = (git -C $Repo rev-parse refs/heads/claude/main 2>$null)
if ($head -ne $Tip) { Say "FATAL: claude/main is $head, not $Tip. nothing deployed"; exit 1 }

$flags = @(); if ($WhatIf) { $flags += '-WhatIf' }
& pwsh -NoProfile -File "$PSScriptRoot\build-deploy.ps1" @flags
if ($LASTEXITCODE -ne 0) { Say "build failed ($LASTEXITCODE). nothing deployed"; exit $LASTEXITCODE }
& pwsh -NoProfile -File "$PSScriptRoot\deploy-hub-only.ps1" @flags
exit $LASTEXITCODE
