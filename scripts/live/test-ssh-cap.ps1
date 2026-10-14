# Proves the deploy's ssh calls are capped and its verdict is the hub's. A fake ssh hangs forever on its first call and
# answers on its second, so the cap, the kill and the single retry show; a second hang fails with the call's name. A
# fake hub response decides whether a room is back. Starts nothing real.
# Usage: pwsh -NoProfile -File scripts/live/test-ssh-cap.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}
$said = [System.Collections.Generic.List[string]]::new()
function Say($m) { $said.Add("$m") }

# Only the helpers are loaded, not live-common's top level, which looks at the machine.
$ast = [System.Management.Automation.Language.Parser]::ParseFile("$PSScriptRoot\live-common.ps1", [ref]$null, [ref]$null)
foreach ($n in 'Invoke-Capped', 'Invoke-CappedRetry', 'Test-RoomBackOnHub') {
  $def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq $n }, $true) |
    Select-Object -First 1
  . ([scriptblock]::Create($def.Extent.Text))
}

$tmp = Join-Path ([IO.Path]::GetTempPath()) "ssh-cap-$PID"
New-Item -ItemType Directory $tmp | Out-Null
try {
  # The fake: hangs for the first $env:FAKE_HANGS calls (counted in a file), then prints its arguments.
  $fake = Join-Path $tmp 'fake-ssh.ps1'
  Set-Content $fake @'
$f = Join-Path $env:FAKE_DIR 'count'
$n = if (Test-Path $f) { [int](Get-Content $f) } else { 0 }
Set-Content $f ($n + 1)
if ($n -lt [int]$env:FAKE_HANGS) { [Threading.Thread]::Sleep(600000) }
"ok $($args -join ' ')"
exit [int]$env:FAKE_EXIT
'@
  $env:FAKE_DIR = $tmp; $env:FAKE_EXIT = '0'
  $pwsh = (Get-Process -Id $PID).Path
  $fakeArgs = @('-NoProfile', '-File', $fake, 'm1mini', 'echo ok')
  $count = Join-Path $tmp 'count'
  function Reset([int]$Hangs) { Remove-Item $count -ErrorAction SilentlyContinue; $env:FAKE_HANGS = "$Hangs"; $said.Clear() }

  Reset 0
  $r = Invoke-Capped $pwsh $fakeArgs 30
  Check 'a call that answers is returned with its output' (-not $r.TimedOut -and $r.Code -eq 0 -and $r.Out -match 'ok m1mini echo ok')
  $env:FAKE_EXIT = '7'
  Reset 0
  $r = Invoke-CappedRetry 'probe' $pwsh $fakeArgs 30
  Check 'a non-zero exit is an answer and is not retried' ($r.Code -eq 7 -and -not $r.TimedOut -and (Get-Content $count) -eq '1')
  $env:FAKE_EXIT = '0'

  Reset 1
  $sw = [Diagnostics.Stopwatch]::StartNew()
  $r = Invoke-CappedRetry 'probe' $pwsh $fakeArgs 8
  Check 'a hang is killed at the cap and the retry answers' (-not $r.TimedOut -and $r.Code -eq 0 -and $r.Out -match 'ok m1mini')
  Check 'it took the cap, not the hang' ($sw.Elapsed.TotalSeconds -lt 120)
  Check 'the hang is said' (($said -join "`n") -match 'probe hung past 8s \(attempt 1 of 2\).*retrying once')

  Reset 2
  $r = Invoke-CappedRetry 'ssh m1mini probe' $pwsh $fakeArgs 8
  Check 'a second hang comes back timed out' ($r.TimedOut -and $r.Code -eq -1)
  Check 'the message names the call' (($said -join "`n") -match 'ssh m1mini probe hung past 8s \(attempt 2 of 2\)')
  Check 'only two attempts were made' ((Get-Content $count) -eq '2')

  # The swap is not idempotent, so it never retries: a hung call runs once, and the deploy uses Invoke-SshOnce for it.
  Reset 1
  $r = Invoke-Capped $pwsh $fakeArgs 8
  Check 'a hung call run once is killed and run exactly once' ($r.TimedOut -and (Get-Content $count) -eq '1')
  $deploy = Get-Content -Raw "$PSScriptRoot\atrium-deploy.ps1"
  Check 'the swap and the detach retry use the no-retry call' ($deploy -match 'Invoke-SshOnce "ssh \$name swap"' -and
    $deploy -match 'Invoke-SshOnce "ssh \$name detach retry"')
  Check 'the no-retry call does not go through the retrying helper' (
    $deploy -match '(?s)function Invoke-SshOnce.*?Invoke-Capped \$ssh.*?\n\}' -and
    $deploy -notmatch '(?s)function Invoke-SshOnce[^}]*Invoke-CappedRetry')

  # --- the verdict comes from the hub's rooms, by since and commit ---
  $HubRooms = 'fake'
  $after = [datetime]'2026-10-14T10:00:00Z'
  $script:hub = $null
  function Invoke-RestMethod { param($Uri, $TimeoutSec) if ($null -eq $script:hub) { throw 'hub down' }; $script:hub }
  function Rooms($name, $since, $commit) {
    [pscustomobject]@{ rooms = @([pscustomobject]@{ name = $name; since = $since; commit = $commit }) }
  }
  $sha = 'abcdef1234567890'
  $script:hub = Rooms 'm1mini' '2026-10-14T10:05:00Z' 'abcdef1'
  Check 'attached after the swap on the new commit is back' (Test-RoomBackOnHub 'm1mini' $after $sha)
  $script:hub = Rooms 'm1mini' '2026-10-14T09:00:00Z' 'abcdef1'
  Check 'a connection from before the swap is not back' (-not (Test-RoomBackOnHub 'm1mini' $after $sha))
  $script:hub = Rooms 'm1mini' '2026-10-14T10:05:00Z' '1111111'
  Check 'attached on another commit is not back' (-not (Test-RoomBackOnHub 'm1mini' $after $sha))
  $script:hub = Rooms 'sg3' '2026-10-14T10:05:00Z' 'abcdef1'
  Check 'another room attached does not count' (-not (Test-RoomBackOnHub 'm1mini' $after $sha))
  $script:hub = $null
  Check 'an unreachable hub is not back' (-not (Test-RoomBackOnHub 'm1mini' $after $sha))
  # The verdict never calls ssh: the fake would have counted it.
  Reset 99
  $script:hub = Rooms 'm1mini' '2026-10-14T10:05:00Z' 'abcdef1'
  Check 'the verdict does not depend on ssh' ((Test-RoomBackOnHub 'm1mini' $after $sha) -and -not (Test-Path $count))
} finally {
  Remove-Item Env:FAKE_DIR, Env:FAKE_HANGS, Env:FAKE_EXIT -ErrorAction SilentlyContinue
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
if ($fail) { Write-Host "$fail FAILED"; exit 1 }
Write-Host 'all passed'
