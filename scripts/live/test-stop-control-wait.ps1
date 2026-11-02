# Proves Stop-Control waits sg4-control's restart_wrap_wait_s plus 45s before a force, and that -Immediate posts ?now=1
# and waits 45s. Fakes the room and the process, posts nothing real.
# Usage: pwsh -NoProfile -File scripts/live/test-stop-control-wait.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}
$said = [System.Collections.Generic.List[string]]::new()
function Say($m) { $said.Add("$m") }

$ast = [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot\live-common.ps1", [ref]$null, [ref]$null)
foreach ($n in 'Invoke-Step', 'Get-WrapWait', 'Stop-Control') {
  $def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq $n }, $true) |
    Select-Object -First 1
  . ([scriptblock]::Create($def.Extent.Text))
}

$CtlShutdown = 'http://fake-ctl/v1/shutdown'
$CtlServes = 'http://fake-ctl/v1/settings'
$RoomServes = 'http://fake-room/v1/settings'
$WhatIf = $false
$calls = [System.Collections.Generic.List[string]]::new()
function Find-Control { , @([pscustomobject]@{ ProcessId = 2147480000 }) }
function Invoke-RestMethod {
  param($Uri, $Method, $TimeoutSec)
  $calls.Add("$Method $Uri")
  if ($Uri -eq $CtlServes) { return [pscustomobject]@{ restart_wrap_wait_s = 120 } }
  if ($Uri -eq $RoomServes) { return [pscustomobject]@{ restart_wrap_wait_s = 999 } }
}
function Wait-Process { param($Id, $Timeout) $calls.Add("wait $Timeout") }
function Get-Process { param($Id) [pscustomobject]@{ Id = $Id } }
function Stop-Process { param($Id, [switch]$Force) $calls.Add("force $Id") }

$Immediate = $false
Stop-Control
Check 'it reads the wrap wait from sg4-control, not claude-sg4' ($calls -contains " $CtlServes" -and $calls -notcontains " $RoomServes")
Check 'it posts a plain shutdown' ($calls -contains "Post $CtlShutdown")
Check 'it waits the wrap wait plus 45s before the force' ($calls -contains 'wait 165' -and $calls[-1] -eq 'force 2147480000')
Check 'it says the wait it used' (($said -join "`n") -match 'wait up to 165s' -and ($said -join "`n") -match 'still up after 165s')

$calls.Clear(); $said.Clear()
$Immediate = $true
Stop-Control
Check '-Immediate posts now=1 and reads no setting' ($calls -contains "Post $CtlShutdown`?now=1" -and $calls -notcontains " $CtlServes")
Check '-Immediate waits 45s' ($calls -contains 'wait 45')

if ($fail) { Write-Host "$fail FAILED"; exit 1 }
Write-Host 'all passed'
