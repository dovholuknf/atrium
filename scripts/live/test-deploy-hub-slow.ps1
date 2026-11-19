# Proves a slow hub health check does not leave the hub stopped: Confirm-HubUp waits again on a running hub, starts only
# one that is not running, through the task scheduler, and the batch ends through Finish. Fakes only.
# Usage: pwsh -NoProfile -File scripts/live/test-deploy-hub-slow.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}
$said = [System.Collections.Generic.List[string]]::new()
function Say($m) { $said.Add("$m") }

$src = Get-Content -Raw "$PSScriptRoot\live-common.ps1"
$ast = [System.Management.Automation.Language.Parser]::ParseInput($src, [ref]$null, [ref]$null)
$def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq 'Confirm-HubUp' }, $true) |
  Select-Object -First 1
. ([scriptblock]::Create($def.Extent.Text))
foreach ($v in 'HubBudget', 'HubTries') {
  if ($src -notmatch "(?m)^\`$$v = (.+)$") { throw "no `$$v in live-common.ps1" }
  Set-Variable $v (Invoke-Expression $Matches[1])
}

$script:waits = 0; $script:starts = @(); $script:running = $true; $script:healthyOn = 3
function Wait-Hub([int]$Seconds) { $script:waits++; if ($script:waits -ge $script:healthyOn) { [pscustomobject]@{ ok = $true; build = 'x' } } }
function Find-Atrium($Sub) { if ($script:running) { [pscustomobject]@{ ProcessId = 1 } } }
function Start-Hub([switch]$ViaTask) { $script:starts += [bool]$ViaTask }

$h = Confirm-HubUp 1 3
Check 'a hub slow to answer is waited on until it does' ($null -ne $h -and $script:waits -eq 3)
Check 'a hub process that is running is never started again' ($script:starts.Count -eq 0)

$script:waits = 0; $script:running = $false; $script:healthyOn = 2
$h = Confirm-HubUp 1 3
Check 'a hub that is not running is started again' ($null -ne $h -and $script:starts.Count -eq 1)
Check 'and through the task scheduler' ($script:starts[0] -eq $true)

$script:waits = 0; $script:healthyOn = 100
Check 'a hub that never answers is $null after every try' ($null -eq (Confirm-HubUp 1 2))
Check 'the budget is minutes and tries more than one' ($HubBudget -ge 120 -and $HubTries -ge 2)

$batch = Get-Content -Raw "$PSScriptRoot\deploy-batch.ps1"
Check 'the batch starts the hub through the task scheduler' ($batch -match 'Start-Hub -ViaTask' -and $batch -notmatch '(?m)^\s*Start-Hub\s*$')
Check 'the batch waits with Confirm-HubUp, not a 25s wait' ($batch -match 'Confirm-HubUp' -and $batch -notmatch 'Wait-Hub 25')
Check 'every end goes through Finish and no exit is left after the hub stops' (
  $batch -match 'Finish 0' -and $batch -match 'Finish 2' -and $batch -notmatch 'nothing is running')
Check 'the result is written and said' ($batch -match 'deploy-result\.txt' -and $batch -match 'Send-CardSay')

if ($fail) { Write-Host "$fail FAILED"; exit 1 }
Write-Host 'all passed'
