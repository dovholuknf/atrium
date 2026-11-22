# Proves Resolve-SshRoute tries the bare name, then "<room>.atrium", and says which answered. A fake probe stands in for
# ssh, so nothing touches the network. Runs under Windows PowerShell 5.1 and pwsh 7.
# Usage: powershell -NoProfile -File scripts/live/test-ssh-route.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}

$ast = [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot\live-common.ps1", [ref]$null, [ref]$null)
foreach ($n in 'Resolve-SshRoute', 'Get-RoomSshUser', 'Get-SshFailure', 'Test-RemoteAccount', 'ConvertTo-Base64Utf8', 'New-DetachedCommand', 'Read-DetachedCommand', 'Wait-SwapDone', 'New-RestartIntoRead', 'Invoke-RestartInto') {
  $def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq $n }, $true) |
    Select-Object -First 1
  . ([scriptblock]::Create($def.Extent.Text))
}
$SwapDone = 'SWAP-DONE'
$HubRestartInto = 'http://127.0.0.1:7778/v1/restart-into'
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
Check 'refused key says refused as the user, not offline' ((Get-SshFailure $r 'claude') -eq "failed - ssh refused as claude, the host answered and rejected the key, local account $env:USERNAME")
$offered = { param($h) [pscustomobject]@{ Code = 255; TimedOut = $false; Out = "debug1: Offering public key: C:/Users/x/.ssh/id_ed25519 ED25519 SHA256:abc`n${h}: Permission denied (publickey)." } }
$r = Resolve-SshRoute 'sgg' $offered 'localai'
Check 'refused key names the local account and the key file offered' ((Get-SshFailure $r 'localai') -match "local account $env:USERNAME offered key C:/Users/x/\.ssh/id_ed25519$")
$RemoteAccount = 'claude'
Check 'the trusted account is let through' ((Test-RemoteAccount 'C:/s/start-atrium-remote.ps1' 'claude') -eq '')
$msg = Test-RemoteAccount 'C:/s/start-atrium-remote.ps1' 'localai'
Check 'a wrong account is refused with who ran it, the account needed and the command' ($msg -match 'run as localai' -and $msg -match 'only the claude account' -and $msg -match 'runas /user:claude "pwsh -NoProfile -File C:/s/start-atrium-remote\.ps1"')
$r =Resolve-SshRoute 'm1mini' { param($h) [pscustomobject]@{ Code = 255; TimedOut = $false; Out = 'Could not resolve hostname' } }
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
try { [void](New-DetachedCommand 'linux-or-other' $script) } catch { $threw = $true }
Check 'an unknown os has no detached command' $threw

$threw = $false
try { [void](New-DetachedCommand 'windows' $script) } catch { $threw = $true }
Check 'windows: no detached command, the room swaps itself' $threw
Check 'the mac read command names the output file' ((Read-DetachedCommand 'mac') -match 'deploy-swap\.out')

$read = New-RestartIntoRead '(Join-Path $HOME ''atrium-zrok.new.exe'')' '(Join-Path $HOME ''.atrium-zrok\room\restart.token'')'
$errs = $null
[void][System.Management.Automation.Language.Parser]::ParseInput($read, [ref]$null, [ref]$errs)
Check 'windows: the restart-into read parses and has no double quote for sshd to mangle' ($errs.Count -eq 0 -and $read -notmatch '"')
$tok = Join-Path ([IO.Path]::GetTempPath()) "restart-token-$PID"
"abc123`n" | Set-Content $tok
$got = Invoke-Expression (New-RestartIntoRead "'C:/x/atrium.new.exe'" "'$tok'") | ConvertFrom-Json
Remove-Item $tok
Check 'windows: the restart-into read answers the exe and the trimmed token as json' ($got.exe -eq 'C:/x/atrium.new.exe' -and $got.token -eq 'abc123')

$script:sent = $null
function Invoke-WebRequest {
  param($Uri, $Method, $Body, $ContentType, $TimeoutSec, [switch]$UseBasicParsing, $Headers)
  $script:sent = $Headers
  [pscustomobject]@{ StatusCode = 202; Content = ' restarting ' }
}
$a = Invoke-RestartInto 'sgg-zrok' 'C:/x/atrium-zrok.new.exe' 'tok' 'deploy abc' 'http://hub/v1/restart-into'
$h = $script:sent
Check 'a 202 is ok and says what the room said' ($a.Ok -and $a.Code -eq 202 -and $a.Out -eq 'restarting')
Check 'the ask names the room and carries its token' ($h['X-Atrium-Room'] -eq 'sgg-zrok' -and $h['X-Atrium-Token'] -eq 'tok')
function Invoke-WebRequest { throw 'connection refused' }
$a = Invoke-RestartInto 'sgg' 'x' 'tok' 'why' 'http://hub/v1/restart-into'
Check 'a hub that does not answer is not ok' (-not $a.Ok -and $a.Code -eq 0 -and $a.Out -match 'refused')
if ($fail -gt 0) { Write-Host "$fail failed"; exit 1 }
Write-Host 'all passed'
