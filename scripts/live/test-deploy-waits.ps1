# Proves the deploy waits what matters: the old room's store (not its port) before the new room starts, and the hub's
# word on a reattach (polled, said aloud, minutes long) before a deploy is called failed. Fakes only: a temp db held by a
# spawned dummy process, and a fake hub. Nothing real is stopped, started or deployed.
# Usage: pwsh -NoProfile -File scripts/live/test-deploy-waits.ps1
$ErrorActionPreference = 'Stop'
$fail = 0
function Check([string]$What, [bool]$Ok) {
  if ($Ok) { Write-Host "PASS  $What" } else { Write-Host "FAIL  $What"; $script:fail++ }
}
$said = [System.Collections.Generic.List[string]]::new()
function Say($m) { $said.Add("$m") }

# Only the named pieces are loaded, not live-common's top level, which looks at the machine.
$src = Get-Content -Raw "$PSScriptRoot\live-common.ps1"
$ast = [System.Management.Automation.Language.Parser]::ParseInput($src, [ref]$null, [ref]$null)
foreach ($n in 'New-StoreWaitScript', 'Wait-RoomAttached') {
  $def = $ast.FindAll({ param($a) $a -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $a.Name -eq $n }, $true) |
    Select-Object -First 1
  . ([scriptblock]::Create($def.Extent.Text))
}
foreach ($v in 'StoreBudget', 'StoreTimeout', 'ReattachBudget', 'ReattachSayEvery', 'StaySeconds') {
  if ($src -notmatch "(?m)^\`$$v = (.+)$") { throw "no `$$v in live-common.ps1" }
  Set-Variable $v (Invoke-Expression $Matches[1])
}

# 1. The store wait. A dummy process holds a temp db exclusively, as an old room that outlived its port would.
$tmp = Join-Path ([IO.Path]::GetTempPath()) "deploy-waits-$PID"
New-Item -ItemType Directory $tmp | Out-Null
$db = Join-Path $tmp 'atrium.db'
Set-Content $db 'x'
$holder = Start-Process pwsh -PassThru -WindowStyle Hidden -ArgumentList '-NoProfile', '-Command',
  "`$f = [IO.File]::Open('$db', 'Open', 'ReadWrite', 'None'); Start-Sleep 120"
try {
  $dl = (Get-Date).AddSeconds(15)
  while ((Get-Date) -lt $dl) {
    try { [IO.File]::Open($db, 'Open', 'ReadWrite', 'None').Dispose() } catch { break }
    Start-Sleep -Milliseconds 200
  }
  $script1 = New-StoreWaitScript 'windows' "'$db'" 2
  $out = @(& pwsh -NoProfile -Command $script1)
  Check 'a held store exits 4 after its budget' ($LASTEXITCODE -eq 4)
  Check 'it said what it was waiting on' (($out -join "`n") -match 'waiting for the old room .* to release')
  Check 'it printed the timeout marker' (@($out | Where-Object { $_ -like "$StoreTimeout*" }).Count -eq 1)
  Check 'the budget is bounded in seconds, not the port' (($out -join "`n") -match 'up to 2s')

  Stop-Process -Id $holder.Id -Force
  $null = $holder.WaitForExit(10000)
  $out = @(& pwsh -NoProfile -Command (New-StoreWaitScript 'windows' "'$db'" 5))
  Check 'once the holder exited the wait passes' ($LASTEXITCODE -eq 0)
  Check 'and prints no timeout marker' (@($out | Where-Object { $_ -like "$StoreTimeout*" }).Count -eq 0)

  $out = @(& pwsh -NoProfile -Command (New-StoreWaitScript 'windows' "'$tmp\none.db'" 2))
  Check 'a store that does not exist yet is not waited on' ($LASTEXITCODE -eq 0)
} finally {
  Stop-Process -Id $holder.Id -Force -ErrorAction SilentlyContinue
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
$mac = New-StoreWaitScript 'mac' '~/.atrium/atrium.db' 7
Check 'the mac snippet has no double quote or dollar, which the outer zsh -lic "..." would expand' ($mac -notmatch '["$`]')
Check 'the mac snippet waits on lsof of the store' ($mac -match 'lsof ~/.atrium/atrium.db' -and $mac -match '\{1\.\.7\}')

# 2. The reattach wait. A fake hub that does not list the room for a while, then does.
$RoomName = 'fake'
$ReattachSayEvery = 0
function Start-Sleep { param($Seconds, $Milliseconds) [Threading.Thread]::Sleep(50) }
function Get-RoomLink { 'up' }
$script:polls = 0; $script:appearAt = 4
function Get-HubRoom {
  $script:polls++
  if ($script:polls -lt $script:appearAt) { return $null }
  [pscustomobject]@{ name = 'fake'; since = (Get-Date).AddSeconds(5).ToUniversalTime().ToString('o') }
}
$r = Wait-RoomAttached (Get-Date) 30 0
Check 'a room that shows up late is accepted, polled not slept on' ($null -ne $r -and $script:polls -ge 4)
Check 'it said "waiting for fake to reattach (Ns of 30s)" while it waited' (($said -join "`n") -match 'waiting for fake to reattach \(\d+s of 30s\)')
Check 'it logged how long the reattach took' (($said -join "`n") -match 'fake reattached after \d+s')

$said.Clear(); $script:polls = 0; $script:appearAt = 100000
$r = Wait-RoomAttached (Get-Date) 1 0
Check 'a room that never shows up is a failure only when the budget runs out' ($null -eq $r)

$deploy = Get-Content -Raw "$PSScriptRoot\atrium-deploy.ps1"
Check 'the reattach budget accepts ziti taking minutes' ($ReattachBudget -ge 300)
Check 'atrium-deploy waits the named reattach budget' ($deploy -match 'Wait-RoomAttached \$start \$ReattachBudget')
Check 'every swap and retry waits for the store before the swap' (([regex]::Matches($deploy, 'WaitStore \+')).Count -ge 6)
Check 'a store that never released is reported with its pid' ($deploy -match 'still held its store')

if ($fail) { Write-Host "$fail FAILED"; exit 1 }
Write-Host 'all passed'
