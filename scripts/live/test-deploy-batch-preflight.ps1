# Proves deploy-batch asks whether the room will refuse the stop BEFORE it sets a hold: a room whose pty host would die
# with it, with runners, and no -KillRunners exits 1 having set no hold, started no deploy and stopped nothing. Runs the
# real deploy-batch.ps1 from a temp copy beside stub helpers that only record, so nothing real is touched.
# Usage: pwsh -NoProfile -File scripts/live/test-deploy-batch-preflight.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}

$ast = [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot\live-common.ps1", [ref]$null, [ref]$null)
$def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq 'Test-StopWouldRefuse' }, $true) |
  Select-Object -First 1
$real = $def.Extent.Text

$tmp = Join-Path ([IO.Path]::GetTempPath()) "deploy-batch-preflight-$PID"
New-Item -ItemType Directory $tmp | Out-Null
try {
  Copy-Item "$PSScriptRoot\deploy-batch.ps1" $tmp
  Set-Content "$tmp\refuse-supervised.ps1" 'function Assert-NotSupervised { param($Targets) }'
  $calls = Join-Path $tmp 'calls.txt'
  $stub = @"
`$Base = '$tmp'
`$AtriumNew = '$tmp'
`$RoomPreflightUrl = 'http://fake/v1/preflight'
`$RoomHoldUrl = 'http://fake/v1/hold'
`$calls = '$calls'
function Say(`$m) { Add-Content `$calls "SAY `$m" }
function Rec(`$m) { Add-Content `$calls `$m }
function Test-NewBuildStamped { `$true }
function Wait-NewContextsDone { Rec 'wait-contexts'; `$true }
function Test-RunnersSurvive { Rec 'runners-survive'; `$false }
function Set-DeployHold { Rec 'set-hold'; `$true }
function Wait-HoldQuiet { Rec 'wait-quiet'; `$true }
function Write-DeployQueue { Rec 'queue' }
function Save-Revert { Rec 'save-revert'; 'rev' }
function Stop-Room { Rec 'stop-room'; `$true }
function Stop-Hub { Rec 'stop-hub' }
function Install-Atrium { Rec 'install'; `$false }
function Invoke-RestMethod { param(`$Uri, `$Method, `$Body, `$Headers, `$TimeoutSec, `$ContentType)
  if (`$Method -eq 'Post') { Rec "post `$Uri `$Body" }
  Get-Content '$tmp\preflight.json' -Raw | ConvertFrom-Json
}
$real
"@
  Set-Content "$tmp\live-common.ps1" $stub

  function Run([string]$Preflight, [string[]]$Args2) {
    Set-Content "$tmp\preflight.json" $Preflight
    Remove-Item $calls -ErrorAction SilentlyContinue
    $null = & pwsh -NoProfile -File "$tmp\deploy-batch.ps1" @Args2
    $script:code = $LASTEXITCODE
    $script:log = if (Test-Path $calls) { Get-Content $calls -Raw } else { '' }
  }

  $dies = '{"pty_host":{"on":true,"in_job":true},"runners":{"host":18,"in_process":8}}'
  Run $dies @()
  Check 'a room that would refuse the stop exits 1' ($code -eq 1)
  Check 'it set no hold' ($log -notmatch 'set-hold')
  Check 'it posted nothing to the room' ($log -notmatch 'post ')
  Check 'it stopped nothing and changed nothing' ($log -notmatch 'stop-room|stop-hub|install|save-revert|wait-contexts')
  Check 'it said REFUSED with the runner counts and -KillRunners' ($log -match 'REFUSED' -and $log -match 'held by the pty host: 18' -and $log -match '-KillRunners')

  Run $dies @('-KillRunners')
  Check 'with -KillRunners the hold is set as before' ($log -match 'set-hold')

  Run '{"pty_host":{"on":true,"in_job":false},"runners":{"host":18,"in_process":8}}' @()
  Check 'a host outside the job is not refused, the hold is set' ($log -match 'set-hold' -and $log -notmatch 'REFUSED')

  Run '{"pty_host":{"on":false,"in_job":false},"runners":{"host":0,"in_process":8}}' @()
  Check 'runners in the room process alone are not refused, the hold is set' ($log -match 'set-hold' -and $log -notmatch 'REFUSED')
} finally {
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

if ($fail) { Write-Host "$fail FAILED"; exit 1 }
Write-Host 'all passed'
