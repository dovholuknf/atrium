# Proves Stop-Control waits sg4-control's restart_wrap_wait_s plus 45s before a force, and that -Immediate posts ?now=1
# and waits 45s, and that a service running ctl-bin's room is stopped and started only through the SCM. Fakes the
# room, the process and the service, posts nothing real.
# Usage: pwsh -NoProfile -File scripts/live/test-stop-control-wait.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}
$said = [System.Collections.Generic.List[string]]::new()
function Say($m) { $said.Add("$m") }

$ast = [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot\live-common.ps1", [ref]$null, [ref]$null)
foreach ($n in 'Invoke-Step', 'Get-WrapWait', 'Get-ControlService', 'Stop-Control') {
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
$CtlBinDir = 'C:\Users\claude\.atrium\ctl-bin'
Remove-Item Env:ATRIUM_CONTROL_SERVICE -ErrorAction SilentlyContinue
$services = @([pscustomobject]@{ Name = 'atrium-room'; PathName = '"C:\Users\claude\.atrium\bin\atrium.exe" room --root x' })
function Get-CimInstance { param($ClassName, $ErrorAction) $services }
function net.exe { $calls.Add("net $args") }
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
Check "claude-sg4's service is not taken for sg4-control's" (-not ($calls -match '^net '))

$calls.Clear(); $said.Clear()
$services += [pscustomobject]@{ Name = 'atrium-ctl'; PathName = '"C:\Users\claude\.atrium\ctl-bin\atrium.exe" room --root x' }
Check "Get-ControlService finds the service running ctl-bin's room" ((Get-ControlService) -eq 'atrium-ctl')
Stop-Control
Check 'a service is stopped with net stop and nothing else' (($calls -join '|') -eq 'net stop atrium-ctl')

$start = Get-Content "$PSScriptRoot\start-atrium-control.ps1" -Raw
Check 'start-atrium-control starts a service with net start, else Start-Jobless' ($start -match 'if \(\$svc\) \{[\s\S]*?net\.exe start \$svc[\s\S]*\} else \{ Invoke-Step "start sg4-control: [\s\S]*Start-Jobless')

if ($fail) { Write-Host "$fail FAILED"; exit 1 }
Write-Host 'all passed'
