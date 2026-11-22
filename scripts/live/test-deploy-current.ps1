# Proves a deploy leaves alone a target that already runs the build: Test-OnCommit judges a running commit and
# Get-LiveCommits reads what the hub says each target runs, with a fake hub. A target the hub cannot place is not current.
# Usage: pwsh -NoProfile -File scripts/live/test-deploy-current.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}

$ast = [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot\live-common.ps1", [ref]$null, [ref]$null)
foreach ($n in 'Test-OnCommit', 'Get-LiveCommits') {
  $def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq $n }, $true) |
    Select-Object -First 1
  . ([scriptblock]::Create($def.Extent.Text))
}
$HubRooms = 'http://fake/_hub/rooms'
$HubQueueJson = 'http://fake/_hub/deploy-queue'

$full = '1abd9c1' + ('0' * 33)
Check 'the same commit is current' (Test-OnCommit $full $full)
Check 'a short commit is current against the full one' (Test-OnCommit '1abd9c1' $full)
Check 'another commit is not current' (-not (Test-OnCommit ('2' * 40) $full))
Check 'an unknown commit is not current' (-not (Test-OnCommit '' $full))
Check 'a commit too short to mean anything is not current' (-not (Test-OnCommit '1abd' $full))

$script:rooms = @{ rooms = @(
    @{ name = 'sg3'; commit = $full },
    @{ name = 'sgg'; setup = @{ commit = '2' * 40 } },
    @{ name = 'sg4-control' },
    @{ name = 'claude-sg4'; commit = $full }) }
$script:queue = @{ hub = $full }
$script:down = @()
function Invoke-RestMethod {
  param($Uri, $TimeoutSec)
  foreach ($d in $script:down) { if ($Uri -like "*$d*") { throw 'connection refused' } }
  if ($Uri -like '*/_hub/rooms') { return [pscustomobject]($script:rooms | ConvertTo-Json -Depth 5 | ConvertFrom-Json) }
  [pscustomobject]($script:queue | ConvertTo-Json | ConvertFrom-Json)
}

$live = Get-LiveCommits
Check 'the hub and each room that says a commit are read' ($live['hub'] -eq $full -and $live['sg3'] -eq $full -and $live['claude-sg4'] -eq $full)
Check 'a room that says its commit in setup is read' ($live['sgg'] -eq ('2' * 40))
Check 'a room that says none is left out' (-not $live.ContainsKey('sg4-control'))
Check 'only the stale and unplaced targets are left to deploy' ((@('hub', 'claude-sg4', 'sgg', 'sg4-control') | Where-Object { -not (Test-OnCommit $live[$_] $full) }) -join ',' -eq 'sgg,sg4-control')

$script:down = @('deploy-queue')
$live = Get-LiveCommits
Check 'a hub with no deploy queue is not placed, so it is deployed' (-not $live.ContainsKey('hub') -and $live['sg3'] -eq $full)
$script:down = @('_hub/rooms', 'deploy-queue')
Check 'a hub that does not answer places nothing' ((Get-LiveCommits).Count -eq 0)

if ($fail) { Write-Host "$fail FAILED"; exit 1 }
Write-Host 'all passed'
