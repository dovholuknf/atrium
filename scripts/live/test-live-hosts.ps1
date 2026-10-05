# Tests Get-LiveHosts and Set-LiveHosts with a fake reader. Touches no live hub, room or environment variable beyond
# this process. Usage: pwsh -NoProfile -File scripts/live/test-live-hosts.ps1
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\live-common.ps1"
$fail = 0
function Check([string]$What, $Got, $Want) {
  if ($Got -ceq $Want) { Write-Host "PASS  $What"; return }
  Write-Host "FAIL  $What (want '$Want', got '$Got')"
  $script:fail++
}

$h = Get-LiveHosts { param($s) @{ User = 'u.example'; Machine = 'm.example' }[$s] }
Check 'User wins over Machine' "$($h.Value)/$($h.Scope)" 'u.example/User'
$h = Get-LiveHosts { param($s) @{ Machine = 'm.example' }[$s] }
Check 'Machine used when User is unset' "$($h.Value)/$($h.Scope)" 'm.example/Machine'
$h = Get-LiveHosts { param($s) @{ User = ''; Machine = 'm.example' }[$s] }
Check 'Machine used when User is empty' "$($h.Value)/$($h.Scope)" 'm.example/Machine'
$h = Get-LiveHosts { param($s) $null }
Check 'unset at both is null' "$($h.Value)|$($h.Scope)" '|'
$said = Get-LiveHostsSaid ([pscustomobject]@{ Value = 'a'; Scope = 'Machine' })
Check 'said, set' $said 'ATRIUM_HOSTS=a, from the Machine environment'

$old = $env:ATRIUM_HOSTS
try {
  $env:ATRIUM_HOSTS = 'caller.example'
  Set-LiveHosts ([pscustomobject]@{ Value = 'x.example'; Scope = 'User' })
  Check 'Set replaces the caller value' $env:ATRIUM_HOSTS 'x.example'
  Set-LiveHosts ([pscustomobject]@{ Value = $null; Scope = $null })
  Check 'Set removes it when unset' ([bool](Test-Path Env:ATRIUM_HOSTS)) $false
} finally {
  if ($null -ne $old) { $env:ATRIUM_HOSTS = $old } else { Remove-Item Env:ATRIUM_HOSTS -ErrorAction SilentlyContinue }
}
if ($fail) { Write-Host "$fail failed"; exit 1 }
Write-Host 'all passed'
