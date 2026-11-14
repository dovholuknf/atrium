# Proves Backup moves the log aside so the new one starts empty, and keeps only the newest -Keep rotations. Runs in a
# temp directory. Usage: pwsh -NoProfile -File scripts/live/test-backup-rotation.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}
function Invoke-Step([string]$What, [scriptblock]$Do) { & $Do }

$ast = [System.Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot "live-common.ps1"), [ref]$null, [ref]$null)
$def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq 'Backup' }, $true) |
  Select-Object -First 1
. ([scriptblock]::Create($def.Extent.Text))

$Base = Join-Path ([IO.Path]::GetTempPath()) "backup-rotation-$PID"
New-Item -ItemType Directory $Base | Out-Null
try {
  $now = Get-Date
  1..8 | ForEach-Object {
    $f = Join-Path $Base "hub.err.2026010$_-000000"
    Set-Content $f "old $_"
    (Get-Item $f).LastWriteTime = $now.AddDays(-20 + $_)
  }
  Set-Content (Join-Path $Base 'hub.err') 'the whole history'
  Backup 'hub.err' $Base 5
  Check 'the live log is gone after the move' (-not (Test-Path (Join-Path $Base 'hub.err')))
  $kept = @(Get-ChildItem $Base -Filter 'hub.err.*')
  Check "at most 5 rotations are kept, found $($kept.Count)" ($kept.Count -eq 5)
  Check 'the newest rotation holds the history' (@($kept | Where-Object { (Get-Content $_.FullName -Raw) -match 'the whole history' }).Count -eq 1)
  Check 'the oldest rotations were deleted' (-not (Test-Path (Join-Path $Base 'hub.err.20260101-000000')))
  Add-Content (Join-Path $Base 'hub.err') 'after'
  Check 'a new log starts with only what follows' ((Get-Content (Join-Path $Base 'hub.err') -Raw).Trim() -eq 'after')
} finally {
  Remove-Item $Base -Recurse -Force -ErrorAction SilentlyContinue
}
if ($fail) { Write-Host "$fail failed"; exit 1 }
