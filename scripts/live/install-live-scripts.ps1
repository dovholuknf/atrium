# Copy this directory's scripts over the live ones in C:\Users\claude\.atrium2\scripts\, and say which changed.
#
# The live copies are what actually runs a deploy, and nothing kept them in step with the repo. By 2026-09-29 they
# had drifted for days: the live Save-Revert still named its snapshot after the hub's board hash, so the snapshot
# taken during the room deadlock held the deadlocked build. Run this after any change to scripts/live lands on
# claude/main. -WhatIf lists what would change and copies nothing.
#
# Every file the scripts read goes with them, not only the .ps1: room-ssh.json is read from beside the scripts, and
# without it the deploy logged in to sgg as claude. Only the README stays behind. -Dest is for tests.
param([switch]$WhatIf, [string]$Dest = 'C:\Users\claude\.atrium2\scripts')

$dest = $Dest
if (-not (Test-Path $dest) -and -not $WhatIf) { New-Item -ItemType Directory -Force $dest | Out-Null }

foreach ($f in Get-ChildItem $PSScriptRoot -File | Where-Object { $_.Extension -ne '.md' }) {
  $live = Join-Path $dest $f.Name
  $state = if (-not (Test-Path $live)) { 'new' }
    elseif ((Get-Content $live -Raw) -ceq (Get-Content $f.FullName -Raw)) { 'same' }
    else { 'changed' }
  if ($state -eq 'same') { continue }
  if ($WhatIf) { "WHATIF: $state $($f.Name)"; continue }
  if ($state -eq 'changed') { Copy-Item $live "$live.$(Get-Date -Format yyyyMMdd-HHmmss).bak" -Force }
  Copy-Item $f.FullName $live -Force
  "$state $($f.Name)"
}
