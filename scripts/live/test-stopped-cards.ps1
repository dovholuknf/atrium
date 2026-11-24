# Tests Get-StoppedCards against fake /v1/preflight answers. Touches no live hub or room.
# Usage: pwsh -NoProfile -File scripts/live/test-stopped-cards.ps1
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\live-common.ps1"
$fail = 0
function Check([string]$What, $Got, $Want) {
  if ($Got -ceq $Want) { Write-Host "PASS  $What"; return }
  Write-Host "FAIL  $What (want '$Want', got '$Got')"
  $script:fail++
}
$script:answer = $null
function Invoke-RestMethod { if ($null -eq $script:answer) { throw 'unreachable' }; $script:answer | ConvertFrom-Json }

$script:answer = '{"pty_host":{"on":false},"runners":{"host":0,"in_process":2,"in_process_cards":["b-card","a-card"]}}'
Check 'names the cards a stop ends' (Get-StoppedCards) 'runners stopped: b-card, a-card'
$script:answer = '{"pty_host":{"on":false},"runners":{"host":0,"in_process":3}}'
Check 'an older room gives the count' (Get-StoppedCards) '3 runner(s) stopped'
$script:answer = '{"pty_host":{"on":true},"runners":{"host":4,"in_process":0,"in_process_cards":[]}}'
Check 'nothing in process says nothing' (Get-StoppedCards) ''
$script:answer = $null
Check 'an unreachable room says nothing' (Get-StoppedCards) ''
if ($fail) { Write-Host "$fail failed"; exit 1 }
Write-Host 'all passed'
