# Proves the deploy's skew check: one line per room the hub knows, STALE and offline rooms fail the run with the exact
# command to deploy them, and -CheckOnly runs only that. The hub's answer is faked, nothing real is touched.
# Usage: pwsh -NoProfile -File scripts/live/test-deploy-skew.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}

$ast = [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot\live-common.ps1", [ref]$null, [ref]$null)
foreach ($n in 'Get-RoomSkew', 'Format-RoomSkew', 'Get-DeployNext', 'Get-RoomSkewReport') {
  $def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq $n }, $true) |
    Select-Object -First 1
  . ([scriptblock]::Create($def.Extent.Text))
}

$hub = '84c788ac1111111111111111111111111111aaaa'
$old = '59f696b5222222222222222222222222222bbbb'
$HubRooms = 'fake-rooms'; $HubInventory = 'fake-inventory'
$attached = @(
  [pscustomobject]@{ name = 'claude-sg4'; commit = $hub; local = $true },
  [pscustomobject]@{ name = 'sg3'; commit = $hub },
  [pscustomobject]@{ name = 'sg4-control'; commit = $old },
  [pscustomobject]@{ name = 'sgg-zrok'; setup = [pscustomobject]@{ commit = $old } })
$inventory = @(
  [pscustomobject]@{ name = 'claude-sg4'; attached = $true }, [pscustomobject]@{ name = 'sg3'; attached = $true },
  [pscustomobject]@{ name = 'sg4-control'; attached = $true }, [pscustomobject]@{ name = 'sgg-zrok'; attached = $true },
  [pscustomobject]@{ name = 'm1mini'; attached = $false; first_seen = '2026-10-01T00:00:00Z' },
  [pscustomobject]@{ name = 'never'; attached = $false })
function Invoke-RestMethod { param($Uri, $TimeoutSec) if ($Uri -eq 'fake-rooms') { @{ rooms = $attached } } else { @{ rooms = $inventory } } }

$rep = Get-RoomSkewReport 'ignored'
$text = $rep.Lines -join "`n"
Check 'a room on the hub commit is ok' ($text -match '(?m)^sg3  84c788ac  ok$')
Check 'a room on another commit is STALE with the hub commit' ($text -match '(?m)^sg4-control  59f696b5  STALE \(hub 84c788ac\)$')
Check 'the commit is read from setup when the room carries none' ($text -match '(?m)^sgg-zrok  59f696b5  STALE ')
Check 'a known room that is not attached is offline' ($text -match '(?m)^m1mini  offline$')
Check 'a room that never connected is left out' ($text -notmatch 'never')
Check 'the hub commit is its local room''s, not the fallback' ($text -notmatch 'ignored')
Check 'stale or offline rooms make it not ok' (-not $rep.Ok)
Check 'and name every one of them' (($rep.Bad -join ',') -eq 'm1mini,sg4-control,sgg-zrok')

$next = (Get-DeployNext $rep.Bad 'X:/atrium-deploy.ps1') -join "`n"
Check 'a deployable room gets the -Rooms command' ($next -match '(?m)^pwsh -NoProfile -File X:/atrium-deploy.ps1 -Rooms m1mini$')
Check 'sg4-control gets the -Control -IKnowItKillsMyShell form' ($next -match '(?m)-File X:/atrium-deploy.ps1 -Control -IKnowItKillsMyShell$')
Check 'sg4-control is not in -Rooms' ($next -notmatch '-Rooms [^\r\n]*sg4-control')
Check 'a room the script cannot deploy is said, not dropped' ($next -match 'sgg-zrok has no deploy target')

$attached = @($attached | ForEach-Object { [pscustomobject]@{ name = $_.name; commit = $hub; local = $_.local } })
$inventory = @($inventory | Where-Object { $_.attached })
$rep = Get-RoomSkewReport 'ignored'
Check 'every room on the hub commit is ok and exits clean' ($rep.Ok -and $rep.Bad.Count -eq 0)

function Invoke-RestMethod { throw 'connection refused' }
$rep = Get-RoomSkewReport $hub
Check 'a hub that does not answer is not ok, and says so' (-not $rep.Ok -and $rep.Lines[0] -match 'hub unreachable')

$deploy = Get-Content -Raw "$PSScriptRoot\atrium-deploy.ps1"
Check '-CheckOnly is a switch that exits before anything is built' ($deploy -match '\[switch\]\$CheckOnly' -and
  $deploy.IndexOf('if ($CheckOnly)') -lt $deploy.IndexOf('Find-Go'))
Check 'leaving -Rooms out warns that unlisted rooms are checked' ($deploy -match "ContainsKey\('Rooms'\)[^}]*checked at the end")
Check 'a deploy ends on the check and exits 1 when it fails' ($deploy -match 'Test-RoomSkew \$commit' -and $deploy -match 'if \(-not \$skewOk\) \{ exit 1 \}')

if ($fail) { Write-Host "$fail FAILED"; exit 1 }
Write-Host 'all passed'
