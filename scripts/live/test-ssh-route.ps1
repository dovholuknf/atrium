# Proves Resolve-SshRoute tries the bare name, then "<room>.atrium", and says which answered. A fake probe stands in for
# ssh, so nothing touches the network. Runs under Windows PowerShell 5.1 and pwsh 7.
# Usage: powershell -NoProfile -File scripts/live/test-ssh-route.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}

$ast = [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot\live-common.ps1", [ref]$null, [ref]$null)
foreach ($n in 'Resolve-SshRoute', 'Get-RoomSshUser', 'Get-SshFailure', 'ConvertTo-Base64Utf8', 'New-DetachedCommand', 'Read-DetachedCommand', 'Wait-SwapDone', 'Invoke-WindowsSwap') {
  $def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq $n }, $true) |
    Select-Object -First 1
  . ([scriptblock]::Create($def.Extent.Text))
}
$SwapDone = 'SWAP-DONE'
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

Reset @{ 'localai@sgg' = 255; 'localai@sgg.atrium' = 0 }
$r = Resolve-SshRoute 'sgg' $probe 'localai'
Check 'a user is put on every candidate and the answering one is kept' ($r.Host -eq 'localai@sgg.atrium' -and ($script:calls -join ',') -eq 'localai@sgg,localai@sgg.atrium')

$tmp = Join-Path ([IO.Path]::GetTempPath()) "room-ssh-$PID.json"
'{ "sgg": { "user": "localai" } }' | Set-Content $tmp
Check 'user read from the file' ((Get-RoomSshUser 'sgg' $null $tmp) -eq 'localai')
Check 'no entry means no user' ((Get-RoomSshUser 'sg3' $null $tmp) -eq '')
Check 'the hub record wins over the file' ((Get-RoomSshUser 'sgg' ([pscustomobject]@{ ssh_user = 'bob' }) $tmp) -eq 'bob')
Remove-Item $tmp
Check 'the shipped file names sgg as localai' ((Get-RoomSshUser 'sgg' $null (Join-Path $PSScriptRoot 'room-ssh.json')) -eq 'localai')

$denied = { param($h) [pscustomobject]@{ Code = 255; TimedOut = $false; Out = "${h}: Permission denied (publickey)." } }
$r = Resolve-SshRoute 'sgg' $denied 'claude'
Check 'refused key says refused as the user, not offline' ((Get-SshFailure $r 'claude') -eq 'failed - ssh refused as claude, the host answered and rejected the key')
$r = Resolve-SshRoute 'm1mini' { param($h) [pscustomobject]@{ Code = 255; TimedOut = $false; Out = 'Could not resolve hostname' } }
Check 'no answer is still offline' ((Get-SshFailure $r) -match '^failed - offline')

# The swap runs detached, so an ssh session that dies mid-swap does not decide anything: the hub does.
$script:polls = 0
$back = { $script:polls++; $script:polls -ge 3 }
$read = { throw 'ssh: connection closed' }
$w = Wait-SwapDone $back $read 30 0
Check 'the swap ssh drops and the room still comes back' ($w.Back -and -not $w.TimedOut -and $script:polls -eq 3)

$script:polls = 0
$read = { 'PORT-WAIT-TIMEOUT' + "`n" + 'SWAP-DONE' }
$w = Wait-SwapDone { $false } $read 30 0
Check 'output ending in SWAP-DONE returns early with what the swap said' (-not $w.Back -and -not $w.TimedOut -and $w.Out -match 'PORT-WAIT-TIMEOUT')

$w = Wait-SwapDone { $false } { throw 'down' } 1 0
Check 'room never back and ssh never answers: times out with no verdict' ($w.TimedOut -and -not $w.Back)

$script = 'echo swap-body ; exit 3'
$mac = New-DetachedCommand 'mac' $script
Check 'mac: runs under nohup, output to a file, from a decoded script' ($mac -match 'nohup sh' -and $mac -match 'deploy-swap\.out' -and $mac -match 'base64 --decode')
$b64 = [regex]::Match($mac, 'echo ([A-Za-z0-9+/=]+) \|').Groups[1].Value
Check 'mac: the script file ends in SWAP-DONE' ([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($b64)) -eq ($script + ' ; echo SWAP-DONE'))
$threw = $false
try { [void](New-DetachedCommand 'windows' $script) } catch { $threw = $true }
Check 'windows has no detached command' $threw

$script:polls = 0
$run = { [pscustomobject]@{ Code = 255; TimedOut = $false; Out = 'client_loop: send disconnect: Connection reset' } }
$back = { $script:polls++; $script:polls -ge 2 }
$w = Invoke-WindowsSwap $run $back 30 0
Check 'windows: the swap session drops and the hub check is still reached' ($w.Back -and -not $w.TimedOut -and $script:polls -eq 2)

$run = { [pscustomobject]@{ Code = 0; TimedOut = $false; Out = 'started' } }
$script:polls = 0
$w = Invoke-WindowsSwap $run { $script:polls++; $true } 30 0
Check 'windows: a swap that finished is returned for the hub check without waiting here' (-not $w.TimedOut -and $script:polls -eq 0 -and $w.Out -eq 'started')

$run = { [pscustomobject]@{ Code = 3; TimedOut = $false; Out = 'PORT-WAIT-TIMEOUT' } }
$w = Invoke-WindowsSwap $run { $true } 30 0
Check 'windows: a verdict the swap printed is returned, not waited past' ($w.Out -eq 'PORT-WAIT-TIMEOUT' -and -not $w.Back)

$run = { [pscustomobject]@{ Code = -1; TimedOut = $true; Out = '' } }
$w = Invoke-WindowsSwap $run { $true } 30 0
Check 'windows: a hung swap is reported hung' $w.TimedOut
Check 'the mac read command names the output file' ((Read-DetachedCommand 'mac') -match 'deploy-swap\.out')
if ($fail -gt 0) { Write-Host "$fail failed"; exit 1 }
Write-Host 'all passed'
