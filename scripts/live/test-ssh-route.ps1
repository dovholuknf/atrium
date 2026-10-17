# Proves Resolve-SshRoute tries the bare name, then "<room>.atrium", and says which answered. A fake probe stands in for
# ssh, so nothing touches the network. Runs under Windows PowerShell 5.1 and pwsh 7.
# Usage: powershell -NoProfile -File scripts/live/test-ssh-route.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}

$ast = [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot\live-common.ps1", [ref]$null, [ref]$null)
$def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq 'Resolve-SshRoute' }, $true) |
  Select-Object -First 1
. ([scriptblock]::Create($def.Extent.Text))

$script:calls = @()
$script:answers = @{}
$probe = {
  param($h)
  $script:calls += $h
  $code = if ($script:answers.ContainsKey($h)) { $script:answers[$h] } else { 255 }
  if ($code -eq 'hang') { return [pscustomobject]@{ Code = -1; TimedOut = $true } }
  return [pscustomobject]@{ Code = $code; TimedOut = $false }
}
function Reset([hashtable]$Answers) { $script:calls = @(); $script:answers = $Answers }

Reset @{ 'sg3' = 255; 'sg3.atrium' = 0 }
$r = Resolve-SshRoute 'sg3' $probe
Check 'bare fails, .atrium works: the .atrium route is chosen' ($r.Host -eq 'sg3.atrium')
Check 'bare fails, .atrium works: both were tried, bare first' (($script:calls -join ',') -eq 'sg3,sg3.atrium')

Reset @{ 'sg3' = 'hang'; 'sg3.atrium' = 0 }
$r = Resolve-SshRoute 'sg3' $probe
Check 'bare hangs, .atrium works: the .atrium route is chosen' ($r.Host -eq 'sg3.atrium')

Reset @{}
$r = Resolve-SshRoute 'm1mini' $probe
Check 'both fail: no host, so the room is offline' ($r.Host -eq '')
Check 'both fail: both routes were tried' (($r.Tried -join ',') -eq 'm1mini,m1mini.atrium' -and $r.Results.Count -eq 2)

Reset @{ 'sgg' = 0; 'sgg.atrium' = 0 }
$r = Resolve-SshRoute 'sgg' $probe
Check 'bare works: the bare name is chosen' ($r.Host -eq 'sgg')
Check 'bare works: .atrium is never called' (($script:calls -join ',') -eq 'sgg')

Reset @{ 'sg3' = 'hang'; 'sg3.atrium' = 'hang' }
$r = Resolve-SshRoute 'sg3' $probe
Check 'both hang: no host and the hang is visible in the results' ($r.Host -eq '' -and @($r.Results | Where-Object { $_.TimedOut }).Count -eq 2)

if ($fail -gt 0) { Write-Host "$fail failed"; exit 1 }
Write-Host 'all passed'
