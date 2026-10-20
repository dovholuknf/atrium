# Proves Stop-Room never ends runners on its own: a 409 from the room (the pty host would die with it) stops the deploy
# and posts nothing more, and only -KillRunners posts {"runners":true}. Fakes the room, posts nothing real.
# Usage: pwsh -NoProfile -File scripts/live/test-stop-room-409.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}
$said = [System.Collections.Generic.List[string]]::new()
function Say($m) { $said.Add("$m") }

# Only the helpers are loaded, not live-common's top level, which looks at the machine.
$ast = [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot\live-common.ps1", [ref]$null, [ref]$null)
foreach ($n in 'Invoke-Step', 'Get-HostInJobLines', 'Stop-Room') {
  $def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq $n }, $true) |
    Select-Object -First 1
  . ([scriptblock]::Create($def.Extent.Text))
}

# The fakes: a room that answers 409 to a plain stop and records every body posted.
$RoomShutdown = 'http://fake/v1/shutdown'
$RoomPreflightUrl = 'http://fake/v1/preflight'
$WhatIf = $false; $Immediate = $true
$posts = [System.Collections.Generic.List[string]]::new()
function Find-Atrium($sub) { , @([pscustomobject]@{ ProcessId = 2147480000 }) }
function Get-WrapWait { 0 }
function Get-ShutdownStatus($err) { 409 }
function Invoke-RestMethod { param($Uri, $TimeoutSec) [pscustomobject]@{ runners = [pscustomobject]@{ host = 3; in_process = 0 } } }
function Invoke-ShutdownPost([string]$Uri, [string]$Body) {
  $posts.Add("$Body")
  if (-not $Body) { throw 'conflict' }
}

$KillRunners = $false
$r = Stop-Room
Check 'a 409 without -KillRunners returns false' ($r -eq $false)
Check 'it posted once, with no body' ($posts.Count -eq 1 -and $posts[0] -eq '')
Check 'it never posted runners:true' (-not ($posts -match 'runners'))
Check 'it said it was refused and how many runners' (($said -join "`n") -match 'REFUSED' -and ($said -join "`n") -match 'held by the pty host: 3')
Check 'it named -KillRunners' (($said -join "`n") -match '-KillRunners')
Check 'it said the cause plainly and what to do' (($said -join "`n") -match 'inside a job object that refused breakaway' -and ($said -join "`n") -match 'jobless start')

$posts.Clear(); $said.Clear()
$KillRunners = $true
$r = Stop-Room
Check 'with -KillRunners it goes on and returns true' ($r -eq $true)
Check 'with -KillRunners the second post carries runners:true' ($posts.Count -eq 2 -and $posts[1] -eq '{"runners":true}')

if ($fail) { Write-Host "$fail FAILED"; exit 1 }
Write-Host 'all passed'
